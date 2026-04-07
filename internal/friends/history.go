package friends

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rw3iss/slackers/internal/types"
)

// ChatHistoryStore manages persistent, optionally encrypted friend chat histories.
type ChatHistoryStore struct {
	baseDir    string
	encrypt    bool // if true, encrypt messages using per-friend PairKey
	mu         sync.Mutex
	dirty      map[string]bool // userIDs with unsaved changes
	cache      map[string][]types.Message
	debounceMs int
}

// NewChatHistoryStore creates a history store at the given directory.
func NewChatHistoryStore(baseDir string, encrypt bool) *ChatHistoryStore {
	return &ChatHistoryStore{
		baseDir:    baseDir,
		encrypt:    encrypt,
		dirty:      make(map[string]bool),
		cache:      make(map[string][]types.Message),
		debounceMs: 5000,
	}
}

// DefaultHistoryDir returns the default history directory.
func DefaultHistoryDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "history")
}

func (s *ChatHistoryStore) filePath(userID string) string {
	safe := filepath.Base(userID) // prevent path traversal
	return filepath.Join(s.baseDir, safe+".json")
}

// Load reads chat history for a friend from disk into cache.
func (s *ChatHistoryStore) Load(userID string) ([]types.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msgs, ok := s.cache[userID]; ok {
		return msgs, nil
	}

	path := s.filePath(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cache[userID] = nil
			return nil, nil
		}
		return nil, fmt.Errorf("reading history for %s: %w", userID, err)
	}

	var msgs []types.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("parsing history for %s: %w", userID, err)
	}

	s.cache[userID] = msgs
	return msgs, nil
}

// Append adds a message to a friend's history and marks it dirty.
func (s *ChatHistoryStore) Append(userID string, msg types.Message, pairKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Encrypt the message text if enabled and we have a key.
	if s.encrypt && pairKey != "" {
		encrypted, err := encryptString(msg.Text, pairKey)
		if err == nil {
			msg.Text = encrypted
			msg.IsEncrypted = true
		}
	}

	s.cache[userID] = append(s.cache[userID], msg)
	s.dirty[userID] = true
}

// Save writes a friend's cached history to disk.
func (s *ChatHistoryStore) Save(userID string) error {
	s.mu.Lock()
	msgs := s.cache[userID]
	delete(s.dirty, userID)
	s.mu.Unlock()

	if msgs == nil {
		return nil
	}

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath(userID), data, 0o600)
}

// SaveDirty saves all friends with unsaved changes.
func (s *ChatHistoryStore) SaveDirty() {
	s.mu.Lock()
	dirtyIDs := make([]string, 0, len(s.dirty))
	for id := range s.dirty {
		dirtyIDs = append(dirtyIDs, id)
	}
	s.mu.Unlock()

	for _, id := range dirtyIDs {
		_ = s.Save(id)
	}
}

// Get returns the cached history for a friend, loading from disk if needed.
func (s *ChatHistoryStore) Get(userID string) []types.Message {
	s.mu.Lock()
	if msgs, ok := s.cache[userID]; ok {
		out := make([]types.Message, len(msgs))
		copy(out, msgs)
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()

	msgs, _ := s.Load(userID)
	return msgs
}

// GetDecrypted returns history with encrypted messages decrypted.
func (s *ChatHistoryStore) GetDecrypted(userID, pairKey string) []types.Message {
	msgs := s.Get(userID)
	if pairKey == "" {
		return msgs
	}
	for i, msg := range msgs {
		if msg.IsEncrypted {
			decrypted, err := decryptString(msg.Text, pairKey)
			if err == nil {
				msgs[i].Text = decrypted
				msgs[i].IsEncrypted = false
			}
		}
	}
	return msgs
}

// Prune removes messages older than the given number of days.
// If days == 0, no pruning is done (keep all).
func (s *ChatHistoryStore) Prune(days int) {
	if days <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, msgs := range s.cache {
		var kept []types.Message
		for _, m := range msgs {
			if m.Timestamp.After(cutoff) {
				kept = append(kept, m)
			}
		}
		if len(kept) != len(msgs) {
			s.cache[userID] = kept
			s.dirty[userID] = true
		}
	}
}

// AppendReply adds a reply message as a child of a parent message.
func (s *ChatHistoryStore) AppendReply(userID, parentMsgID string, reply types.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.cache[userID]
	for i, msg := range msgs {
		if msg.MessageID == parentMsgID {
			msgs[i].Replies = append(msgs[i].Replies, reply)
			s.dirty[userID] = true
			break
		}
	}
}

// UpdateReaction adds or updates a reaction on a message in the cache.
func (s *ChatHistoryStore) UpdateReaction(userID, messageID, emoji, reactUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.cache[userID]
	for i, msg := range msgs {
		if msg.MessageID == messageID {
			found := false
			for j, r := range msg.Reactions {
				if r.Emoji == emoji {
					// Add user to existing reaction if not already there.
					for _, uid := range r.UserIDs {
						if uid == reactUserID {
							found = true
							break
						}
					}
					if !found {
						msgs[i].Reactions[j].UserIDs = append(msgs[i].Reactions[j].UserIDs, reactUserID)
						msgs[i].Reactions[j].Count++
					}
					found = true
					break
				}
			}
			if !found {
				msgs[i].Reactions = append(msgs[i].Reactions, types.Reaction{
					Emoji:   emoji,
					UserIDs: []string{reactUserID},
					Count:   1,
				})
			}
			s.dirty[userID] = true
			break
		}
	}
}

// --- Encryption helpers ---
// Uses AES-256-GCM with the PairKey (base64-encoded 32 bytes).

func deriveAESKey(pairKeyB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(pairKeyB64)
	if err != nil {
		return nil, err
	}
	if len(key) < 32 {
		// Pad short keys (shouldn't happen with proper ECDH).
		padded := make([]byte, 32)
		copy(padded, key)
		return padded, nil
	}
	return key[:32], nil
}

func encryptString(plaintext, pairKeyB64 string) (string, error) {
	key, err := deriveAESKey(pairKeyB64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptString(ciphertextB64, pairKeyB64 string) (string, error) {
	key, err := deriveAESKey(pairKeyB64)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
