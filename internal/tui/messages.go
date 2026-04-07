package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/format"
	"github.com/rw3iss/slackers/internal/types"
)

// ReactModeSelectMsg is sent when the user selects a message to react to.
type ReactModeSelectMsg struct{ MessageID string }

// emojiLookup maps shortcodes to unicode for reaction rendering.
var emojiLookup map[string]string

func init() {
	emojiLookup = make(map[string]string)
	for _, e := range format.AllEmojis() {
		emojiLookup[e.Code] = e.Emoji
	}
}

// LoadMoreContextMsg requests loading more messages before the current context.
type LoadMoreContextMsg struct {
	ChannelID string
	OldestTS  string
}

// MoreContextLoadedMsg carries additional context messages prepended to the view.
type MoreContextLoadedMsg struct {
	Messages []types.Message
}

// FileDownloadMsg requests downloading a file.
type FileDownloadMsg struct {
	File types.FileInfo
}

// selectableItem tracks a file in the message view that can be selected.
type selectableItem struct {
	file types.FileInfo
}

// MessageViewModel displays messages in a scrollable viewport.
type MessageViewModel struct {
	viewport    viewport.Model
	messages    []types.Message
	users       map[string]string
	channelName string
	secureLabel string
	focused     bool
	autoScroll  bool
	width       int
	height      int

	// File selection mode
	selectMode  bool
	selectables []selectableItem
	selectIdx   int

	// React mode — select a message to react to
	reactMode bool
	reactIdx  int // index into messages

	// Context mode (search result viewing)
	contextMode     bool
	contextMessages []types.Message
	contextTarget   int
	contextChannel  string
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

func (m *MessageViewModel) SetMessages(msgs []types.Message) {
	m.messages = msgs
	m.contextMode = false
	m.rebuildContent()
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// SetMessagesSilent updates messages without resetting scroll position.
// Used for background polling updates.
func (m *MessageViewModel) SetMessagesSilent(msgs []types.Message) {
	wasAtBottom := m.viewport.AtBottom()
	m.messages = msgs
	m.contextMode = false
	m.rebuildContent()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

func (m *MessageViewModel) AppendMessage(msg types.Message) {
	if m.contextMode {
		return
	}
	m.messages = append(m.messages, msg)
	m.rebuildContent()
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// SetContextMessages enters context mode showing messages around a search result.
func (m *MessageViewModel) SetContextMessages(msgs []types.Message, targetIdx int, channelName string) {
	m.contextMode = true
	m.contextMessages = msgs
	m.contextTarget = targetIdx
	m.channelName = channelName
	if len(msgs) > 0 {
		m.contextChannel = msgs[0].ChannelID
	}
	m.rebuildContent()

	// Scroll to the target message. Estimate ~4 lines per message (header + date possible + text + blank).
	approxLine := targetIdx * 4
	m.viewport.SetYOffset(approxLine)
}

// PrependContextMessages adds older messages to the context view.
func (m *MessageViewModel) PrependContextMessages(msgs []types.Message) {
	if !m.contextMode || len(msgs) == 0 {
		return
	}
	m.contextTarget += len(msgs)
	m.contextMessages = append(msgs, m.contextMessages...)
	prevOffset := m.viewport.YOffset
	m.rebuildContent()
	// Adjust scroll to keep the same messages visible (new lines added above).
	newLines := len(msgs) * 4 // approximate
	m.viewport.SetYOffset(prevOffset + newLines)
}

func (m *MessageViewModel) ExitContextMode() {
	if !m.contextMode {
		return
	}
	m.contextMode = false
	m.rebuildContent()
	m.viewport.GotoBottom()
}

func (m *MessageViewModel) InContextMode() bool {
	return m.contextMode
}

// ContextOldestTimestamp returns the timestamp of the oldest message in context, for load-more.
func (m *MessageViewModel) ContextOldestTimestamp() string {
	if !m.contextMode || len(m.contextMessages) == 0 {
		return ""
	}
	ts := m.contextMessages[0].Timestamp
	return fmt.Sprintf("%d.%06d", ts.Unix(), ts.Nanosecond()/1000)
}

// ContextChannelID returns the channel ID of the context view.
func (m *MessageViewModel) ContextChannelID() string {
	return m.contextChannel
}

func (m *MessageViewModel) SetUsers(users map[string]string) {
	m.users = users
}

func (m *MessageViewModel) SetChannelName(name string) {
	m.channelName = name
}

func (m *MessageViewModel) SetSecureLabel(label string) {
	m.secureLabel = label
}

func (m *MessageViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.rebuildContent()
}

func (m *MessageViewModel) SetFocused(focused bool) {
	m.focused = focused
}

// FileAtClick returns the file at the given Y coordinate in the viewport, or nil.
func (m *MessageViewModel) FileAtClick(y int) *types.FileInfo {
	// Get the rendered content and find which line was clicked.
	content := m.viewport.View()
	lines := strings.Split(content, "\n")

	// y is relative to the message pane. Account for border (1) + header line (1) + 1.
	lineIdx := y - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}

	clickedLine := lines[lineIdx]

	// Check if this line contains a [FILE:...] pattern.
	if !strings.Contains(clickedLine, "[FILE:") {
		return nil
	}

	// Match it to a selectable file by name.
	for _, s := range m.selectables {
		if strings.Contains(clickedLine, "[FILE:"+s.file.Name+"]") {
			f := s.file
			return &f
		}
	}
	return nil
}

// ExitSelectMode leaves file selection mode.
func (m *MessageViewModel) ExitSelectMode() {
	if m.selectMode {
		m.selectMode = false
		m.rebuildContent()
	}
}

// EnterReactMode enters message selection mode for adding reactions.
func (m *MessageViewModel) EnterReactMode() bool {
	if len(m.messages) > 0 {
		m.reactMode = true
		m.reactIdx = len(m.messages) - 1
		m.rebuildContent()
		return true
	}
	return false
}

// ExitReactMode exits react mode.
func (m *MessageViewModel) ExitReactMode() {
	if m.reactMode {
		m.reactMode = false
		m.rebuildContent()
	}
}

// InReactMode returns whether react mode is active.
func (m *MessageViewModel) InReactMode() bool {
	return m.reactMode
}

// SelectedMessageID returns the MessageID of the currently selected message in react mode.
func (m *MessageViewModel) SelectedMessageID() string {
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(m.messages) {
		return ""
	}
	return m.messages[m.reactIdx].MessageID
}

// EnterFileSelectMode activates file selection if there are files available.
func (m *MessageViewModel) EnterFileSelectMode() bool {
	if len(m.selectables) > 0 {
		m.selectMode = true
		m.selectIdx = len(m.selectables) - 1
		m.rebuildContent()
		return true
	}
	return false
}

// Update delegates to the viewport when focused.
func (m MessageViewModel) Update(msg tea.Msg) (MessageViewModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Toggle file select mode with 'f'.
		km := DefaultKeyMap()
		if key.Matches(keyMsg, km.ToggleFileSelect) {
			if len(m.selectables) > 0 {
				m.selectMode = !m.selectMode
				if m.selectMode {
					m.selectIdx = len(m.selectables) - 1
				}
				m.rebuildContent()
			}
			return m, nil
		}

		// File selection navigation.
		if m.selectMode {
			switch keyMsg.String() {
			case "up":
				if m.selectIdx > 0 {
					m.selectIdx--
					m.rebuildContent()
				}
				return m, nil
			case "down":
				if m.selectIdx < len(m.selectables)-1 {
					m.selectIdx++
					m.rebuildContent()
				}
				return m, nil
			case "enter":
				if m.selectIdx >= 0 && m.selectIdx < len(m.selectables) {
					f := m.selectables[m.selectIdx].file
					m.selectMode = false
					m.rebuildContent()
					return m, func() tea.Msg {
						return FileDownloadMsg{File: f}
					}
				}
			case "esc":
				m.selectMode = false
				m.rebuildContent()
				return m, nil
			}
		}

		// React mode navigation.
		if m.reactMode {
			switch keyMsg.String() {
			case "up":
				if m.reactIdx > 0 {
					m.reactIdx--
					m.rebuildContent()
				}
				return m, nil
			case "down":
				if m.reactIdx < len(m.messages)-1 {
					m.reactIdx++
					m.rebuildContent()
				}
				return m, nil
			case "enter":
				// Signal react mode selection — model.go opens emoji picker.
				msgID := m.SelectedMessageID()
				m.reactMode = false
				m.rebuildContent()
				return m, func() tea.Msg {
					return ReactModeSelectMsg{MessageID: msgID}
				}
			case "esc":
				m.reactMode = false
				m.rebuildContent()
				return m, nil
			}
		}

		// Load-more in context mode when at the top.
		if m.contextMode {
			if keyMsg.String() == "ctrl+u" || keyMsg.String() == "pgup" {
				if m.viewport.YOffset <= 0 && m.contextChannel != "" {
					oldestTS := m.ContextOldestTimestamp()
					return m, func() tea.Msg {
						return LoadMoreContextMsg{
							ChannelID: m.contextChannel,
							OldestTS:  oldestTS,
						}
					}
				}
			}
		}
	}

	var cmd tea.Cmd
	atBottom := m.viewport.AtBottom()

	m.viewport, cmd = m.viewport.Update(msg)

	if !atBottom && !m.viewport.AtBottom() {
		m.autoScroll = false
	} else if m.viewport.AtBottom() {
		m.autoScroll = true
		if m.contextMode {
			m.contextMode = false
			m.rebuildContent()
			m.viewport.GotoBottom()
		}
	}

	return m, cmd
}

// View returns the rendered viewport with a sticky date header.
func (m MessageViewModel) View() string {
	headerParts := []string{}
	if m.channelName != "" {
		headerParts = append(headerParts, lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(m.channelName))
	}
	if m.secureLabel != "" {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Render(" "+m.secureLabel))
	}

	// Determine the date of the first visible message for the sticky date bar.
	dateStr := m.visibleDate()
	if dateStr != "" {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(ColorMuted).Render("  "+dateStr))
	}

	if m.selectMode {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(ColorHighlight).Render("  [FILE SELECT: ↑↓ navigate | Enter: download | f/Esc: exit]"))
	} else if m.contextMode {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(ColorHighlight).Render("  [Context - PgUp: load more | scroll bottom: exit]"))
	} else if len(m.selectables) > 0 {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(ColorMuted).Render("  [f: select files]"))
	}

	header := strings.Join(headerParts, "") + "\n"

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

