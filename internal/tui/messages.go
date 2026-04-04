package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/format"
	"github.com/rw3iss/slackers/internal/types"
)

// MessageViewModel displays messages in a scrollable viewport.
type MessageViewModel struct {
	viewport    viewport.Model
	messages    []types.Message
	users       map[string]string // userID -> displayName
	channelName string
	focused     bool
	autoScroll  bool
	width       int
	height      int
}

// NewMessageView creates a new message view model.
func NewMessageView() MessageViewModel {
	vp := viewport.New(0, 0)
	return MessageViewModel{
		viewport:   vp,
		users:      make(map[string]string),
		autoScroll: true,
	}
}

// SetMessages stores messages and rebuilds viewport content.
func (m *MessageViewModel) SetMessages(msgs []types.Message) {
	m.messages = msgs
	m.rebuildContent()
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// AppendMessage appends a single message, rebuilds content, and auto-scrolls if enabled.
func (m *MessageViewModel) AppendMessage(msg types.Message) {
	m.messages = append(m.messages, msg)
	m.rebuildContent()
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// SetUsers sets the user ID to display name mapping.
func (m *MessageViewModel) SetUsers(users map[string]string) {
	m.users = users
}

// SetChannelName sets the current channel name for the header.
func (m *MessageViewModel) SetChannelName(name string) {
	m.channelName = name
}

// SetSize resizes the viewport.
func (m *MessageViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.rebuildContent()
}

// SetFocused sets the focus state.
func (m *MessageViewModel) SetFocused(focused bool) {
	m.focused = focused
}

// Update delegates to the viewport when focused.
func (m MessageViewModel) Update(msg tea.Msg) (MessageViewModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	var cmd tea.Cmd

	// Track if user scrolled away from bottom
	atBottom := m.viewport.AtBottom()

	m.viewport, cmd = m.viewport.Update(msg)

	// If user scrolled up, disable auto-scroll; if at bottom, re-enable
	if !atBottom && !m.viewport.AtBottom() {
		m.autoScroll = false
	} else if m.viewport.AtBottom() {
		m.autoScroll = true
	}

	return m, cmd
}

// View returns the rendered viewport.
func (m MessageViewModel) View() string {
	header := ""
	if m.channelName != "" {
		header = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(m.channelName) + "\n"
	}

	content := header + m.viewport.View()

	style := MessagePaneStyle
	if m.focused {
		style = MessagePaneActiveStyle
	}

	return style.
		Width(m.width).
		Height(m.height).
		Render(content)
}

// rebuildContent formats all messages into a string for the viewport.
func (m *MessageViewModel) rebuildContent() {
	m.viewport.SetContent(m.renderMessages())
}

// renderMessages formats all messages into a displayable string.
func (m *MessageViewModel) renderMessages() string {
	if len(m.messages) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("  No messages yet.")
	}

	var lines []string
	maxWidth := m.width - 4 // account for padding
	if maxWidth < 20 {
		maxWidth = 20
	}

	for _, msg := range m.messages {
		name := msg.UserName
		if name == "" {
			if dn, ok := m.users[msg.UserID]; ok {
				name = dn
			} else {
				name = msg.UserID
			}
		}

		ts := msg.Timestamp.Format("15:04")
		nameStyle := UserNameStyle.Foreground(UserColor(name))

		headerLine := fmt.Sprintf("%s  %s",
			nameStyle.Render(name),
			TimestampStyle.Render(ts),
		)

		// Format message text using the format package
		text := format.FormatMessage(msg.Text, m.users)

		// Word-wrap the text
		wrapped := wordWrap(text, maxWidth)
		textLines := strings.Split(wrapped, "\n")

		lines = append(lines, headerLine)
		for _, tl := range textLines {
			lines = append(lines, "  "+MessageTextStyle.Render(tl))
		}
		lines = append(lines, "") // blank line between messages
	}

	return strings.Join(lines, "\n")
}

// wordWrap wraps text to the given width.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
			continue
		}

		words := strings.Fields(line)
		currentLen := 0
		first := true
		for _, word := range words {
			if first {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(word)
				currentLen = len(word)
				first = false
			} else if currentLen+1+len(word) > width {
				result.WriteString("\n")
				result.WriteString(word)
				currentLen = len(word)
			} else {
				result.WriteString(" ")
				result.WriteString(word)
				currentLen += 1 + len(word)
			}
		}
	}

	return result.String()
}
