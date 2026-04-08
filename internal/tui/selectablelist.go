package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// SelectableList is a tiny value-type primitive that owns the
// cursor / bounds-clamp / navigation logic shared by eight overlay
// models (hidden channels, notifications overlay, shortcuts
// editor, settings, friends config, files list, friend request,
// theme picker).
//
// It intentionally does NOT know how to render — each overlay keeps
// its own View() because rendering layouts vary wildly. It just
// manages the integer cursor and the standard keybindings
// (up/down/k/j/pgup/pgdn/home/end) so each overlay stops
// reimplementing the same bounds-clamp math.
//
// Usage:
//
//	type MyOverlayModel struct {
//	    list SelectableList
//	    // ...
//	}
//
//	func (m *MyOverlayModel) Update(msg tea.Msg) (MyOverlayModel, tea.Cmd) {
//	    if km, ok := msg.(tea.KeyMsg); ok {
//	        m.list.SetCount(len(m.items))
//	        if m.list.HandleKey(km) {
//	            return m, nil
//	        }
//	    }
//	    // overlay-specific keys (enter, esc, filter input, …)
//	}
//
//	// In View(): reference m.list.Selected for the cursor.
type SelectableList struct {
	// Selected is the 0-based cursor index. -1 when Count == 0.
	Selected int
	// Count is the number of rows the cursor can land on. Set by
	// the caller on every Update before HandleKey so wrapping and
	// clamping operate on the correct range.
	Count int
	// WrapAround makes up-at-top jump to the bottom and
	// down-at-bottom jump to the top. Off by default (clamped).
	WrapAround bool
	// PageSize is the step used by PageUp / PageDown. 0 means
	// "use a sensible default of 5".
	PageSize int
}

// SetCount updates the item count and clamps Selected to stay in
// bounds. Call at the top of every Update pass so navigation
// always sees the current row count.
func (l *SelectableList) SetCount(n int) {
	l.Count = n
	l.clamp()
}

// clamp brings Selected into [0, Count-1] or sets it to -1 when
// the list is empty.
func (l *SelectableList) clamp() {
	if l.Count <= 0 {
		l.Selected = -1
		return
	}
	if l.Selected < 0 {
		l.Selected = 0
	}
	if l.Selected >= l.Count {
		l.Selected = l.Count - 1
	}
}

// Navigate moves the cursor by delta, honouring WrapAround.
func (l *SelectableList) Navigate(delta int) {
	if l.Count <= 0 {
		l.Selected = -1
		return
	}
	newSel := l.Selected + delta
	if l.WrapAround {
		// Handle arbitrary delta by modulo.
		newSel = ((newSel % l.Count) + l.Count) % l.Count
	} else {
		if newSel < 0 {
			newSel = 0
		}
		if newSel >= l.Count {
			newSel = l.Count - 1
		}
	}
	l.Selected = newSel
}

// Home moves the cursor to the first row.
func (l *SelectableList) Home() {
	if l.Count > 0 {
		l.Selected = 0
	}
}

// End moves the cursor to the last row.
func (l *SelectableList) End() {
	if l.Count > 0 {
		l.Selected = l.Count - 1
	}
}

// pageStep returns the effective page size for PageUp/PageDown.
func (l SelectableList) pageStep() int {
	if l.PageSize > 0 {
		return l.PageSize
	}
	return 5
}

// PageUp moves the cursor up by PageSize rows (clamped).
func (l *SelectableList) PageUp() {
	l.Navigate(-l.pageStep())
}

// PageDown moves the cursor down by PageSize rows (clamped).
func (l *SelectableList) PageDown() {
	l.Navigate(l.pageStep())
}

// HandleKey consumes the standard navigation bindings and
// returns true if the key was handled. Overlays should call this
// early in their Update and only fall through to overlay-specific
// key handling when it returns false.
//
// Recognised bindings:
//
//	up   / k      → Navigate(-1)
//	down / j      → Navigate(+1)
//	pgup          → PageUp
//	pgdown        → PageDown
//	home          → Home
//	end           → End
//
// Enter, Esc, Tab, and all printable keys are left to the caller
// so filter inputs and activation actions keep working.
func (l *SelectableList) HandleKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "k":
		l.Navigate(-1)
		return true
	case "down", "j":
		l.Navigate(1)
		return true
	case "pgup":
		l.PageUp()
		return true
	case "pgdown":
		l.PageDown()
		return true
	case "home":
		l.Home()
		return true
	case "end":
		l.End()
		return true
	}
	return false
}

// Current returns the current Selected index, or -1 if the list
// is empty. Handy as a null-safe accessor so callers don't have
// to bounds-check on every render.
func (l SelectableList) Current() int {
	if l.Count <= 0 {
		return -1
	}
	return l.Selected
}
