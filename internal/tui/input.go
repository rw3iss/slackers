package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	inputMinHeight = 1
	inputMaxHeight = 10
)

// InputMode defines the behavior of Enter vs Alt+Enter.
type InputMode int

const (
	InputModeNormal InputMode = iota
	InputModeEdit
)

// InputSendMsg signals the model that the user wants to send the message.
type InputSendMsg struct{ Text string }

// InputModel represents the multi-line text input bar.
type InputModel struct {
	textarea textarea.Model
	focused  bool
	width    int
	height   int
	mode     InputMode
	history  []string
	histIdx  int
	draft    string
	maxHist  int
}

// NewInput creates a new input model.
func NewInput() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Enter to send)"
	ta.Prompt = "> "
	ta.CharLimit = 0 // no limit
	ta.MaxHeight = 0 // no max height limit on content
	ta.SetHeight(inputMinHeight)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()

	return InputModel{
		textarea: ta,
		height:   inputMinHeight,
		histIdx:  -1,
		maxHist:  20,
	}
}

func (m *InputModel) SetSize(w int) {
	m.width = w
	m.textarea.SetWidth(w - 4)
}

func (m *InputModel) SetFocused(focused bool) {
	m.focused = focused
	if focused {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

func (m *InputModel) Value() string {
	return m.textarea.Value()
}

func (m *InputModel) SetValue(s string) {
	m.textarea.SetValue(s)
	m.autoResize()
}

// InsertAtCursor inserts text at the current cursor position.
func (m *InputModel) InsertAtCursor(s string) {
	m.textarea.InsertString(s)
	m.autoResize()
}

func (m *InputModel) Reset() {
	m.textarea.Reset()
	m.height = inputMinHeight
	m.textarea.SetHeight(inputMinHeight)
	m.histIdx = -1
	m.draft = ""
}

func (m *InputModel) Mode() InputMode {
	return m.mode
}

func (m *InputModel) ToggleMode() {
	if m.mode == InputModeNormal {
		m.mode = InputModeEdit
		m.textarea.Placeholder = "Edit mode (Enter = new line)"
	} else {
		m.mode = InputModeNormal
		m.textarea.Placeholder = "Type a message... (Enter to send)"
	}
}

func (m *InputModel) SetHistory(history []string)  { m.history = history }
func (m *InputModel) SetMaxHistory(n int)           { if n < 1 { n = 1 }; m.maxHist = n }
func (m *InputModel) History() []string             { return m.history }

func (m *InputModel) PushHistory(msg string) {
	if msg == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == msg {
		return
	}
	m.history = append(m.history, msg)
	if len(m.history) > m.maxHist {
		m.history = m.history[len(m.history)-m.maxHist:]
	}
}

// DisplayHeight returns the current height for the input area including borders.
func (m *InputModel) DisplayHeight() int {
	return m.height + 2
}

// autoResize adjusts the visible textarea height based on content lines, capped at max.
func (m *InputModel) autoResize() {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	if lines != m.height {
		m.height = lines
		m.textarea.SetHeight(lines)
	}
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyUp:
			// In edit mode, Up only moves cursor — no history.
			if m.mode == InputModeNormal {
				row := m.textarea.Line()
				if row == 0 && len(m.history) > 0 {
					if m.histIdx == -1 {
						m.draft = m.textarea.Value()
						m.histIdx = len(m.history) - 1
					} else if m.histIdx > 0 {
						m.histIdx--
					}
					m.textarea.SetValue(m.history[m.histIdx])
					m.autoResize()
					return m, nil
				}
			}

		case tea.KeyDown:
			// In edit mode, Down only moves cursor — no history.
			if m.mode == InputModeNormal {
				row := m.textarea.Line()
				totalLines := strings.Count(m.textarea.Value(), "\n")
				if row >= totalLines && m.histIdx != -1 {
					if m.histIdx < len(m.history)-1 {
						m.histIdx++
						m.textarea.SetValue(m.history[m.histIdx])
					} else {
						m.histIdx = -1
						m.textarea.SetValue(m.draft)
					}
					m.autoResize()
					return m, nil
				}
			}

		case tea.KeyCtrlJ:
			// Ctrl+Enter (Ctrl+J) — send in edit mode, newline in normal mode.
			if m.mode == InputModeEdit {
				text := m.textarea.Value()
				if strings.TrimSpace(text) != "" {
					return m, func() tea.Msg { return InputSendMsg{Text: text} }
				}
				return m, nil
			}

		case tea.KeyEnter:
			if keyMsg.Alt {
				// Alt+Enter: alternate action.
				if m.mode == InputModeNormal {
					// Normal: Alt+Enter = new line. Pre-expand.
					newLines := strings.Count(m.textarea.Value(), "\n") + 2
					if newLines > inputMaxHeight {
						newLines = inputMaxHeight
					}
					if newLines > m.height {
						m.height = newLines
						m.textarea.SetHeight(newLines)
					}
				} else {
					// Edit: Alt+Enter = send.
					text := m.textarea.Value()
					if strings.TrimSpace(text) != "" {
						return m, func() tea.Msg { return InputSendMsg{Text: text} }
					}
					return m, nil
				}
			} else {
				// Plain Enter.
				if m.mode == InputModeNormal {
					// Normal: Enter = send.
					text := m.textarea.Value()
					if strings.TrimSpace(text) != "" {
						return m, func() tea.Msg { return InputSendMsg{Text: text} }
					}
					return m, nil
				}
				// Edit: Enter = new line.
				// Pre-expand height so the textarea doesn't scroll when adding the line.
				newLines := strings.Count(m.textarea.Value(), "\n") + 2
				if newLines > inputMaxHeight {
					newLines = inputMaxHeight
				}
				if newLines > m.height {
					m.height = newLines
					m.textarea.SetHeight(newLines)
				}
				// Fall through to textarea to insert the newline.
			}
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.autoResize()
	return m, cmd
}

func (m InputModel) View() string {
	style := InputStyle
	if m.focused {
		style = InputActiveStyle
	}

	// In edit mode, show a subtle indicator instead of a big label.
	if m.mode == InputModeEdit {
		style = style.BorderForeground(ColorHighlight)
	}

	return style.
		Width(m.width).
		Render(m.textarea.View())
}
