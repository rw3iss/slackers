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
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Multiaddr string `json:"multiaddr"`
	AddedAt   int64  `json:"added_at"`
	Online    bool   `json:"-"`
}

type FriendRequest struct {
	Type      string `json:"type"`       // "friend_request", "friend_accept", "friend_reject"
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Multiaddr string `json:"multiaddr"`
}

type FriendStore struct {
	friends []Friend
	path    string
	mu      sync.RWMutex
}

func NewFriendStore(path string) *FriendStore {
	return &FriendStore{path: path}
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
			return nil
		}
		return fmt.Errorf("reading friends: %w", err)
	}
	return json.Unmarshal(data, &s.friends)
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
	for _, existing := range s.friends {
		if existing.UserID == f.UserID {
			return fmt.Errorf("friend %s already exists", f.UserID)
		}
	}
	if f.AddedAt == 0 {
		f.AddedAt = time.Now().Unix()
	}
	s.friends = append(s.friends, f)
	return nil
}

func (s *FriendStore) Remove(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			s.friends = append(s.friends[:i], s.friends[i+1:]...)
			return
		}
	}
}

func (s *FriendStore) Get(userID string) *Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			return &s.friends[i]
		}
	}
	return nil
}

func (s *FriendStore) SetOnline(userID string, online bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			s.friends[i].Online = online
			return
		}
	}
}

func (s *FriendStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.friends)
}
