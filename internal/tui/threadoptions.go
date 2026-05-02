package tui

// Right-click context menu for sidebar Threads-group items and rows
// in the global Threads overlay (Plan B). Built on the shared
// PopupMenu, like the other context menus.

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/threads"
)

// ThreadOptionsAction represents a chosen action from the threads
// right-click menu.
type ThreadOptionsAction int

const (
	ThreadActionNone ThreadOptionsAction = iota

	// ThreadActionClose removes the thread from the active store.
	// The thread will reappear if a future message re-triggers any
	// detection rule for the local user.
	ThreadActionClose

	// ThreadActionGoToChannel switches to the parent's channel
	// without auto-opening the reply-detail view. Lighter than the
	// default Enter activation.
	ThreadActionGoToChannel
)

// ThreadOptionsSelectMsg is emitted when the user picks an entry
// from the threads context menu.
type ThreadOptionsSelectMsg struct {
	Action ThreadOptionsAction
	Ref    threads.ThreadRef
}

// ThreadOptionsModel is the popup menu rendered next to a
// right-clicked thread item.
type ThreadOptionsModel struct {
	menu    PopupMenu
	ref     threads.ThreadRef
	actions []ThreadOptionsAction
}

// NewThreadOptions builds a popup anchored at (x, y) for the given
// thread reference.
func NewThreadOptions(ref threads.ThreadRef, x, y int) ThreadOptionsModel {
	items := []PopupMenuItem{
		{Label: "Go to Channel", Index: 0},
		{Label: "Close Thread", Index: 1},
	}
	actions := []ThreadOptionsAction{
		ThreadActionGoToChannel,
		ThreadActionClose,
	}
	menu := NewPopupMenu("Thread", items, x, y)
	return ThreadOptionsModel{
		menu:    menu,
		ref:     ref,
		actions: actions,
	}
}

// Ref returns the thread the popup was opened for.
func (m ThreadOptionsModel) Ref() threads.ThreadRef { return m.ref }

func (m *ThreadOptionsModel) SetSize(w, h int) { m.menu.SetSize(w, h) }

func (m ThreadOptionsModel) ClickInside(x, y int) bool { return m.menu.ClickInside(x, y) }

func (m ThreadOptionsModel) Update(msg tea.Msg) (ThreadOptionsModel, tea.Cmd) {
	var idx int
	var confirmed bool
	m.menu, idx, confirmed = m.menu.Update(msg)
	if confirmed && idx >= 0 && idx < len(m.actions) {
		action := m.actions[idx]
		ref := m.ref
		return m, func() tea.Msg {
			return ThreadOptionsSelectMsg{
				Action: action,
				Ref:    ref,
			}
		}
	}
	return m, nil
}

func (m ThreadOptionsModel) View(bgContent string) string {
	return m.menu.View(bgContent)
}
