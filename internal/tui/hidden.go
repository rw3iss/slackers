package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// UnhideChannelMsg is sent when the user unhides a channel.
type UnhideChannelMsg struct{ ChannelID string }

// HiddenChannelsModel provides an overlay to view and unhide hidden channels.
// Cursor / navigation is managed by a SelectableList value so the model only
// has to own the channel data, filter input, and unhide action.
type HiddenChannelsModel struct {
	channels []types.Channel
	aliases  map[string]string
	filter   textinput.Model
	list     SelectableList // cursor + navigation over the filtered view
	width    int
	height   int
}

// NewHiddenChannelsModel creates a new hidden channels overlay.
func NewHiddenChannelsModel(channels []types.Channel, aliases map[string]string) HiddenChannelsModel {
	if aliases == nil {
		aliases = make(map[string]string)
	}

	// Sort by type: public, private, DM, group.
	sort.SliceStable(channels, func(i, j int) bool {
		return hiddenSortOrder(channels[i]) < hiddenSortOrder(channels[j])
	})

	ti := textinput.New()
	ti.Placeholder = "Filter hidden channels..."
	ti.CharLimit = 64
	ti.Focus()

	return HiddenChannelsModel{
		channels: channels,
		aliases:  aliases,
		filter:   ti,
		list:     SelectableList{WrapAround: false, PageSize: 5},
	}
}

func hiddenSortOrder(ch types.Channel) int {
	switch {
	case !ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 0
	case ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 1
	case ch.IsDM:
		return 2
	default:
		return 3
	}
}

func hiddenSectionHeader(ch types.Channel) string {
	switch {
	case ch.IsDM:
		return "@ Direct Messages"
	case ch.IsGroup:
		return "Group Chats"
	case ch.IsPrivate:
		return "# Private"
	default:
		return "# Channels"
	}
}

// displayName returns the alias-overridden name used for filter
// matching and rendering. Includes the "#" prefix for regular
// channels so a filter like "#gen" matches the rendered form.
func (m HiddenChannelsModel) displayName(ch types.Channel) string {
	name := ch.Name
	if alias, ok := m.aliases[ch.ID]; ok && alias != "" {
		name = alias
	}
	if !ch.IsDM && !ch.IsGroup {
		name = "#" + name
	}
	return name
}

// filtered returns the slice of channels that match the current
// filter query (case-insensitive substring on the display name).
// An empty query returns all channels.
func (m HiddenChannelsModel) filtered() []types.Channel {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.channels
	}
	out := make([]types.Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		if strings.Contains(strings.ToLower(m.displayName(ch)), q) {
			out = append(out, ch)
		}
	}
	return out
}

// SetSize sets the overlay dimensions.
func (m *HiddenChannelsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key and mouse events in the hidden channels overlay.
func (m HiddenChannelsModel) Update(msg tea.Msg) (HiddenChannelsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		list := m.filtered()
		m.list.SetCount(len(list))
		// Standard navigation keys go through the shared primitive.
		if m.list.HandleKey(msg) {
			return m, nil
		}
		switch msg.String() {
		case "enter":
			sel := m.list.Current()
			if sel < 0 || sel >= len(list) {
				return m, nil
			}
			ch := list[sel]
			// Remove the channel from the master list so
			// the next render reflects the unhide.
			for i := range m.channels {
				if m.channels[i].ID == ch.ID {
					m.channels = append(m.channels[:i], m.channels[i+1:]...)
					break
				}
			}
			// Clamp selection against the *new* filtered list.
			m.list.SetCount(len(m.filtered()))
			return m, func() tea.Msg {
				return UnhideChannelMsg{ChannelID: ch.ID}
			}
		}
		// All other keys (including letters, backspace, etc.)
		// go to the filter input. The list's SetCount call
		// above already clamps the selection against the new
		// filtered list after every keystroke.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.list.SetCount(len(m.filtered()))
		return m, cmd
	case tea.MouseMsg:
		m.list.SetCount(len(m.filtered()))
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.list.Navigate(-1)
		case tea.MouseButtonWheelDown:
			m.list.Navigate(1)
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
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	list := m.filtered()
	if len(list) == 0 {
		if strings.TrimSpace(m.filter.Value()) != "" {
			b.WriteString(dimStyle.Render("  No matches"))
		} else {
			b.WriteString(dimStyle.Render("  No hidden channels"))
		}
	} else {
		// Scroll window around selected.
		maxVisible := m.height - 14
		if maxVisible < 5 {
			maxVisible = 5
		}
		if maxVisible > len(list) {
			maxVisible = len(list)
		}
		sel := m.list.Current()
		start := 0
		if sel >= maxVisible {
			start = sel - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(list) {
			end = len(list)
		}

		lastHeader := ""
		for i := start; i < end; i++ {
			ch := list[i]
			header := hiddenSectionHeader(ch)
			if header != lastHeader {
				if lastHeader != "" {
					b.WriteString("\n")
				}
				b.WriteString(SectionHeaderStyle.Render(header))
				b.WriteString("\n")
				lastHeader = header
			}

			name := m.displayName(ch)
			if i == sel {
				b.WriteString(ChannelSelectedStyle.Render("> " + name))
			} else {
				b.WriteString(ChannelItemStyle.Render("  " + name))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Type to filter · ↑/↓ navigate · Enter: unhide · Esc: close"))

	content := b.String()

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(50, m.width-4)).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
