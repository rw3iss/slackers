package tui

// Right-click context menu for [FRIEND:...] pills rendered inside chat
// messages. Mirrors MsgOptionsModel / SidebarOptionsModel — same popup
// chrome, same hit-testing, same item-row layout — but the items are
// chosen based on whether the embedded contact card is already a
// friend in the local store. See handlers_ui.go for the right-click
// detection that opens this overlay, and model.go for the action
// dispatch (FriendCardOptionsSelectMsg).

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// FriendCardOptionsSelectMsg signals which option the user chose. The
// full ContactCard travels with the message so the dispatcher in
// model.go has everything it needs without re-resolving from a key.
type FriendCardOptionsSelectMsg struct {
	Action FriendCardOptionsAction
	Card   friends.ContactCard
}

type friendCardOptionsItem struct {
	label  string
	action FriendCardOptionsAction
}

// FriendCardOptionsModel is the popup menu rendered next to a
// right-clicked friend pill.
type FriendCardOptionsModel struct {
	card       friends.ContactCard
	items      []friendCardOptionsItem
	selected   int
	x, y       int // anchor position (click coords)
	minX       int // minimum X (chat history left edge)
	finalX     int
	finalY     int
	boxW, boxH int
	width      int
	height     int
}

// NewFriendCardOptions builds a popup for the given contact card.
// The (isFriend, isSelf) flags pick the right item set:
//
//   - isSelf:  View Contact Info, Copy Contact Info
//     (no Add / merge / overwrite — you can't friend yourself)
//   - isFriend: View Friend Profile, View Contact Info, Copy Contact Info
//     (View Friend Profile jumps to the editable Friends Config page;
//     View Contact Info opens the temporary card view with the
//     merge / overwrite buttons inside)
//   - default: Add Friend, View Contact Info, Copy Contact Info
//
// View Contact Info is always present so the user can always see
// the raw card properties, even for their own card.
//
// The caller (handlers_ui.go) is responsible for the friend lookup
// and the self check so the menu stays a thin view-layer concern.
func NewFriendCardOptions(card friends.ContactCard, isFriend, isSelf bool, x, y, minX int) FriendCardOptionsModel {
	var items []friendCardOptionsItem
	switch {
	case isSelf:
		items = []friendCardOptionsItem{
			{"View Contact Info", FriendCardActionViewContactInfo},
			{"Copy Contact Info", FriendCardActionCopyContactInfo},
		}
	case isFriend:
		items = []friendCardOptionsItem{
			{"View Friend Profile", FriendCardActionViewFriendProfile},
			{"View Contact Info", FriendCardActionViewContactInfo},
			{"Copy Contact Info", FriendCardActionCopyContactInfo},
		}
	default:
		items = []friendCardOptionsItem{
			{"Add Friend", FriendCardActionAddFriend},
			{"View Contact Info", FriendCardActionViewContactInfo},
			{"Copy Contact Info", FriendCardActionCopyContactInfo},
		}
	}
	return FriendCardOptionsModel{
		card:  card,
		items: items,
		x:     x,
		y:     y,
		minX:  minX,
	}
}

// Card returns the contact card the popup was opened for.
func (m FriendCardOptionsModel) Card() friends.ContactCard { return m.card }

func (m *FriendCardOptionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	sample := m.renderBox()
	m.boxH = strings.Count(sample, "\n") + 1
	m.boxW = lipgloss.Width(sample)

	m.finalX = m.x
	if m.finalX < m.minX {
		m.finalX = m.minX
	}
	if m.finalX+m.boxW > m.width {
		m.finalX = m.width - m.boxW - 1
	}
	if m.finalX < m.minX {
		m.finalX = m.minX
	}

	m.finalY = m.y
	if m.finalY+m.boxH > m.height {
		m.finalY = m.height - m.boxH - 1
	}
	if m.finalY < 0 {
		m.finalY = 0
	}
}

// ClickInside reports whether (x,y) is inside the popup box (with a
// 1-cell buffer so edge clicks still register).
func (m FriendCardOptionsModel) ClickInside(x, y int) bool {
	const buffer = 1
	return x >= m.finalX-buffer && x < m.finalX+m.boxW+buffer &&
		y >= m.finalY-buffer && y < m.finalY+m.boxH+buffer
}

func (m FriendCardOptionsModel) renderBox() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Friend Card"))
	b.WriteString("\n")
	for i, item := range m.items {
		b.WriteString("\n")
		cursor := "  "
		style := ChannelItemStyle
		if i == m.selected {
			cursor = "> "
			style = ChannelSelectedStyle
		}
		b.WriteString(style.Render(cursor + item.label))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑↓ Enter Esc"))

	// Box wide enough to fit the longest item plus cursor + padding.
	maxLen := len("Friend Card")
	for _, it := range m.items {
		if l := len(it.label) + 2; l > maxLen {
			maxLen = l
		}
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Width(maxLen + 4)

	return boxStyle.Render(b.String())
}

func (m FriendCardOptionsModel) Update(msg tea.Msg) (FriendCardOptionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
			}
		case "enter":
			if m.selected < 0 || m.selected >= len(m.items) {
				return m, nil
			}
			item := m.items[m.selected]
			card := m.card
			return m, func() tea.Msg {
				return FriendCardOptionsSelectMsg{
					Action: item.action,
					Card:   card,
				}
			}
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			// Items each span 2 rows (blank line before each). The
			// -3 offset matches SidebarOptionsModel — clicks on a
			// label land on that label instead of the row above.
			delta := msg.Y - m.finalY - 3
			row := delta / 2
			if row >= 0 && row < len(m.items) {
				m.selected = row
				item := m.items[row]
				card := m.card
				return m, func() tea.Msg {
					return FriendCardOptionsSelectMsg{
						Action: item.action,
						Card:   card,
					}
				}
			}
		}
	}
	return m, nil
}

// View overlays the popup onto bgContent at its clamped anchor.
func (m FriendCardOptionsModel) View(bgContent string) string {
	box := m.renderBox()
	boxLines := strings.Split(box, "\n")
	posX := m.finalX
	posY := m.finalY

	bgLines := strings.Split(bgContent, "\n")
	for len(bgLines) < m.height {
		bgLines = append(bgLines, "")
	}

	for i, line := range boxLines {
		row := posY + i
		if row >= len(bgLines) {
			break
		}
		bgLines[row] = ansiTruncatePad(bgLines[row], posX) + line
	}

	return strings.Join(bgLines, "\n")
}
