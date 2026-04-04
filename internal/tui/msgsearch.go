package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// MsgSearchSelectMsg is sent when the user selects a search result.
type MsgSearchSelectMsg struct {
	ChannelID string
	Timestamp time.Time
}

// MsgSearchResultsMsg carries search results back to the model.
type MsgSearchResultsMsg struct {
	Results []types.SearchResult
}

// MsgSearchModel provides a message search overlay.
type MsgSearchModel struct {
	input     textinput.Model
	results   []types.SearchResult
	selected  int
	scopeAll  bool // false = current channel, true = all channels
	channelID string
	loading   bool
	noResults bool
	slackSvc  slackpkg.SlackService
	width     int
	height    int
	debounce  time.Time
}

// NewMsgSearchModel creates a new message search overlay.
func NewMsgSearchModel(svc slackpkg.SlackService, currentChannelID string) MsgSearchModel {
	ti := textinput.New()
	ti.Placeholder = "Search messages..."
	ti.Focus()
	ti.CharLimit = 128

	return MsgSearchModel{
		input:     ti,
		channelID: currentChannelID,
		slackSvc:  svc,
		scopeAll:  currentChannelID == "",
	}
}

// SetSize sets the overlay dimensions.
func (m *MsgSearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the message search overlay.
func (m MsgSearchModel) Update(msg tea.Msg) (MsgSearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil
		case "tab":
			m.scopeAll = !m.scopeAll
			m.results = nil
			m.noResults = false
			m.selected = 0
			// Re-search with new scope
			if m.input.Value() != "" {
				m.loading = true
				return m, m.searchCmd()
			}
			return m, nil
		case "enter":
			if len(m.results) > 0 && m.selected < len(m.results) {
				r := m.results[m.selected]
				return m, func() tea.Msg {
					return MsgSearchSelectMsg{
						ChannelID: r.ChannelID,
						Timestamp: r.Message.Timestamp,
					}
				}
			}
			return m, nil
		}

	case MsgSearchResultsMsg:
		m.loading = false
		m.results = msg.Results
		m.noResults = len(msg.Results) == 0 && m.input.Value() != ""
		m.selected = 0
		return m, nil
	}

	prevValue := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Trigger search when input changes (simple debounce: search on every change)
	if m.input.Value() != prevValue {
		query := m.input.Value()
		if query == "" {
			m.results = nil
			m.noResults = false
			m.loading = false
			return m, cmd
		}
		m.loading = true
		return m, tea.Batch(cmd, m.searchCmd())
	}

	return m, cmd
}

func (m *MsgSearchModel) searchCmd() tea.Cmd {
	query := m.input.Value()
	svc := m.slackSvc
	channelID := ""
	if !m.scopeAll {
		channelID = m.channelID
	}

	return func() tea.Msg {
		results, err := svc.SearchMessages(query, channelID, 20)
		if err != nil {
			return MsgSearchResultsMsg{}
		}
		return MsgSearchResultsMsg{Results: results}
	}
}

// View renders the message search overlay.
func (m MsgSearchModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	scopeStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	inactiveScopeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	resultUserStyle := lipgloss.NewStyle().
		Bold(true)

	resultTimeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	resultChannelStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Search Messages"))
	b.WriteString("\n\n")

	// Scope toggle
	currentLabel := "Current Channel"
	allLabel := "All Channels"
	if m.scopeAll {
		b.WriteString("  " + inactiveScopeStyle.Render(currentLabel) + "  " + scopeStyle.Render("["+allLabel+"]"))
	} else {
		b.WriteString("  " + scopeStyle.Render("["+currentLabel+"]") + "  " + inactiveScopeStyle.Render(allLabel))
	}
	b.WriteString("    ")
	b.WriteString(dimStyle.Render("(Tab to toggle)"))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("  Searching..."))
	} else if m.noResults {
		b.WriteString(dimStyle.Render("  No results found."))
	} else if len(m.results) > 0 {
		// Each result takes ~3 lines (header + text + blank). Calculate how many fit.
		linesPerResult := 3
		availableLines := m.height - 14 // header, scope, input, footer
		maxVisible := availableLines / linesPerResult
		if maxVisible < 3 {
			maxVisible = 3
		}
		if maxVisible > len(m.results) {
			maxVisible = len(m.results)
		}

		start := 0
		if m.selected >= maxVisible {
			start = m.selected - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.results) {
			end = len(m.results)
		}

		for i := start; i < end; i++ {
			r := m.results[i]
			cursor := "  "
			nameStyle := resultUserStyle
			if i == m.selected {
				cursor = "> "
				nameStyle = selectedStyle
			}

			text := strings.ReplaceAll(r.Message.Text, "\n", " ")
			maxTextLen := m.width - 16
			if maxTextLen < 20 {
				maxTextLen = 20
			}
			if len(text) > maxTextLen {
				text = text[:maxTextLen-3] + "..."
			}

			timeStr := r.Message.Timestamp.Format("Jan 2 15:04")

			line := fmt.Sprintf("%s%s  %s",
				cursor,
				nameStyle.Render(r.Message.UserName),
				resultTimeStyle.Render(timeStr),
			)
			if m.scopeAll {
				line += "  " + resultChannelStyle.Render("#"+r.ChannelName)
			}
			b.WriteString(line)
			b.WriteString("\n")
			b.WriteString("    " + text)
			b.WriteString("\n\n") // blank line between results
		}
	}

	b.WriteString(dimStyle.Render("  Enter: go to message | Tab: toggle scope | Esc: close"))

	content := b.String()

	// Use nearly the full terminal size.
	boxWidth := m.width - 4
	if boxWidth > m.width {
		boxWidth = m.width
	}
	boxHeight := m.height - 2

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth).
		MaxHeight(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
