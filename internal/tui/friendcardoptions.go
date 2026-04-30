package tui

// Right-click context menu for [FRIEND:...] pills rendered inside chat
// messages. Uses the shared PopupMenu for rendering and navigation;
// provides its own action types and dispatch logic.

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/friends"
)

// FriendCardOptionsAction represents a chosen action from the friend
// card right-click menu.
type FriendCardOptionsAction int

const (
	FriendCardActionNone FriendCardOptionsAction = iota
	FriendCardActionAddFriend
	FriendCardActionViewContactInfo   // not-yet-friend → temporary contact view
	FriendCardActionViewFriendProfile // already a friend → friends config edit page
	FriendCardActionCopyContactInfo
)

// FriendCardOptionsSelectMsg signals which option the user chose.
type FriendCardOptionsSelectMsg struct {
	Action FriendCardOptionsAction
	Card   friends.ContactCard
}

// FriendCardOptionsModel is the popup menu rendered next to a
// right-clicked friend pill.
type FriendCardOptionsModel struct {
	menu    PopupMenu
	card    friends.ContactCard
	actions []FriendCardOptionsAction
}

// NewFriendCardOptions builds a popup for the given contact card.
// The (isFriend, isSelf) flags pick the right item set.
func NewFriendCardOptions(card friends.ContactCard, isFriend, isSelf bool, x, y, minX int) FriendCardOptionsModel {
	var items []PopupMenuItem
	var actions []FriendCardOptionsAction

	add := func(label string, action FriendCardOptionsAction) {
		items = append(items, PopupMenuItem{Label: label, Index: len(items)})
		actions = append(actions, action)
	}

	switch {
	case isSelf:
		add("View Contact Info", FriendCardActionViewContactInfo)
		add("Copy Contact Info", FriendCardActionCopyContactInfo)
	case isFriend:
		add("View Friend Profile", FriendCardActionViewFriendProfile)
		add("View Contact Info", FriendCardActionViewContactInfo)
		add("Copy Contact Info", FriendCardActionCopyContactInfo)
	default:
		add("Add Friend", FriendCardActionAddFriend)
		add("View Contact Info", FriendCardActionViewContactInfo)
		add("Copy Contact Info", FriendCardActionCopyContactInfo)
	}

	menu := NewPopupMenu("Friend Card", items, x, y)
	menu.SetMinX(minX)
	return FriendCardOptionsModel{
		menu:    menu,
		card:    card,
		actions: actions,
	}
}

// Card returns the contact card the popup was opened for.
func (m FriendCardOptionsModel) Card() friends.ContactCard { return m.card }

func (m *FriendCardOptionsModel) SetSize(w, h int) { m.menu.SetSize(w, h) }

func (m FriendCardOptionsModel) ClickInside(x, y int) bool { return m.menu.ClickInside(x, y) }

func (m FriendCardOptionsModel) Update(msg tea.Msg) (FriendCardOptionsModel, tea.Cmd) {
	var idx int
	var confirmed bool
	m.menu, idx, confirmed = m.menu.Update(msg)
	if confirmed && idx >= 0 && idx < len(m.actions) {
		action := m.actions[idx]
		card := m.card
		return m, func() tea.Msg {
			return FriendCardOptionsSelectMsg{
				Action: action,
				Card:   card,
			}
		}
	}
	return m, nil
}

func (m FriendCardOptionsModel) View(bgContent string) string {
	return m.menu.View(bgContent)
}
