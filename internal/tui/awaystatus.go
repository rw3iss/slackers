package tui

// Away status panel. Opened via Alt-A (configurable) or from
// the Settings overlay. Provides a toggle for the away flag and
// a message textarea. On save, the model persists the config and
// broadcasts the status to all friends.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AwayStatusClosedMsg is dispatched when the user saves / cancels
// the away panel. The model handler persists the config and
// broadcasts the status change.
type AwayStatusClosedMsg struct {
	Enabled bool
	Message string
	Cleared bool // true → user explicitly cleared the status
}

// AwayStatusModel is the overlay state.
type AwayStatusModel struct {
	enabled bool
	input   textinput.Model
	focus   int // 0 = toggle, 1 = message input, 2 = save, 3 = clear
	width   int
	height  int
}

// NewAwayStatusModel builds the away status panel from the
// current config values.
func NewAwayStatusModel(enabled bool, message string) AwayStatusModel {
	ti := textinput.New()
	ti.Placeholder = "Away message (optional)"
	ti.CharLimit = 120
	ti.Width = 50
	ti.SetValue(message)
	if enabled {
		ti.Focus()
	}
	return AwayStatusModel{
		enabled: enabled,
		input:   ti,
		focus:   0,
	}
}

func (m *AwayStatusModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m AwayStatusModel) Update(msg tea.Msg) (AwayStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// When focused on the message input, forward ALL
		// keystrokes to the textinput first — only Tab and Esc
		// escape the input. This ensures capital letters, space,
		// arrows, etc. all work normally while typing.
		if m.focus == 1 {
			switch msg.String() {
			case "esc":
				return m, nil
			case "tab":
				m.focus = (m.focus + 1) % 4
				m.input.Blur()
				return m, nil
			case "shift+tab":
				m.focus = (m.focus + 3) % 4
				m.input.Blur()
				return m, nil
			case "enter":
				// Enter in the input field → move to Save
				m.focus = 2
				m.input.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}
		// Non-input focus: handle navigation and actions.
		switch msg.String() {
		case "esc":
			return m, nil
		case "tab", "down":
			m.focus = (m.focus + 1) % 4
			if m.focus == 1 {
				m.input.Focus()
			}
			return m, nil
		case "shift+tab", "up":
			m.focus = (m.focus + 3) % 4
			if m.focus == 1 {
				m.input.Focus()
			}
			return m, nil
		case "enter", " ":
			switch m.focus {
			case 0: // toggle
				m.enabled = !m.enabled
				return m, nil
			case 2: // save
				return m, func() tea.Msg {
					return AwayStatusClosedMsg{
						Enabled: m.enabled,
						Message: strings.TrimSpace(m.input.Value()),
					}
				}
			case 3: // clear
				return m, func() tea.Msg {
					return AwayStatusClosedMsg{
						Enabled: false,
						Message: "",
						Cleared: true,
					}
				}
			}
		}
	}
	return m, nil
}

func (m AwayStatusModel) View() string {
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	checkStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var b strings.Builder

	// Toggle row.
	check := "[ ]"
	if m.enabled {
		check = checkStyle.Render("[x]")
	}
	label := "Away"
	if m.focus == 0 {
		label = selStyle.Render("▶ " + label)
	} else {
		label = "  " + label
	}
	b.WriteString("  " + check + " " + label + "\n\n")

	// Message input.
	prefix := "  "
	if m.focus == 1 {
		prefix = selStyle.Render("▶ ")
	}
	b.WriteString(prefix + "Message: " + m.input.View() + "\n\n")

	// Save button.
	saveLabel := "  Save"
	if m.focus == 2 {
		saveLabel = selStyle.Render("▶ Save")
	}
	b.WriteString(saveLabel + "\n")

	// Clear button.
	clearLabel := "  Clear status"
	if m.focus == 3 {
		clearLabel = selStyle.Render("▶ Clear status")
	}
	b.WriteString(clearLabel + "\n")

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Tab: navigate · Enter: toggle/save/clear · Esc: cancel"))

	scaffold := OverlayScaffold{
		Title:       "Away Status",
		Footer:      "",
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 60,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
