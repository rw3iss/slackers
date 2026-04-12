// Package notifications collects user-facing events from across the
// app — unread messages, message reactions, incoming friend requests,
// and so on — into a single store the UI can browse and act on.
package notifications

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Type identifies the kind of notification so the UI can pick a
// per-type renderer and the model can handle activation correctly.
type Type string

const (
	// TypeUnreadMessage is created when a new message arrives in a
	// channel the user is not currently viewing. Cleared automatically
	// when the user opens that channel and the unread state is reset.
	TypeUnreadMessage Type = "unread_message"

	// TypeReaction is created when someone reacts to one of your
	// messages while you're not currently viewing that conversation.
	TypeReaction Type = "reaction"

	// TypeFriendRequest is created when a P2P friend request arrives.
	// Activating it opens the friend request modal pre-populated with
	// the requesting peer's identity. Cleared once the user accepts,
	// rejects, or independently befriends the peer.
	TypeFriendRequest Type = "friend_request"
)

// Notification is a single entry in the store. The same struct serves
// every Type — the type-specific fields are populated as needed.
type Notification struct {
	ID          string    `json:"id"`
	Type        Type      `json:"type"`
	WorkspaceID string    `json:"workspace_id,omitempty"` // empty for P2P
	ChannelID   string    `json:"channel_id"`
	MessageID string    `json:"message_id,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	UserName  string    `json:"user_name,omitempty"`
	Text      string    `json:"text,omitempty"`
	Timestamp time.Time `json:"timestamp"`

	// Reaction-specific.
	Emoji            string `json:"emoji,omitempty"`
	TargetMessageTxt string `json:"target_message_text,omitempty"`

	// FriendRequest-specific.
	FriendPublicKey string `json:"friend_public_key,omitempty"`
	FriendMultiaddr string `json:"friend_multiaddr,omitempty"`
}

// MaxItems caps the number of notifications retained in memory and
// on disk. Older entries beyond this limit are discarded on insert
// so the store can't grow unbounded on a long-running session.
const MaxItems = 500

// Store is a thread-safe in-memory + on-disk notification log.
type Store struct {
	path  string
	mu    sync.Mutex
	items []Notification

	// saveTimer is the pending debounced save. Set by any
	// mutation method and cleared by the flush goroutine.
	// Access protected by mu.
	saveTimer *time.Timer
}

// NewStore creates a store backed by the given file path. The file is
// created on the first Save call.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// scheduleSaveLocked arms the debounced save timer, coalescing any
// previously-pending write into a single one. Caller must hold s.mu.
// The idle window matches the config-save window so settings + notif
// bursts collapse at roughly the same rhythm.
func (s *Store) scheduleSaveLocked() {
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(750*time.Millisecond, func() {
		_ = s.Save()
	})
}

// FlushPending writes any pending debounced save synchronously.
// Call on clean shutdown so last-second mutations aren't lost.
func (s *Store) FlushPending() {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()
	_ = s.Save()
}

// DefaultPath returns the standard notifications file location.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "notifications.json")
}

// Load reads the notifications file from disk. Missing file is OK.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.items = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.items)
}

// Save persists the current notifications to disk.
func (s *Store) Save() error {
	s.mu.Lock()
	data, err := json.MarshalIndent(s.items, "", "  ")
	itemsLen := len(s.items)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if itemsLen == 0 {
		// Still write so deletions persist.
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Add appends a notification (auto-generates ID + timestamp if blank).
// If a notification with the same Type+ChannelID+MessageID already
// exists, no duplicate is added and the existing notification is
// returned unchanged. If the total item count would exceed MaxItems
// after the append, the oldest entries are dropped from the head of
// the slice — the newest are always preserved.
func (s *Store) Add(n Notification) Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.items {
		if existing.Type == n.Type && existing.ChannelID == n.ChannelID && existing.MessageID == n.MessageID && existing.MessageID != "" {
			return existing
		}
	}
	if n.ID == "" {
		n.ID = randomID()
	}
	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}
	s.items = append(s.items, n)
	if len(s.items) > MaxItems {
		// Drop the oldest overflow in one shot. Slice trim, no copy.
		s.items = s.items[len(s.items)-MaxItems:]
	}
	s.scheduleSaveLocked()
	return n
}

// Remove deletes a notification by ID. Returns true on success.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, n := range s.items {
		if n.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			s.scheduleSaveLocked()
			return true
		}
	}
	return false
}

// All returns a copy of every stored notification, newest first.
func (s *Store) All() []Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Notification, len(s.items))
	copy(out, s.items)
	// Reverse so callers see newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Count returns the total number of stored notifications.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// ClearChannel removes every TypeUnreadMessage and TypeReaction
// notification belonging to the given channel ID. Friend-request
// notifications are not touched here — they're cleared via
// ClearFriendRequest after the user accepts/rejects/befriends.
// Returns the number of removed entries.
func (s *Store) ClearChannel(channelID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []Notification
	removed := 0
	for _, n := range s.items {
		if n.ChannelID == channelID && (n.Type == TypeUnreadMessage || n.Type == TypeReaction) {
			removed++
			continue
		}
		kept = append(kept, n)
	}
	s.items = kept
	if removed > 0 {
		s.scheduleSaveLocked()
	}
	return removed
}

// ClearFriendRequest removes any pending friend-request notification
// for the given peer user ID. Used after the user befriends them
// (whether through the notification flow or any other path).
func (s *Store) ClearFriendRequest(peerUserID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []Notification
	removed := 0
	for _, n := range s.items {
		if n.Type == TypeFriendRequest && n.UserID == peerUserID {
			removed++
			continue
		}
		kept = append(kept, n)
	}
	s.items = kept
	if removed > 0 {
		s.scheduleSaveLocked()
	}
	return removed
}

// CountByType returns counts by Type for status bar / badge use.
func (s *Store) CountByType() map[Type]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Type]int)
	for _, n := range s.items {
		out[n.Type]++
	}
	return out
}

// randomID returns a short hex-encoded random identifier.
func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
