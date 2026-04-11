package secure

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rw3iss/slackers/internal/debug"
)

// debugLog is a thin wrapper around the slackers debug package so
// the rest of this file can log with a short call. When --debug is
// off the wrapper is a no-op (Log() itself checks the enabled flag).
func debugLog(format string, args ...interface{}) {
	debug.Log(format, args...)
}

const (
	P2PProtocol = protocol.ID("/slackers/msg/1.0.0")

	// P2P message types.
	MsgTypeMessage           = "message"
	MsgTypePing              = "ping"
	MsgTypePong              = "pong"
	MsgTypeFriendRequest     = "friend_request"
	MsgTypeFriendAccept      = "friend_accept"
	MsgTypeFriendReject      = "friend_reject"
	MsgTypeDisconnect        = "disconnect"
	MsgTypeFileOffer         = "file_offer"        // sender offers a file
	MsgTypeFileRequest       = "file_request"      // receiver requests the file data
	MsgTypeFileData          = "file_data"         // sender sends file chunk (base64)
	MsgTypeFileCancel        = "file_cancel"       // sender cancels a previously offered file
	MsgTypeKeyRotate         = "key_rotate"        // sender proposes a new public key for this friendship
	MsgTypeKeyRotateAck      = "key_rotate_ack"    // receiver accepts and returns its new public key
	MsgTypeReaction          = "reaction"          // add an emoji reaction
	MsgTypeReactionRemove    = "reaction_remove"   // remove an emoji reaction
	MsgTypeDelete            = "delete"            // request to delete a message authored by sender
	MsgTypeDeleteAck         = "delete_ack"        // ack from peer that the deletion succeeded
	MsgTypeEdit              = "edit"              // request to edit a message authored by sender
	MsgTypeEditAck           = "edit_ack"          // ack from peer that the edit succeeded
	MsgTypeProfileSync       = "profile_sync"      // sender announces their current contact card JSON
	MsgTypeRequestPending    = "request_pending"   // asks the peer to scan its history and resend any pending messages addressed to us
	MsgTypeStatusUpdate      = "status_update"     // sender announces a status change (online/offline/away/back)
	MsgTypeEmote             = "emote"             // text emote (/laugh, /wave, etc.) — renders with special style
	MsgTypeBrowseRequest     = "browse_request"    // request to list a remote shared folder
	MsgTypeBrowseResponse    = "browse_response"   // response with directory listing JSON
	MsgTypeFileRequestByPath = "file_request_path" // download a file by relative path from shared folder
	MsgTypePlugin            = "plugin"             // plugin-to-plugin custom message

	// Protocol for file transfers (separate from messaging).
	P2PFileProtocol = protocol.ID("/slackers/file/1.0.0")
)

// P2PMessage is the wire format for messages sent over P2P.
type P2PMessage struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Timestamp int64  `json:"ts"`
	SenderID  string `json:"sender_id"`

	// MessageID is the sender's locally-generated id for this
	// message (text messages only). Receivers store messages under
	// the sender's id so cross-instance replies/reactions/edits/
	// deletes can target it later.
	MessageID string `json:"message_id,omitempty"`

	// ReplyToMsgID, when set on a regular text message, marks it as
	// a reply to the message with that ID in the recipient's store.
	ReplyToMsgID string `json:"reply_to_msg_id,omitempty"`

	// File transfer fields (only used for file_offer/file_request/file_data).
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileID   string `json:"file_id,omitempty"`

	// Reaction fields (only used for reaction type).
	TargetMsgID   string `json:"target_msg_id,omitempty"`
	ReactionEmoji string `json:"reaction_emoji,omitempty"`

	// Emote fields (only used for MsgTypeEmote). The template is
	// the raw emote text with $variables still in place; the
	// receiver expands them locally so $receiver becomes "you"
	// on their screen instead of their own display name.
	// SenderName carries the sender's display name for $sender.
	EmoteTemplate string `json:"emote_template,omitempty"`
	SenderName    string `json:"sender_name,omitempty"`

	// Status fields (used by status_update, ping, and pong).
	// Piggyback local status on pings so remote peers learn our
	// state without a separate broadcast. Status types:
	// "online", "offline", "away", "back".
	StatusType    string `json:"status_type,omitempty"`
	StatusMessage string `json:"status_message,omitempty"`

	// SharedFolder carries the basename of the sender's shared
	// folder (or "" if none). Included in status_update messages
	// so peers know whether "Browse Shared Files" should be offered.
	SharedFolder string `json:"shared_folder,omitempty"`

	// Browse request sort parameters (used by browse_request).
	BrowseSortBy  string `json:"browse_sort_by,omitempty"`  // "name", "size", "modified", "type"
	BrowseSortDir string `json:"browse_sort_dir,omitempty"` // "asc" or "desc"

	// Plugin message fields (used by MsgTypePlugin).
	PluginName string `json:"plugin_name,omitempty"`
	PluginData string `json:"plugin_data,omitempty"` // JSON payload
}

