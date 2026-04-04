package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// ChannelListModel represents the sidebar channel list.
type ChannelListModel struct {
	channels []types.Channel
	selected int
	unread   map[string]bool
	focused  bool
	width    int
	height   int
}

// NewChannelList creates a new channel list model.
func NewChannelList() ChannelListModel {
	return ChannelListModel{
		unread: make(map[string]bool),
	}
}

// SetChannels stores the channel list.
func (m *ChannelListModel) SetChannels(channels []types.Channel) {
	m.channels = channels
	if m.selected >= len(channels) {
		m.selected = 0
	}
}

// SetSize sets the dimensions of the channel list.
func (m *ChannelListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetFocused sets the focus state.
func (m *ChannelListModel) SetFocused(focused bool) {
	m.focused = focused
}

// MarkUnread marks a channel as having unread messages.
func (m *ChannelListModel) MarkUnread(channelID string) {
	m.unread[channelID] = true
}

// ClearUnread clears the unread marker for a channel.
func (m *ChannelListModel) ClearUnread(channelID string) {
	delete(m.unread, channelID)
}

// SelectedChannel returns the currently highlighted channel or nil.
func (m *ChannelListModel) SelectedChannel() *types.Channel {
	if len(m.channels) == 0 || m.selected < 0 || m.selected >= len(m.channels) {
		return nil
	}
	ch := m.orderedChannels()
	if m.selected >= len(ch) {
		return nil
	}
	return &ch[m.selected]
}

// orderedChannels returns channels grouped: public, private, DMs, groups.
func (m *ChannelListModel) orderedChannels() []types.Channel {
	var pub, priv, dms, groups []types.Channel
	for _, ch := range m.channels {
		switch {
		case ch.IsDM:
			dms = append(dms, ch)
		case ch.IsGroup:
			groups = append(groups, ch)
		case ch.IsPrivate:
			priv = append(priv, ch)
		default:
			pub = append(pub, ch)
		}
	}
	var ordered []types.Channel
	ordered = append(ordered, pub...)
	ordered = append(ordered, priv...)
	ordered = append(ordered, dms...)
	ordered = append(ordered, groups...)
	return ordered
}

// Update handles key events when focused.
func (m ChannelListModel) Update(msg tea.Msg) (ChannelListModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	km := DefaultKeyMap()
	ordered := m.orderedChannels()
	total := len(ordered)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, km.Up):
			if m.selected > 0 {
				m.selected--
			}
		case key.Matches(msg, km.Down):
			if m.selected < total-1 {
				m.selected++
			}
		}
	}

	return m, nil
}

// View renders the channel list.
func (m ChannelListModel) View() string {
	ordered := m.orderedChannels()

	var pub, priv, dms, groups []types.Channel
	for _, ch := range m.channels {
		switch {
		case ch.IsDM:
			dms = append(dms, ch)
		case ch.IsGroup:
			groups = append(groups, ch)
		case ch.IsPrivate:
			priv = append(priv, ch)
		default:
			pub = append(pub, ch)
		}
	}

	var b strings.Builder
	idx := 0

	// Channels section (public)
	if len(pub) > 0 {
		b.WriteString(SectionHeaderStyle.Render("# Channels"))
		b.WriteString("\n")
		for _, ch := range pub {
			b.WriteString(m.renderItem(ch, idx, ordered))
			b.WriteString("\n")
			idx++
		}
	}

	// Private channels
	if len(priv) > 0 {
		b.WriteString(SectionHeaderStyle.Render("🔒 Private"))
		b.WriteString("\n")
		for _, ch := range priv {
			b.WriteString(m.renderItem(ch, idx, ordered))
			b.WriteString("\n")
			idx++
		}
	}

	// DMs
	if len(dms) > 0 {
		b.WriteString(SectionHeaderStyle.Render("@ Direct Messages"))
		b.WriteString("\n")
		for _, ch := range dms {
			b.WriteString(m.renderItem(ch, idx, ordered))
			b.WriteString("\n")
			idx++
		}
	}

	// Groups
	if len(groups) > 0 {
		b.WriteString(SectionHeaderStyle.Render("Group Chats"))
		b.WriteString("\n")
		for _, ch := range groups {
			b.WriteString(m.renderItem(ch, idx, ordered))
			b.WriteString("\n")
			idx++
		}
	}

	content := b.String()

	style := SidebarStyle
	if m.focused {
		style = SidebarActiveStyle
	}

	return style.
		Width(m.width).
		Height(m.height).
		Render(content)
}

// renderItem renders a single channel item.
func (m ChannelListModel) renderItem(ch types.Channel, idx int, _ []types.Channel) string {
	prefix := "  "
	if idx == m.selected {
		prefix = "> "
	}

	name := ch.Name
	if ch.IsDM {
		name = ch.Name
	} else if ch.IsPrivate {
		name = ch.Name
	} else {
		name = fmt.Sprintf("#%s", ch.Name)
	}

	var style lipgloss.Style
	switch {
	case idx == m.selected:
		style = ChannelSelectedStyle
	case m.unread[ch.ID]:
		name = "* " + name
		style = ChannelUnreadStyle
	default:
		style = ChannelItemStyle
	}

	return style.Render(prefix + name)
}