// visibleDate estimates the date of messages currently visible at the top of the viewport.
func (m MessageViewModel) visibleDate() string {
	var msgs []types.Message
	if m.contextMode {
		msgs = m.contextMessages
	} else {
		msgs = m.messages
	}
	if len(msgs) == 0 {
		return ""
	}

	// Estimate which message is at the top of the viewport.
	// ~3-4 lines per message.
	approxIdx := m.viewport.YOffset / 4
	if approxIdx < 0 {
		approxIdx = 0
	}
	if approxIdx >= len(msgs) {
		approxIdx = len(msgs) - 1
	}

	ts := msgs[approxIdx].Timestamp
	today := time.Now()
	if ts.Year() == today.Year() && ts.YearDay() == today.YearDay() {
		return "Today"
	}
	yesterday := today.AddDate(0, 0, -1)
	if ts.Year() == yesterday.Year() && ts.YearDay() == yesterday.YearDay() {
		return "Yesterday"
	}
	if ts.Year() == today.Year() {
		return ts.Format("Mon, Jan 2")
	}
	return ts.Format("Mon, Jan 2, 2006")
}

func (m *MessageViewModel) rebuildContent() {
	// Rebuild selectables from current messages.
	m.selectables = nil
	msgs := m.messages
	if m.contextMode {
		msgs = m.contextMessages
	}
	for _, msg := range msgs {
		for _, f := range msg.Files {
			m.selectables = append(m.selectables, selectableItem{file: f})
		}
	}
	if m.selectIdx >= len(m.selectables) {
		m.selectIdx = len(m.selectables) - 1
	}
	if m.selectIdx < 0 {
		m.selectIdx = 0
	}

	if m.contextMode {
		m.viewport.SetContent(m.renderContextMessages())
	} else {
		m.viewport.SetContent(m.renderMessages())
	}
}

