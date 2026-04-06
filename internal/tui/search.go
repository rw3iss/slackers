package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// SearchSelectMsg is sent when the user selects a channel from search.
type SearchSelectMsg struct{ ChannelID string }

// SearchModel provides a channel search/filter overlay.
type SearchModel struct {
	input    textinput.Model
	all      []types.Channel
	filtered []types.Channel
	aliases  map[string]string
	selected int
	width    int
	height   int
}

// NewSearchModel creates a new search overlay.
func NewSearchModel(channels []types.Channel, aliases map[string]string) SearchModel {
	ti := textinput.New()
	ti.Placeholder = "Search channels..."
	ti.Focus()
	ti.CharLimit = 64

	if aliases == nil {
		aliases = make(map[string]string)
	}

	return SearchModel{
		input:    ti,
		all:      channels,
		filtered: channels,
		aliases:  aliases,
	}
}

// SetSize sets the overlay dimensions.
func (m *SearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *SearchModel) filter() {
	query := strings.ToLower(m.input.Value())
	if query == "" {
		m.filtered = m.all
		m.selected = 0
		return
	}

	var results []types.Channel
	for _, ch := range m.all {
		name := ch.Name
		if alias, ok := m.aliases[ch.ID]; ok && alias != "" {
			name = alias
		}
		if strings.Contains(strings.ToLower(name), query) {
			results = append(results, ch)
		}
	}
	m.filtered = results
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
}

// Update handles key and mouse events in the search overlay.
func (m SearchModel) Update(msg tea.Msg) (SearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			if len(m.filtered) > 0 && m.selected < len(m.filtered) {
				ch := m.filtered[m.selected]
				return m, func() tea.Msg {
					return SearchSelectMsg{ChannelID: ch.ID}
				}
			}
			return m, nil
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter()
	return m, cmd
}

// View renders the search overlay.
func (m SearchModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Search Channels"))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	maxResults := m.height - 12
	if maxResults < 5 {
		maxResults = 5
	}
	if maxResults > len(m.filtered) {
		maxResults = len(m.filtered)
	}

	// Scroll window around selected
	start := 0
	if m.selected >= maxResults {
		start = m.selected - maxResults + 1
	}
	end := start + maxResults
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  No channels found"))
	} else {
		for i := start; i < end; i++ {
			ch := m.filtered[i]
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
	b.WriteString(dimStyle.Render("  Enter: select | Esc: cancel"))

	content := b.String()

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(55, m.width-4)).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
