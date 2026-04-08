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
	textarea    textarea.Model
	focused     bool
	width       int
	height      int
	mode        InputMode
	history     []string
	histIdx     int
	draft       string
	maxHist     int
	escSeqPart  bool // true if we just saw alt+O (first half of Shift+Enter)
	escapedOnce bool // true after first escape (next escape clears)

	// friendResolver, if set, is called on every paste with the
	// pasted text. It should return a non-empty replacement string
	// when the paste is recognised as a friend contact card (JSON
	// blob or SLF1./SLF2. hash); the input rewrites the paste in
	// place. Empty return = leave the paste alone.
	friendResolver func(string) string
}

// SetFriendResolver wires a callback used to detect pasted friend
// contact cards and rewrite them to compact [FRIEND:me] / [FRIEND:id]
// markers in the textarea.
func (m *InputModel) SetFriendResolver(fn func(string) string) {
	m.friendResolver = fn
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

// CursorToStart moves the cursor to the very beginning of the input
// (row 0, column 0), regardless of the current line.
func (m *InputModel) CursorToStart() {
	// CursorStart only moves to the start of the current line; walk up to
	// row 0 then jump to column 0 so the cursor lands at the very top-left.
	for m.textarea.Line() > 0 {
		m.textarea.CursorUp()
	}
	m.textarea.CursorStart()
}

// CursorAtStart returns true when the cursor is on the first row at the
// first column.
func (m *InputModel) CursorAtStart() bool {
	return m.textarea.Line() == 0
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

func (m *InputModel) SetHistory(history []string) { m.history = history }
func (m *InputModel) SetMaxHistory(n int) {
	if n < 1 {
		n = 1
	}
	m.maxHist = n
}
func (m *InputModel) History() []string { return m.history }

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

// autoResize lets the underlying textarea grow to fit its full content
// without a hard cap so it never has to scroll internally — bubbles'
// viewport-based scrolling has a stale-content bug. Our View() then clips
// the rendered output to inputMaxHeight rows around the cursor.
//
// m.height is the visible (post-clip) row count used for layout.
func (m *InputModel) autoResize() {
	val := m.textarea.Value()
	contentLines := strings.Count(val, "\n") + 1
	if val == "" {
		contentLines = inputMinHeight
	}
	if contentLines < inputMinHeight {
		contentLines = inputMinHeight
	}
	visible := contentLines
	if visible > inputMaxHeight {
		visible = inputMaxHeight
	}
	m.height = visible
	// Tell bubbles textarea to render every content row — no scrolling.
	m.textarea.SetHeight(contentLines)
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
				if m.mode == InputModeEdit {
					// Edit: Alt+Enter = send.
					text := m.textarea.Value()
					if strings.TrimSpace(text) != "" {
						return m, func() tea.Msg { return InputSendMsg{Text: text} }
					}
					return m, nil
				}
				// Normal: Alt+Enter = new line. Insert manually so the same
				// path is used as Shift+Enter.
				m.textarea.InsertString("\n")
				m.autoResize()
				return m, nil
			}
			if m.mode == InputModeNormal {
				// Normal: Enter = send.
				text := m.textarea.Value()
				if strings.TrimSpace(text) != "" {
					return m, func() tea.Msg { return InputSendMsg{Text: text} }
				}
				return m, nil
			}
			// Edit: plain Enter = new line. Insert via the same explicit
			// path as Shift+Enter to avoid the textarea's KeyEnter handler
			// adding a phantom row.
			m.textarea.InsertString("\n")
			m.autoResize()
			return m, nil
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

	// Detect pasted file paths and wrap them in [FILE:path], plus
	// pasted friend contact cards (JSON / SLF hash) and wrap them in
	// [FRIEND:me] or [FRIEND:<id>].
	newVal := m.textarea.Value()
	if newVal != prevVal {
		diff := extractNewText(prevVal, newVal)
		if diff != "" {
			wrapped := wrapFilePaths(diff)
			if wrapped != diff {
				m.textarea.SetValue(strings.Replace(newVal, diff, wrapped, 1))
				m.autoResize()
			} else if m.friendResolver != nil {
				if marker := m.friendResolver(strings.TrimSpace(diff)); marker != "" {
					m.textarea.SetValue(strings.Replace(newVal, diff, marker, 1))
					// Re-run autoResize so the textarea shrinks
					// back to fit the (much shorter) marker —
					// otherwise it stays expanded to whatever
					// the original multi-line paste needed.
					m.autoResize()
				}
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

	rendered := m.textarea.View()
	lines := strings.Split(rendered, "\n")

	// Defensive: clip to the textarea's own logical line count so any
	// trailing phantom row from bubbles textarea's render padding is
	// dropped before our visible-window math runs.
	expected := m.textarea.LineCount()
	if expected < inputMinHeight {
		expected = inputMinHeight
	}
	if len(lines) > expected {
		lines = lines[:expected]
	}

	visible := m.height
	if visible < inputMinHeight {
		visible = inputMinHeight
	}

	// If the content already fits, just pad to the visible height so the
	// box doesn't shrink mid-frame.
	if len(lines) <= visible {
		for len(lines) < visible {
			lines = append(lines, "")
		}
	} else {
		// Scroll the window so the cursor row is always inside it.
		cursorRow := m.textarea.Line()
		scrollOff := 0
		if cursorRow >= visible {
			scrollOff = cursorRow - visible + 1
		}
		if scrollOff+visible > len(lines) {
			scrollOff = len(lines) - visible
		}
		if scrollOff < 0 {
			scrollOff = 0
		}
		lines = lines[scrollOff : scrollOff+visible]
	}

	return style.
		Width(m.width).
		Render(strings.Join(lines, "\n"))
}
