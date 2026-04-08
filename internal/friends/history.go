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

// FilePath returns the on-disk location of a friend's chat history
// file. Used by callers that want to show the path in confirmation
// prompts before destructive operations.
func (s *ChatHistoryStore) FilePath(userID string) string {
	return s.filePath(userID)
}

// ClearHistory removes the on-disk history file for a friend, drops
// the cache for that user, and writes back an empty file so the
// next save call has somewhere to go. Returns the path that was
// cleared (or would have been) for status messages.
func (s *ChatHistoryStore) ClearHistory(userID string) (string, error) {
	s.mu.Lock()
	delete(s.cache, userID)
	delete(s.dirty, userID)
	s.mu.Unlock()
	path := s.filePath(userID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, err
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return path, err
	}
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		return path, err
	}
	return path, nil
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
// The returned slice is always a fresh copy so callers can mutate it
// (e.g. AddReactionLocal) without aliasing the store's internal cache.
func (s *ChatHistoryStore) Get(userID string) []types.Message {
	s.mu.Lock()
	if msgs, ok := s.cache[userID]; ok {
		out := cloneMessages(msgs)
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()

	msgs, _ := s.Load(userID)
	// Load stores the slice directly in s.cache; return a clone so the
	// caller's view is detached from the store's internal state.
	return cloneMessages(msgs)
}

// cloneMessages returns a deep-enough copy of a message slice: each
// Message is value-copied, and its Reactions / Replies slices are also
// copied so that mutations on the returned slice (e.g. appending to a
// reaction's UserIDs) cannot leak back into the source.
func cloneMessages(src []types.Message) []types.Message {
	if src == nil {
		return nil
	}
	out := make([]types.Message, len(src))
	for i, m := range src {
		out[i] = m
		if len(m.Reactions) > 0 {
			rs := make([]types.Reaction, len(m.Reactions))
			for j, r := range m.Reactions {
				rs[j] = r
				if len(r.UserIDs) > 0 {
					rs[j].UserIDs = append([]string(nil), r.UserIDs...)
				}
			}
			out[i].Reactions = rs
		}
		if len(m.Replies) > 0 {
			out[i].Replies = cloneMessages(m.Replies)
		}
	}
	return out
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

// EditMessage updates the text of a message (top-level or nested reply)
// in the cache. Returns true if a matching message was found and updated.
func (s *ChatHistoryStore) EditMessage(userID, messageID, newText string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := findMsgPtr(s.cache[userID], messageID)
	if target == nil {
		return false
	}
	target.Text = newText
	s.dirty[userID] = true
	return true
}

// DeleteMessage removes a message (top-level or nested reply) from the cache.
// Returns true if a message was found and removed.
func (s *ChatHistoryStore) DeleteMessage(userID, messageID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.cache[userID]
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			s.cache[userID] = append(msgs[:i], msgs[i+1:]...)
			s.dirty[userID] = true
			return true
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				msgs[i].Replies = append(msgs[i].Replies[:j], msgs[i].Replies[j+1:]...)
				s.dirty[userID] = true
				return true
			}
		}
	}
	return false
}

// SetPending toggles the Pending flag on a message (top-level or nested
// reply) in a friend's history cache. Returns true if the target was
// found so callers can trigger a save on success.
func (s *ChatHistoryStore) SetPending(userID, messageID string, pending bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := findMsgPtr(s.cache[userID], messageID)
	if target == nil {
		return false
	}
	if target.Pending == pending {
		return false
	}
	target.Pending = pending
	s.dirty[userID] = true
	return true
}