func (m *MessageViewModel) renderMessages() string {
	if len(m.messages) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("  No messages yet.")
	}
	return m.renderMessageList(m.messages, -1)
}

func (m *MessageViewModel) renderContextMessages() string {
	if len(m.contextMessages) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("  No context messages.")
	}

	divider := lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", m.width-6))
	loadMore := lipgloss.NewStyle().Foreground(ColorAccent).Render("  ▲ Press PgUp or Ctrl-U at top to load earlier messages")

	var b strings.Builder
	b.WriteString(loadMore + "\n")
	b.WriteString(divider + "\n\n")
	b.WriteString(m.renderMessageList(m.contextMessages, m.contextTarget))
	b.WriteString("\n" + divider + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("  Scroll to bottom to return to live view"))
	b.WriteString("\n\n\n")

	return b.String()
}

func (m *MessageViewModel) renderMessageList(msgs []types.Message, highlightIdx int) string {
	var lines []string
	maxWidth := m.width - 4
	if maxWidth < 20 {
		maxWidth = 20
	}

	highlightBg := lipgloss.NewStyle().Background(lipgloss.Color("236"))
	dateSepStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	fileStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	fileSelectedStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Background(lipgloss.Color("236"))

	var lastDate string
	fileIdx := 0 // tracks which selectable file we're at

	for i, msg := range msgs {
		name := msg.UserName
		if name == "" {
			if dn, ok := m.users[msg.UserID]; ok {
				name = dn
			} else {
				name = msg.UserID
			}
		}

		msgDate := msg.Timestamp.Format("Mon, Jan 2, 2006")
		if msgDate != lastDate {
			if lastDate != "" {
				lines = append(lines, "")
			}
			sep := fmt.Sprintf("── %s ──", msgDate)
			lines = append(lines, dateSepStyle.Render("  "+sep))
			lines = append(lines, "")
			lastDate = msgDate
		}

		ts := msg.Timestamp.Format("Jan 2 15:04")
		nameStyle := UserNameStyle.Foreground(UserColor(name))

		headerLine := fmt.Sprintf("%s  %s",
			nameStyle.Render(name),
			TimestampStyle.Render(ts),
		)

		// Highlight selected message in react mode.
		if m.reactMode && i == m.reactIdx {
			reactHighlight := lipgloss.NewStyle().Background(lipgloss.Color("237"))
			headerLine = reactHighlight.Render(headerLine + " [react: Enter to pick emoji]")
		}

		text := format.FormatMessage(msg.Text, m.users)
		wrapped := wordWrap(text, maxWidth)
		textLines := strings.Split(wrapped, "\n")

		if i == highlightIdx {
			headerLine = highlightBg.Render("► " + headerLine)
			lines = append(lines, headerLine)
			for _, tl := range textLines {
				lines = append(lines, highlightBg.Render("  "+MessageTextStyle.Render(tl)))
			}
		} else {
			lines = append(lines, headerLine)
			for _, tl := range textLines {
				lines = append(lines, "  "+MessageTextStyle.Render(tl))
			}
		}

		// Render file attachments.
		for _, f := range msg.Files {
			isSelected := m.selectMode && fileIdx == m.selectIdx
			sizeStr := formatFileSize(f.Size)
			if isSelected {
				lines = append(lines, fileSelectedStyle.Render(
					fmt.Sprintf("  > [FILE:%s] (%s) — Enter to download", f.Name, sizeStr)))
			} else {
				lines = append(lines, fileStyle.Render(
					fmt.Sprintf("    [FILE:%s] (%s)", f.Name, sizeStr)))
			}
			fileIdx++
		}

		// Render reactions.
		if len(msg.Reactions) > 0 {
			var reactionParts []string
			reactionStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("252")).
				Padding(0, 1)
			for _, r := range msg.Reactions {
				emoji := r.Emoji
				if e, ok := emojiLookup[r.Emoji]; ok {
					emoji = e
				}
				reactionParts = append(reactionParts, reactionStyle.Render(
					fmt.Sprintf("%s %d", emoji, r.Count)))
			}
			lines = append(lines, "    "+strings.Join(reactionParts, " "))
		}

		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

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
