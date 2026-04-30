package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// MsgOptionsAction represents a chosen action from the options menu.
type MsgOptionsAction int

const (
	MsgActionNone MsgOptionsAction = iota
	MsgActionReact
	MsgActionReply
	MsgActionEdit
	MsgActionDelete
	MsgActionCopy
	MsgActionViewFile
)

// MsgOptionsSelectMsg signals which option the user chose.
type MsgOptionsSelectMsg struct {
	Action    MsgOptionsAction
	MessageID string
	Preview   string
}

// MsgOptionsModel is a small popup menu shown next to a clicked message.
type MsgOptionsModel struct {
	menu      PopupMenu
	messageID string
	preview   string
	actions   []MsgOptionsAction
}

// NewMsgOptions creates an options popup at the given position.
// minX is the minimum left X (e.g. chat history left edge).
// allowAuthorActions controls whether "Edit" and "Delete" are shown.
// hasFiles adds a "View File" entry.
func NewMsgOptions(messageID, preview string, x, y, minX int, allowAuthorActions, hasFiles bool) MsgOptionsModel {
	var items []PopupMenuItem
	var actions []MsgOptionsAction

	add := func(label string, action MsgOptionsAction) {
		items = append(items, PopupMenuItem{Label: label, Index: len(items)})
		actions = append(actions, action)
	}

	add("React", MsgActionReact)
	add("Reply", MsgActionReply)
	if allowAuthorActions {
		add("Edit", MsgActionEdit)
	}
	add("Copy Message", MsgActionCopy)
	if hasFiles {
		add("View File", MsgActionViewFile)
	}
	if allowAuthorActions {
		add("Delete", MsgActionDelete)
	}

	menu := NewPopupMenu("Options", items, x, y)
	menu.SetMinX(minX)
	menu.SetFixedWidth(18)
	return MsgOptionsModel{
		menu:      menu,
		messageID: messageID,
		preview:   preview,
		actions:   actions,
	}
}

func (m *MsgOptionsModel) SetSize(w, h int) { m.menu.SetSize(w, h) }

func (m MsgOptionsModel) ClickInside(x, y int) bool { return m.menu.ClickInside(x, y) }

func (m MsgOptionsModel) Update(msg tea.Msg) (MsgOptionsModel, tea.Cmd) {
	var idx int
	var confirmed bool
	m.menu, idx, confirmed = m.menu.Update(msg)
	if confirmed && idx >= 0 && idx < len(m.actions) {
		action := m.actions[idx]
		msgID := m.messageID
		preview := m.preview
		return m, func() tea.Msg {
			return MsgOptionsSelectMsg{
				Action:    action,
				MessageID: msgID,
				Preview:   preview,
			}
		}
	}
	return m, nil
}

func (m MsgOptionsModel) View(bgContent string) string {
	return m.menu.View(bgContent)
}