// P2PNode manages the libp2p host and peer connections.
type P2PNode struct {
	host         host.Host
	port         int
	address      string
	ctx          context.Context
	cancel       context.CancelFunc
	onMessage    func(peerSlackID string, msg P2PMessage)
	onFileServed func(fileID string) // called when a peer finishes downloading
	// peerLookup lets the incoming stream handler resolve an unknown
	// remote peer ID (one we never dialed ourselves) to a local user
	// ID by scanning the friend store's multiaddrs. Set by the model
	// layer. Returns "" when the peer is not a known friend.
	peerLookup func(peerIDStr string) string
	// SharedFolderLookup resolves a relative path within the
	// user's shared folder to an absolute local path. Returns an
	// error if the path escapes the shared root or no folder is
	// shared. Set by the model layer.
	SharedFolderLookup func(relativePath string) (string, error)
	peerMap            map[string]peer.ID
	slackMap           map[peer.ID]string
	sharedFiles        map[string]string // fileID -> local file path (files we've offered)
	mu                 sync.RWMutex
}

// SetPeerLookup wires the friend-store lookup used by the incoming
// stream handler to identify otherwise-unknown remote peers. Calling
// with nil disables the fallback.
func (n *P2PNode) SetPeerLookup(fn func(peerIDStr string) string) {
	n.mu.Lock()
	n.peerLookup = fn
	n.mu.Unlock()
}

// SetFileServedCallback registers a callback fired after a peer
// completes downloading one of our shared files. Used by the UI to
// flip the file's "uploading…" indicator off.
func (n *P2PNode) SetFileServedCallback(fn func(fileID string)) {
	n.onFileServed = fn
}

// hostKeyPath returns the on-disk location of the persisted libp2p
// host private key. Stored alongside the rest of the slackers config
// so the peer ID stays stable across restarts (otherwise every share
// of a multiaddr is invalidated the next time the app starts).
func hostKeyPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "p2p_host.key")
}

// loadOrCreateHostKey returns a persisted Ed25519 libp2p host
// private key, generating and saving a new one on first use.
func loadOrCreateHostKey() (crypto.PrivKey, error) {
	path := hostKeyPath()
	if data, err := os.ReadFile(path); err == nil {
		raw, derr := base64.StdEncoding.DecodeString(string(data))
		if derr == nil {
			if priv, perr := crypto.UnmarshalPrivateKey(raw); perr == nil {
				return priv, nil
			}
		}
	}
	// Generate a fresh Ed25519 keypair and persist it.
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}
	raw, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling host key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating host key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
		return nil, fmt.Errorf("saving host key: %w", err)
	}
	return priv, nil
}

