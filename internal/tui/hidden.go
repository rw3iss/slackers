package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// UnhideChannelMsg is sent when the user unhides a channel.
type UnhideChannelMsg struct{ ChannelID string }

// HiddenChannelsModel provides an overlay to view and unhide hidden channels.
type HiddenChannelsModel struct {
	channels []types.Channel
	aliases  map[string]string
	selected int
	width    int
	height   int
}

// NewHiddenChannelsModel creates a new hidden channels overlay.
func NewHiddenChannelsModel(channels []types.Channel, aliases map[string]string) HiddenChannelsModel {
	if aliases == nil {
		aliases = make(map[string]string)
	}
	return HiddenChannelsModel{
		channels: channels,
		aliases:  aliases,
	}
}

// SetSize sets the overlay dimensions.
func (m *HiddenChannelsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the hidden channels overlay.
func (m HiddenChannelsModel) Update(msg tea.Msg) (HiddenChannelsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.channels)-1 {
				m.selected++
			}
		case "enter":
			if len(m.channels) > 0 && m.selected < len(m.channels) {
				ch := m.channels[m.selected]
				// Remove from local list
				m.channels = append(m.channels[:m.selected], m.channels[m.selected+1:]...)
				if m.selected >= len(m.channels) && m.selected > 0 {
					m.selected--
				}
				return m, func() tea.Msg {
					return UnhideChannelMsg{ChannelID: ch.ID}
				}
			}
		}
	}
	return m, nil
}

// View renders the hidden channels overlay.
func (m HiddenChannelsModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Hidden Channels"))
	b.WriteString("\n\n")

	if len(m.channels) == 0 {
		b.WriteString(dimStyle.Render("  No hidden channels"))
	} else {
		for i, ch := range m.channels {
			name := ch.Name
			if alias, ok := m.aliases[ch.ID]; ok && alias != "" {
				name = alias
			}
			if !ch.IsDM && !ch.IsGroup {
				name = "#" + name
			}

			if i == m.selected {
				b.WriteString(ChannelSelectedStyle.Render("> " + name))
			} else {
				b.WriteString(ChannelItemStyle.Render("  " + name))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Enter: unhide | Esc: close"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(50, m.width-4)).
		MaxHeight(m.height - 4)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
