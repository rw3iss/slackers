package secure

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	P2PProtocol = protocol.ID("/slackers/msg/1.0.0")

	// P2P message types.
	MsgTypeMessage       = "message"
	MsgTypePing          = "ping"
	MsgTypePong          = "pong"
	MsgTypeFriendRequest = "friend_request"
	MsgTypeFriendAccept  = "friend_accept"
	MsgTypeFriendReject  = "friend_reject"
	MsgTypeDisconnect    = "disconnect"
	MsgTypeFileOffer     = "file_offer"    // sender offers a file
	MsgTypeFileRequest   = "file_request"  // receiver requests the file data
	MsgTypeFileData      = "file_data"     // sender sends file chunk (base64)

	// Protocol for file transfers (separate from messaging).
	P2PFileProtocol = protocol.ID("/slackers/file/1.0.0")
)

// P2PMessage is the wire format for messages sent over P2P.
type P2PMessage struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Timestamp int64  `json:"ts"`
	SenderID  string `json:"sender_id"`

	// File transfer fields (only used for file_offer/file_request/file_data).
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileID   string `json:"file_id,omitempty"` // unique ID for this transfer
}

// P2PNode manages the libp2p host and peer connections.
type P2PNode struct {
	host        host.Host
	port        int
	address     string
	ctx         context.Context
	cancel      context.CancelFunc
	onMessage   func(peerSlackID string, msg P2PMessage)
	peerMap     map[string]peer.ID
	slackMap    map[peer.ID]string
	sharedFiles map[string]string // fileID -> local file path (files we've offered)
	mu          sync.RWMutex
}

// NewP2PNode creates and starts a libp2p host.
func NewP2PNode(port int, address string, onMessage func(string, P2PMessage)) (*P2PNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.NATPortMap(),        // attempt UPnP port mapping
		libp2p.EnableHolePunching(), // NAT hole punching
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating libp2p host: %w", err)
	}

	node := &P2PNode{
		host:        h,
		port:        port,
		address:     address,
		ctx:         ctx,
		cancel:      cancel,
		onMessage:   onMessage,
		peerMap:     make(map[string]peer.ID),
		slackMap:    make(map[peer.ID]string),
		sharedFiles: make(map[string]string),
	}

	// Set stream handler for incoming messages.
	h.SetStreamHandler(P2PProtocol, node.handleStream)

	// Set stream handler for file transfers.
	h.SetStreamHandler(P2PFileProtocol, node.handleFileRequest)

	return node, nil
}

// Multiaddr returns the node's multiaddress for sharing with peers.
func (n *P2PNode) Multiaddr() string {
	addrs := n.host.Addrs()
	id := n.host.ID()
	if len(addrs) == 0 {
		return ""
	}
	// Prefer the configured address if set.
	if n.address != "" {
		return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", n.address, n.port, id)
	}
	// Use the first non-loopback address.
	for _, addr := range addrs {
		full := fmt.Sprintf("%s/p2p/%s", addr, id)
		if a := addr.String(); len(a) > 5 && a[:5] != "/ip4/" {
			continue
		}
		// Skip loopback.
		if a := addr.String(); len(a) > 14 && a[:14] == "/ip4/127.0.0.1" {
			continue
		}
		return full
	}
	return fmt.Sprintf("%s/p2p/%s", addrs[0], id)
}

// PeerID returns the node's libp2p peer ID as a string.
func (n *P2PNode) PeerID() string {
	return n.host.ID().String()
}

// ConnectToPeer connects to a peer using their multiaddress.
func (n *P2PNode) ConnectToPeer(slackUserID, multiaddr string) error {
	maddr, err := ma.NewMultiaddr(multiaddr)
	if err != nil {
		return fmt.Errorf("parsing multiaddr: %w", err)
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return fmt.Errorf("extracting peer info: %w", err)
	}

	if err := n.host.Connect(n.ctx, *peerInfo); err != nil {
		return fmt.Errorf("connecting to peer: %w", err)
	}

	n.mu.Lock()
	n.peerMap[slackUserID] = peerInfo.ID
	n.slackMap[peerInfo.ID] = slackUserID
	n.mu.Unlock()

	return nil
}

// SendMessage sends a message to a connected peer.
func (n *P2PNode) SendMessage(slackUserID string, msg P2PMessage) error {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return fmt.Errorf("peer %s not connected", slackUserID)
	}

	stream, err := n.host.NewStream(n.ctx, peerID, P2PProtocol)
	if err != nil {
		return fmt.Errorf("opening stream: %w", err)
	}
	defer stream.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	// Write length-prefixed message.
	data = append(data, '\n')
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	return nil
}

// IsConnected checks if a peer has an active P2P connection.
func (n *P2PNode) IsConnected(slackUserID string) bool {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return false
	}
	return n.host.Network().Connectedness(peerID) == network.Connected
}

