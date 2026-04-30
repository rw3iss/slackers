package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// SidebarOptionsAction represents a chosen action from the sidebar
// channel context menu.
type SidebarOptionsAction int

const (
	SidebarActionNone SidebarOptionsAction = iota
	SidebarActionHide
	SidebarActionRename
	SidebarActionInvite
	SidebarActionViewContact
	SidebarActionRemoveFriend
	SidebarActionBrowseFiles
	SidebarActionStartAudioCall
)

// SidebarOptionsSelectMsg signals which option the user chose from
// the sidebar channel context menu.
type SidebarOptionsSelectMsg struct {
	Action    SidebarOptionsAction
	ChannelID string
	UserID    string
}

type sidebarOptionsItem struct {
	label  string
	action SidebarOptionsAction
}

// SidebarOptionsModel is a popup menu rendered next to a right-clicked
// channel entry in the sidebar.
type SidebarOptionsModel struct {
	menu      PopupMenu
	channelID string
	userID    string
	actions   []SidebarOptionsAction
}

// NewSidebarOptions builds a popup for the given channel.
func NewSidebarOptions(channelID, userID string, items []sidebarOptionsItem, x, y int) SidebarOptionsModel {
	var menuItems []PopupMenuItem
	var actions []SidebarOptionsAction
	for i, it := range items {
		menuItems = append(menuItems, PopupMenuItem{Label: it.label, Index: i})
		actions = append(actions, it.action)
	}
	menu := NewPopupMenu("Channel", menuItems, x, y)
	return SidebarOptionsModel{
		menu:      menu,
		channelID: channelID,
		userID:    userID,
		actions:   actions,
	}
}

func (m *SidebarOptionsModel) SetSize(w, h int) { m.menu.SetSize(w, h) }

func (m SidebarOptionsModel) ClickInside(x, y int) bool { return m.menu.ClickInside(x, y) }

func (m SidebarOptionsModel) Update(msg tea.Msg) (SidebarOptionsModel, tea.Cmd) {
	var idx int
	var confirmed bool
	m.menu, idx, confirmed = m.menu.Update(msg)
	if confirmed && idx >= 0 && idx < len(m.actions) {
		action := m.actions[idx]
		chID := m.channelID
		uID := m.userID
		return m, func() tea.Msg {
			return SidebarOptionsSelectMsg{
				Action:    action,
				ChannelID: chID,
				UserID:    uID,
			}
		}
	}
	return m, nil
}

func (m SidebarOptionsModel) View(bgContent string) string {
	return m.menu.View(bgContent)
}
