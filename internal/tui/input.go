package tui

import (
	"os"
	"path/filepath"
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
	textarea   textarea.Model
	focused    bool
	width      int
	height     int
	mode       InputMode
	history    []string
	histIdx    int
	draft      string
	maxHist    int
	escSeqPart  bool // true if we just saw alt+O (first half of Shift+Enter)
	escapedOnce bool // true after first escape (next escape clears)
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

// CursorToStart moves the cursor to the start of the input.
func (m *InputModel) CursorToStart() {
	m.textarea.CursorStart()
}

// AtStart returns true if escape was already pressed once (ready to clear).
// Used to detect double-escape behavior (first esc → cursor home, second → clear).
func (m *InputModel) AtStart() bool {
	return m.escapedOnce
}

// MarkEscapeOnce sets the flag that the user pressed escape once.
func (m *InputModel) MarkEscapeOnce() {
	m.escapedOnce = true
}

// ClearEscapeOnce clears the flag.
func (m *InputModel) ClearEscapeOnce() {
	m.escapedOnce = false
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

// autoResize adjusts the visible textarea height to fit content, capped at max.
func (m *InputModel) autoResize() {
	val := m.textarea.Value()
	lines := strings.Count(val, "\n") + 1
	if val == "" {
		lines = inputMinHeight
	}
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	m.height = lines
	m.textarea.SetHeight(lines)
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Handle Shift+Enter escape sequence (\eOM).
		// Terminals like Konsole send this as alt+O followed by M.
		str := keyMsg.String()
		if str == "alt+O" {
			m.escSeqPart = true
			return m, nil
		}
		if m.escSeqPart {
			m.escSeqPart = false
			if str == "M" {
				// Shift+Enter detected — insert newline.
				newLines := strings.Count(m.textarea.Value(), "\n") + 2
				if newLines > inputMaxHeight {
					newLines = inputMaxHeight
				}
				if newLines > m.height {
					m.height = newLines
					m.textarea.SetHeight(newLines)
				}
				m.textarea.InsertString("\n")
				m.autoResize()
				return m, nil
			}
			// Not the expected 'M' — process normally.
		}

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

	prevVal := m.textarea.Value()
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.autoResize()

	// Any input action clears the escape-once flag.
	if _, ok := msg.(tea.KeyMsg); ok {
		m.escapedOnce = false
	}

	// Detect pasted file paths and wrap them in [FILE:path].
	newVal := m.textarea.Value()
	if newVal != prevVal {
		diff := extractNewText(prevVal, newVal)
		if diff != "" {
			wrapped := wrapFilePaths(diff)
			if wrapped != diff {
				// Replace the pasted text with the wrapped version.
				m.textarea.SetValue(strings.Replace(newVal, diff, wrapped, 1))
			}
		}
	}

	return m, cmd
}

// extractNewText returns the text that was added between prev and next values.
// Only returns content for paste-like operations (multiple chars added at once).
func extractNewText(prev, next string) string {
	if len(next) <= len(prev)+1 {
		// Single character typed — not a paste.
		return ""
	}
	// Find the inserted portion.
	// Simple heuristic: if next starts with prev, the diff is the suffix.
	if strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	// If next ends with prev suffix, the diff is the prefix.
	if strings.HasSuffix(next, prev) {
		return next[:len(next)-len(prev)]
	}
	// More complex insertion — check if the diff is large enough to be a paste.
	if len(next)-len(prev) > 3 {
		// Try to find the inserted text by common prefix/suffix.
		prefix := 0
		for prefix < len(prev) && prefix < len(next) && prev[prefix] == next[prefix] {
			prefix++
		}
		suffix := 0
		for suffix < len(prev)-prefix && suffix < len(next)-prefix &&
			prev[len(prev)-1-suffix] == next[len(next)-1-suffix] {
			suffix++
		}
		if prefix+suffix <= len(next) {
			return next[prefix : len(next)-suffix]
		}
	}
	return ""
}

// wrapFilePaths finds file paths in text and wraps them with [FILE:path].
func wrapFilePaths(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}

	// Expand ~ to home directory for checking.
	home, _ := os.UserHomeDir()

	// Check each line — a pasted path is usually a single line or one path per line.
	lines := strings.Split(trimmed, "\n")
	changed := false
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Must look like an absolute path or ~/path.
		if !strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "~/") {
			continue
		}

		// Already wrapped?
		if strings.Contains(line, "[FILE:") {
			continue
		}

		// Expand ~ for stat check.
		checkPath := line
		if strings.HasPrefix(checkPath, "~/") {
			checkPath = filepath.Join(home, checkPath[2:])
		}

		// Verify the file exists and is NOT a directory.
		info, err := os.Stat(checkPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}

		// Wrap it.
		lines[i] = "[FILE:" + line + "]"
		changed = true
	}

	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
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
