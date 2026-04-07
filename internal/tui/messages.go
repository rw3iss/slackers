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

// DeleteMessageRequestMsg is sent when the user requests to delete a message.
// The model performs authorship + confirmation handling.
type DeleteMessageRequestMsg struct {
	MessageID string
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
	reactionSelIdx  int // -1 = none. 0..len(reactions)-1 = a reaction. len(reactions) = "reply list" virtual element (when parent has replies in inline mode)
	// Inline-mode reply list navigation: when reactionSelIdx == len(reactions),
	// the user is targeting the reply list. replyIdx selects an individual reply.
	replyIdx            int // -1 = none, otherwise index into selected parent's Replies
	replyReactionSelIdx int // -1 = none, otherwise index into selected reply's Reactions

	// Thread view — viewing a single message + its replies
	threadMode      bool
	threadParent    *types.Message
	threadParentIdx int // original index in main messages
	savedScrollOff  int // viewport YOffset before entering thread, restored on exit

	// Inline collapse — track which message replies are collapsed
	collapsedReplies map[string]bool

	// boxFirstMessage tells renderMessageList to wrap the first message in
	// horizontal rules so it stands out (used by the thread view).
	boxFirstMessage bool

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
// x and y are relative to the messages pane (0 = pane left edge / top).
func (m *MessageViewModel) ReactionAtClick(x, y int) (string, string) {
	// Pane layout: top border (1) + header line (1) + viewport content.
	// Left side: border (1) + padding (1) + content.
	contentX := x - 2
	absLine := y - 2 + m.viewport.YOffset
	const xBuffer = 2 // extra forgiving cells on each side
	for _, h := range m.reactionHits {
		if h.line == absLine && contentX >= h.startCol-xBuffer && contentX < h.endCol+xBuffer {
			return h.messageID, h.emoji
		}
	}
	return "", ""
}

// MessageReactions returns the reactions on the message with the given ID.
// Searches both top-level messages and nested replies.
func (m *MessageViewModel) MessageReactions(messageID string) []types.Reaction {
	if msg := m.findMessage(messageID); msg != nil {
		return msg.Reactions
	}
	return nil
}

// MessageByID returns a pointer to the message with the given ID (top-level
// or nested reply), or nil if not found.
func (m *MessageViewModel) MessageByID(messageID string) *types.Message {
	return m.findMessage(messageID)
}

// DeleteMessageLocal removes a message (top-level or nested reply) from the
// in-memory view. Returns true if a message was removed.
func (m *MessageViewModel) DeleteMessageLocal(messageID string) bool {
	for i := range m.messages {
		if m.messages[i].MessageID == messageID {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			// Clamp react cursor.
			if m.reactIdx >= len(m.messages) {
				m.reactIdx = len(m.messages) - 1
			}
			if m.reactIdx < 0 {
				m.reactIdx = 0
			}
			m.rebuildContent()
			return true
		}
		for j := range m.messages[i].Replies {
			if m.messages[i].Replies[j].MessageID == messageID {
				m.messages[i].Replies = append(m.messages[i].Replies[:j], m.messages[i].Replies[j+1:]...)
				m.rebuildContent()
				return true
			}
		}
	}
	return false
}

// findMessage returns a pointer to the message with the given ID, searching
// both top-level messages and their replies. Returns nil if not found.
func (m *MessageViewModel) findMessage(messageID string) *types.Message {
	for i := range m.messages {
		if m.messages[i].MessageID == messageID {
			return &m.messages[i]
		}
		for j := range m.messages[i].Replies {
			if m.messages[i].Replies[j].MessageID == messageID {
				return &m.messages[i].Replies[j]
			}
		}
	}
	return nil
}

// findMessagePreview returns the message preview text for the given ID, or empty.
func (m *MessageViewModel) findMessagePreview(messageID string) string {
	msg := m.findMessage(messageID)
	if msg == nil {
		return ""
	}
	preview := msg.Text
	if len(preview) > 40 {
		preview = preview[:40] + "..."
	}
	return preview
}

// ReplyLineMessageID returns the parent message ID if the line clicked is a "X replies" line.
// Accepts a 1-line buffer above and below the actual line for forgiving hit-testing.
func (m *MessageViewModel) ReplyLineMessageID(y int) string {
	// Convert viewport y to absolute content line number.
	absLine := y - 1 + m.viewport.YOffset
	for _, off := range []int{0, -1, 1} {
		if id, ok := m.replyLineMsgID[absLine+off]; ok {
			return id
		}
	}
	return ""
}

// FileAtClick returns the file at the given Y coordinate in the viewport, or nil.

// MessageAtClick returns the message ID and preview at the given Y coordinate.
// Uses the rendered line→message map for accurate resolution. Walks back from
// the clicked line to find the closest preceding message header (parent or reply).
func (m *MessageViewModel) MessageAtClick(y int) (string, string) {
	absLine := y - 1 + m.viewport.YOffset
	for i := absLine; i >= 0; i-- {
		if id, ok := m.lineToMsgID[i]; ok {
			return id, m.findMessagePreview(id)
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

// addReactionTo mutates the given message to add a reaction from userID.
// Returns false if the user already had this reaction (no-op).
func addReactionTo(msg *types.Message, emoji, userID string) bool {
	for j, r := range msg.Reactions {
		if r.Emoji != emoji {
			continue
		}
		for _, uid := range r.UserIDs {
			if uid == userID {
				return false
			}
		}
		msg.Reactions[j].UserIDs = append(msg.Reactions[j].UserIDs, userID)
		msg.Reactions[j].Count++
		return true
	}
	msg.Reactions = append(msg.Reactions, types.Reaction{
		Emoji:   emoji,
		UserIDs: []string{userID},
		Count:   1,
	})
	return true
}

// removeReactionFrom mutates the given message to remove userID's emoji reaction.
// Returns true if a change was made.
func removeReactionFrom(msg *types.Message, emoji, userID string) bool {
	for j, r := range msg.Reactions {
		if r.Emoji != emoji {
			continue
		}
		for k, uid := range r.UserIDs {
			if uid != userID {
				continue
			}
			msg.Reactions[j].UserIDs = append(r.UserIDs[:k], r.UserIDs[k+1:]...)
			msg.Reactions[j].Count--
			if msg.Reactions[j].Count <= 0 {
				msg.Reactions = append(msg.Reactions[:j], msg.Reactions[j+1:]...)
			}
			return true
		}
	}
	return false
}

// AddReactionLocal optimistically adds a reaction to a message in the current view.
// Searches both top-level messages and nested replies. Returns true on success.
func (m *MessageViewModel) AddReactionLocal(messageID, emoji, userID string) bool {
	wasAtBottom := m.viewport.AtBottom()
	target := m.findMessage(messageID)
	if target == nil {
		return false
	}
	if !addReactionTo(target, emoji, userID) {
		// Already had this reaction; nothing to update.
		return true
	}
	m.rebuildContent()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
	return true
}

// RemoveReactionLocal removes a user's reaction from a message in the current view.
// Searches both top-level messages and nested replies.
func (m *MessageViewModel) RemoveReactionLocal(messageID, emoji, userID string) bool {
	target := m.findMessage(messageID)
	if target == nil {
		return false
	}
	if !removeReactionFrom(target, emoji, userID) {
		return false
	}
	m.rebuildContent()
	return true
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
	view := m.viewMessages()
	if len(view) > 0 {
		// Exit any other selection modes first.
		m.selectMode = false
		m.reactMode = true
		m.reactIdx = len(view) - 1
		m.reactionSelIdx = -1
		m.replyIdx = -1
		m.replyReactionSelIdx = -1
		m.rebuildContent()
		return true
	}
	return false
}

// SelectedReplyMessageID returns the ID of the currently-selected inline reply,
// or empty if none.
func (m *MessageViewModel) SelectedReplyMessageID() string {
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(m.messages) {
		return ""
	}
	parent := &m.messages[m.reactIdx]
	if m.reactionSelIdx != len(parent.Reactions) {
		return ""
	}
	if m.replyIdx < 0 || m.replyIdx >= len(parent.Replies) {
		return ""
	}
	return parent.Replies[m.replyIdx].MessageID
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

// viewMessages returns the message slice currently displayed (thread view → parent+replies,
// context mode → contextMessages, otherwise top-level m.messages).
func (m *MessageViewModel) viewMessages() []types.Message {
	if m.threadMode && m.threadParent != nil {
		out := []types.Message{*m.threadParent}
		out = append(out, m.threadParent.Replies...)
		return out
	}
	if m.contextMode {
		return m.contextMessages
	}
	return m.messages
}

// SelectedMessageID returns the MessageID of the currently selected message in react mode.
func (m *MessageViewModel) SelectedMessageID() string {
	view := m.viewMessages()
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(view) {
		return ""
	}
	return view[m.reactIdx].MessageID
}

// scrollToReactCursor scrolls the viewport so the currently selected message
// in react/select mode is visible.
func (m *MessageViewModel) scrollToReactCursor() {
	view := m.viewMessages()
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(view) {
		return
	}
	targetID := view[m.reactIdx].MessageID
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
// In thread mode this resolves through the displayed thread slice.
func (m *MessageViewModel) SelectedMessage() *types.Message {
	view := m.viewMessages()
	if !m.reactMode || m.reactIdx < 0 || m.reactIdx >= len(view) {
		return nil
	}
	// Resolve back to a pointer in the underlying source (so mutations stick).
	if m.threadMode && m.threadParent != nil {
		if m.reactIdx == 0 {
			// Parent: find by ID in m.messages so writes propagate.
			if p := m.findMessage(m.threadParent.MessageID); p != nil {
				return p
			}
		} else {
			replyIdx := m.reactIdx - 1
			if replyIdx >= 0 && replyIdx < len(m.threadParent.Replies) {
				if r := m.findMessage(m.threadParent.Replies[replyIdx].MessageID); r != nil {
					return r
				}
			}
		}
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
			view := m.viewMessages()
			sel := m.SelectedMessage()
			// Whether the parent has an inline reply list (a virtual sub-element after reactions).
			// Disabled in thread mode since replies are first-class items there.
			hasInlineReplies := !m.threadMode && sel != nil && m.replyFormat == "inline" && len(sel.Replies) > 0 && !m.collapsedReplies[sel.MessageID]
			subElems := 0
			if sel != nil {
				subElems = len(sel.Reactions)
				if hasInlineReplies {
					subElems++ // virtual "reply list" element
				}
			}
			onReplyList := sel != nil && hasInlineReplies && m.reactionSelIdx == len(sel.Reactions)

			switch keyMsg.String() {
			case "up":
				// If reply list is active and a reply is selected, navigate within replies.
				if onReplyList && m.replyIdx > 0 {
					m.replyIdx--
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					return m, nil
				}
				if onReplyList && m.replyIdx == 0 {
					// Move out of reply list back to parent navigation.
					m.replyIdx = -1
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, nil
				}
				if m.reactIdx > 0 {
					m.reactIdx--
					m.reactionSelIdx = -1
					m.replyIdx = -1
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					m.scrollToReactCursor()
				}
				return m, nil
			case "down":
				if onReplyList {
					if m.replyIdx < 0 {
						m.replyIdx = 0
					} else if m.replyIdx < len(sel.Replies)-1 {
						m.replyIdx++
					}
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					return m, nil
				}
				if m.reactIdx < len(view)-1 {
					m.reactIdx++
					m.reactionSelIdx = -1
					m.replyIdx = -1
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					m.scrollToReactCursor()
				}
				return m, nil
			case "left":
				// On a selected reply: cycle its own reactions.
				if onReplyList && m.replyIdx >= 0 && m.replyIdx < len(sel.Replies) {
					reply := sel.Replies[m.replyIdx]
					if len(reply.Reactions) > 0 {
						if m.replyReactionSelIdx <= 0 {
							m.replyReactionSelIdx = len(reply.Reactions) - 1
						} else {
							m.replyReactionSelIdx--
						}
						m.rebuildContent()
					}
					return m, nil
				}
				if sel != nil && subElems > 0 {
					if m.reactionSelIdx <= 0 {
						m.reactionSelIdx = subElems - 1
					} else {
						m.reactionSelIdx--
					}
					if !(hasInlineReplies && m.reactionSelIdx == len(sel.Reactions)) {
						m.replyIdx = -1
						m.replyReactionSelIdx = -1
					}
					m.rebuildContent()
				}
				return m, nil
			case "right":
				if onReplyList && m.replyIdx >= 0 && m.replyIdx < len(sel.Replies) {
					reply := sel.Replies[m.replyIdx]
					if len(reply.Reactions) > 0 {
						if m.replyReactionSelIdx >= len(reply.Reactions)-1 {
							m.replyReactionSelIdx = 0
						} else {
							m.replyReactionSelIdx++
						}
						m.rebuildContent()
					}
					return m, nil
				}
				if sel != nil && subElems > 0 {
					if m.reactionSelIdx >= subElems-1 {
						m.reactionSelIdx = 0
					} else {
						m.reactionSelIdx++
					}
					if !(hasInlineReplies && m.reactionSelIdx == len(sel.Reactions)) {
						m.replyIdx = -1
						m.replyReactionSelIdx = -1
					}
					m.rebuildContent()
				}
				return m, nil
			case "enter", " ":
				// If a reply-reaction is selected, toggle it.
				if onReplyList && m.replyIdx >= 0 && m.replyIdx < len(sel.Replies) {
					reply := sel.Replies[m.replyIdx]
					if m.replyReactionSelIdx >= 0 && m.replyReactionSelIdx < len(reply.Reactions) {
						emoji := reply.Reactions[m.replyReactionSelIdx].Emoji
						msgID := reply.MessageID
						m.reactMode = false
						m.reactionSelIdx = -1
						m.replyIdx = -1
						m.replyReactionSelIdx = -1
						m.rebuildContent()
						return m, func() tea.Msg {
							return ToggleReactionMsg{MessageID: msgID, Emoji: emoji}
						}
					}
					// Enter on a reply with no reaction selected → start a reply to it.
					msgID := reply.MessageID
					preview := reply.Text
					if len(preview) > 40 {
						preview = preview[:40] + "..."
					}
					m.reactMode = false
					m.rebuildContent()
					return m, func() tea.Msg {
						return ReplyToMessageMsg{MessageID: msgID, Preview: preview}
					}
				}
				// Parent reaction selected → toggle it.
				if sel != nil && m.reactionSelIdx >= 0 && m.reactionSelIdx < len(sel.Reactions) {
					emoji := sel.Reactions[m.reactionSelIdx].Emoji
					msgID := sel.MessageID
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return ToggleReactionMsg{MessageID: msgID, Emoji: emoji}
					}
				}
				// Enter on a parent message:
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
				// 'r' opens emoji picker for reaction. Target is the selected reply
				// (if any) or the parent message.
				msgID := m.SelectedReplyMessageID()
				if msgID == "" {
					msgID = m.SelectedMessageID()
				}
				m.reactMode = false
				m.rebuildContent()
				return m, func() tea.Msg {
					return ReactModeSelectMsg{MessageID: msgID}
				}
			case "d", "x":
				// 'd' / 'x' requests deletion of the selected message (parent or reply).
				// The model verifies authorship and prompts for confirmation.
				msgID := m.SelectedReplyMessageID()
				if msgID == "" {
					msgID = m.SelectedMessageID()
				}
				if msgID == "" {
					return m, nil
				}
				return m, func() tea.Msg {
					return DeleteMessageRequestMsg{MessageID: msgID}
				}
			case "esc":
				// Esc unwinds: reply-reaction → reply → reactionSelIdx → exit react mode.
				if onReplyList && m.replyReactionSelIdx >= 0 {
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					return m, nil
				}
				if onReplyList && m.replyIdx >= 0 {
					m.replyIdx = -1
					m.rebuildContent()
					return m, nil
				}
				if m.reactionSelIdx >= 0 {
					m.reactionSelIdx = -1
					m.replyIdx = -1
					m.replyReactionSelIdx = -1
					m.rebuildContent()
					return m, nil
				}
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
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	if m.channelName != "" {
		title := m.channelName
		if m.threadMode {
			title = "[Thread] " + title
		}
		headerParts = append(headerParts, titleStyle.Render(title))
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

	if m.threadMode {
		headerParts = append(headerParts, lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("  (Esc to exit thread, type to reply)"))
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
	// In thread view, always show the parent message's date — the thread is
	// pinned to that message, not to the user's full chat history.
	if m.threadMode && m.threadParent != nil {
		return formatRelativeDate(m.threadParent.Timestamp)
	}
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

	return formatRelativeDate(msgs[approxIdx].Timestamp)
}

func formatRelativeDate(ts time.Time) string {
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
// The thread title and exit hint are now part of the pane header (View()) so
// they don't take up viewport rows here.
func (m *MessageViewModel) renderThreadView() string {
	if m.threadParent == nil {
		return ""
	}

	// Render parent + replies as a temporary message slice.
	saved := m.messages
	saveReplyFmt := m.replyFormat
	saveBox := m.boxFirstMessage
	thread := []types.Message{*m.threadParent}
	thread = append(thread, m.threadParent.Replies...)
	m.messages = thread
	m.replyFormat = ""        // don't recursively render replies inside thread
	m.boxFirstMessage = true  // visually box the parent message
	rendered := m.renderMessages()
	m.messages = saved
	m.replyFormat = saveReplyFmt
	m.boxFirstMessage = saveBox

	return rendered
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
			hasReactions := len(msg.Reactions) > 0
			hasInlineReplies := !m.threadMode && m.replyFormat == "inline" && len(msg.Replies) > 0 && !m.collapsedReplies[msg.MessageID]
			var hint string
			switch {
			case hasReactions && hasInlineReplies:
				hint = " [Enter: reply  r: react  d: delete  ←/→: reactions/replies  ↓: into replies]"
			case hasReactions:
				hint = " [Enter: reply  r: react  d: delete  ←/→: select reaction]"
			case hasInlineReplies:
				hint = " [Enter: reply  r: react  d: delete  →: select reply list  ↓: into replies]"
			default:
				hint = " [Enter: reply  r: react  d: delete]"
			}
			headerLine = selectHighlight.Render(headerLine + hint)
		}

		text := format.FormatMessage(msg.Text, m.users)
		wrapped := wordWrap(text, maxWidth)
		textLines := strings.Split(wrapped, "\n")

		// Top rule for the boxed first message (used by thread view).
		if m.boxFirstMessage && i == 0 {
			ruleStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
			ruleW := maxWidth - 2
			if ruleW < 10 {
				ruleW = 10
			}
			lines = append(lines, ruleStyle.Render("  "+strings.Repeat("─", ruleW)))
		}

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
			reactionStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("252")).
				Padding(0, 1)
			selectedReactionStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("240")).
				Foreground(ColorPrimary).
				Bold(true).
				Padding(0, 1)
			// Highlight when this parent message has its reply list active.
			replyListActive := m.reactMode && i == m.reactIdx && m.reactionSelIdx == len(msg.Reactions) && replyCount > 0
			for ri, reply := range msg.Replies {
				rTime := reply.Timestamp.Format("15:04")
				rName := reply.UserName
				if rName == "" {
					rName = reply.UserID
				}
				// Track this reply's header line for click → reply resolution.
				if reply.MessageID != "" {
					m.lineToMsgID[len(lines)] = reply.MessageID
				}
				header := replyHeaderStyle.Render(fmt.Sprintf("↳ %s %s", rName, rTime))
				if replyListActive && ri == m.replyIdx {
					selectHighlight := lipgloss.NewStyle().Background(lipgloss.Color("237"))
					header = selectHighlight.Render(header + " [r: react  d: delete  Esc: back]")
				}
				lines = append(lines, replyIndent+header)
				rText := format.FormatMessage(reply.Text, m.users)
				for _, rLine := range strings.Split(rText, "\n") {
					lines = append(lines, replyIndent+"  "+rLine)
				}
				// Reactions row for this reply.
				if len(reply.Reactions) > 0 {
					var parts []string
					var widths []int
					for rj, r := range reply.Reactions {
						emoji := r.Emoji
						if e, ok := emojiLookup[r.Emoji]; ok {
							emoji = e
						}
						var rendered string
						if replyListActive && ri == m.replyIdx && rj == m.replyReactionSelIdx {
							rendered = selectedReactionStyle.Render(fmt.Sprintf("%s %d", emoji, r.Count))
						} else {
							rendered = reactionStyle.Render(fmt.Sprintf("%s %d", emoji, r.Count))
						}
						parts = append(parts, rendered)
						widths = append(widths, lipgloss.Width(rendered))
					}
					// reactions sit indented under the reply text body.
					reactionLineCol := len(replyIndent) + 2
					currentLine := len(lines)
					col := reactionLineCol
					for k, w := range widths {
						m.reactionHits = append(m.reactionHits, reactionHit{
							messageID: reply.MessageID,
							emoji:     reply.Reactions[k].Emoji,
							line:      currentLine,
							startCol:  col,
							endCol:    col + w,
						})
						col += w + 1
					}
					lines = append(lines, replyIndent+"  "+strings.Join(parts, " "))
				}
			}
		}

		// Bottom rule for the boxed first message (used by thread view).
		if m.boxFirstMessage && i == 0 {
			ruleStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
			ruleW := maxWidth - 2
			if ruleW < 10 {
				ruleW = 10
			}
			lines = append(lines, ruleStyle.Render("  "+strings.Repeat("─", ruleW)))
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
