package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FriendRequestSentMsg struct {
	UserID string
	Name   string
}

type FriendRequestReceivedMsg struct {
	UserID    string
	Name      string
	PublicKey string
	Multiaddr string
}

type FriendRequestRespondMsg struct {
	UserID    string
	Name      string
	Accepted  bool
	PublicKey string
	Multiaddr string
}

type FriendRequestModel struct {
	userID    string
	userName  string
	incoming  bool
	selected  int // 0 = accept/send, 1 = cancel/reject
	width     int
	height    int
	publicKey string
	multiaddr string
}

func NewOutgoingFriendRequest(userID, userName string) FriendRequestModel {
	return FriendRequestModel{
		userID:   userID,
		userName: userName,
		incoming: false,
	}
}

func NewIncomingFriendRequest(userID, userName, pubKey, multiaddr string) FriendRequestModel {
	return FriendRequestModel{
		userID:    userID,
		userName:  userName,
		incoming:  true,
		publicKey: pubKey,
		multiaddr: multiaddr,
	}
}

func (m *FriendRequestModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m FriendRequestModel) Update(msg tea.Msg) (FriendRequestModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			m.selected = 0
		case "right", "l":
			m.selected = 1
		case "tab":
			m.selected = (m.selected + 1) % 2
		case "enter":
			if m.incoming {
				return m, func() tea.Msg {
					return FriendRequestRespondMsg{
						UserID:    m.userID,
						Name:      m.userName,
						Accepted:  m.selected == 0,
						PublicKey: m.publicKey,
						Multiaddr: m.multiaddr,
					}
				}
			}
			if m.selected == 0 {
				return m, func() tea.Msg {
					return FriendRequestSentMsg{UserID: m.userID, Name: m.userName}
				}
			}
			return m, func() tea.Msg {
				return FriendRequestRespondMsg{Accepted: false}
			}
		}
	}
	return m, nil
}

func (m FriendRequestModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	nameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	if m.incoming {
		b.WriteString(titleStyle.Render("Friend Request"))
		b.WriteString("\n\n")
		b.WriteString("  " + nameStyle.Render(m.userName) + " wants to be your friend.\n")
		b.WriteString("  Accept to enable private P2P chat.\n")
	} else {
		b.WriteString(titleStyle.Render("Add Friend"))
		b.WriteString("\n\n")
		b.WriteString("  Send a friend request to " + nameStyle.Render(m.userName) + "?\n")
		b.WriteString("  This enables private P2P chat outside Slack.\n")
	}

	b.WriteString("\n")

	yesLabel := "  Send  "
	noLabel := " Cancel "
	if m.incoming {
		yesLabel = " Accept "
		noLabel = " Reject "
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Background(lipgloss.Color("236"))
	inactiveStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if m.selected == 0 {
		b.WriteString("  " + activeStyle.Render("["+yesLabel+"]") + "  " + inactiveStyle.Render(" "+noLabel+" "))
	} else {
		b.WriteString("  " + inactiveStyle.Render(" "+yesLabel+" ") + "  " + activeStyle.Render("["+noLabel+"]"))
	}

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Tab: switch | Enter: confirm | Esc: cancel"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(50, m.width-4))

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