// NewP2PNode creates and starts a libp2p host.
func NewP2PNode(port int, address string, onMessage func(string, P2PMessage)) (*P2PNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	priv, err := loadOrCreateHostKey()
	if err != nil {
		cancel()
		return nil, err
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.NATPortMap(),         // attempt UPnP port mapping
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

// ConnectToPeer connects to a peer using their multiaddress. The
// dial is wrapped in a short timeout context so a dead/offline
// peer never blocks the caller for more than a few seconds —
// libp2p's default dial ceiling is much longer, which was
// freezing callers running on the bubbletea Update loop whenever
// a friend went offline.
func (n *P2PNode) ConnectToPeer(slackUserID, multiaddr string) error {
	maddr, err := ma.NewMultiaddr(multiaddr)
	if err != nil {
		debugLog("[p2p] ConnectToPeer parse error uid=%s maddr=%q err=%v", slackUserID, multiaddr, err)
		return fmt.Errorf("parsing multiaddr: %w", err)
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		debugLog("[p2p] ConnectToPeer addrinfo error uid=%s err=%v", slackUserID, err)
		return fmt.Errorf("extracting peer info: %w", err)
	}

	// Asymmetric-connection recovery: if libp2p already has a live
	// connection to this peer (e.g. the peer dialed US first while
	// we were offline, or a previous dial succeeded but we lost the
	// peerMap entry across a restart), short-circuit the dial and
	// just populate peerMap. Without this check, a failing re-dial
	// (NAT eviction, hairpin routing, transient timeout) would leave
	// peerMap empty and messages would fail with "peer not connected"
	// even though a perfectly usable connection exists.
	if n.host.Network().Connectedness(peerInfo.ID) == network.Connected {
		n.mu.Lock()
		n.peerMap[slackUserID] = peerInfo.ID
		n.slackMap[peerInfo.ID] = slackUserID
		n.mu.Unlock()
		debugLog("[p2p] ConnectToPeer short-circuit uid=%s pid=%s (already connected)", slackUserID, peerInfo.ID)
		return nil
	}

	debugLog("[p2p] ConnectToPeer dialing uid=%s pid=%s maddr=%s", slackUserID, peerInfo.ID, multiaddr)
	dialCtx, cancel := context.WithTimeout(n.ctx, 3*time.Second)
	defer cancel()
	dialErr := n.host.Connect(dialCtx, *peerInfo)

	// Re-check connectedness AFTER the dial attempt. Even if Connect
	// returned an error, libp2p may have picked up or retained a
	// connection via another address (e.g. an inbound stream from
	// the same peer raced the dial). Treat a successful connection
	// as authoritative regardless of the Connect() return value.
	if n.host.Network().Connectedness(peerInfo.ID) == network.Connected {
		n.mu.Lock()
		n.peerMap[slackUserID] = peerInfo.ID
		n.slackMap[peerInfo.ID] = slackUserID
		n.mu.Unlock()
		if dialErr != nil {
			debugLog("[p2p] ConnectToPeer dial err=%v but connected via existing link uid=%s", dialErr, slackUserID)
		} else {
			debugLog("[p2p] ConnectToPeer dial ok uid=%s pid=%s", slackUserID, peerInfo.ID)
		}
		return nil
	}

	if dialErr != nil {
		debugLog("[p2p] ConnectToPeer dial failed uid=%s pid=%s err=%v", slackUserID, peerInfo.ID, dialErr)
		return fmt.Errorf("connecting to peer: %w", dialErr)
	}
	debugLog("[p2p] ConnectToPeer dial returned nil but not connected uid=%s pid=%s", slackUserID, peerInfo.ID)
	return fmt.Errorf("dial returned but peer not connected")
}

// SendMessage sends a message to a connected peer.
func (n *P2PNode) SendMessage(slackUserID string, msg P2PMessage) error {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		debugLog("[p2p] SendMessage uid=%s type=%s FAIL peerMap miss", slackUserID, msg.Type)
		return fmt.Errorf("peer %s not connected (peerMap miss)", slackUserID)
	}

	if n.host.Network().Connectedness(peerID) != network.Connected {
		debugLog("[p2p] SendMessage uid=%s type=%s FAIL libp2p not connected pid=%s", slackUserID, msg.Type, peerID)
		return fmt.Errorf("peer %s not connected (libp2p disconnected)", slackUserID)
	}

	stream, err := n.host.NewStream(n.ctx, peerID, P2PProtocol)
	if err != nil {
		debugLog("[p2p] SendMessage uid=%s type=%s FAIL stream err=%v", slackUserID, msg.Type, err)
		return fmt.Errorf("opening stream: %w", err)
	}
	defer stream.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		debugLog("[p2p] SendMessage uid=%s type=%s FAIL marshal err=%v", slackUserID, msg.Type, err)
		return fmt.Errorf("marshaling message: %w", err)
	}

	// Write length-prefixed message.
	data = append(data, '\n')
	if _, err := stream.Write(data); err != nil {
		debugLog("[p2p] SendMessage uid=%s type=%s FAIL write err=%v", slackUserID, msg.Type, err)
		return fmt.Errorf("writing message: %w", err)
	}

	debugLog("[p2p] SendMessage uid=%s type=%s ok bytes=%d", slackUserID, msg.Type, len(data))
	return nil
}

