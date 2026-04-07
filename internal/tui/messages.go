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

// ReplyToMessageMsg is sent when the user starts a reply to a message.
type ReplyToMessageMsg struct {
	MessageID string
	Preview   string
}

// ThreadOpenedMsg signals the user opened a thread view for a message.
type ThreadOpenedMsg struct {
	MessageID string
}

// ToggleReactionMsg is sent when the user wants to toggle a reaction on a message.
type ToggleReactionMsg struct {
	MessageID string
	Emoji     string
}

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

// reactionHit tracks a clickable reaction badge for mouse hit-testing.
type reactionHit struct {
	messageID string
	emoji     string
	line      int // absolute line in viewport content
	startCol  int // visible column where the badge starts
	endCol    int // visible column where the badge ends
}

// MessageViewModel displays messages in a scrollable viewport.
type MessageViewModel struct {
	viewport    viewport.Model
	messages    []types.Message
	users       map[string]string
	channelName string
	secureLabel string
	replyFormat string
	focused     bool
	autoScroll  bool
	width       int
	height      int

	// File selection mode
	selectMode  bool
	selectables []selectableItem
	selectIdx   int

	// React mode — select a message to react to
	reactMode       bool
	reactIdx        int // index into messages
	reactionSelIdx  int // -1 = no reaction selected; otherwise index into reactions of selected message

	// Thread view — viewing a single message + its replies
	threadMode      bool
	threadParent    *types.Message
	threadParentIdx int // original index in main messages
	savedScrollOff  int // viewport YOffset before entering thread, restored on exit

	// Inline collapse — track which message replies are collapsed
	collapsedReplies map[string]bool

	// Line-to-message map for click resolution (rebuilt in renderMessages).
	lineToMsgID    map[int]string // line index → message ID for the message at that line
	replyLineMsgID map[int]string // line index → parent message ID for "X replies" lines
	reactionHits   []reactionHit  // clickable reaction badges with positions

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
		viewport:         vp,
		users:            make(map[string]string),
		autoScroll:       true,
		collapsedReplies: make(map[string]bool),
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

func (m *MessageViewModel) SetReplyFormat(format string) {
	m.replyFormat = format
	m.rebuildContent()
}

func (m *MessageViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Viewport height = pane height - 2 borders - 1 header line.
	vpH := h - 3
	if vpH < 1 {
		vpH = 1
	}
	m.viewport.Width = w - 2 // account for borders
	m.viewport.Height = vpH
	m.rebuildContent()
}

func (m *MessageViewModel) SetFocused(focused bool) {
	m.focused = focused
}

// ReactionAtClick returns the (messageID, emoji) of a reaction badge at the click position, or empty.
func (m *MessageViewModel) ReactionAtClick(x, y int) (string, string) {
	// x is the click X relative to the messages pane (0 = pane left edge).
	// y is the click Y relative to the messages pane (0 = pane top).
	// Account for: pane border (1 col) and the header line (1 row).
	contentX := x - 1 // strip border
	absLine := y - 1 + m.viewport.YOffset
	for _, h := range m.reactionHits {
		if h.line == absLine && contentX >= h.startCol && contentX < h.endCol {
			return h.messageID, h.emoji
		}
	}
	return "", ""
}

// MessageReactions returns the reactions on the message at the given index in the cache.
func (m *MessageViewModel) MessageReactions(messageID string) []types.Reaction {
	for _, msg := range m.messages {
		if msg.MessageID == messageID {
			return msg.Reactions
		}
	}
	return nil
}

// ReplyLineMessageID returns the parent message ID if the line clicked is a "X replies" line.
func (m *MessageViewModel) ReplyLineMessageID(y int) string {
	// Convert viewport y to absolute content line number.
	absLine := y - 1 + m.viewport.YOffset
	if id, ok := m.replyLineMsgID[absLine]; ok {
		return id
	}
	return ""
}

// FileAtClick returns the file at the given Y coordinate in the viewport, or nil.

// MessageAtClick returns the message ID and preview at the given Y coordinate.
// Uses the rendered line→message map for accurate resolution.
func (m *MessageViewModel) MessageAtClick(y int) (string, string) {
	absLine := y - 1 + m.viewport.YOffset
	// Walk back from the clicked line to find the closest preceding header.
	for i := absLine; i >= 0; i-- {
		if id, ok := m.lineToMsgID[i]; ok {
			// Find this message to get the preview.
			for _, msg := range m.messages {
				if msg.MessageID == id {
					preview := msg.Text
					if len(preview) > 40 {
						preview = preview[:40] + "..."
					}
					return id, preview
				}
			}
		}
	}
	return "", ""
}

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

// AddReactionLocal optimistically adds a reaction to a message in the current view.
// Returns true if the message was found and updated.
func (m *MessageViewModel) AddReactionLocal(messageID, emoji, userID string) bool {
	for i := range m.messages {
		if m.messages[i].MessageID != messageID {
			continue
		}
		found := false
		for j, r := range m.messages[i].Reactions {
			if r.Emoji == emoji {
				// Don't double-add the same user.
				for _, uid := range r.UserIDs {
					if uid == userID {
						return true
					}
				}
				m.messages[i].Reactions[j].UserIDs = append(m.messages[i].Reactions[j].UserIDs, userID)
				m.messages[i].Reactions[j].Count++
				found = true
				break
			}
		}
		if !found {
			m.messages[i].Reactions = append(m.messages[i].Reactions, types.Reaction{
				Emoji:   emoji,
				UserIDs: []string{userID},
				Count:   1,
			})
		}
		m.rebuildContent()
		return true
	}
	return false
}

// RemoveReactionLocal removes a user's reaction from a message in the current view.
func (m *MessageViewModel) RemoveReactionLocal(messageID, emoji, userID string) bool {
	for i := range m.messages {
		if m.messages[i].MessageID != messageID {
			continue
		}
		for j, r := range m.messages[i].Reactions {
			if r.Emoji != emoji {
				continue
			}
			for k, uid := range r.UserIDs {
				if uid == userID {
					m.messages[i].Reactions[j].UserIDs = append(r.UserIDs[:k], r.UserIDs[k+1:]...)
					m.messages[i].Reactions[j].Count--
					if m.messages[i].Reactions[j].Count <= 0 {
						m.messages[i].Reactions = append(m.messages[i].Reactions[:j], m.messages[i].Reactions[j+1:]...)
					}
					m.rebuildContent()
					return true
				}
			}
		}
	}
	return false
}

// RemovePendingMatching removes any "pending-" messages that match the given text.
// Used to dedupe optimistic local appends when the real message arrives via socket.
func (m *MessageViewModel) RemovePendingMatching(text string) {
	filtered := m.messages[:0]
	for _, msg := range m.messages {
		if strings.HasPrefix(msg.MessageID, "pending-") && msg.Text == text {
			continue
		}
		filtered = append(filtered, msg)
	}
	m.messages = filtered
	m.rebuildContent()
}

// EnterReactMode enters message selection mode for adding reactions.
func (m *MessageViewModel) EnterReactMode() bool {
	if len(m.messages) > 0 {
		// Exit any other selection modes first.
		m.selectMode = false
		m.reactMode = true
		m.reactIdx = len(m.messages) - 1
		m.reactionSelIdx = -1
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

// InReactMode returns whether select (formerly react) mode is active.
func (m *MessageViewModel) InReactMode() bool {
	return m.reactMode
}

// InSelectMode returns whether file select mode is active.
func (m *MessageViewModel) InSelectMode() bool {
	return m.selectMode
}

// SelectedMessageID returns the MessageID of the currently selected message in react mode.
func (m *MessageViewModel) SelectedMessageID() string {
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(m.messages) {
		return ""
	}
	return m.messages[m.reactIdx].MessageID
}

// scrollToReactCursor scrolls the viewport so the currently selected message
// in react/select mode is visible.
func (m *MessageViewModel) scrollToReactCursor() {
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(m.messages) {
		return
	}
	targetID := m.messages[m.reactIdx].MessageID
	if targetID == "" {
		return
	}
	// Find which line the selected message starts at.
	targetLine := -1
	for line, id := range m.lineToMsgID {
		if id == targetID {
			targetLine = line
			break
		}
	}
	if targetLine < 0 {
		return
	}
	top := m.viewport.YOffset
	bottom := top + m.viewport.Height - 1
	if targetLine < top {
		// Scroll up so the line is at the top of the viewport.
		m.viewport.SetYOffset(targetLine)
	} else if targetLine > bottom-2 {
		// Scroll down so the line is near the bottom (leave 2-line margin).
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 3)
	}
}

// SelectedMessage returns the currently selected message in react mode.
func (m *MessageViewModel) SelectedMessage() *types.Message {
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(m.messages) {
		return nil
	}
	return &m.messages[m.reactIdx]
}

// EnterThreadMode shows only the parent message and its replies.
func (m *MessageViewModel) EnterThreadMode(parentIdx int) bool {
	if parentIdx < 0 || parentIdx >= len(m.messages) {
		return false
	}
	parent := m.messages[parentIdx]
	// Save scroll position before switching views.
	m.savedScrollOff = m.viewport.YOffset
	m.threadMode = true
	m.threadParent = &parent
	m.threadParentIdx = parentIdx
	m.reactMode = false
	m.rebuildContent()
	m.viewport.GotoBottom()
	return true
}

// ExitThreadMode returns to the regular message list view.
func (m *MessageViewModel) ExitThreadMode() {
	m.threadMode = false
	m.threadParent = nil
	m.rebuildContent()
	// Restore scroll position.
	m.viewport.SetYOffset(m.savedScrollOff)
}

// InThreadMode returns whether thread view is active.
func (m *MessageViewModel) InThreadMode() bool {
	return m.threadMode
}

// ThreadParentID returns the ID of the message whose thread is open.
func (m *MessageViewModel) ThreadParentID() string {
	if m.threadParent == nil {
		return ""
	}
	return m.threadParent.MessageID
}

// ToggleReplyCollapse toggles inline reply collapse for a message ID.
func (m *MessageViewModel) ToggleReplyCollapse(msgID string) {
	if m.collapsedReplies == nil {
		m.collapsedReplies = make(map[string]bool)
	}
	m.collapsedReplies[msgID] = !m.collapsedReplies[msgID]
	m.rebuildContent()
}

// EnterFileSelectMode activates file selection if there are files available.
func (m *MessageViewModel) EnterFileSelectMode() bool {
	if len(m.selectables) > 0 {
		// Exit any other selection modes first.
		m.reactMode = false
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
		// Esc exits thread mode.
		if m.threadMode && keyMsg.String() == "esc" {
			m.ExitThreadMode()
			return m, nil
		}
		// Toggle file select mode with 'f'.
		km := DefaultKeyMap()
		if key.Matches(keyMsg, km.ToggleFileSelect) {
			if len(m.selectables) > 0 {
				m.selectMode = !m.selectMode
				if m.selectMode {
					m.reactMode = false // exit react mode if active
					m.selectIdx = len(m.selectables) - 1
				}
				m.rebuildContent()
			}
			return m, nil
		}

		// 's' enters select mode (formerly react mode).
		if !m.selectMode && !m.reactMode && keyMsg.String() == "s" {
			if len(m.messages) > 0 {
				m.EnterReactMode()
			}
			return m, nil
		}

		// Plain Up/Down arrows in chat history auto-enter select mode and navigate.
		// Modifiers (Ctrl, Shift) and PgUp/PgDn fall through to viewport scrolling.
		if !m.selectMode && !m.reactMode && !m.contextMode {
			s := keyMsg.String()
			if s == "up" || s == "down" {
				if m.EnterReactMode() {
					// Don't move on first entry — user sees the last message highlighted.
					m.scrollToReactCursor()
					return m, nil
				}
			}
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
					m.reactionSelIdx = -1
					m.rebuildContent()
					m.scrollToReactCursor()
				}
				return m, nil
			case "down":
				if m.reactIdx < len(m.messages)-1 {
					m.reactIdx++
					m.reactionSelIdx = -1
					m.rebuildContent()
					m.scrollToReactCursor()
				}
				return m, nil
			case "left":
				sel := m.SelectedMessage()
				if sel != nil && len(sel.Reactions) > 0 {
					if m.reactionSelIdx <= 0 {
						m.reactionSelIdx = len(sel.Reactions) - 1
					} else {
						m.reactionSelIdx--
					}
					m.rebuildContent()
				}
				return m, nil
			case "right":
				sel := m.SelectedMessage()
				if sel != nil && len(sel.Reactions) > 0 {
					if m.reactionSelIdx >= len(sel.Reactions)-1 {
						m.reactionSelIdx = 0
					} else {
						m.reactionSelIdx++
					}
					m.rebuildContent()
				}
				return m, nil
			case "enter":
				// If a reaction is selected, toggle it via parent model.
				selR := m.SelectedMessage()
				if selR != nil && m.reactionSelIdx >= 0 && m.reactionSelIdx < len(selR.Reactions) {
					emoji := selR.Reactions[m.reactionSelIdx].Emoji
					msgID := selR.MessageID
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return ToggleReactionMsg{MessageID: msgID, Emoji: emoji}
					}
				}
				// Enter on a message:
				// - inside mode + has replies → enter thread view
				// - otherwise → start reply mode (insert [REPLY:id] in input)
				sel := m.SelectedMessage()
				if sel == nil {
					return m, nil
				}
				msgID := sel.MessageID
				preview := sel.Text
				if len(preview) > 40 {
					preview = preview[:40] + "..."
				}
				if m.replyFormat == "inside" && len(sel.Replies) > 0 {
					m.EnterThreadMode(m.reactIdx)
					return m, func() tea.Msg {
						return ThreadOpenedMsg{MessageID: msgID}
					}
				}
				m.reactMode = false
				m.rebuildContent()
				return m, func() tea.Msg {
					return ReplyToMessageMsg{MessageID: msgID, Preview: preview}
				}
			case "r":
				// 'r' opens emoji picker for reaction.
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
	// In thread mode, refresh the thread parent from current messages.
	if m.threadMode && m.threadParent != nil {
		// Find the latest version of the parent message in m.messages.
		for i := range m.messages {
			if m.messages[i].MessageID == m.threadParent.MessageID {
				p := m.messages[i]
				m.threadParent = &p
				m.threadParentIdx = i
				break
			}
		}
	}

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
	} else if m.threadMode {
		m.viewport.SetContent(m.renderThreadView())
	} else {
		m.viewport.SetContent(m.renderMessages())
	}
}

// renderThreadView renders the thread parent + its replies in a focused view.
func (m *MessageViewModel) renderThreadView() string {
	if m.threadParent == nil {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("── Thread ──"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("(Esc to exit thread, type to reply)"))
	b.WriteString("\n\n")

	// Render parent + replies as a temporary message slice.
	saved := m.messages
	saveReplyFmt := m.replyFormat
	thread := []types.Message{*m.threadParent}
	thread = append(thread, m.threadParent.Replies...)
	m.messages = thread
	m.replyFormat = "" // don't recursively render replies inside thread
	rendered := m.renderMessages()
	m.messages = saved
	m.replyFormat = saveReplyFmt

	b.WriteString(rendered)
	return b.String()
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
	// Reset line maps for click resolution.
	m.lineToMsgID = make(map[int]string)
	m.replyLineMsgID = make(map[int]string)
	m.reactionHits = nil

	var lines []string
	maxWidth := m.width - 4
	if maxWidth < 20 {
		maxWidth = 20
	}

	highlightBg := lipgloss.NewStyle().Background(lipgloss.Color("236"))
	dateSepStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	fileSelectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Background(lipgloss.Color("236"))

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

		// Highlight selected message in select mode.
		if m.reactMode && i == m.reactIdx {
			selectHighlight := lipgloss.NewStyle().Background(lipgloss.Color("237"))
			label := " [Enter: reply  r: react  ←/→: select reaction]"
			if len(msg.Reactions) == 0 {
				label = " [Enter: reply  r: react]"
			}
			headerLine = selectHighlight.Render(headerLine + label)
		}

		text := format.FormatMessage(msg.Text, m.users)
		wrapped := wordWrap(text, maxWidth)
		textLines := strings.Split(wrapped, "\n")

		// Track which line this message starts at.
		if msg.MessageID != "" {
			m.lineToMsgID[len(lines)] = msg.MessageID
		}

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

		// Build reply count + reactions row.
		replyCount := len(msg.Replies)
		if replyCount > 0 || len(msg.Reactions) > 0 {
			replyLabel := ""
			if replyCount > 0 {
				replyStyle := lipgloss.NewStyle().Foreground(ColorAccent).Italic(true)
				replyLabel = replyStyle.Render(fmt.Sprintf("%d %s", replyCount, pluralReplies(replyCount)))
			}
			var reactionParts []string
			var reactionWidths []int
			if len(msg.Reactions) > 0 {
				reactionStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("236")).
					Foreground(lipgloss.Color("252")).
					Padding(0, 1)
				selectedReactionStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("240")).
					Foreground(ColorPrimary).
					Bold(true).
					Padding(0, 1)
				for ri, r := range msg.Reactions {
					emoji := r.Emoji
					if e, ok := emojiLookup[r.Emoji]; ok {
						emoji = e
					}
					var rendered string
					if m.reactMode && i == m.reactIdx && ri == m.reactionSelIdx {
						rendered = selectedReactionStyle.Render(fmt.Sprintf("%s %d", emoji, r.Count))
					} else {
						rendered = reactionStyle.Render(fmt.Sprintf("%s %d", emoji, r.Count))
					}
					reactionParts = append(reactionParts, rendered)
					reactionWidths = append(reactionWidths, lipgloss.Width(rendered))
				}
			}
			reactionsStr := strings.Join(reactionParts, " ")

			// Compute the start column of the reactions block.
			// Layout: "    " (4 spaces) + replyLabel + "  " (2 spaces) + reactions (if both)
			//        OR "    " + reactions (if no reply)
			reactionStartCol := 4
			if replyLabel != "" && reactionsStr != "" {
				reactionStartCol = 4 + lipgloss.Width(replyLabel) + 2
			}

			// Record reaction badge positions for click hit-testing.
			currentLine := len(lines)
			if len(msg.Reactions) > 0 {
				col := reactionStartCol
				for i, w := range reactionWidths {
					m.reactionHits = append(m.reactionHits, reactionHit{
						messageID: msg.MessageID,
						emoji:     msg.Reactions[i].Emoji,
						line:      currentLine,
						startCol:  col,
						endCol:    col + w,
					})
					col += w + 1 // +1 for space separator
				}
			}

			if replyLabel != "" && reactionsStr != "" {
				m.replyLineMsgID[currentLine] = msg.MessageID
				lines = append(lines, "    "+replyLabel+"  "+reactionsStr)
			} else if replyLabel != "" {
				m.replyLineMsgID[currentLine] = msg.MessageID
				lines = append(lines, "    "+replyLabel)
			} else {
				lines = append(lines, "    "+reactionsStr)
			}
		}

		// Inline reply rendering (if enabled).
		if m.replyFormat == "inline" && replyCount > 0 && !m.collapsedReplies[msg.MessageID] {
			replyIndent := "        "
			replyHeaderStyle := lipgloss.NewStyle().Foreground(ColorMuted)
			for _, reply := range msg.Replies {
				rTime := reply.Timestamp.Format("15:04")
				rName := reply.UserName
				if rName == "" {
					rName = reply.UserID
				}
				header := replyHeaderStyle.Render(fmt.Sprintf("↳ %s %s", rName, rTime))
				lines = append(lines, replyIndent+header)
				rText := format.FormatMessage(reply.Text, m.users)
				for _, rLine := range strings.Split(rText, "\n") {
					lines = append(lines, replyIndent+"  "+rLine)
				}
			}
		}

		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func pluralReplies(n int) string {
	if n == 1 {
		return "reply"
	}
	return "replies"
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
