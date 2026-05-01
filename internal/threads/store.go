package threads

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// fileSchema is the on-disk JSON shape. Versioned so future changes
// can migrate without breaking older configs.
type fileSchema struct {
	Version       int              `json:"version"`
	Active        []ThreadSnapshot `json:"active"`
	LastClearedAt time.Time        `json:"last_cleared_at,omitzero"`
}

// ThreadStore is the persistent set of active thread snapshots. It
// mirrors notifications.Store: sync.Mutex-guarded in-memory state
// with a debounced save and a fan-out channel that notifies the UI
// of mutations.
type ThreadStore struct {
	path  string
	mu    sync.Mutex
	items []ThreadSnapshot

	// lastClearedAt persists across restarts so the scheduler can
	// decide whether to run an immediate sweep on boot.
	lastClearedAt time.Time

	// saveTimer coalesces rapid mutations into a single disk write.
	saveTimer *time.Timer

	// changedSubs is a slice of receivers (one per UI subscriber) the
	// store sends a beacon to whenever items change. Non-blocking.
	// Subscribers convert the signal to a tea.Msg via tea.Cmd.
	changedSubs []chan struct{}
}

// NewStore constructs a store backed by the given file path. The
// file is created on the first Save call. Call Load() to populate
// from disk before use.
func NewStore(path string) *ThreadStore {
	return &ThreadStore{path: path}
}

// Load reads the threads file from disk. Missing file is OK — the
// store starts empty.
func (s *ThreadStore) Load() error {
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
	var f fileSchema
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	s.items = f.Active
	s.lastClearedAt = f.LastClearedAt
	return nil
}

// Save persists the current state to disk synchronously. Most
// callers should rely on the debounced save scheduled by the
// mutation methods; Save is exposed for the FlushPending shutdown
// path and for tests.
func (s *ThreadStore) Save() error {
	s.mu.Lock()
	f := fileSchema{
		Version:       1,
		Active:        append([]ThreadSnapshot(nil), s.items...),
		LastClearedAt: s.lastClearedAt,
	}
	s.mu.Unlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// scheduleSaveLocked arms the debounced save timer. Caller holds mu.
// The 750 ms idle window matches notifications.Store so settings +
// notif + thread bursts collapse at the same rhythm.
func (s *ThreadStore) scheduleSaveLocked() {
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(750*time.Millisecond, func() {
		_ = s.Save()
	})
}

// FlushPending writes any pending debounced save synchronously.
// Call from the model's shutdown path so last-second mutations
// aren't lost.
func (s *ThreadStore) FlushPending() {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()
	_ = s.Save()
}

// notifyChangedLocked delivers a non-blocking beacon to every
// subscriber. Caller holds mu. Subscribers that aren't currently
// reading from their channel are silently skipped — they'll pick up
// the next change.
func (s *ThreadStore) notifyChangedLocked() {
	for _, ch := range s.changedSubs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ChangedSub returns a channel that fires whenever the active set
// mutates. The store retains a reference; do not close the returned
// channel. UI callers wrap it in a tea.Cmd that emits ThreadsChangedMsg.
func (s *ThreadStore) ChangedSub() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{}, 1)
	s.changedSubs = append(s.changedSubs, ch)
	return ch
}

// Add inserts a snapshot. Returns true if the snapshot was newly
// added; false if a snapshot for the same Ref already existed (in
// which case the existing snapshot is updated in place — name,
// participants, last activity refreshed, AddedAt and Reason kept
// from the original).
func (s *ThreadStore) Add(snap ThreadSnapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snap.AddedAt.IsZero() {
		snap.AddedAt = time.Now()
	}
	for i, existing := range s.items {
		if existing.Ref == snap.Ref {
			// Refresh denormalized fields but preserve original
			// AddedAt and Reason — those represent the original
			// detection event, not the latest activity.
			updated := existing
			updated.ChannelName = snap.ChannelName
			updated.ParentText = snap.ParentText
			updated.ParentAuthorID = snap.ParentAuthorID
			updated.ParentAuthorName = snap.ParentAuthorName
			updated.Participants = snap.Participants
			if snap.LastActivityTS > updated.LastActivityTS {
				updated.LastActivityTS = snap.LastActivityTS
			}
			s.items[i] = updated
			s.scheduleSaveLocked()
			s.notifyChangedLocked()
			return false
		}
	}
	s.items = append(s.items, snap)
	s.scheduleSaveLocked()
	s.notifyChangedLocked()
	return true
}

