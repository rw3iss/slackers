package secure

import (
	"fmt"
	"sync"
)

// SessionState represents the current state of a secure session.
type SessionState int

const (
	SessionDisconnected SessionState = iota
	SessionDiscovering
	SessionEncryptedRelay // E2E via Slack DMs
	SessionP2PConnecting
	SessionP2PConnected // Direct P2P
)

// String returns a display label for the session state.
func (s SessionState) String() string {
	switch s {
	case SessionDisconnected:
		return ""
	case SessionDiscovering:
		return "discovering..."
	case SessionEncryptedRelay:
		return "E2E"
	case SessionP2PConnecting:
		return "P2P connecting..."
	case SessionP2PConnected:
		return "P2P"
	default:
		return ""
	}
}

// Session holds the cryptographic state for a secure conversation with a peer.
type Session struct {
	PeerID        string
	PeerPublicKey [32]byte
	SharedSecret  [32]byte
	EncryptionKey []byte
	State         SessionState
}

// SessionManager manages secure sessions for all peers.
type SessionManager struct {
	ownKeyPair *KeyPair
	sessions   map[string]*Session // peerUserID -> Session
	mu         sync.RWMutex
}

// NewSessionManager creates a new session manager with the local keypair.
func NewSessionManager(kp *KeyPair) *SessionManager {
	return &SessionManager{
		ownKeyPair: kp,
		sessions:   make(map[string]*Session),
	}
}

// GetOrCreateSession returns the session for a peer, creating one if needed.
func (sm *SessionManager) GetOrCreateSession(peerID string, peerPubKey [32]byte) (*Session, error) {
	sm.mu.RLock()
	if sess, ok := sm.sessions[peerID]; ok {
		sm.mu.RUnlock()
		return sess, nil
	}
	sm.mu.RUnlock()

	// Derive shared secret.
	shared, err := ComputeSharedSecret(sm.ownKeyPair.PrivateKey, peerPubKey)
	if err != nil {
		return nil, fmt.Errorf("computing shared secret for %s: %w", peerID, err)
	}

	// Derive encryption key.
	encKey, err := DeriveEncryptionKey(shared)
	if err != nil {
		return nil, fmt.Errorf("deriving encryption key for %s: %w", peerID, err)
	}

	sess := &Session{
		PeerID:        peerID,
		PeerPublicKey: peerPubKey,
		SharedSecret:  shared,
		EncryptionKey: encKey,
		State:         SessionEncryptedRelay,
	}

	sm.mu.Lock()
	sm.sessions[peerID] = sess
	sm.mu.Unlock()

	return sess, nil
}

// GetSession returns the session for a peer, or nil if none exists.
func (sm *SessionManager) GetSession(peerID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[peerID]
}

// SetState updates the connection state for a peer's session.
func (sm *SessionManager) SetState(peerID string, state SessionState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sess, ok := sm.sessions[peerID]; ok {
		sess.State = state
	}
}

// RemoveSession removes a peer's session.
func (sm *SessionManager) RemoveSession(peerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, peerID)
}

// EncryptMessage encrypts a message for a peer.
func (sm *SessionManager) EncryptMessage(peerID string, plaintext string) (string, error) {
	sm.mu.RLock()
	sess, ok := sm.sessions[peerID]
	sm.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no session for peer %s", peerID)
	}
	return Encrypt(sess.EncryptionKey, plaintext)
}

// DecryptMessage decrypts a message from a peer.
func (sm *SessionManager) DecryptMessage(peerID string, ciphertext string) (string, error) {
	sm.mu.RLock()
	sess, ok := sm.sessions[peerID]
	sm.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no session for peer %s", peerID)
	}
	return Decrypt(sess.EncryptionKey, ciphertext)
}

// OwnFingerprint returns the fingerprint of the local keypair.
func (sm *SessionManager) OwnFingerprint() string {
	return sm.ownKeyPair.Fingerprint()
}

// OwnPublicKeyBase64 returns the local public key as base64.
func (sm *SessionManager) OwnPublicKeyBase64() string {
	return sm.ownKeyPair.PublicKeyBase64()
}
