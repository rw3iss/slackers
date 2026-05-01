package threads

import (
	"sync"
	"time"

	"github.com/rw3iss/slackers/internal/debug"
)

// Scheduler runs the auto-clear sweep on a recurring in-process
// timer. It's OS-agnostic (no cron daemon dependency) and survives
// across app restarts because the store persists last_cleared_at.
//
// On Start:
//   - Reads the persisted last_cleared_at from the store.
//   - If it's older than `interval` (or zero), runs a sweep
//     immediately so a long-offline gap doesn't accumulate stale
//     threads.
//   - Arms a timer that fires every interval.
//
// SetInterval reconfigures live: stops the timer, runs a sweep with
// the new cutoff, re-arms.
type Scheduler struct {
	store *ThreadStore

	mu       sync.Mutex
	interval time.Duration
	timer    *time.Timer
	stopped  bool
}

// NewScheduler builds a scheduler. interval=0 disables the sweep
// entirely (no timer armed).
func NewScheduler(store *ThreadStore, interval time.Duration) *Scheduler {
	return &Scheduler{store: store, interval: interval}
}

// Start runs the catch-up sweep (if needed) and arms the recurring
// timer. Calling Start more than once is safe — subsequent calls
// re-arm using the current interval.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = false
	if s.interval <= 0 {
		debug.Log("[threads] scheduler disabled (interval=0)")
		return
	}
	debug.Log("[threads] scheduler starting interval=%s", s.interval)
	s.runIfDueLocked()
	s.armLocked()
}

// SetInterval reconfigures the sweep cadence. Passing 0 disables it.
// Always runs an immediate sweep with the new cutoff so the change
// takes visible effect right away.
func (s *Scheduler) SetInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.interval = d
	debug.Log("[threads] scheduler interval=%s", d)
	if s.stopped {
		return
	}
	if d <= 0 {
		return
	}
	s.runOnceLocked()
	s.armLocked()
}

// Stop cancels the timer. Safe to call from shutdown.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// runIfDueLocked runs a sweep when last_cleared_at is older than the
// current interval. Caller holds mu.
func (s *Scheduler) runIfDueLocked() {
	last := s.store.LastClearedAt()
	if last.IsZero() || time.Since(last) >= s.interval {
		s.runOnceLocked()
	}
}

// runOnceLocked performs a single sweep with the current interval as
// the cutoff. Caller holds mu.
func (s *Scheduler) runOnceLocked() {
	cutoff := time.Now().Add(-s.interval)
	removed := s.store.ClearOlderThan(cutoff)
	s.store.SetLastClearedAt(time.Now())
	debug.Log("[threads] auto-clear cutoff=%s removed=%d total=%d",
		cutoff.Format(time.RFC3339), removed, s.store.Count())
}

// armLocked starts a new timer for the next sweep. Caller holds mu.
func (s *Scheduler) armLocked() {
	s.timer = time.AfterFunc(s.interval, func() {
		s.mu.Lock()
		if s.stopped || s.interval <= 0 {
			s.mu.Unlock()
			return
		}
		s.runOnceLocked()
		s.armLocked()
		s.mu.Unlock()
	})
}
