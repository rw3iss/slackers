package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/format"
	"github.com/rw3iss/slackers/internal/friends"
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

// MessageCopyRequestMsg is sent when the user requests to copy a
// single message to the clipboard. The model resolves the message
// by ID and writes a simple "UserName  Timestamp\nText" block via
// the shared copyToClipboard helper. Only the initial message is
// copied — replies and reactions are intentionally omitted so that
// selecting multiple messages and copying each produces a clean
// contiguous block.
type MessageCopyRequestMsg struct {
	MessageID string
}

// CopySnippetRequestMsg is sent when the user presses `c` in
// message or output select mode while the cursor is on an
// in-message code snippet. The Text field carries the raw
// snippet body (no backticks). The model funnels it through
// copyToClipboard and surfaces a status-bar confirmation.
type CopySnippetRequestMsg struct {
	Text string
}

// EditMessageRequestMsg is sent when the user requests to edit a message.
// The model verifies authorship and pre-fills the input with the message
// text wrapped in [EDIT:id] syntax.
type EditMessageRequestMsg struct {
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

// slackEmojiAliases maps Slack-specific reaction shortcodes that don't match
// the canonical names in our emoji database to a canonical equivalent.
var slackEmojiAliases = map[string]string{
	"+1":               "thumbsup",
	"-1":               "thumbsdown",
	"100":              "100",
	"raised_hands":     "raised_hands",
	"clap":             "clap",
	"tada":             "tada",
	"fire":             "fire",
	"heart":            "heart",
	"smile":            "smile",
	"smiley":           "smiley",
	"joy":              "joy",
	"sob":              "sob",
	"thinking_face":    "thinking",
	"white_check_mark": "white_check_mark",
	"heavy_check_mark": "heavy_check_mark",
	"x":                "x",
}

// resolveEmoji turns a Slack reaction shortcode into a renderable string.
// Handles modifier suffixes (e.g. "pray::skin-tone-5") by stripping the
// "::..." tail and falling back to the base emoji. Also resolves common
// Slack-specific aliases like "+1" → thumbsup → 👍. If still nothing
// matches, wraps the result in colons (":code:") so the user still sees
// a recognizable label.
func resolveEmoji(code string) string {
	if e, ok := emojiLookup[code]; ok {
		return e
	}
	// Strip Slack modifier suffix and try the base.
	base := code
	if i := strings.Index(code, "::"); i >= 0 {
		base = code[:i]
		if e, ok := emojiLookup[base]; ok {
			return e
		}
	}
	// Try the Slack alias map.
	if alias, ok := slackEmojiAliases[base]; ok {
		if e, ok := emojiLookup[alias]; ok {
			return e
		}
	}
	return ":" + code + ":"
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

// CancelUploadRequestMsg is sent when the user wants to cancel the
// upload of a file that's still in flight. The model handles it by
// queuing a confirmation prompt at the status bar.
type CancelUploadRequestMsg struct {
	File types.FileInfo
}

// FriendCardClickedMsg is dispatched when the user clicks (or
// activates) a [FRIEND:...] pill rendered inside a chat message.
// The model decides whether to import, view, or merge based on
// whether the friend already exists in the local store.
type FriendCardClickedMsg struct {
	Card friends.ContactCard
}

// selectableItem tracks a file in the message view that can be selected.
type selectableItem struct {
	file types.FileInfo
}

// friendCardHit tracks a clickable [FRIEND:...] pill rendered inside
// a chat message body. The model uses these for mouse hit-testing.
type friendCardHit struct {
	cardKey  string // key into MessageViewModel.friendCards
	line     int    // absolute line in viewport content
	startCol int
	endCol   int
}

// codeSnippetHit tracks an inline code span (`...`) rendered inside
// a chat message body. Used for select-mode sub-item navigation so
// the user can press right-arrow to cursor onto a snippet and 'c'
// to copy it to the clipboard. Siblings: friendCardHit (cards),
// reactionHit (reactions), files (via selectables).
//
// The raw payload is what gets copied; line / col positions are
// recorded so the renderer can flip the highlighted span to the
// selected style without re-parsing the marker at render time.
type codeSnippetHit struct {
	raw      string // literal text between the backticks
	msgIdx   int    // index of the parent message (matches i in renderMessageList)
	localIdx int    // 0-based snippet index within the parent message
	line     int    // absolute line in viewport content
	startCol int
	endCol   int
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
	viewport          viewport.Model
	messages          []types.Message
	users             map[string]string
	channelName       string
	secureLabel       string
	isFriendCh        bool
	friendDetailsHint string
	replyFormat       string

	// Cached local-user identity, used by ownership-dependent UI
	// (e.g. hiding the "d: delete" hint on messages the local
	// user didn't author). Set once at startup and refreshed
	// whenever the model learns a new id (e.g. after Slack
	// AuthTest completes).
	localSlackUserID string // Slack workspace user id (xUxx...)
	localSlackerID   string // friend-system slacker id
	focused          bool
	autoScroll       bool
	width            int
	height           int

	// File selection mode
	selectMode  bool
	selectables []selectableItem
	selectIdx   int

	// React mode — select a message to react to
	reactMode      bool
	reactIdx       int // index into messages
	reactionSelIdx int // -1 = none. 0..len(reactions)-1 = a reaction. len(reactions) = "reply list" virtual element (when parent has replies in inline mode)
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
	expandedReplies map[string]bool

	// boxFirstMessage tells renderMessageList to wrap the first message in
	// horizontal rules so it stands out (used by the thread view).
	boxFirstMessage bool

	// itemSpacing controls vertical padding between rendered messages.
	// 0 = compact (default), 1 = relaxed (extra blank below each message),
	// 2 = comfortable (extra blank above and below message text + reactions row).
	itemSpacing int

	// Line-to-message map for click resolution (rebuilt in renderMessages).
	lineToMsgID    map[int]string // line index → message ID for the message at that line
	replyLineMsgID map[int]string // line index → parent message ID for "X replies" lines
	reactionHits   []reactionHit  // clickable reaction badges with positions

	// Friend-card pills rendered inside message text. Each is a
	// clickable button that activates an import-or-update prompt.
	friendCardHits []friendCardHit
	// friendCards maps a per-render key (e.g. "fc-3") to the
	// decoded ContactCard so the click handler can resolve back to
	// the full data without re-parsing or carrying the entire blob
	// in the rendered text.
	friendCards map[string]friends.ContactCard
	// codeSnippetHits records every inline `code` span rendered
	// inside chat messages. Used by select mode for sub-item
	// navigation (cursor onto a snippet → 'c' copies it) and
	// by the render pass to apply the highlighted style to the
	// currently selected snippet. Rebuilt per render.
	codeSnippetHits []codeSnippetHit

	// formattedTextCache memoises format.FormatMessage results
	// keyed by message ID. Populated lazily on first render and
	// invalidated whenever the message text or user map changes
	// (SetMessages, SetUsers, AppendMessage, EditMessageLocal).
	// Without this cache every message re-runs Slack mrkdwn
	// parsing (multiple regex passes) on every render cycle.
	formattedTextCache map[string]string

	// Context mode (search result viewing)
	contextMode     bool
	contextMessages []types.Message
	contextTarget   int
	contextChannel  string

	// Per-message render state used by rewriteFriendCards and
	// rewriteCodeSnippets to know which pill / snippet they're
	// currently substituting and whether that substitution is
	// the one selected in react/select mode. Both counters are
	// reset at the top of every parent message in
	// renderMessageList.
	renderingMsgIdx       int
	renderingCardCount    int
	renderingSnippetCount int
}

// NewMessageView creates a new message view model.
func NewMessageView() MessageViewModel {
	vp := viewport.New(0, 0)
	return MessageViewModel{
		viewport:        vp,
		users:           make(map[string]string),
		autoScroll:      true,
		expandedReplies: make(map[string]bool),
	}
}

func (m *MessageViewModel) SetMessages(msgs []types.Message) {
	m.messages = msgs
	m.contextMode = false
	// Full list replacement — drop the formatted-text cache so
	// stale entries don't linger indefinitely across channel
	// switches. Re-populated lazily on the next render.
	m.formattedTextCache = nil
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
	m.formattedTextCache = nil
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
	// One new message — cache only loses correctness for that one
	// entry, so nothing needs to be dropped here.
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

	// Scroll to the target message using the EXACT line index
	// populated by rebuildContent. The previous approximation
	// (targetIdx * 4) ignored wrapped text, reactions, replies,
	// date separators, and item spacing, so the target landed a
	// page or more off from where it should have.
	if targetIdx >= 0 && targetIdx < len(msgs) {
		m.ScrollToMessage(msgs[targetIdx].MessageID)
	}
}

// PrependContextMessages adds older messages to the context view.
func (m *MessageViewModel) PrependContextMessages(msgs []types.Message) {
	if !m.contextMode || len(msgs) == 0 {
		return
	}
	m.contextTarget += len(msgs)
	m.contextMessages = append(msgs, m.contextMessages...)
	m.rebuildContent()
	// Re-anchor to the original target message after the rebuild.
	// The message ID is stable across rebuilds; its line index
	// (from lineToMsgID) is now recomputed for the expanded list,
	// so the viewport stays locked to the user's original anchor.
	if m.contextTarget >= 0 && m.contextTarget < len(m.contextMessages) {
		m.ScrollToMessage(m.contextMessages[m.contextTarget].MessageID)
	}
}

// ScrollToMessage positions the viewport so the message identified
// by messageID is visible, roughly one-quarter from the top of the
// visible area (leaving context above and below).
//
// This is the single source of truth for "scroll to a specific
// message" across every chat mode: normal Slack channel view,
// friend chat view, search-result context view, and the react-cursor
// helper all route through the same line map (lineToMsgID, populated
// by rebuildContent). The map stores the exact line index each
// message starts at, so this helper works correctly regardless of:
//   - wrapped long messages (multi-line body)
//   - reactions rendered under messages
//   - thread reply counts / inline reply expansion
//   - file attachments
//   - date separators between days
//   - the user's item-spacing config (compact / relaxed / comfortable)
//
// Callers should invoke rebuildContent (or a SetMessages variant
// that does it internally) before calling ScrollToMessage so the
// line map reflects current state.
func (m *MessageViewModel) ScrollToMessage(messageID string) {
	if messageID == "" {
		return
	}
	targetLine := -1
	for line, id := range m.lineToMsgID {
		if id == messageID {
			targetLine = line
			break
		}
	}
	if targetLine < 0 {
		return
	}
	// Place the message roughly 1/4 from the top of the viewport
	// so surrounding context is visible above and below without
	// the selected row sitting right at the edge.
	offset := targetLine - m.viewport.Height/4
	if offset < 0 {
		offset = 0
	}
	m.viewport.SetYOffset(offset)
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
	// User display-name changes can affect @mentions inside
	// cached formatted text; invalidate so everything re-parses.
	m.formattedTextCache = nil
}

// formatText returns the user-visible rendered form of a message body,
// memoised by message ID. The cache is invalidated by any code path
// that can change the raw text (SetMessages, SetMessagesSilent,
// AppendMessage, EditMessageLocal) or the user display-name map
// (SetUsers). Reactions / deletes don't affect the text so they
// don't need to touch the cache.
func (m *MessageViewModel) formatText(messageID, raw string) string {
	if m.formattedTextCache == nil {
		m.formattedTextCache = make(map[string]string)
	}
	if messageID != "" {
		if cached, ok := m.formattedTextCache[messageID]; ok {
			return cached
		}
	}
	out := format.FormatMessage(raw, m.users)
	if messageID != "" {
		m.formattedTextCache[messageID] = out
	}
	return out
}

func (m *MessageViewModel) SetChannelName(name string) {
	m.channelName = name
}

func (m *MessageViewModel) SetSecureLabel(label string) {
	m.secureLabel = label
}

// SetIsFriendChannel marks the current channel as a friend channel so the
// header renders the friend-details cog and uses the friend-style indicator.
func (m *MessageViewModel) SetIsFriendChannel(b bool) {
	m.isFriendCh = b
}

// SetLocalIdentity caches the IDs the renderer compares against to
// determine if a given message is authored by the local user. Either
// or both can be empty (the comparison just falls back to the next
// rule). Triggers a re-render so existing select-mode hints reflect
// the new authorship calculation.
func (m *MessageViewModel) SetLocalIdentity(slackUserID, slackerID string) {
	if m.localSlackUserID == slackUserID && m.localSlackerID == slackerID {
		return
	}
	m.localSlackUserID = slackUserID
	m.localSlackerID = slackerID
	m.rebuildContent()
}

// isMyMessage reports whether a message was authored by the local
// user, using the cached identity fields. Locally-sent friend
// messages use UserID=="me"; Slack messages use the workspace id;
// outgoing P2P friend messages may also carry the slacker id.
func (m *MessageViewModel) isMyMessage(msg types.Message) bool {
	return m.IsMyUserID(msg.UserID)
}

// IsMyUserID reports whether uid matches any of the identities the
// local user is known by across both Slack and friend chats:
//   - the literal legacy alias "me"
//   - the Slack workspace user ID (from AuthTest)
//   - the friend-system slacker ID (stable across sessions, works
//     even without Slack auth)
//
// This is the single source of truth for "is this me?" questions
// about reactions, messages, and any other user-keyed state. The
// reaction toggle logic consults this helper so that a reaction
// the user added in a previous session is still recognised as theirs
// on return, regardless of which of the three identity spaces was
// active when the reaction was first stored.
func (m *MessageViewModel) IsMyUserID(uid string) bool {
	if uid == "" {
		return false
	}
	if uid == "me" {
		return true
	}
	if m.localSlackUserID != "" && uid == m.localSlackUserID {
		return true
	}
	if m.localSlackerID != "" && uid == m.localSlackerID {
		return true
	}
	return false
}

// RemoveMyReactionsFromEmoji walks every reaction group on the given
// message matching emoji and removes every entry whose user ID
// matches IsMyUserID. Returns the list of user IDs that were actually
// removed, in the order they were encountered — the caller may need
// to echo these to the persistence layer (friendHistory) or the
// remote backend to keep state consistent.
//
// Empty groups are dropped entirely. This is the operation toggleReaction
// uses for the "unreact" path: it collapses any duplicate groups that
// might exist for the same emoji (from legacy storage bugs or race
// conditions between optimistic local adds and Slack reaction_added
// events) down to a single canonical state.
func (m *MessageViewModel) RemoveMyReactionsFromEmoji(messageID, emoji string) []string {
	target := m.findMessage(messageID)
	if target == nil {
		return nil
	}
	var removed []string
	out := target.Reactions[:0]
	for _, r := range target.Reactions {
		if r.Emoji != emoji {
			out = append(out, r)
			continue
		}
		kept := make([]string, 0, len(r.UserIDs))
		for _, uid := range r.UserIDs {
			if m.IsMyUserID(uid) {
				removed = append(removed, uid)
				continue
			}
			kept = append(kept, uid)
		}
		if len(kept) == 0 {
			// Drop the group entirely so the UI no longer shows a
			// chip for this emoji.
			continue
		}
		r.UserIDs = kept
		r.Count = len(kept)
		out = append(out, r)
	}
	target.Reactions = out
	if len(removed) > 0 {
		m.rebuildContent()
	}
	return removed
}

// SetFriendDetailsHint sets a short string (e.g. "Alt+I") that the
// header renders just left of the secure-status / cog area when on a
// friend chat. Empty disables the hint.
func (m *MessageViewModel) SetFriendDetailsHint(s string) {
	m.friendDetailsHint = s
}

// IsFriendChannel reports whether the message view is showing a friend chat.
func (m MessageViewModel) IsFriendChannel() bool {
	return m.isFriendCh
}

// FriendCogGlyph is the icon rendered in the upper-right of friend chats.
const friendCogGlyph = "⚙\ufe0f"

// FriendCogPaneClickArea returns the (startCol, endCol) range, in pane
// content coordinates (0 = first column inside the border+padding), of
// the friend-details cog in the header line. Returns (0,0) when the cog
// is not currently shown.
func (m MessageViewModel) FriendCogPaneClickArea() (int, int) {
	if !m.isFriendCh {
		return 0, 0
	}
	// Match rebuildContent / header layout: m.width - 5 leaves one
	// column of right-side gutter between content and the border.
	contentW := m.width - 5
	if contentW <= 0 {
		return 0, 0
	}
	cw := lipgloss.Width(friendCogGlyph)
	// Be generous with the click area: extend a couple of columns to
	// the left of the rendered glyph and a few columns to the right
	// (covering right padding + border) so the cog catches clicks
	// regardless of off-by-one differences in emoji width reporting
	// across terminals.
	start := contentW - cw - 2
	end := contentW + 4
	if start < 0 {
		start = 0
	}
	return start, end
}

func (m *MessageViewModel) SetReplyFormat(format string) {
	m.replyFormat = format
	m.rebuildContent()
}

// SetItemSpacing sets the vertical-spacing level for chat messages.
// 0 = compact, 1 = relaxed, 2 = comfortable. Values are clamped to 0..2.
func (m *MessageViewModel) SetItemSpacing(n int) {
	if n < 0 {
		n = 0
	}
	if n > 2 {
		n = 2
	}
	if m.itemSpacing != n {
		m.itemSpacing = n
		m.rebuildContent()
	}
}

func (m *MessageViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Viewport height = pane height - 1 header line - 1 bottom gutter.
	vpH := h - 2
	if vpH < 1 {
		vpH = 1
	}
	m.viewport.Width = w - 2 // account for borders
	m.viewport.Height = vpH
	m.rebuildContent()
}

func (m *MessageViewModel) SetFocused(focused bool) {
	wasFocused := m.focused
	m.focused = focused
	// Leaving the messages pane should drop the user out of any modal sub-modes
	// (react/select, file-select) so they don't get stuck in a hidden state.
	if wasFocused && !focused {
		dirty := false
		if m.reactMode {
			m.reactMode = false
			m.reactionSelIdx = -1
			m.replyIdx = -1
			m.replyReactionSelIdx = -1
			dirty = true
		}
		if m.selectMode {
			m.selectMode = false
			dirty = true
		}
		if dirty {
			m.rebuildContent()
		}
	}
}

// ReactionAtClick returns the (messageID, emoji) of a reaction badge at the click position, or empty.
// x and y are relative to the messages pane (0 = pane left edge / top).
func (m *MessageViewModel) ReactionAtClick(x, y int) (string, string) {
	// Pane layout: top border (1) + header line (1) + viewport content.
	// Left side: border (1) + padding (1) + content.
	contentX := x - 2
	absLine := y - 2 + m.viewport.YOffset
	const xBuffer = 2 // extra forgiving cells on each side
	// Accept clicks on the line above the reaction badge as well, so the
	// "top half" of wide emoji glyphs (which often render slightly above
	// their reported row) still register.
	for _, h := range m.reactionHits {
		matchesY := h.line == absLine || h.line == absLine+1
		if matchesY && contentX >= h.startCol-xBuffer && contentX < h.endCol+xBuffer {
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

// Refresh re-renders the cached viewport content. Call this after a theme
// switch so the cached ANSI codes are rebuilt against the new colors.
func (m *MessageViewModel) Refresh() {
	// Theme change doesn't affect the raw formatted-text output
	// (that's mrkdwn → plain, no theme involvement), so only the
	// ANSI-coloured content built downstream needs a rebuild.
	m.rebuildContent()
}

// EditMessageLocal replaces the text of a message (top-level or nested reply)
// in the in-memory view. Returns true if a matching message was found.
func (m *MessageViewModel) EditMessageLocal(messageID, newText string) bool {
	target := m.findMessage(messageID)
	if target == nil {
		return false
	}
	target.Text = newText
	// Invalidate the cached formatted-text entry for this message
	// so the next render re-runs mrkdwn parsing against the new
	// body.
	if m.formattedTextCache != nil {
		delete(m.formattedTextCache, messageID)
	}
	m.rebuildContent()
	return true
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
// Accepts a forgiving buffer (1 row above, 2 rows below) for hit-testing,
// which also covers the extra blank lines added by message item spacing.
func (m *MessageViewModel) ReplyLineMessageID(y int) string {
	// Convert viewport y to absolute content line number.
	// Pane chrome: top border (1) + header line (1) → subtract 2.
	absLine := y - 2 + m.viewport.YOffset
	for _, off := range []int{0, -1, 1, 2} {
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
	// Same pane chrome as FileAtClick / ReactionAtClick: top border (1)
	// + header line (1) before the viewport content begins.
	absLine := y - 2 + m.viewport.YOffset
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

	// y is relative to the message pane. Subtract border (1) + header line (1)
	// to map from screen Y to viewport content row.
	lineIdx := y - 2
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}

	// Try the clicked line first, then the line below as a fall-back so the
	// user can click slightly above a wide-emoji file row and still hit it.
	for _, off := range []int{0, 1} {
		idx := lineIdx + off
		if idx < 0 || idx >= len(lines) {
			continue
		}
		clickedLine := lines[idx]
		if !strings.Contains(clickedLine, "[FILE:") {
			continue
		}
		for _, s := range m.selectables {
			if strings.Contains(clickedLine, "[FILE:"+s.file.Name+"]") {
				f := s.file
				return &f
			}
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

// MessageIDForFile returns the ID of the message containing the given
// file ID, walking nested replies. Empty if not found.
func (m *MessageViewModel) MessageIDForFile(fileID string) string {
	for i := range m.messages {
		for _, f := range m.messages[i].Files {
			if f.ID == fileID {
				return m.messages[i].MessageID
			}
		}
		for j := range m.messages[i].Replies {
			for _, f := range m.messages[i].Replies[j].Files {
				if f.ID == fileID {
					return m.messages[i].Replies[j].MessageID
				}
			}
		}
	}
	return ""
}

// SelectedFile returns the currently-selected file in file-select mode,
// or nil if none.
func (m *MessageViewModel) SelectedFile() *types.FileInfo {
	if !m.selectMode || m.selectIdx < 0 || m.selectIdx >= len(m.selectables) {
		return nil
	}
	f := m.selectables[m.selectIdx].file
	return &f
}

// SetFileUploaded marks a file (identified by FileInfo.ID) inside a
// specific message as no longer uploading and triggers a re-render.
// Returns true if the file was found and updated.
func (m *MessageViewModel) SetFileUploaded(messageID, fileID string) bool {
	target := m.findMessage(messageID)
	if target == nil {
		return false
	}
	for i := range target.Files {
		if target.Files[i].ID == fileID {
			if !target.Files[i].Uploading {
				return true
			}
			target.Files[i].Uploading = false
			m.rebuildContent()
			return true
		}
	}
	return false
}

// RemoveMessage removes a message (by ID) from the current view
// and re-renders. Used when a file-only message has its last file
// cancelled, leaving an empty ghost entry that should be cleaned
// up rather than displayed as a bare header with no content.
func (m *MessageViewModel) RemoveMessage(messageID string) bool {
	for i := range m.messages {
		if m.messages[i].MessageID == messageID {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			delete(m.formattedTextCache, messageID)
			m.rebuildContent()
			return true
		}
	}
	return false
}

// RemoveFile removes a file (by ID) from a message and re-renders.
// Used when a user cancels an upload before it completes.
func (m *MessageViewModel) RemoveFile(messageID, fileID string) bool {
	target := m.findMessage(messageID)
	if target == nil {
		return false
	}
	for i := range target.Files {
		if target.Files[i].ID == fileID {
			target.Files = append(target.Files[:i], target.Files[i+1:]...)
			m.rebuildContent()
			return true
		}
	}
	return false
}

// inboundFriendMarkerPattern matches [FRIEND:<token>] inside an
// incoming message body. The token can be JSON, an SLF1./SLF2. hash,
// or already a short label (in which case we leave it alone). Used
// by the renderer to swap full hashes for compact pills.
var inboundFriendMarkerPattern = regexp.MustCompile(`\[FRIEND:([^\]]+)\]`)

// collapseFriendMarkers walks the raw message text and collapses any
// decodable [FRIEND:<long-blob>] marker into a short reference token
// like [FRIEND:#0]. The original marker is decoded once and the
// resulting card is cached on the view model under that index, so
// the per-line rewriteFriendCards pass can resolve the placeholder
// without re-parsing.
//
// This MUST run before wordWrap — long JSON markers otherwise wrap
// across multiple lines and the regex (which requires '[FRIEND:' and
// ']' on the same line) silently misses them, leaving the raw blob
// in the chat output.
func (m *MessageViewModel) collapseFriendMarkers(text string) string {
	if !strings.Contains(text, "[FRIEND:") {
		return text
	}
	if m.friendCards == nil {
		m.friendCards = make(map[string]friends.ContactCard)
	}
	out := text
	idx := 0
	for {
		loc := inboundFriendMarkerPattern.FindStringIndex(out[idx:])
		if loc == nil {
			break
		}
		start := idx + loc[0]
		end := idx + loc[1]
		raw := out[start:end]
		inner := raw[len("[FRIEND:") : len(raw)-1]
		// Skip already-collapsed placeholders so this is idempotent
		// across re-renders.
		if strings.HasPrefix(inner, "#") {
			idx = end
			continue
		}
		card, err := friends.ParseAnyContactCard(inner)
		if err != nil {
			idx = end
			continue
		}
		key := fmt.Sprintf("fc-%d", len(m.friendCards))
		m.friendCards[key] = card
		placeholder := "[FRIEND:#" + key + "]"
		out = out[:start] + placeholder + out[end:]
		idx = start + len(placeholder)
	}
	return out
}

// rewriteFriendCards walks the given line, finds any [FRIEND:<blob>]
// markers whose payload decodes to a ContactCard, replaces them with
// a styled compact pill (`[FRIEND:<name-or-email-or-id>]`), and
// records the resulting click hit area. Each successful decode adds
// an entry to m.friendCards keyed by a render-local id; the click
// handler uses that id to resolve back to the full card.
//
// Returns the rewritten line. Lines without a friend marker (or with
// only undecodable markers) are returned untouched.
func (m *MessageViewModel) rewriteFriendCards(line string, lineIdx int) string {
	if !strings.Contains(line, "[FRIEND:") {
		return line
	}
	pillStyle := FriendCardPillStyle
	pillSelectedStyle := FriendCardPillSelectedStyle
	if m.friendCards == nil {
		m.friendCards = make(map[string]friends.ContactCard)
	}
	// Determine whether the message currently being rendered owns
	// the cursor selection in react/select mode, and if so which
	// card index is highlighted. The renderingCardCount is
	// incremented per substituted pill in this message — see the
	// reset in renderMessageList at the top of every parent.
	selKind, selIdx := m.selectedItemKind()
	cardSelectedInMsg := m.reactMode &&
		m.renderingMsgIdx == m.reactIdx &&
		selKind == ItemCard
	out := line
	idx := 0
	for {
		match := inboundFriendMarkerPattern.FindStringIndex(out[idx:])
		if match == nil {
			break
		}
		matchStart := idx + match[0]
		matchEnd := idx + match[1]
		raw := out[matchStart:matchEnd]
		inner := raw[len("[FRIEND:") : len(raw)-1]
		var card friends.ContactCard
		var key string
		if strings.HasPrefix(inner, "#") {
			// Placeholder reference produced by
			// collapseFriendMarkers earlier in the render.
			key = strings.TrimPrefix(inner, "#")
			cached, ok := m.friendCards[key]
			if !ok {
				idx = matchEnd
				continue
			}
			card = cached
		} else {
			parsed, err := friends.ParseAnyContactCard(inner)
			if err != nil {
				// Not a decodable hash/json, leave the marker
				// alone and skip past it.
				idx = matchEnd
				continue
			}
			card = parsed
			// Cache the decoded card. The full marker stays in
			// the underlying message text on disk; the pill is
			// just a render-time substitution.
			key = fmt.Sprintf("fc-%d", len(m.friendCards))
			m.friendCards[key] = card
		}
		label := friendCardDisplayName(card)
		pillText := "👤 Friend: " + label
		// Pick the selected vs idle pill style based on whether
		// the current substitution corresponds to the cursor in
		// react/select mode. The per-message render counter
		// (renderingCardCount) is incremented for every pill
		// emitted from the same parent message, regardless of
		// which body line they sit on.
		style := pillStyle
		if cardSelectedInMsg && selIdx == m.renderingCardCount {
			style = pillSelectedStyle
		}
		m.renderingCardCount++
		pillRendered := style.Render(pillText)
		// Compute the click hit area for this pill in the *rewritten* line.
		// We need to know what column the pill ends up at after the
		// substitution. Walk the visible width of out[:matchStart].
		preWidth := lipgloss.Width(out[:matchStart])
		pillWidth := lipgloss.Width(pillRendered)
		m.friendCardHits = append(m.friendCardHits, friendCardHit{
			cardKey:  key,
			line:     lineIdx,
			startCol: preWidth,
			endCol:   preWidth + pillWidth,
		})
		out = out[:matchStart] + pillRendered + out[matchEnd:]
		// Continue scanning after the inserted pill.
		idx = matchStart + len(pillRendered)
	}
	return out
}

// rewriteCodeSnippets walks the given rendered body line, finds
// every inline `...` code span, replaces each with a styled
// italic span, and records the hit in codeSnippetHits so select
// mode can sub-cursor onto it.
//
// This is called per-line from renderMessageList after
// rewriteFriendCards, so it only sees body lines (not headers,
// not reactions, not file rows). The per-message hit counter
// (renderingSnippetCount, reset at the top of each parent
// message in renderMessageList) is used to match each
// substitution against the select-mode cursor's localIdx.
//
// Fenced ```...``` blocks are deliberately NOT rewritten here —
// they're multi-line and the chat render path wraps one body line
// at a time, so a fenced block wouldn't survive the wrap-and-
// rewrite pipeline. Fenced block support in chat is a follow-up;
// the Output view already handles them via its own parser.
func (m *MessageViewModel) rewriteCodeSnippets(line string, lineIdx int) string {
	if !strings.Contains(line, "`") {
		return line
	}
	idleStyle := CodeSnippetStyle
	selStyle := CodeSnippetSelectedStyle
	selKind, selIdx := m.selectedItemKind()
	snippetSelectedInMsg := m.reactMode &&
		m.renderingMsgIdx == m.reactIdx &&
		selKind == ItemCodeSnippet

	out := line
	idx := 0
	for {
		match := inlineCodePat.FindStringIndex(out[idx:])
		if match == nil {
			break
		}
		matchStart := idx + match[0]
		matchEnd := idx + match[1]
		raw := out[matchStart+1 : matchEnd-1] // strip backticks
		style := idleStyle
		if snippetSelectedInMsg && selIdx == m.renderingSnippetCount {
			style = selStyle
		}
		rendered := style.Render(raw)
		preWidth := lipgloss.Width(out[:matchStart])
		renderedWidth := lipgloss.Width(rendered)
		m.codeSnippetHits = append(m.codeSnippetHits, codeSnippetHit{
			raw:      raw,
			msgIdx:   m.renderingMsgIdx,
			localIdx: m.renderingSnippetCount,
			line:     lineIdx,
			startCol: preWidth,
			endCol:   preWidth + renderedWidth,
		})
		m.renderingSnippetCount++
		out = out[:matchStart] + rendered + out[matchEnd:]
		idx = matchStart + len(rendered)
	}
	return out
}

// friendCardDisplayName returns the best short label for a friend
// card pill. Order of preference:
//  1. Real Name from the card (e.g. "Ryan Weiss")
//  2. Email (e.g. "ryan@example.com")
//  3. ShortPeerID derived from the multiaddr (e.g. "WFKy7Pmh")
//  4. "(unknown)" only if literally nothing is available.
//
// This is purely a display helper — the underlying message text
// retains the full [FRIEND:...] marker so the original card data
// is always recoverable on click.
func friendCardDisplayName(card friends.ContactCard) string {
	if s := strings.TrimSpace(card.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(card.Email); s != "" {
		return s
	}
	if s := friends.ShortPeerID(card); s != "" {
		return s
	}
	return "(unknown)"
}

// FriendCardAtClick returns the cached friend card whose pill was
// clicked at (paneX, paneY) in the messages pane. The coordinates
// match the same scheme as ReactionAtClick — pane-relative X / Y
// after the caller has stripped the sidebar offset.
//
// The hit-test is intentionally generous: ±3 cells horizontally and
// ±1 row vertically. The leading 👤 emoji is a wide glyph and
// different terminals report its bounding box differently — Konsole
// puts it on the line above its actual cell, others split it across
// the boundary. Better to over-match here than to fall through to
// the parent message menu (which is what was happening on right
// click before).
func (m *MessageViewModel) FriendCardAtClick(x, y int) *friends.ContactCard {
	contentX := x - 2
	absLine := y - 2 + m.viewport.YOffset
	const xBuffer = 3
	for _, h := range m.friendCardHits {
		dy := h.line - absLine
		if dy < -1 || dy > 1 {
			continue
		}
		if contentX >= h.startCol-xBuffer && contentX < h.endCol+xBuffer {
			debug.Log("[friend-pill] hit: click=(%d,%d) absLine=%d hit.line=%d cols=[%d,%d) → %s",
				x, y, absLine, h.line, h.startCol, h.endCol, h.cardKey)
			if card, ok := m.friendCards[h.cardKey]; ok {
				return &card
			}
		}
	}
	debug.Log("[friend-pill] miss: click=(%d,%d) contentX=%d absLine=%d hits=%d",
		x, y, contentX, absLine, len(m.friendCardHits))
	for i, h := range m.friendCardHits {
		debug.Log("[friend-pill]   hit[%d]: line=%d cols=[%d,%d) key=%s",
			i, h.line, h.startCol, h.endCol, h.cardKey)
	}
	return nil
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

// SelectedItemKind classifies the intra-message item currently
// highlighted in select/react mode. The integer index inside the
// message is returned alongside via SelectedItemKindAndIdx().
//
// The walk order matches the priority the select-mode left/right
// arrow navigation uses:
//
//	cards → files → code snippets → reactions → reply list
//
// with sub-items grouped by kind (all cards, then all files,
// then all snippets, etc.). Code snippets sit between the
// content-level items (cards/files) and the feedback-level items
// (reactions/reply list) because they're sub-items of the
// message body text itself.
type SelectedItemKind int

const (
	ItemNone SelectedItemKind = iota
	ItemCard
	ItemFile
	ItemCodeSnippet
	ItemReaction
	ItemReplyList
)

// messageContactCards parses every decodable [FRIEND:<blob>] marker
// in the given message text and returns the resulting cards in
// document order. Placeholder references ([FRIEND:#fc-N]) are
// skipped — they only exist after a render-time collapse pass.
//
// Used by select-mode navigation to enumerate the selectable
// contact cards inside a parent message without depending on the
// renderer's transient friendCardHits map.
func messageContactCards(text string) []friends.ContactCard {
	if !strings.Contains(text, "[FRIEND:") {
		return nil
	}
	matches := inboundFriendMarkerPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]friends.ContactCard, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		inner := match[1]
		if strings.HasPrefix(inner, "#") {
			continue
		}
		if c, err := friends.ParseAnyContactCard(inner); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// itemCounts returns the number of selectable items per category
// for the given parent message and whether the inline reply list
// virtual element should be present at the end.
func (m *MessageViewModel) itemCounts(msg *types.Message) (cards, files, snippets, reactions int, hasReplyList bool) {
	if msg == nil {
		return 0, 0, 0, 0, false
	}
	cards = len(messageContactCards(msg.Text))
	files = len(msg.Files)
	snippets = len(parseCodeSnippets(msg.Text))
	reactions = len(msg.Reactions)
	hasReplyList = !m.threadMode && m.replyFormat == "inline" &&
		len(msg.Replies) > 0 && m.expandedReplies[msg.MessageID]
	return
}

// totalSubElems returns the total number of selectable items inside
// a parent message. This is the upper bound for left/right cycling
// through reactionSelIdx in select mode.
func (m *MessageViewModel) totalSubElems(msg *types.Message) int {
	cards, files, snippets, reactions, hasReplyList := m.itemCounts(msg)
	n := cards + files + snippets + reactions
	if hasReplyList {
		n++
	}
	return n
}

// selectedItemKind interprets the current reactionSelIdx against the
// selected parent message and returns which kind of item is
// highlighted plus the local index inside that category. Returns
// (ItemNone, -1) when nothing is selected.
//
// The reactionSelIdx field name is kept for backward compatibility
// with the older reaction-only navigation; in the new model it
// indexes into a combined list:
//
//	[ cards | files | snippets | reactions | replyList? ]
//
// so a value of e.g. cards + 1 means "the second file".
func (m *MessageViewModel) selectedItemKind() (SelectedItemKind, int) {
	sel := m.SelectedMessage()
	if sel == nil || m.reactionSelIdx < 0 {
		return ItemNone, -1
	}
	idx := m.reactionSelIdx
	cards, files, snippets, reactions, hasReplyList := m.itemCounts(sel)
	if idx < cards {
		return ItemCard, idx
	}
	idx -= cards
	if idx < files {
		return ItemFile, idx
	}
	idx -= files
	if idx < snippets {
		return ItemCodeSnippet, idx
	}
	idx -= snippets
	if idx < reactions {
		return ItemReaction, idx
	}
	idx -= reactions
	if hasReplyList && idx == 0 {
		return ItemReplyList, 0
	}
	return ItemNone, -1
}

// SelectedContactCard returns the contact card currently highlighted
// in select mode, or nil if the selection isn't on a card.
func (m *MessageViewModel) SelectedContactCard() *friends.ContactCard {
	sel := m.SelectedMessage()
	if sel == nil {
		return nil
	}
	kind, idx := m.selectedItemKind()
	if kind != ItemCard {
		return nil
	}
	cards := messageContactCards(sel.Text)
	if idx < 0 || idx >= len(cards) {
		return nil
	}
	c := cards[idx]
	return &c
}

// SelectedFileInItem returns the file currently highlighted in
// select mode (the new in-react-mode file selection — distinct from
// the older standalone file-pick selectMode). Returns nil if the
// current selection isn't on a file.
func (m *MessageViewModel) SelectedFileInItem() *types.FileInfo {
	sel := m.SelectedMessage()
	if sel == nil {
		return nil
	}
	kind, idx := m.selectedItemKind()
	if kind != ItemFile {
		return nil
	}
	if idx < 0 || idx >= len(sel.Files) {
		return nil
	}
	f := sel.Files[idx]
	return &f
}

// SelectedCodeSnippet returns the raw text of the currently
// highlighted in-message code snippet, or "" if the cursor isn't
// on a snippet. Used by the `c`-key copy action in select mode.
func (m *MessageViewModel) SelectedCodeSnippet() string {
	sel := m.SelectedMessage()
	if sel == nil {
		return ""
	}
	kind, idx := m.selectedItemKind()
	if kind != ItemCodeSnippet {
		return ""
	}
	snippets := parseCodeSnippets(sel.Text)
	if idx < 0 || idx >= len(snippets) {
		return ""
	}
	return snippets[idx]
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
// scrollToSelectedSubItem ensures the currently selected in-
// message sub-item (card, file, snippet, or reaction) is visible
// in the viewport. Called after left/right arrow nav in select
// mode so cycling through items of a long wrapped message
// doesn't leave the cursor off-screen.
//
// Looks the target line up in the appropriate hit array based on
// the selected kind. File rows aren't in any hit array (they're
// rendered inline by line position), so for ItemFile we fall
// back to the parent message scroll logic.
//
// Only adjusts YOffset when the target is outside [top, bottom];
// otherwise leaves the scroll position alone.
func (m *MessageViewModel) scrollToSelectedSubItem() {
	kind, idx := m.selectedItemKind()
	if kind == ItemNone {
		return
	}
	var targetLine int = -1
	switch kind {
	case ItemCodeSnippet:
		for _, h := range m.codeSnippetHits {
			if h.msgIdx == m.reactIdx && h.localIdx == idx {
				targetLine = h.line
				break
			}
		}
	case ItemReaction:
		// reactionHits store msg message id + emoji. Walk the
		// list and pick the Nth matching reaction on the
		// currently selected message.
		sel := m.SelectedMessage()
		if sel == nil {
			return
		}
		count := 0
		for _, h := range m.reactionHits {
			if h.messageID != sel.MessageID {
				continue
			}
			if count == idx {
				targetLine = h.line
				break
			}
			count++
		}
	case ItemCard:
		// Walk friend card hits in render order and match by
		// localIdx. The hit records don't carry a msg index
		// but since rebuildContent rebuilds them per-render,
		// the N-th entry for a message is the N-th card
		// rendered for the currently selected message.
		sel := m.SelectedMessage()
		if sel == nil {
			return
		}
		// Find the parent message's header line so we can
		// limit our scan to cards rendered after it.
		headerLine := -1
		for line, id := range m.lineToMsgID {
			if id == sel.MessageID {
				headerLine = line
				break
			}
		}
		if headerLine < 0 {
			return
		}
		count := 0
		for _, h := range m.friendCardHits {
			if h.line <= headerLine {
				continue
			}
			if count == idx {
				targetLine = h.line
				break
			}
			count++
		}
	default:
		// ItemFile / ItemReplyList / ItemNone fall through to
		// the parent message scroll below.
	}
	if targetLine < 0 {
		// Fall back to the parent message scroll behaviour.
		m.scrollToReactCursor()
		return
	}
	top := m.viewport.YOffset
	bottom := top + m.viewport.Height - 1
	if targetLine < top {
		m.viewport.SetYOffset(targetLine)
	} else if targetLine > bottom-1 {
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 2)
	}
}

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
	if m.expandedReplies == nil {
		m.expandedReplies = make(map[string]bool)
	}
	m.expandedReplies[msgID] = !m.expandedReplies[msgID]
	m.rebuildContent()
}

// ExpandReplies forces a message's inline reply tree to render
// expanded. Used when a new reply arrives so the user sees it
// immediately instead of having to click "X replies" to expand.
func (m *MessageViewModel) ExpandReplies(msgID string) {
	if msgID == "" {
		return
	}
	if m.expandedReplies == nil {
		m.expandedReplies = make(map[string]bool)
	}
	if m.expandedReplies[msgID] {
		return
	}
	m.expandedReplies[msgID] = true
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
					if f.Uploading {
						// Don't trigger a download for in-flight
						// uploads — instead surface a cancel request
						// so the model can prompt for confirmation.
						return m, func() tea.Msg {
							return CancelUploadRequestMsg{File: f}
						}
					}
					m.selectMode = false
					m.rebuildContent()
					return m, func() tea.Msg {
						return FileDownloadMsg{File: f}
					}
				}
			case "c", "C":
				// 'c' in file-select mode serves two purposes:
				//   * Uploading file → request upload cancellation.
				//   * Completed file → copy its contents to the
				//     clipboard (if it looks like text).
				if m.selectIdx >= 0 && m.selectIdx < len(m.selectables) {
					f := m.selectables[m.selectIdx].file
					if f.Uploading {
						return m, func() tea.Msg {
							return CancelUploadRequestMsg{File: f}
						}
					}
					return m, func() tea.Msg {
						return FileCopyRequestMsg{File: f}
					}
				}
				return m, nil
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
			// Total selectable sub-elements inside the parent
			// message: contact cards, file rows, reactions, and an
			// optional "reply list" virtual element. The combined
			// list is walked left/right via reactionSelIdx.
			subElems := m.totalSubElems(sel)
			selKind, _ := m.selectedItemKind()
			onReplyList := selKind == ItemReplyList

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
					// Recompute kind after the move; clear nested
					// reply state unless we landed on the reply
					// list virtual element.
					newKind, _ := m.selectedItemKind()
					if newKind != ItemReplyList {
						m.replyIdx = -1
						m.replyReactionSelIdx = -1
					}
					m.rebuildContent()
					m.scrollToSelectedSubItem()
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
					newKind, _ := m.selectedItemKind()
					if newKind != ItemReplyList {
						m.replyIdx = -1
						m.replyReactionSelIdx = -1
					}
					m.rebuildContent()
					m.scrollToSelectedSubItem()
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
				if sel != nil {
					if kind, idx := m.selectedItemKind(); kind == ItemReaction && idx >= 0 && idx < len(sel.Reactions) {
						emoji := sel.Reactions[idx].Emoji
						msgID := sel.MessageID
						m.reactMode = false
						m.reactionSelIdx = -1
						m.rebuildContent()
						return m, func() tea.Msg {
							return ToggleReactionMsg{MessageID: msgID, Emoji: emoji}
						}
					}
				}
				// Card selected → emit View Contact Info (Enter
				// is the natural "open" key — matches how Enter
				// on a reaction toggles it). The user can also
				// press a/v/c for explicit actions.
				if card := m.SelectedContactCard(); card != nil {
					c := *card
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FriendCardOptionsSelectMsg{
							Action: FriendCardActionViewContactInfo,
							Card:   c,
						}
					}
				}
				// File selected → trigger download via the same
				// FilesListDownloadMsg path the all-files browser
				// uses (the model handler resolves the destination
				// folder and kicks off startDownload).
				if file := m.SelectedFileInItem(); file != nil {
					f := *file
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FilesListDownloadMsg{File: f}
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
			case "e":
				// 'e' opens the inline edit flow for the selected
				// author-owned message. The hint in the header
				// only shows this for messages the local user
				// authored, but gate it here too so the key is
				// a no-op on someone else's message instead of
				// routing a nonsensical edit request to the
				// model.
				msgID := m.SelectedReplyMessageID()
				if msgID == "" {
					msgID = m.SelectedMessageID()
				}
				if msgID == "" {
					return m, nil
				}
				target := m.MessageByID(msgID)
				if target == nil || !m.isMyMessage(*target) {
					return m, nil
				}
				m.reactMode = false
				m.rebuildContent()
				return m, func() tea.Msg {
					return EditMessageRequestMsg{MessageID: msgID}
				}
			case "a":
				// 'a' = Add Friend on the currently-selected
				// contact card (if any). Routes through the same
				// FriendCardOptionsSelectMsg dispatch the
				// right-click menu uses, so the existing
				// conflict / merge / replace flow runs unchanged.
				if card := m.SelectedContactCard(); card != nil {
					c := *card
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FriendCardOptionsSelectMsg{
							Action: FriendCardActionAddFriend,
							Card:   c,
						}
					}
				}
				return m, nil
			case "v":
				// 'v' = View Contact Info on the currently
				// selected card. Opens the temporary contact
				// card view modal (the model decides between
				// new / existing / self).
				if card := m.SelectedContactCard(); card != nil {
					c := *card
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FriendCardOptionsSelectMsg{
							Action: FriendCardActionViewContactInfo,
							Card:   c,
						}
					}
				}
				return m, nil
			case "c":
				// 'c' has four meanings depending on what's
				// selected inside the current message:
				//   - card selected → Copy Contact Info
				//   - file selected → copy file contents via
				//     the existing FileCopyRequestMsg flow
				//   - code snippet selected → copy the raw
				//     snippet text via CopySnippetRequestMsg
				//   - nothing selected → copy the parent
				//     message itself (author + timestamp +
				//     text, no replies/reactions) via
				//     MessageCopyRequestMsg
				if card := m.SelectedContactCard(); card != nil {
					c := *card
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FriendCardOptionsSelectMsg{
							Action: FriendCardActionCopyContactInfo,
							Card:   c,
						}
					}
				}
				if file := m.SelectedFileInItem(); file != nil {
					f := *file
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return FileCopyRequestMsg{File: f}
					}
				}
				if snippet := m.SelectedCodeSnippet(); snippet != "" {
					m.reactMode = false
					m.reactionSelIdx = -1
					m.rebuildContent()
					return m, func() tea.Msg {
						return CopySnippetRequestMsg{Text: snippet}
					}
				}
				// Fall back to copying the whole message.
				msgID := m.SelectedReplyMessageID()
				if msgID == "" {
					msgID = m.SelectedMessageID()
				}
				if msgID == "" {
					return m, nil
				}
				return m, func() tea.Msg {
					return MessageCopyRequestMsg{MessageID: msgID}
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
	// For non-friend channels, show the secure label inline (right after the
	// title). Friend channels render the label right-aligned alongside the
	// cog icon further down, so skip it here in that case.
	if m.secureLabel != "" && !m.isFriendCh {
		headerParts = append(headerParts, MessageHeaderSecureStyle.Render(" "+m.secureLabel))
	}

	// Determine the date of the first visible message for the sticky date bar.
	dateStr := m.visibleDate()
	if dateStr != "" {
		headerParts = append(headerParts, MessageHeaderDateStyle.Render("  "+dateStr))
	}

	if m.selectMode {
		headerParts = append(headerParts, MessageHeaderHighlight.Render("  [FILE SELECT: ↑↓ navigate | Enter: download | c: copy | f/Esc: exit]"))
	} else if m.contextMode {
		headerParts = append(headerParts, MessageHeaderHighlight.Render("  [Context - PgUp: load more | scroll bottom: exit]"))
	} else if len(m.selectables) > 0 {
		headerParts = append(headerParts, MessageHeaderDateStyle.Render("  [f: select files]"))
	}

	if m.threadMode {
		headerParts = append(headerParts, MessageHeaderHintStyle.Render("  (Esc to exit thread, type to reply)"))
	}

	headerLeft := strings.Join(headerParts, "")

	// Right-aligned section: friend channels show "<secure label>  ⚙" so the
	// user can click the cog to open Friend Details. Hit-testing of the cog
	// is done by the model via FriendCogPaneClickArea, which derives the
	// position from the same content-width math used here.
	header := headerLeft
	if m.isFriendCh {
		// Reserve 1 extra col on the right as a visual gutter so
		// header text / cog don't butt up against the border.
		// Matches rebuildContent's maxWidth calculation below.
		contentW := m.width - 5
		if contentW < 0 {
			contentW = 0
		}
		hintStyle := MessageHeaderHintStyle
		rightStyle := MessageHeaderSecureStyle
		cogStyle := MessageCogStyle
		rightVisible := strings.TrimLeft(m.secureLabel, " ")
		// Compose right block plain text to measure visible width.
		// Layout: "<hint>  <secure label>  ⚙"
		var rightPlainParts []string
		if m.friendDetailsHint != "" {
			rightPlainParts = append(rightPlainParts, m.friendDetailsHint)
		}
		if rightVisible != "" {
			rightPlainParts = append(rightPlainParts, rightVisible)
		}
		rightPlainParts = append(rightPlainParts, friendCogGlyph)
		rightPlain := strings.Join(rightPlainParts, "  ")
		rightVisW := lipgloss.Width(rightPlain)
		leftVisW := lipgloss.Width(headerLeft)
		pad := contentW - leftVisW - rightVisW
		if pad < 1 {
			pad = 1
		}
		var rightRenderedParts []string
		if m.friendDetailsHint != "" {
			rightRenderedParts = append(rightRenderedParts, hintStyle.Render(m.friendDetailsHint))
		}
		if rightVisible != "" {
			rightRenderedParts = append(rightRenderedParts, rightStyle.Render(rightVisible))
		}
		rightRenderedParts = append(rightRenderedParts, cogStyle.Render(friendCogGlyph))
		rightRendered := strings.Join(rightRenderedParts, "  ")
		header = headerLeft + strings.Repeat(" ", pad) + rightRendered
	}

	header += "\n"
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
	m.replyFormat = ""       // don't recursively render replies inside thread
	m.boxFirstMessage = true // visually box the parent message
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

	// One col narrower than the message content width (m.width - 5)
	// so the divider lines up with the same right-side gutter.
	divider := lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", m.width-7))
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
	m.friendCardHits = nil
	m.friendCards = make(map[string]friends.ContactCard)
	m.codeSnippetHits = nil

	var lines []string
	// m.width - 5 = pane width minus 2 borders, 2 cells of
	// Padding(0,1), and 1 extra column reserved as a right-side
	// visual gutter so message text never butts up against the
	// border. Lipgloss then pads the slot (which is m.width - 4
	// wide) with one trailing space on every line.
	maxWidth := m.width - 5
	if maxWidth < 20 {
		maxWidth = 20
	}

	highlightBg := MessageHighlightBgStyle
	dateSepStyle := MessageDateSepStyle
	fileStyle := MessageFileStyle
	fileSelectedStyle := MessageFileSelectedStyle

	var lastDate string
	fileIdx := 0 // tracks which selectable file we're at

	for i, msg := range msgs {
		// Reset per-message render state used by rewriteFriendCards
		// and rewriteCodeSnippets — both need the index/counter
		// to start fresh at every parent message so the cursor
		// matching is unambiguous.
		m.renderingMsgIdx = i
		m.renderingCardCount = 0
		m.renderingSnippetCount = 0

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
			// Suppress the trailing blank when the very next thing is the
			// boxed first message's top rule — keeps date and rule adjacent.
			if !(m.boxFirstMessage && i == 0) {
				lines = append(lines, "")
			}
			lastDate = msgDate
		}

		ts := msg.Timestamp.Format("Jan 2 15:04")
		nameStyle := UserNameStyle.Foreground(UserColor(name))

		headerLine := fmt.Sprintf("%s  %s",
			nameStyle.Render(name),
			TimestampStyle.Render(ts),
		)
		// Pending badge: shown for friend P2P messages that haven't
		// been delivered yet. Flag is cleared automatically once the
		// peer comes back online and the message is re-sent.
		if msg.Pending {
			headerLine += "  " + MessagePendingStyle.Render("⏳ pending")
		}

		// Highlight selected message in select mode.
		if m.reactMode && i == m.reactIdx {
			selectHighlight := MessageSelectBgStyle
			cards, files, snippets, _, hasInlineReplies := m.itemCounts(&msg)
			hasReactions := len(msg.Reactions) > 0
			// Only authors can edit or delete their own messages,
			// so hide the "e: edit" / "d: delete" hints when the
			// local user isn't the author.
			authorHint := ""
			if m.isMyMessage(msg) {
				if msg.IsEmote {
					// Emotes can be deleted but not edited.
					authorHint = "  d: delete"
				} else {
					authorHint = "  e: edit  d: delete"
				}
			}
			// Item-specific hint takes priority when the cursor
			// has cycled onto a sub-item inside the message.
			// Otherwise we show the default reply / react / nav
			// hints, listing only the sub-item categories that
			// actually exist on this message.
			var hint string
			selKind, _ := m.selectedItemKind()
			switch selKind {
			case ItemCard:
				hint = " [a: add friend  v: view info  c: copy info  ←/→: navigate]"
			case ItemFile:
				hint = " [Enter: download  c: copy contents  ←/→: navigate]"
			case ItemCodeSnippet:
				hint = " [c: copy snippet  ←/→: navigate]"
			default:
				navParts := []string{}
				if cards > 0 || files > 0 || snippets > 0 {
					navParts = append(navParts, "items")
				}
				if hasReactions {
					navParts = append(navParts, "reactions")
				}
				if hasInlineReplies {
					navParts = append(navParts, "replies")
				}
				navHint := ""
				if len(navParts) > 0 {
					navHint = "  ←/→: " + strings.Join(navParts, "/")
				}
				replyDownHint := ""
				if hasInlineReplies {
					replyDownHint = "  ↓: into replies"
				}
				// Copy is always available on a selected message
				// (any user can copy any message they can see).
				hint = " [Enter: reply  r: react" + authorHint + "  c: copy" + navHint + replyDownHint + "]"
			}
			headerLine = selectHighlight.Render(headerLine + hint)
		}

		// Pick the body text style based on whether this is an
		// emote action or a regular message. Emotes render in
		// italic + the theme's emote color (purple-ish).
		bodyTextStyle := MessageTextStyle
		if msg.IsEmote {
			bodyTextStyle = EmoteMessageStyle
		}

		text := m.formatText(msg.MessageID, msg.Text)
		// Collapse long [FRIEND:<json|hash>] markers into short
		// [FRIEND:#fc-N] reference tokens *before* word-wrapping,
		// so the marker stays on a single line and the per-line
		// rewriteFriendCards pass can find it.
		text = m.collapseFriendMarkers(text)
		wrapped := wordWrap(text, maxWidth)
		var textLines []string
		if wrapped != "" {
			textLines = strings.Split(wrapped, "\n")
		}

		// Top rule for the boxed first message (used by thread view).
		if m.boxFirstMessage && i == 0 {
			ruleStyle := MessageThreadRuleStyle
			ruleW := maxWidth
			if ruleW < 10 {
				ruleW = 10
			}
			lines = append(lines, ruleStyle.Render(strings.Repeat("─", ruleW)))
		}

		// Track which line this message starts at.
		if msg.MessageID != "" {
			m.lineToMsgID[len(lines)] = msg.MessageID
		}

		if i == highlightIdx {
			headerLine = highlightBg.Render("► " + headerLine)
			lines = append(lines, headerLine)
			// Spacing 1+ adds a blank line below the message header (above
			// the message text). Spacing 2+ also adds one below the text.
			if m.itemSpacing >= 1 {
				lines = append(lines, "")
			}
			for _, tl := range textLines {
				rendered := highlightBg.Render("  " + bodyTextStyle.Render(tl))
				rendered = m.rewriteFriendCards(rendered, len(lines))
				rendered = m.rewriteCodeSnippets(rendered, len(lines))
				lines = append(lines, rendered)
			}
			if m.itemSpacing >= 2 && len(textLines) > 0 {
				lines = append(lines, "")
			}
		} else {
			lines = append(lines, headerLine)
			if m.itemSpacing >= 1 {
				lines = append(lines, "")
			}
			for _, tl := range textLines {
				rendered := "  " + bodyTextStyle.Render(tl)
				rendered = m.rewriteFriendCards(rendered, len(lines))
				rendered = m.rewriteCodeSnippets(rendered, len(lines))
				lines = append(lines, rendered)
			}
			if m.itemSpacing >= 2 && len(textLines) > 0 {
				lines = append(lines, "")
			}
		}

		// Render file attachments. The "selected" highlight fires
		// for two distinct cursor sources:
		//   1. The standalone file-pick selectMode (Ctrl-Up / 'f')
		//   2. React/select mode when the cursor has cycled onto
		//      this file row inside the parent message (the new
		//      arrow-key navigation)
		//
		// The visual cursor "> " + selected style is enough to
		// communicate which file is active — keyboard hints
		// (Enter / c) live in the parent message header next to
		// the username. Don't decorate the file label itself; we
		// don't want the file row to mutate or grow when selected.
		uploadingStyle := MessageFileUploadingStyle
		for fi, f := range msg.Files {
			pickSelected := m.selectMode && fileIdx == m.selectIdx
			reactSelected := false
			if m.reactMode && i == m.reactIdx {
				kind, idx := m.selectedItemKind()
				if kind == ItemFile && idx == fi {
					reactSelected = true
				}
			}
			isSelected := pickSelected || reactSelected
			sizeStr := formatFileSize(f.Size)
			switch {
			case f.Uploading && isSelected:
				lines = append(lines, fileSelectedStyle.Render(
					fmt.Sprintf("  > [FILE:%s] (%s) uploading…", f.Name, sizeStr)))
			case f.Uploading:
				lines = append(lines, uploadingStyle.Render(
					fmt.Sprintf("  [FILE:%s] (%s) uploading…", f.Name, sizeStr)))
			case isSelected:
				lines = append(lines, fileSelectedStyle.Render(
					fmt.Sprintf("  > [FILE:%s] (%s)", f.Name, sizeStr)))
			default:
				lines = append(lines, fileStyle.Render(
					fmt.Sprintf("  [FILE:%s] (%s)", f.Name, sizeStr)))
			}
			fileIdx++
		}

		// Build reply count + reactions row.
		replyCount := len(msg.Replies)
		if replyCount > 0 || len(msg.Reactions) > 0 {
			// Spacing 1+ adds a blank line before the row.
			if m.itemSpacing >= 1 {
				lines = append(lines, "")
			}
			replyLabel := ""
			if replyCount > 0 {
				replyLabel = MessageReplyLabelStyle.Render(fmt.Sprintf("%d %s", replyCount, pluralReplies(replyCount)))
			}
			var reactionParts []string
			var reactionWidths []int
			if len(msg.Reactions) > 0 {
				reactionStyle := MessageReactionStyle
				selectedReactionStyle := MessageReactionSelStyle
				for ri, r := range msg.Reactions {
					emoji := resolveEmoji(r.Emoji)
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
			// Layout when reply label present: "  " (2 spaces) + replyLabel + "  " + reactions
			// Layout when only reactions:      "   " (3 spaces) + reactions
			reactionStartCol := 3
			if replyLabel != "" && reactionsStr != "" {
				reactionStartCol = 2 + lipgloss.Width(replyLabel) + 2
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
				lines = append(lines, "  "+replyLabel+"  "+reactionsStr)
			} else if replyLabel != "" {
				m.replyLineMsgID[currentLine] = msg.MessageID
				lines = append(lines, "  "+replyLabel)
			} else {
				lines = append(lines, "   "+reactionsStr)
			}
		}

		// Inline reply rendering (if enabled).
		if m.replyFormat == "inline" && replyCount > 0 && m.expandedReplies[msg.MessageID] {
			replyIndent := "    "
			replyHeaderStyle := MessageHeaderDateStyle
			reactionStyle := MessageReactionStyle
			selectedReactionStyle := MessageReactionSelStyle
			// Spacing-1 adds 1 blank between the "X replies" row and the
			// reply list, plus 1 between each reply. Spacing-2 adds 2.
			betweenReplies := 0
			if m.itemSpacing >= 1 {
				betweenReplies = m.itemSpacing
			}
			for k := 0; k < betweenReplies; k++ {
				lines = append(lines, "")
			}
			// Highlight when this parent message has its reply list active.
			replyListActive := m.reactMode && i == m.reactIdx && m.reactionSelIdx == len(msg.Reactions) && replyCount > 0
			for ri, reply := range msg.Replies {
				if ri > 0 {
					for k := 0; k < betweenReplies; k++ {
						lines = append(lines, "")
					}
				}
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
					selectHighlight := MessageSelectBgStyle
					replyDelete := ""
					if m.isMyMessage(reply) {
						replyDelete = "  d: delete"
					}
					header = selectHighlight.Render(header + " [r: react" + replyDelete + "  Esc: back]")
				}
				lines = append(lines, replyIndent+header)
				rText := m.formatText(reply.MessageID, reply.Text)
				for _, rLine := range strings.Split(rText, "\n") {
					lines = append(lines, replyIndent+"  "+MessageTextStyle.Render(rLine))
				}
				// Reactions row for this reply.
				if len(reply.Reactions) > 0 {
					var parts []string
					var widths []int
					for rj, r := range reply.Reactions {
						emoji := resolveEmoji(r.Emoji)
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
			ruleStyle := MessageThreadRuleStyle
			ruleW := maxWidth
			if ruleW < 10 {
				ruleW = 10
			}
			lines = append(lines, ruleStyle.Render(strings.Repeat("─", ruleW)))
		}

		// Trailing blank line(s) — number depends on the spacing level.
		// 0 = 1 blank, 1 = 2 blanks, 2 = 3 blanks.
		trailingBlanks := 1 + m.itemSpacing
		for k := 0; k < trailingBlanks; k++ {
			lines = append(lines, "")
		}
	}

	// Trim trailing blank lines so the latest message hugs the bottom of
	// the viewport instead of leaving padding below it.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
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