// PeerMultiaddr returns a best-effort full multiaddr (/ip4/.../tcp/.../p2p/<id>)
// for a connected friend, looked up from the host's peerstore. Returns
// "" if the peer isn't currently mapped or has no usable addresses.
// Skips loopback addresses; prefers the first non-loopback ip4 address.
func (n *P2PNode) PeerMultiaddr(slackUserID string) string {
	n.mu.RLock()
	pid, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return ""
	}
	addrs := n.host.Peerstore().Addrs(pid)
	for _, a := range addrs {
		s := a.String()
		// Only return ip4/tcp addresses; skip loopback.
		if len(s) < 5 || s[:5] != "/ip4/" {
			continue
		}
		if len(s) >= 14 && s[:14] == "/ip4/127.0.0.1" {
			continue
		}
		return fmt.Sprintf("%s/p2p/%s", s, pid)
	}
	if len(addrs) > 0 {
		return fmt.Sprintf("%s/p2p/%s", addrs[0].String(), pid)
	}
	return ""
}

// DisconnectPeer drops the libp2p connection to a friend (if any).
// Used by the per-friend inactivity timeout so we don't hold open
// permanent connections to every friend in the user's list — only
// the chats they're actively interacting with.
func (n *P2PNode) DisconnectPeer(slackUserID string) {
	n.mu.RLock()
	pid, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return
	}
	_ = n.host.Network().ClosePeer(pid)
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

	// Look up the Slack user ID for this peer. If we never dialed
	// the remote (inbound-only connection), the peer isn't yet in
	// slackMap — fall back to the friend-store lookup callback,
	// which matches the peer ID against each friend's multiaddr
	// and returns the owning friend's UserID. On a hit, also
	// register both directions of the peer/slack maps so future
	// SendMessage calls can route directly.
	remotePeer := s.Conn().RemotePeer()
	n.mu.RLock()
	slackID, ok := n.slackMap[remotePeer]
	lookup := n.peerLookup
	n.mu.RUnlock()
	if !ok {
		if lookup != nil {
			if resolved := lookup(remotePeer.String()); resolved != "" {
				slackID = resolved
				n.mu.Lock()
				n.slackMap[remotePeer] = resolved
				n.peerMap[resolved] = remotePeer
				n.mu.Unlock()
				ok = true
				debugLog("[p2p] handleStream fallback hit pid=%s -> uid=%s (peerMap populated)", remotePeer, resolved)
			} else {
				debugLog("[p2p] handleStream lookup miss pid=%s (no matching friend multiaddr)", remotePeer)
			}
		}
	}
	if !ok {
		slackID = "unknown"
		debugLog("[p2p] handleStream pid=%s → slackID=unknown (message will be dropped)", remotePeer)
	}

	reader := bufio.NewReader(s)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				debugLog("[p2p] handleStream read err uid=%s pid=%s err=%v", slackID, remotePeer, err)
			}
			return
		}

		var msg P2PMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			debugLog("[p2p] handleStream unmarshal err uid=%s pid=%s err=%v raw=%q", slackID, remotePeer, err, string(line))
			continue
		}

		debugLog("[p2p] handleStream recv uid=%s type=%s msgID=%s", slackID, msg.Type, msg.MessageID)
		if n.onMessage != nil {
			n.onMessage(slackID, msg)
		}
	}
}

