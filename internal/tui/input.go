package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel represents the text input bar with message history.
type InputModel struct {
	textInput textinput.Model
	focused   bool
	width     int
	history   []string // sent message history (oldest first)
	histIdx   int      // -1 = composing new, 0..len-1 = browsing history
	draft     string   // saves in-progress text when browsing history
	maxHist   int      // max history entries
}

// NewInput creates a new input model.
func NewInput() InputModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message... (Enter to send)"
	ti.Prompt = "> "
	ti.CharLimit = 4000

	return InputModel{
		textInput: ti,
		histIdx:   -1,
		maxHist:   20,
	}
}

// SetSize sets the width of the input.
func (m *InputModel) SetSize(w int) {
	m.width = w
	m.textInput.Width = w - 6
}

// SetFocused sets the focus state and focuses/blurs the text input.
func (m *InputModel) SetFocused(focused bool) {
	m.focused = focused
	if focused {
		m.textInput.Focus()
	} else {
		m.textInput.Blur()
	}
}

// Value returns the current input text.
func (m *InputModel) Value() string {
	return m.textInput.Value()
}

// SetValue sets the input text.
func (m *InputModel) SetValue(s string) {
	m.textInput.SetValue(s)
	m.textInput.CursorEnd()
}

// Reset clears the input and resets history browsing.
func (m *InputModel) Reset() {
	m.textInput.Reset()
	m.histIdx = -1
	m.draft = ""
}

// SetHistory loads persisted history.
func (m *InputModel) SetHistory(history []string) {
	m.history = history
}

// SetMaxHistory sets the max history size.
func (m *InputModel) SetMaxHistory(n int) {
	if n < 1 {
		n = 1
	}
	m.maxHist = n
}

// History returns the current history slice.
func (m *InputModel) History() []string {
	return m.history
}

// PushHistory adds a sent message to history.
func (m *InputModel) PushHistory(msg string) {
	if msg == "" {
		return
	}
	// Don't duplicate the last entry.
	if len(m.history) > 0 && m.history[len(m.history)-1] == msg {
		return
	}
	m.history = append(m.history, msg)
	// Trim to max.
	if len(m.history) > m.maxHist {
		m.history = m.history[len(m.history)-m.maxHist:]
	}
}

// Update delegates to the text input when focused, with history navigation.
func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up":
			if len(m.history) == 0 {
				return m, nil
			}
			if m.histIdx == -1 {
				// Save current draft before browsing.
				m.draft = m.textInput.Value()
				m.histIdx = len(m.history) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.textInput.SetValue(m.history[m.histIdx])
			m.textInput.CursorEnd()
			return m, nil

		case "down":
			if m.histIdx == -1 {
				return m, nil
			}
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.textInput.SetValue(m.history[m.histIdx])
				m.textInput.CursorEnd()
			} else {
				// Back to draft.
				m.histIdx = -1
				m.textInput.SetValue(m.draft)
				m.textInput.CursorEnd()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the input bar.
func (m InputModel) View() string {
	style := InputStyle
	if m.focused {
		style = InputActiveStyle
	}

	return style.
		Width(m.width).
		Render(m.textInput.View())
}
