package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// ChatOptionsAction represents a chosen action from the chat-pane context menu.
type ChatOptionsAction int

const (
	ChatActionNone ChatOptionsAction = iota
	ChatActionViewContact
	ChatActionBrowseShared
	ChatActionViewFiles
	ChatActionSendFile
	ChatActionAudioCall
	ChatActionRename
)

// ChatOptionsSelectMsg is emitted when the user picks an entry from the
// chat-pane right-click menu.
type ChatOptionsSelectMsg struct {
	Action    ChatOptionsAction
	ChannelID string
	UserID    string
}

type chatOptionsItem struct {
	label  string
	action ChatOptionsAction
}

// ChatOptionsModel is the right-click context menu for the chat/messages pane.
type ChatOptionsModel struct {
	menu      PopupMenu
	channelID string
	userID    string
	actions   []ChatOptionsAction
}

// NewChatOptions builds a context menu anchored at (x, y).
func NewChatOptions(channelID, userID string, items []chatOptionsItem, x, y int) ChatOptionsModel {
	var menuItems []PopupMenuItem
	var actions []ChatOptionsAction
	for i, it := range items {
		menuItems = append(menuItems, PopupMenuItem{Label: it.label, Index: i})
		actions = append(actions, it.action)
	}
	menu := NewPopupMenu("Chat", menuItems, x, y)
	return ChatOptionsModel{
		menu:      menu,
		channelID: channelID,
		userID:    userID,
		actions:   actions,
	}
}

// SetBounds records the visible chat pane boundaries for clamping.
func (m *ChatOptionsModel) SetBounds(chatLeft, chatTop, chatRight, chatBottom int) {
	m.menu.SetBounds(chatLeft, chatTop, chatRight, chatBottom)
}

func (m *ChatOptionsModel) SetSize(w, h int) { m.menu.SetSize(w, h) }

func (m ChatOptionsModel) ClickInside(x, y int) bool { return m.menu.ClickInside(x, y) }

func (m ChatOptionsModel) Update(msg tea.Msg) (ChatOptionsModel, tea.Cmd) {
	var idx int
	var confirmed bool
	m.menu, idx, confirmed = m.menu.Update(msg)
	if confirmed && idx >= 0 && idx < len(m.actions) {
		action := m.actions[idx]
		chID := m.channelID
		uID := m.userID
		return m, func() tea.Msg {
			return ChatOptionsSelectMsg{
				Action:    action,
				ChannelID: chID,
				UserID:    uID,
			}
		}
	}
	return m, nil
}

func (m ChatOptionsModel) View(bgContent string) string {
	return m.menu.View(bgContent)
}