// DumpState returns a multi-line snapshot of the P2P node's current
// routing state: the peerMap and slackMap plus libp2p host-level
// connectedness for every registered peer. Used by the "Test
// Connection" friend-config action and by --debug instrumentation
// to diagnose asymmetric-connection bugs without having to add
// more ad-hoc print statements.
func (n *P2PNode) DumpState() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	var b bytes.Buffer
	fmt.Fprintf(&b, "P2P node state @ %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "  host ID:     %s\n", n.host.ID())
	fmt.Fprintf(&b, "  listening:   %s\n", n.address)
	fmt.Fprintf(&b, "  peerMap (%d entries):\n", len(n.peerMap))
	for slackID, pid := range n.peerMap {
		conn := n.host.Network().Connectedness(pid)
		addrs := n.host.Peerstore().Addrs(pid)
		fmt.Fprintf(&b, "    %s → %s  [libp2p=%s addrs=%d]\n", slackID, pid, conn, len(addrs))
		for _, a := range addrs {
			fmt.Fprintf(&b, "      - %s\n", a)
		}
	}
	fmt.Fprintf(&b, "  slackMap (%d entries):\n", len(n.slackMap))
	for pid, slackID := range n.slackMap {
		fmt.Fprintf(&b, "    %s → %s\n", pid, slackID)
	}
	// Also include any libp2p peers that are connected but NOT in
	// our map — these are the "orphan connections" that point to
	// the asymmetric-connection bug when they're from known friends.
	connected := n.host.Network().Peers()
	orphans := 0
	for _, pid := range connected {
		if _, ok := n.slackMap[pid]; !ok {
			orphans++
		}
	}
	if orphans > 0 {
		fmt.Fprintf(&b, "  orphan connections (libp2p connected but not in slackMap): %d\n", orphans)
		for _, pid := range connected {
			if _, ok := n.slackMap[pid]; ok {
				continue
			}
			addrs := n.host.Peerstore().Addrs(pid)
			fmt.Fprintf(&b, "    %s  addrs=%d\n", pid, len(addrs))
			for _, a := range addrs {
				fmt.Fprintf(&b, "      - %s\n", a)
			}
		}
	}
	return b.String()
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

// CancelFileOffer revokes a previously offered file: removes it from the
// local serving table and notifies the peer so they can clean up any
// in-flight download or pending offer state on their end.
func (n *P2PNode) CancelFileOffer(slackUserID, fileID string) error {
	n.mu.Lock()
	delete(n.sharedFiles, fileID)
	n.mu.Unlock()
	msg := P2PMessage{
		Type:      MsgTypeFileCancel,
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

	// Create destination file — avoid overwriting existing files.
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	preExisted := false
	if _, statErr := os.Stat(destPath); statErr == nil {
		preExisted = true
		ext := filepath.Ext(destPath)
		base := strings.TrimSuffix(destPath, ext)
		destPath = base + "_download" + ext
	}
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}
	defer out.Close()

	// Read file data from stream.
	if _, err := io.Copy(out, stream); err != nil {
		if !preExisted {
			os.Remove(destPath)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("receiving file: %w", err)
	}

	return nil
}

// DownloadFileByPath requests a file from a friend's shared folder
// by relative path (rather than by pre-registered file ID). The
// server validates that the path is within the shared root and
// streams the file back over the /slackers/file/1.0.0 protocol.
func (n *P2PNode) DownloadFileByPath(ctx context.Context, slackUserID, relativePath, destPath string) error {
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
	debugLog("[file-dl] requesting path=%q from peer=%s", relativePath, slackUserID)
	req := P2PMessage{Type: MsgTypeFileRequestByPath, Text: relativePath}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("sending file request: %w", err)
	}
	// Close the write side so the server's reader sees EOF cleanly
	// and doesn't block waiting for more request data.
	if err := stream.CloseWrite(); err != nil {
		return fmt.Errorf("closing write side: %w", err)
	}
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Check if the file already exists — never delete pre-existing files
	// on failure (the source and dest could be the same directory).
	preExisted := false
	if _, statErr := os.Stat(destPath); statErr == nil {
		preExisted = true
		// Append a suffix to avoid overwriting the original.
		ext := filepath.Ext(destPath)
		base := strings.TrimSuffix(destPath, ext)
		destPath = base + "_download" + ext
	}
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}
	defer out.Close()
	written, err := io.Copy(out, stream)
	if err != nil {
		if !preExisted {
			os.Remove(destPath)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("receiving file: %w", err)
	}
	if written == 0 {
		if !preExisted {
			os.Remove(destPath)
		}
		return fmt.Errorf("server returned empty response (file may not exist or shared folder is not configured)")
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

	// Path-based download from the shared folder.
	if req.Type == MsgTypeFileRequestByPath && req.Text != "" {
		if n.SharedFolderLookup == nil {
			debugLog("[file-serve] SharedFolderLookup is nil, cannot serve %q", req.Text)
			return
		}
		localPath, err := n.SharedFolderLookup(req.Text)
		if err != nil {
			debugLog("[file-serve] path validation failed for %q: %v", req.Text, err)
			return
		}
		f, err := os.Open(localPath)
		if err != nil {
			debugLog("[file-serve] cannot open %q: %v", localPath, err)
			return
		}
		defer f.Close()
		io.Copy(s, f)
		// Close the write side so the client's io.Copy sees EOF.
		s.CloseWrite()
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
	if _, err := io.Copy(s, f); err == nil && n.onFileServed != nil {
		n.onFileServed(req.FileID)
	}
}

// BroadcastStatus sends a status update to all connected peers.
// statusType is one of "online", "offline", "away", "back".
// statusMsg is an optional human-readable message (e.g. "BRB
// lunch" for away).
//
// The broadcast iterates every known peer; if the peer is
// already connected, the status is sent immediately over a
// new stream. If not connected, we skip rather than dial — the
// status will be exchanged on the next ping cycle (which
// piggybacks local status on the ping message) or when the peer
// connects to us.
//
// Errors per-peer are silently swallowed so one unreachable
// friend doesn't break the broadcast to others.
func (n *P2PNode) BroadcastStatus(statusType, statusMsg, sharedFolder string) {
	n.mu.RLock()
	peers := make(map[string]peer.ID)
	for uid, pid := range n.peerMap {
		peers[uid] = pid
	}
	n.mu.RUnlock()

	msg := P2PMessage{
		Type:          MsgTypeStatusUpdate,
		Timestamp:     time.Now().Unix(),
		StatusType:    statusType,
		StatusMessage: statusMsg,
		SharedFolder:  sharedFolder,
	}
	for _, pid := range peers {
		if n.host.Network().Connectedness(pid) != network.Connected {
			continue
		}
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

// SendPluginMessage sends a custom plugin message to a specific peer.
func (n *P2PNode) SendPluginMessage(slackUserID, pluginName, data string) error {
	msg := P2PMessage{
		Type:       MsgTypePlugin,
		Timestamp:  time.Now().Unix(),
		PluginName: pluginName,
		PluginData: data,
	}
	return n.SendMessage(slackUserID, msg)
}

// BroadcastDisconnect sends a disconnect message to all connected
// peers. Kept as a convenience alias for backward compatibility
// with call sites that predate BroadcastStatus; internally it
// sends a MsgTypeDisconnect (not MsgTypeStatusUpdate) so older
// peers that don't understand status_update still shut down
// cleanly.
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