// handleStream processes incoming P2P message streams.
func (n *P2PNode) handleStream(s network.Stream) {
	defer s.Close()

	// Look up the Slack user ID for this peer.
	remotePeer := s.Conn().RemotePeer()
	n.mu.RLock()
	slackID, ok := n.slackMap[remotePeer]
	n.mu.RUnlock()
	if !ok {
		slackID = "unknown"
	}

	reader := bufio.NewReader(s)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				// Connection closed or error.
			}
			return
		}

		var msg P2PMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if n.onMessage != nil {
			n.onMessage(slackID, msg)
		}
	}
}

// RegisterPeer maps a Slack user ID to a libp2p peer ID without connecting.
func (n *P2PNode) RegisterPeer(slackUserID string, peerID peer.ID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peerMap[slackUserID] = peerID
	n.slackMap[peerID] = slackUserID
}

// PingPeer sends a ping to a peer and returns true if they respond.
func (n *P2PNode) PingPeer(slackUserID string) bool {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return false
	}

	if n.host.Network().Connectedness(peerID) != network.Connected {
		return false
	}

	msg := P2PMessage{Type: MsgTypePing, SenderID: slackUserID, Timestamp: time.Now().Unix()}
	stream, err := n.host.NewStream(n.ctx, peerID, P2PProtocol)
	if err != nil {
		return false
	}
	defer stream.Close()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, err = stream.Write(data)
	return err == nil
}

// ShareFile registers a local file for P2P sharing and returns a unique file ID.
func (n *P2PNode) ShareFile(localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", localPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", localPath)
	}

	b := make([]byte, 8)
	rand.Read(b)
	fileID := hex.EncodeToString(b)

	n.mu.Lock()
	n.sharedFiles[fileID] = localPath
	n.mu.Unlock()

	return fileID, nil
}

// SendFileOffer sends a file offer message to a peer.
func (n *P2PNode) SendFileOffer(slackUserID, fileID, fileName string, fileSize int64) error {
	msg := P2PMessage{
		Type:      MsgTypeFileOffer,
		FileName:  fileName,
		FileSize:  fileSize,
		FileID:    fileID,
		SenderID:  slackUserID,
		Timestamp: time.Now().Unix(),
	}
	return n.SendMessage(slackUserID, msg)
}

// DownloadFileFromPeer requests and downloads a file from a connected peer.
func (n *P2PNode) DownloadFileFromPeer(ctx context.Context, slackUserID, fileID, destPath string) error {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return fmt.Errorf("peer %s not connected", slackUserID)
	}

	stream, err := n.host.NewStream(ctx, peerID, P2PFileProtocol)
	if err != nil {
		return fmt.Errorf("opening file stream: %w", err)
	}
	defer stream.Close()

	// Send file request.
	req := P2PMessage{Type: MsgTypeFileRequest, FileID: fileID}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("sending file request: %w", err)
	}

	// Create destination file.
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}
	defer out.Close()

	// Read file data from stream.
	if _, err := io.Copy(out, stream); err != nil {
		os.Remove(destPath)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("receiving file: %w", err)
	}

	return nil
}

// handleFileRequest processes incoming file download requests on the file protocol.
func (n *P2PNode) handleFileRequest(s network.Stream) {
	defer s.Close()

	reader := bufio.NewReader(s)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var req P2PMessage
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	if req.Type != MsgTypeFileRequest || req.FileID == "" {
		return
	}

	n.mu.RLock()
	localPath, ok := n.sharedFiles[req.FileID]
	n.mu.RUnlock()
	if !ok {
		return
	}

	f, err := os.Open(localPath)
	if err != nil {
		return
	}
	defer f.Close()

	// Stream file data directly.
	io.Copy(s, f)
}

// BroadcastDisconnect sends a disconnect message to all connected peers.
func (n *P2PNode) BroadcastDisconnect() {
	n.mu.RLock()
	peers := make(map[string]peer.ID)
	for uid, pid := range n.peerMap {
		peers[uid] = pid
	}
	n.mu.RUnlock()

	msg := P2PMessage{Type: MsgTypeDisconnect, Timestamp: time.Now().Unix()}
	for _, pid := range peers {
		if n.host.Network().Connectedness(pid) == network.Connected {
			stream, err := n.host.NewStream(n.ctx, pid, P2PProtocol)
			if err != nil {
				continue
			}
			data, _ := json.Marshal(msg)
			data = append(data, '\n')
			stream.Write(data)
			stream.Close()
		}
	}
}

// Close broadcasts disconnect to peers and shuts down the P2P node.
func (n *P2PNode) Close() error {
	n.BroadcastDisconnect()
	n.cancel()
	return n.host.Close()
}