// PendingForResend returns the messages in a friend's history that
// still have the Pending flag set, walking newest-first and stopping
// as soon as the first already-delivered (non-Pending, non-me) entry
// is hit — under the assumption that everything before a confirmed
// delivery would already have been re-sent on a prior connect. The
// returned slice is ordered oldest-first (send order) and includes
// both top-level messages and nested replies authored locally. Each
// returned message has its Text decrypted using pairKey if the
// store's at-rest encryption is on.
func (s *ChatHistoryStore) PendingForResend(userID, pairKey string) []types.Message {
	s.mu.Lock()
	msgs := s.cache[userID]
	s.mu.Unlock()

	var out []types.Message
	// Walk top-level messages in reverse; stop at the first locally
	// authored (UserID=="me") message that is NOT pending, since
	// anything older should have been dealt with already.
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.UserID == "me" {
			if !m.Pending {
				break
			}
			// Decrypt in-place for send.
			if m.IsEncrypted && pairKey != "" {
				if plain, err := decryptString(m.Text, pairKey); err == nil {
					m.Text = plain
					m.IsEncrypted = false
				}
			}
			out = append(out, m)
		}
		// Walk replies newest-first too so any pending reply under
		// a top-level message gets included. Replies share the
		// same "stop at first delivered local" rule.
		for j := len(m.Replies) - 1; j >= 0; j-- {
			r := m.Replies[j]
			if r.UserID != "me" {
				continue
			}
			if !r.Pending {
				continue
			}
			if r.IsEncrypted && pairKey != "" {
				if plain, err := decryptString(r.Text, pairKey); err == nil {
					r.Text = plain
					r.IsEncrypted = false
				}
			}
			out = append(out, r)
		}
	}
	// Reverse out to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// findMsgPtr returns a pointer to the message with the given ID, searching
// both top-level messages and their nested replies.
func findMsgPtr(msgs []types.Message, messageID string) *types.Message {
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			return &msgs[i]
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				return &msgs[i].Replies[j]
			}
		}
	}
	return nil
}

// RemoveReaction removes a user's reaction from a message in the cache.
// Searches both top-level messages and nested replies.
func (s *ChatHistoryStore) RemoveReaction(userID, messageID, emoji, reactUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := findMsgPtr(s.cache[userID], messageID)
	if target == nil {
		return
	}
	for j, r := range target.Reactions {
		if r.Emoji != emoji {
			continue
		}
		for k, uid := range r.UserIDs {
			if uid != reactUserID {
				continue
			}
			target.Reactions[j].UserIDs = append(r.UserIDs[:k], r.UserIDs[k+1:]...)
			target.Reactions[j].Count--
			if target.Reactions[j].Count <= 0 {
				target.Reactions = append(target.Reactions[:j], target.Reactions[j+1:]...)
			}
			s.dirty[userID] = true
			return
		}
	}
}

// RemoveAllReactionAliases strips every entry in `aliases` from all
// reaction groups on the given message that match `emoji`. Empty
// groups are collapsed. Used by toggleReaction to clean up multiple
// stored identities (e.g. legacy "me" + canonical slacker ID) in a
// single pass, so the persisted cache agrees with the in-memory view.
func (s *ChatHistoryStore) RemoveAllReactionAliases(userID, messageID, emoji string, aliases []string) {
	if len(aliases) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	target := findMsgPtr(s.cache[userID], messageID)
	if target == nil {
		return
	}
	aliasSet := make(map[string]struct{}, len(aliases))
	for _, a := range aliases {
		aliasSet[a] = struct{}{}
	}
	out := target.Reactions[:0]
	changed := false
	for _, r := range target.Reactions {
		if r.Emoji != emoji {
			out = append(out, r)
			continue
		}
		kept := make([]string, 0, len(r.UserIDs))
		for _, uid := range r.UserIDs {
			if _, drop := aliasSet[uid]; drop {
				changed = true
				continue
			}
			kept = append(kept, uid)
		}
		if len(kept) == 0 {
			changed = true
			continue
		}
		if len(kept) != len(r.UserIDs) {
			changed = true
		}
		r.UserIDs = kept
		r.Count = len(kept)
		out = append(out, r)
	}
	target.Reactions = out
	if changed {
		s.dirty[userID] = true
	}
}

// UpdateReaction adds or updates a reaction on a message in the cache.
// Searches both top-level messages and nested replies.
func (s *ChatHistoryStore) UpdateReaction(userID, messageID, emoji, reactUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := findMsgPtr(s.cache[userID], messageID)
	if target == nil {
		return
	}
	for j, r := range target.Reactions {
		if r.Emoji != emoji {
			continue
		}
		for _, uid := range r.UserIDs {
			if uid == reactUserID {
				return
			}
		}
		target.Reactions[j].UserIDs = append(target.Reactions[j].UserIDs, reactUserID)
		target.Reactions[j].Count++
		s.dirty[userID] = true
		return
	}
	target.Reactions = append(target.Reactions, types.Reaction{
		Emoji:   emoji,
		UserIDs: []string{reactUserID},
		Count:   1,
	})
	s.dirty[userID] = true
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
