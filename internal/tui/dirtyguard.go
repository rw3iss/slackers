package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DirtyGuard is a reusable component for overlays that need to detect
// unsaved changes before allowing navigation away. When the guard is
// active (dirty=true and a leave-shortcut is pressed), it captures the
// pending action and prompts the user. Only Ctrl+C / quit and save/cancel
// pass through while the guard prompt is showing.
//
// Usage:
//   1. Embed DirtyGuard in your overlay model.
//   2. Call guard.MarkDirty() when any edit happens.
//   3. Call guard.MarkClean() after a successful save.
//   4. When a navigation key is pressed, call guard.Intercept(msg).
//      If it returns (true, nil) the guard is now prompting — render the prompt.
//      If it returns (false, nil) the overlay is clean — proceed with navigation.
//   5. In the prompt state, call guard.HandlePrompt(msg):
//      - Returns PromptConfirm → execute the pending action and close.
//      - Returns PromptCancel → clear the prompt, stay in the overlay.
//      - Returns PromptIgnore → prompt ate the key, do nothing.
type DirtyGuard struct {
	dirty      bool
	prompting  bool
	pendingKey string // the key string that triggered the prompt
}

// PromptResult indicates the outcome of HandlePrompt.
type PromptResult int

const (
	PromptIgnore  PromptResult = iota // prompt consumed the key, no action
	PromptConfirm                     // user confirmed leaving
	PromptCancel                      // user cancelled, stay in overlay
)

// MarkDirty flags the guard as having unsaved changes.
func (g *DirtyGuard) MarkDirty() { g.dirty = true }

// MarkClean clears the dirty flag (e.g. after save).
func (g *DirtyGuard) MarkClean() {
	g.dirty = false
	g.prompting = false
	g.pendingKey = ""
}

// IsDirty returns whether there are unsaved changes.
func (g *DirtyGuard) IsDirty() bool { return g.dirty }

// IsPrompting returns whether the guard is currently showing a prompt.
func (g *DirtyGuard) IsPrompting() bool { return g.prompting }

// PendingKey returns the key that triggered the prompt (for executing after confirm).
func (g *DirtyGuard) PendingKey() string { return g.pendingKey }

// Intercept checks whether the given key should be intercepted by the guard.
// Returns true if the guard is now prompting (overlay should show the prompt).
// Returns false if the overlay is clean and should proceed with the action.
func (g *DirtyGuard) Intercept(key string) bool {
	if !g.dirty {
		return false
	}
	g.prompting = true
	g.pendingKey = key
	return true
}

// HandlePrompt processes a key while the prompt is showing.
func (g *DirtyGuard) HandlePrompt(key string) PromptResult {
	switch key {
	case "y", "Y", "enter":
		g.prompting = false
		return PromptConfirm
	case "n", "N", "esc":
		g.prompting = false
		g.pendingKey = ""
		return PromptCancel
	default:
		return PromptIgnore
	}
}

// PromptText returns a standard prompt message for the guard.
func (g *DirtyGuard) PromptText() string {
	return "Unsaved changes — discard? (y/N)"
}

// ---------------------------------------------------------------------------
// Timed message helper
// ---------------------------------------------------------------------------

// TimedMessage manages a status message that auto-clears after a duration.
// Embed it in overlay models that show transient feedback (e.g. "Saved").
type TimedMessage struct {
	text string
	id   int
}

// TimedMessageClearMsg is the tea.Msg that clears a timed message.
type TimedMessageClearMsg struct {
	ID int
}

// Set shows a message and returns a tea.Cmd that will clear it after ttl.
func (t *TimedMessage) Set(msg string, ttl time.Duration) tea.Cmd {
	t.id++
	t.text = msg
	id := t.id
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return TimedMessageClearMsg{ID: id}
	})
}

// Clear immediately clears the message (used for manual clear).
func (t *TimedMessage) Clear() {
	t.id++
	t.text = ""
}

// HandleClear should be called when a TimedMessageClearMsg arrives.
// Returns true if the message was cleared (ID matched).
func (t *TimedMessage) HandleClear(id int) bool {
	if id == t.id {
		t.text = ""
		return true
	}
	return false
}

// Text returns the current message (empty string if cleared).
func (t *TimedMessage) Text() string { return t.text }

// SetImmediate sets a message without a timer (stays until manually cleared
// or replaced).
func (t *TimedMessage) SetImmediate(msg string) {
	t.id++
	t.text = msg
}
