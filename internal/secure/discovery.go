package secure

import (
	"fmt"
	"sync"

	"github.com/slack-go/slack"
)

const (
	// ProfileFieldKey is the Slack profile field used for P2P discovery.
	ProfileFieldKey = "XF_SLACKERS_P2P"
)

// PeerInfo holds discovered information about a Slackers P2P peer.
type PeerInfo struct {
	UserID    string
	PublicKey [32]byte
	PubKeyB64 string
}

// PeerDiscovery manages discovery of other Slackers users via Slack profile fields.
type PeerDiscovery struct {
	api   *slack.Client
	token string
	cache map[string]*PeerInfo
	mu    sync.RWMutex
}

// NewPeerDiscovery creates a new peer discovery service.
func NewPeerDiscovery(api *slack.Client, token string) *PeerDiscovery {
	return &PeerDiscovery{
		api:   api,
		token: token,
		cache: make(map[string]*PeerInfo),
	}
}

// PublishPublicKey sets the P2P public key in the user's Slack profile status.
// Uses the status text with a hidden prefix that normal users won't see.
func (pd *PeerDiscovery) PublishPublicKey(pubKeyB64 string) error {
	// Use custom status fields - the "XFields" approach.
	// Simpler: store in the user profile's "title" or use users.profile.set
	// with a custom field. For now, use the Slack API directly.
	return pd.api.SetUserCustomStatus(
		"slackers-p2p:"+pubKeyB64,
		"",
		0,
	)
}

// DiscoverPeer checks if a user has Slackers P2P enabled.
func (pd *PeerDiscovery) DiscoverPeer(userID string) (*PeerInfo, error) {
	pd.mu.RLock()
	if peer, ok := pd.cache[userID]; ok {
		pd.mu.RUnlock()
		return peer, nil
	}
	pd.mu.RUnlock()

	// Fetch user info.
	user, err := pd.api.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("fetching user info: %w", err)
	}

	// Check the user's status text or title for our P2P key marker.
	// We look for "slackers-p2p:<base64 pubkey>" in the status text.
	pubKeyB64 := ""
	statusText := user.Profile.StatusText
	if len(statusText) > 14 && statusText[:14] == "slackers-p2p:" {
		pubKeyB64 = statusText[14:]
	}

	if pubKeyB64 == "" {
		return nil, nil
	}

	pubKey, err := PublicKeyFromBase64(pubKeyB64)
	if err != nil {
		return nil, nil
	}

	peer := &PeerInfo{
		UserID:    userID,
		PublicKey: pubKey,
		PubKeyB64: pubKeyB64,
	}

	pd.mu.Lock()
	pd.cache[userID] = peer
	pd.mu.Unlock()

	return peer, nil
}

// ClearCache removes a peer from the discovery cache.
func (pd *PeerDiscovery) ClearCache(userID string) {
	pd.mu.Lock()
	delete(pd.cache, userID)
	pd.mu.Unlock()
}

