package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel represents the text input bar.
type InputModel struct {
	textInput textinput.Model
	focused   bool
	width     int
}

// NewInput creates a new input model.
func NewInput() InputModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message... (Enter to send)"
	ti.Prompt = "> "
	ti.CharLimit = 4000

	return InputModel{
		textInput: ti,
	}
}

// SetSize sets the width of the input.
func (m *InputModel) SetSize(w int) {
	m.width = w
	m.textInput.Width = w - 6 // account for prompt and padding
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

// Reset clears the input.
func (m *InputModel) Reset() {
	m.textInput.Reset()
}

// Update delegates to the text input when focused.
func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if !m.focused {
		return m, nil
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