// Remove deletes a snapshot by Ref. Returns true on success.
func (s *ThreadStore) Remove(ref ThreadRef) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, snap := range s.items {
		if snap.Ref == ref {
			s.items = append(s.items[:i], s.items[i+1:]...)
			s.scheduleSaveLocked()
			s.notifyChangedLocked()
			return true
		}
	}
	return false
}

// Get returns the snapshot for the given Ref, or zero/false.
func (s *ThreadStore) Get(ref ThreadRef) (ThreadSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snap := range s.items {
		if snap.Ref == ref {
			return snap, true
		}
	}
	return ThreadSnapshot{}, false
}

// Touch bumps the LastActivityTS for an existing snapshot. Returns
// true if the snapshot was found and updated. Used when a new reply
// arrives in a thread that's already tracked — the sidebar resorts
// to put it on top.
func (s *ThreadStore) Touch(ref ThreadRef, lastActivityTS string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, snap := range s.items {
		if snap.Ref == ref {
			if lastActivityTS > snap.LastActivityTS {
				s.items[i].LastActivityTS = lastActivityTS
				s.scheduleSaveLocked()
				s.notifyChangedLocked()
			}
			return true
		}
	}
	return false
}

// Active returns a copy of the active snapshots, sorted by
// LastActivityTS (newest first). Snapshots without a LastActivityTS
// fall back to AddedAt for the comparison.
func (s *ThreadStore) Active() []ThreadSnapshot {
	s.mu.Lock()
	out := make([]ThreadSnapshot, len(s.items))
	copy(out, s.items)
	s.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		ai := activitySortKey(out[i])
		aj := activitySortKey(out[j])
		return ai > aj // descending: newest first
	})
	return out
}

// activitySortKey returns a string suitable for descending sort.
// Slack timestamps are lexicographically comparable; AddedAt is
// formatted in RFC3339Nano so it's also comparable when used as a
// fallback. Mixing the two is safe because the function is only
// called during sort within the same Active() call.
func activitySortKey(s ThreadSnapshot) string {
	if s.LastActivityTS != "" {
		return s.LastActivityTS
	}
	if !s.AddedAt.IsZero() {
		return s.AddedAt.UTC().Format(time.RFC3339Nano)
	}
	return ""
}

// Count returns the number of active snapshots.
func (s *ThreadStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// ClearOlderThan removes every snapshot whose LastActivityTS (or
// AddedAt, when LastActivityTS is empty) is older than cutoff.
// Returns the number removed.
func (s *ThreadStore) ClearOlderThan(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return 0
	}
	kept := s.items[:0]
	removed := 0
	for _, snap := range s.items {
		t := snapshotActivityTime(snap)
		if !t.IsZero() && t.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, snap)
	}
	if removed > 0 {
		s.items = kept
		s.lastClearedAt = time.Now()
		s.scheduleSaveLocked()
		s.notifyChangedLocked()
	}
	return removed
}

// snapshotActivityTime returns the activity time as a time.Time, or
// zero when neither LastActivityTS nor AddedAt is set. Slack
// timestamps look like "1714502500.001234" — the integer part is
// Unix seconds.
func snapshotActivityTime(s ThreadSnapshot) time.Time {
	if s.LastActivityTS != "" {
		if secs, ok := parseSlackTS(s.LastActivityTS); ok {
			return time.Unix(secs, 0)
		}
	}
	return s.AddedAt
}

// parseSlackTS extracts the integer seconds portion of a
// "<seconds>.<micros>" Slack timestamp.
func parseSlackTS(ts string) (int64, bool) {
	var secs int64
	for i := 0; i < len(ts); i++ {
		c := ts[i]
		if c == '.' {
			break
		}
		if c < '0' || c > '9' {
			return 0, false
		}
		secs = secs*10 + int64(c-'0')
	}
	if secs == 0 {
		return 0, false
	}
	return secs, true
}

// LastClearedAt returns the persisted last-cleared timestamp.
func (s *ThreadStore) LastClearedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastClearedAt
}

// SetLastClearedAt overrides the last-cleared timestamp. Used by the
// scheduler after a sweep.
func (s *ThreadStore) SetLastClearedAt(t time.Time) {
	s.mu.Lock()
	s.lastClearedAt = t
	s.scheduleSaveLocked()
	s.mu.Unlock()
}
