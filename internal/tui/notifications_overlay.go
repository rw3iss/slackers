package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/notifications"
)

// NotificationsOpenMsg requests the notifications overlay to open.
type NotificationsOpenMsg struct{}

// NotificationsCloseMsg signals the overlay should close.
type NotificationsCloseMsg struct{}

// NotificationActivateMsg is dispatched when the user hits Enter on a
// notification — the model navigates to the relevant chat / message
// and (when appropriate) clears the notification.
type NotificationActivateMsg struct {
	Notif notifications.Notification
}

// NotificationDeleteMsg removes the highlighted notification (the 'x'
// key). The store is updated by the parent model.
type NotificationDeleteMsg struct {
	NotifID string
}

// NotificationsOverlayModel renders a list of notifications similar to
// the search-results page. The user can navigate with up/down,
// activate with Enter, or delete with x. Cursor + navigation state
// is managed by SelectableList so the overlay only owns the
// activation / dismiss key bindings.
type NotificationsOverlayModel struct {
	items     []notifications.Notification
	list      SelectableList
	scrollOff int
	width     int
	height    int
}

// NewNotificationsOverlay constructs the overlay from a snapshot of
// notifications. The caller should pass a fresh snapshot each open so
// the list reflects current state.
func NewNotificationsOverlay(items []notifications.Notification) NotificationsOverlayModel {
	return NotificationsOverlayModel{
		items: items,
		list:  SelectableList{WrapAround: true, PageSize: 0 /* derived from visibleEntries */},
	}
}

// SetSize sets the available render area.
func (m *NotificationsOverlayModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SelectedNotification returns the currently highlighted notification
// or a zero value if the list is empty.
func (m NotificationsOverlayModel) SelectedNotification() notifications.Notification {
	idx := m.list.Current()
	if idx < 0 || idx >= len(m.items) {
		return notifications.Notification{}
	}
	return m.items[idx]
}

// Update handles key events for the overlay.
func (m NotificationsOverlayModel) Update(msg tea.Msg) (NotificationsOverlayModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Page size follows the visible row estimate so PgUp/PgDn
		// jump one visible page at a time.
		m.list.PageSize = m.visibleEntries()
		m.list.SetCount(len(m.items))
		if m.list.HandleKey(keyMsg) {
			m.ensureVisible()
			return m, nil
		}
		switch keyMsg.String() {
		case "enter":
			n := m.SelectedNotification()
			if n.ID == "" {
				return m, nil
			}
			return m, func() tea.Msg { return NotificationActivateMsg{Notif: n} }
		case "x", "delete":
			n := m.SelectedNotification()
			if n.ID == "" {
				return m, nil
			}
			id := n.ID
			return m, func() tea.Msg { return NotificationDeleteMsg{NotifID: id} }
		case "esc":
			return m, func() tea.Msg { return NotificationsCloseMsg{} }
		}
	}
	return m, nil
}

// SetItems replaces the list (used after a deletion to refresh in place).
func (m *NotificationsOverlayModel) SetItems(items []notifications.Notification) {
	m.items = items
	m.list.SetCount(len(items))
}

// visibleEntries estimates how many entries fit in the box. Each
// entry renders as 3 lines (header + body + spacer).
func (m NotificationsOverlayModel) visibleEntries() int {
	overhead := 8 // border + title + footer + padding
	avail := m.height - overhead
	if avail < 3 {
		return 1
	}
	return avail / 3
}

func (m *NotificationsOverlayModel) ensureVisible() {
	vis := m.visibleEntries()
	sel := m.list.Current()
	if sel < 0 {
		return
	}
	if sel < m.scrollOff {
		m.scrollOff = sel
	}
	if sel >= m.scrollOff+vis {
		m.scrollOff = sel - vis + 1
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the notifications overlay.
func (m NotificationsOverlayModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Notifications (%d)", len(m.items))))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(emptyStyle.Render("  No notifications."))
		b.WriteString("\n")
	} else {
		vis := m.visibleEntries()
		end := m.scrollOff + vis
		if end > len(m.items) {
			end = len(m.items)
		}
		sel := m.list.Current()
		for i := m.scrollOff; i < end; i++ {
			b.WriteString(m.renderEntry(m.items[i], i == sel))
			b.WriteString("\n")
		}
		if m.scrollOff > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more above", m.scrollOff)))
			b.WriteString("\n")
		}
		if end < len(m.items) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more below", len(m.items)-end)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑/↓: navigate | Enter: open | x: dismiss | Esc: close"))

	content := b.String()

	boxWidth := m.width - 4
	if boxWidth < 30 {
		boxWidth = 30
	}
	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth).
		Height(boxHeight).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// renderEntry returns a per-type rendered notification block (3 lines).
func (m NotificationsOverlayModel) renderEntry(n notifications.Notification, selected bool) string {
	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("> ")
	}
	headerStyle := lipgloss.NewStyle().Bold(true)
	if selected {
		headerStyle = headerStyle.Foreground(ColorPrimary)
	} else {
		headerStyle = headerStyle.Foreground(ColorAccent)
	}
	bodyStyle := lipgloss.NewStyle().Foreground(ColorDescText)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	tagStyle := lipgloss.NewStyle().Foreground(ColorHighlight)

	var header, body string
	switch n.Type {
	case notifications.TypeUnreadMessage:
		who := n.UserName
		if who == "" {
			who = n.UserID
		}
		header = fmt.Sprintf("%s %s in %s",
			tagStyle.Render("[message]"),
			headerStyle.Render(who),
			dimStyle.Render(channelLabel(n.ChannelID)))
		body = "  " + bodyStyle.Render(truncate(n.Text, 120))
	case notifications.TypeReaction:
		who := n.UserName
		if who == "" {
			who = n.UserID
		}
		header = fmt.Sprintf("%s %s reacted %s in %s",
			tagStyle.Render("[reaction]"),
			headerStyle.Render(who),
			lipgloss.NewStyle().Bold(true).Render(n.Emoji),
			dimStyle.Render(channelLabel(n.ChannelID)))
		body = "  " + bodyStyle.Render("on: "+truncate(n.TargetMessageTxt, 100))
	case notifications.TypeFriendRequest:
		who := n.UserName
		if who == "" {
			who = n.UserID
		}
		header = fmt.Sprintf("%s %s wants to be friends",
			tagStyle.Render("[friend request]"),
			headerStyle.Render(who))
		body = "  " + dimStyle.Render("Enter to open the request and accept/reject")
	default:
		header = fmt.Sprintf("[%s] %s", n.Type, n.UserName)
		body = "  " + bodyStyle.Render(n.Text)
	}

	when := dimStyle.Render("  " + n.Timestamp.Format("Mon 15:04"))
	return cursor + header + "\n" + body + "\n" + when
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// channelLabel returns a short display label for a channel ID. Friend
// channel IDs are shown as "DM", everything else passes through.
func channelLabel(id string) string {
	if strings.HasPrefix(id, "friend:") {
		return "friend chat"
	}
	return id
}
