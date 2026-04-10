package friends

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Friend struct {
	UserID         string `json:"user_id"`
	SlackerID      string `json:"slacker_id,omitempty"`
	Name           string `json:"name"`
	Email          string `json:"email,omitempty"`
	PublicKey      string `json:"public_key"`
	PairKey        string `json:"pair_key,omitempty"` // per-pair encryption key (base64)
	Multiaddr      string `json:"multiaddr"`
	Endpoint       string `json:"endpoint,omitempty"` // IP/hostname
	Port           int    `json:"port,omitempty"`
	AddedAt        int64  `json:"added_at"`
	LastOnline     int64  `json:"last_online,omitempty"`
	ConnectionType string `json:"connection_type,omitempty"` // "p2p", "e2e", ""
	Online         bool   `json:"-"`
	// AwayStatus is the friend's current status: "online", "away",
	// "back", "offline", or empty (not yet known). Runtime-only —
	// refreshed by the ping cycle and status_update messages, not
	// persisted to disk.
	AwayStatus  string `json:"-"`
	AwayMessage string `json:"-"` // optional status text (e.g. "BRB lunch")
}

// ContactCard is the shareable JSON format for exchanging friend info.
// The multiaddr already encodes the IP and port — separate Endpoint /
// Port fields are no longer included.
type ContactCard struct {
	Version   int    `json:"version"`
	SlackerID string `json:"slacker_id"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	PublicKey string `json:"public_key"`
	Multiaddr string `json:"multiaddr"`
}

// ToContactCard converts a Friend to a shareable ContactCard.
func (f Friend) ToContactCard() ContactCard {
	return ContactCard{
		Version:   1,
		SlackerID: f.SlackerID,
		Name:      f.Name,
		Email:     f.Email,
		PublicKey: f.PublicKey,
		Multiaddr: f.Multiaddr,
	}
}

// FriendFromCard creates a Friend from a ContactCard. The Endpoint /
// Port fields on Friend (kept for legacy in-memory state only) are
// left empty — every consumer should rely on Multiaddr.
func FriendFromCard(card ContactCard) Friend {
	return Friend{
		SlackerID: card.SlackerID,
		Name:      card.Name,
		Email:     card.Email,
		PublicKey: card.PublicKey,
		Multiaddr: card.Multiaddr,
	}
}

// MyContactCard builds a contact card for the local user.
func MyContactCard(slackerID, name, email, publicKey, multiaddr string) ContactCard {
	return ContactCard{
		Version:   1,
		SlackerID: slackerID,
		Name:      name,
		Email:     email,
		PublicKey: publicKey,
		Multiaddr: multiaddr,
	}
}

type FriendRequest struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Multiaddr string `json:"multiaddr"`
}

// FriendStore is a thread-safe, JSON-backed list of Friend records
// with a secondary byUserID index for O(1) Get / Update / SetOnline
// / UpdateLastOnline lookups. Every write path that can add, remove,
// or reorder entries must call reindexLocked() before releasing the
// write lock so the index stays consistent with the slice.
type FriendStore struct {
	friends  []Friend
	byUserID map[string]int // UserID → index into friends slice
	path     string
	mu       sync.RWMutex
}

// reindexLocked rebuilds byUserID from the current friends slice.
// Caller must already hold the write lock.
func (s *FriendStore) reindexLocked() {
	s.byUserID = make(map[string]int, len(s.friends))
	for i, f := range s.friends {
		if f.UserID != "" {
			s.byUserID[f.UserID] = i
		}
	}
}

func NewFriendStore(path string) *FriendStore {
	return &FriendStore{
		path:     path,
		byUserID: make(map[string]int),
	}
}

func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "friends.json")
}

func (s *FriendStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.friends = nil
			s.reindexLocked()
			return nil
		}
		return fmt.Errorf("reading friends: %w", err)
	}
	if err := json.Unmarshal(data, &s.friends); err != nil {
		return err
	}
	s.reindexLocked()
	return nil
}

func (s *FriendStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.friends, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *FriendStore) All() []Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Friend, len(s.friends))
	copy(out, s.friends)
	return out
}

func (s *FriendStore) Add(f Friend) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Generate a UserID if not provided (for non-Slack friends)
	// so the index lookup below sees the eventual value.
	if f.UserID == "" && f.SlackerID != "" {
		f.UserID = "slacker:" + f.SlackerID
	}
	if f.UserID != "" {
		if _, exists := s.byUserID[f.UserID]; exists {
			return fmt.Errorf("friend %s already exists", f.UserID)
		}
	}
	if f.SlackerID != "" {
		// SlackerID collision still requires a linear scan because
		// the index is keyed on UserID, but this only fires on Add.
		for _, existing := range s.friends {
			if existing.SlackerID == f.SlackerID {
				return fmt.Errorf("friend with SlackerID %s already exists", f.SlackerID)
			}
		}
	}
	if f.AddedAt == 0 {
		f.AddedAt = time.Now().Unix()
	}
	s.friends = append(s.friends, f)
	if f.UserID != "" {
		s.byUserID[f.UserID] = len(s.friends) - 1
	}
	return nil
}

func (s *FriendStore) Remove(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.byUserID[userID]
	if !ok {
		return
	}
	s.friends = append(s.friends[:idx], s.friends[idx+1:]...)
	// Every subsequent entry shifted — reindex the whole map.
	s.reindexLocked()
}

func (s *FriendStore) Get(userID string) *Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx, ok := s.byUserID[userID]; ok && idx < len(s.friends) {
		return &s.friends[idx]
	}
	return nil
}

func (s *FriendStore) SetOnline(userID string, online bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx, ok := s.byUserID[userID]; ok && idx < len(s.friends) {
		s.friends[idx].Online = online
	}
}

// SetStatus updates a friend's away/online status and optional
// message. statusType is one of "online", "offline", "away",
// "back". The Online bool is derived from the status automatically:
// "online"/"back" → true, "offline" → false, "away" → stays true
// (they're reachable, just busy). Thread-safe.
func (s *FriendStore) SetStatus(userID, statusType, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.byUserID[userID]
	if !ok || idx >= len(s.friends) {
		return
	}
	s.friends[idx].AwayStatus = statusType
	s.friends[idx].AwayMessage = message
	switch statusType {
	case "online", "back":
		s.friends[idx].Online = true
		s.friends[idx].AwayMessage = "" // clear away message on return
	case "offline":
		s.friends[idx].Online = false
	case "away":
		s.friends[idx].Online = true // reachable, just away
	}
}

func (s *FriendStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.friends)
}

// Update replaces a friend's fields by UserID.
func (s *FriendStore) Update(f Friend) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.byUserID[f.UserID]
	if !ok || idx >= len(s.friends) {
		return fmt.Errorf("friend %s not found", f.UserID)
	}
	// Preserve runtime-only fields.
	f.Online = s.friends[idx].Online
	f.AwayStatus = s.friends[idx].AwayStatus
	f.AwayMessage = s.friends[idx].AwayMessage
	s.friends[idx] = f
	// UserID is the map key; Update can't change it, but in case
	// a buggy caller passes a different one, the record at idx
	// already has the old UserID so the map is still correct.
	return nil
}

// UpdateLastOnline records the current time as last online for a friend.
func (s *FriendStore) UpdateLastOnline(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx, ok := s.byUserID[userID]; ok && idx < len(s.friends) {
		s.friends[idx].LastOnline = time.Now().Unix()
	}
}

// FindConflict checks if a friend conflicts with existing entries.
// Returns the conflicting friend's UserID, or "" if no conflict.
// Conflict is detected by any of: UserID, SlackerID, Email, PublicKey,
// or Multiaddr — so re-imports of the same person under a slightly
// different identifier still resolve to the existing record.
func (s *FriendStore) FindConflict(f Friend) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, existing := range s.friends {
		if f.UserID != "" && existing.UserID == f.UserID {
			return existing.UserID
		}
		if f.SlackerID != "" && existing.SlackerID == f.SlackerID {
			return existing.UserID
		}
		if f.Email != "" && existing.Email == f.Email {
			return existing.UserID
		}
		if f.PublicKey != "" && existing.PublicKey == f.PublicKey {
			return existing.UserID
		}
		if f.Multiaddr != "" && existing.Multiaddr == f.Multiaddr {
			return existing.UserID
		}
	}
	return ""
}

// FindByCard locates an existing friend that matches the given contact
// card by SlackerID, PublicKey, or Multiaddr (in that priority order).
// Returns a copy of the matched friend, or nil if no match is found.
// This is the canonical lookup used by the inbound friend-card import
// flow so a re-shared profile resolves to the same record even when
// the SlackerID changed.
func (s *FriendStore) FindByCard(card ContactCard) *Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matchOn := func(f Friend) bool {
		if card.SlackerID != "" && f.SlackerID == card.SlackerID {
			return true
		}
		if card.PublicKey != "" && f.PublicKey == card.PublicKey {
			return true
		}
		if card.Multiaddr != "" && f.Multiaddr == card.Multiaddr {
			return true
		}
		uid := card.SlackerID
		if uid != "" {
			uid = "slacker:" + uid
		}
		if uid != "" && f.UserID == uid {
			return true
		}
		return false
	}
	for i, f := range s.friends {
		if matchOn(f) {
			cp := s.friends[i]
			return &cp
		}
	}
	return nil
}

// Import merges friends from a list, using conflict resolution.
// If overwrite is true, conflicting entries are replaced. Otherwise skipped.
// Returns counts of (added, skipped, overwritten).
func (s *FriendStore) Import(incoming []Friend, overwrite bool) (added, skipped, overwritten int) {
	for _, f := range incoming {
		conflictID := s.FindConflict(f)
		if conflictID != "" {
			if overwrite {
				s.Remove(conflictID)
				if f.AddedAt == 0 {
					f.AddedAt = time.Now().Unix()
				}
				s.mu.Lock()
				s.friends = append(s.friends, f)
				if f.UserID != "" {
					s.byUserID[f.UserID] = len(s.friends) - 1
				}
				s.mu.Unlock()
				overwritten++
			} else {
				skipped++
			}
		} else {
			_ = s.Add(f)
			added++
		}
	}
	return
}

// ExportJSON returns all friends as formatted JSON bytes.
func (s *FriendStore) ExportJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.MarshalIndent(s.friends, "", "  ")
}

// ImportJSON parses a JSON byte slice into a list of friends.
func ImportJSON(data []byte) ([]Friend, error) {
	var friends []Friend
	if err := json.Unmarshal(data, &friends); err != nil {
		return nil, fmt.Errorf("parsing friends JSON: %w", err)
	}
	return friends, nil
}
