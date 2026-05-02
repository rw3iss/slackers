package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/threads"
	"github.com/rw3iss/slackers/internal/types"
)

// Sort modes for channels.
const (
	SortByType   = "type"   // default: group by type (public, private, DM, group)
	SortByName   = "name"   // alphabetical by display name
	SortByRecent = "recent" // most recent message first (falls back to type)
)

// sidebarRow represents one logical entry in the sidebar — a section
// header, a channel, or a Threads-group thread item. Thread items
// occupy two visual lines (channel name on top, participants below);
// see displayLineMap and View() for how the dual-row rendering keeps
// the click hit-test in lock-step with the rendered output.
type sidebarRow struct {
	isHeader    bool
	headerKey   string // "channels", "private", "dm", "group", "threads"
	headerLabel string
	channel     *types.Channel
	// isThread marks a row that displays a tracked thread. When set,
	// `thread` carries the snapshot and the row renders as two
	// visual lines instead of one.
	isThread bool
	thread   *threads.ThreadSnapshot
}

// ChannelListModel represents the sidebar channel list.
type ChannelListModel struct {
	channels []types.Channel
	rows     []sidebarRow // computed: headers + channels interleaved
	// rowIndexByID maps a channel ID to its index into rows. Built
	// by buildRows so SelectByID is O(1) instead of an O(n) scan.
	// Headers do not appear in this map.
	rowIndexByID map[string]int
	selected     int
	scrollOff    int
	unread       map[string]bool
	hidden       map[string]bool
	showHidden   bool
	aliases      map[string]string
	latestTS     map[string]string
	collapsed    map[string]bool // headerKey -> collapsed
	sortBy       string
	sortAsc      bool
	focused      bool
	width        int
	height       int
	itemSpacing  int // empty lines after each item / header (0..3)

	// friendStatus carries per-friend online/away state SEPARATE
	// from the unread map. Keyed by channel ID ("friend:<uid>").
	friendStatus map[string]FriendDisplayStatus

	// bulkUpdate > 0 suspends the automatic buildRows() call at
	// the end of every setter. BeginBulk / EndBulk wrap a chunk
	// of setters (used by ChannelsLoadedMsg which applies
	// channels + friends + hidden + aliases + collapsed + sort
	// in a single message) so the sidebar is only rebuilt once
	// at the end instead of once per setter.
	bulkUpdate int

	// workspaceName is the display name of the active workspace,
	// shown as a centred header at the top of the sidebar.
	workspaceName string
	// multipleWorkspaces is true when more than one workspace is
	// signed in; causes a ⇅ indicator to appear next to the name.
	multipleWorkspaces bool

	// activeThreads is the latest set of tracked threads — pushed
	// into the sidebar by the model whenever the threads.Store
	// reports a change. The "Threads" sidebar group materialises
	// from this slice on each buildRows; an empty slice hides the
	// group entirely.
	activeThreads []threads.ThreadSnapshot
}

// FriendDisplayStatus tracks a friend's online/away state for
// the sidebar renderer. Separate from the unread map so "online"
// and "has unread messages" are distinct visual indicators.
type FriendDisplayStatus struct {
	Online      bool
	AwayStatus  string // "online", "away", "back", "offline", ""
	AwayMessage string
}

// SetFriendStatus replaces the friend status map. Called by the
// model after FriendPingMsg and FriendStatusUpdateMsg.
func (m *ChannelListModel) SetFriendStatus(statuses map[string]FriendDisplayStatus) {
	m.friendStatus = statuses
}

// SetWorkspaceName sets the active workspace display name shown in the
// sidebar header. Pass an empty string to hide the header.
func (m *ChannelListModel) SetWorkspaceName(name string) {
	m.workspaceName = name
}

// SetMultipleWorkspaces controls whether the ⇅ indicator is shown next
// to the workspace name (true when more than one workspace is signed in).
func (m *ChannelListModel) SetMultipleWorkspaces(v bool) {
	m.multipleWorkspaces = v
}

// BeginBulkUpdate suspends automatic buildRows() calls until a
// matching EndBulkUpdate. Pair calls 1:1 — nesting is supported via
// an internal counter. Use when applying multiple channel-list
// setters back-to-back so the sidebar doesn't get rebuilt for each.
func (m *ChannelListModel) BeginBulkUpdate() {
	m.bulkUpdate++
}

// EndBulkUpdate decrements the bulk counter and forces a single
// buildRows() on the final call. Always call in a defer or pair it
// with BeginBulkUpdate to avoid leaving buildRows suppressed.
func (m *ChannelListModel) EndBulkUpdate() {
	if m.bulkUpdate > 0 {
		m.bulkUpdate--
	}
	if m.bulkUpdate == 0 {
		m.buildRows()
	}
}

// rebuild runs buildRows unless we're inside a bulk update, in which
// case it's deferred until EndBulkUpdate.
func (m *ChannelListModel) rebuild() {
	if m.bulkUpdate > 0 {
		return
	}
	m.buildRows()
}

// SetItemSpacing sets the number of blank lines rendered after every
// channel row and header (0..2, clamped).
func (m *ChannelListModel) SetItemSpacing(n int) {
	if n < 0 {
		n = 0
	}
	if n > 2 {
		n = 2
	}
	m.itemSpacing = n
}

// NewChannelList creates a new channel list model.
func NewChannelList() ChannelListModel {
	return ChannelListModel{
		unread:       make(map[string]bool),
		hidden:       make(map[string]bool),
		aliases:      make(map[string]string),
		latestTS:     make(map[string]string),
		collapsed:    make(map[string]bool),
		rowIndexByID: make(map[string]int),
		sortBy:       SortByType,
		sortAsc:      true,
	}
}

func (m *ChannelListModel) SetChannels(channels []types.Channel) {
	m.channels = channels
	m.rebuild()
}

// SetFriendChannels sets the friend channels that render at the top of the sidebar.
func (m *ChannelListModel) SetFriendChannels(friends []types.Channel) {
	// Remove existing friend channels.
	var workspace []types.Channel
	for _, ch := range m.channels {
		if !ch.IsFriend {
			workspace = append(workspace, ch)
		}
	}
	// Prepend friends so they appear first.
	m.channels = append(friends, workspace...)
	m.rebuild()
}

func (m *ChannelListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *ChannelListModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m *ChannelListModel) SetHiddenChannels(ids []string) {
	m.hidden = make(map[string]bool, len(ids))
	for _, id := range ids {
		m.hidden[id] = true
	}
}

func (m *ChannelListModel) SetAliases(aliases map[string]string) {
	if aliases == nil {
		m.aliases = make(map[string]string)
	} else {
		m.aliases = aliases
	}
}

// SetLatestTimestamps updates the latest message timestamps for channels (used by "recent" sort).
func (m *ChannelListModel) SetLatestTimestamps(ts map[string]string) {
	m.latestTS = ts
}

// UpdateLatestTimestamp updates a single channel's latest timestamp.
func (m *ChannelListModel) UpdateLatestTimestamp(channelID, ts string) {
	if m.latestTS == nil {
		m.latestTS = make(map[string]string)
	}
	m.latestTS[channelID] = ts
}

// SetCollapsedGroups sets which section groups are collapsed.
func (m *ChannelListModel) SetCollapsedGroups(keys []string) {
	m.collapsed = make(map[string]bool, len(keys))
	for _, k := range keys {
		m.collapsed[k] = true
	}
	m.rebuild()
}

// CollapsedGroups returns the list of collapsed section keys.
func (m *ChannelListModel) CollapsedGroups() []string {
	var keys []string
	for k, v := range m.collapsed {
		if v {
			keys = append(keys, k)
		}
	}
	return keys
}

// ToggleCollapse toggles a section header's collapsed state.
func (m *ChannelListModel) ToggleCollapse(headerKey string) {
	m.collapsed[headerKey] = !m.collapsed[headerKey]
}

func (m *ChannelListModel) SetSort(sortBy string, ascending bool) {
	m.sortBy = sortBy
	m.sortAsc = ascending
	m.rebuild()
}

// ToggleShowHidden toggles whether hidden channels are shown inline.
// The scroll position is reset to the top on toggle — otherwise the
// list can end up scrolled way below the first row when the total
// row count shrinks (hiding them again), leaving a nearly-blank
// viewport until the user manually scrolls back up.
func (m *ChannelListModel) ToggleShowHidden() {
	m.showHidden = !m.showHidden
	m.buildRows()
	m.scrollOff = 0
}

// ShowingHidden returns whether hidden channels are currently shown.
func (m *ChannelListModel) ShowingHidden() bool {
	return m.showHidden
}

func (m *ChannelListModel) HideChannel(id string) {
	m.hidden[id] = true
	m.buildRows()
}

func (m *ChannelListModel) UnhideChannel(id string) {
	delete(m.hidden, id)
	m.buildRows()
}

func (m *ChannelListModel) HiddenChannelIDs() []string {
	ids := make([]string, 0, len(m.hidden))
	for id := range m.hidden {
		ids = append(ids, id)
	}
	return ids
}

func (m *ChannelListModel) HiddenChannelsList() []types.Channel {
	var result []types.Channel
	for _, ch := range m.channels {
		if m.hidden[ch.ID] {
			result = append(result, ch)
		}
	}
	return result
}

func (m *ChannelListModel) AllChannels() []types.Channel {
	return m.channels
}

func (m *ChannelListModel) MarkUnread(channelID string) {
	m.unread[channelID] = true
}

// IsUnread returns true if the channel is currently marked unread.
func (m *ChannelListModel) IsUnread(channelID string) bool {
	return m.unread[channelID]
}

func (m *ChannelListModel) ClearUnread(channelID string) {
	delete(m.unread, channelID)
}

// UnreadChannelIDs returns a snapshot of every channel ID currently
// marked as unread. Used by the background read-state reconciler to
// know which channels to query `conversations.info` for.
func (m *ChannelListModel) UnreadChannelIDs() []string {
	out := make([]string, 0, len(m.unread))
	for id := range m.unread {
		out = append(out, id)
	}
	return out
}

func (m *ChannelListModel) displayName(ch types.Channel) string {
	if alias, ok := m.aliases[ch.ID]; ok && alias != "" {
		return alias
	}
	return ch.Name
}

// visibleChannels returns channels in display order, respecting hide/sort.
// The sort comparators previously called strings.ToLower(displayName(...))
// twice per comparison, allocating fresh strings on every compare. We now
// precompute the lowercase name once per filtered entry and use the
// precomputed value from the closures, which cuts sort allocations from
// O(n log n) strings to O(n).
func (m *ChannelListModel) visibleChannels() []types.Channel {
	var filtered []types.Channel
	for _, ch := range m.channels {
		if m.hidden[ch.ID] && !m.showHidden {
			continue
		}
		filtered = append(filtered, ch)
	}

	// Precompute lowercase display names parallel to filtered.
	// Only build this slice for sort modes that actually need it.
	var lowerNames []string
	needsLower := m.sortBy == SortByName || m.sortBy == SortByType
	if needsLower {
		lowerNames = make([]string, len(filtered))
		for i := range filtered {
			lowerNames[i] = strings.ToLower(m.displayName(filtered[i]))
		}
	}
	// When we swap entries during sort we have to keep lowerNames
	// in lockstep. sort.SliceStable doesn't expose a Swap hook, so
	// we build an index slice, sort THAT, and then emit the result.
	idx := make([]int, len(filtered))
	for i := range idx {
		idx[i] = i
	}

	switch m.sortBy {
	case SortByName:
		sort.SliceStable(idx, func(i, j int) bool {
			a := lowerNames[idx[i]]
			b := lowerNames[idx[j]]
			if m.sortAsc {
				return a < b
			}
			return a > b
		})
	case SortByRecent:
		sort.SliceStable(idx, func(i, j int) bool {
			tsI := m.latestTS[filtered[idx[i]].ID]
			tsJ := m.latestTS[filtered[idx[j]].ID]
			if tsI == "" {
				tsI = "0"
			}
			if tsJ == "" {
				tsJ = "0"
			}
			if m.sortAsc {
				return tsI < tsJ // oldest first
			}
			return tsI > tsJ // newest first (most useful default)
		})
	default: // SortByType
		sort.SliceStable(idx, func(i, j int) bool {
			oi := channelSortOrder(filtered[idx[i]])
			oj := channelSortOrder(filtered[idx[j]])
			if oi != oj {
				if m.sortAsc {
					return oi < oj
				}
				return oi > oj
			}
			return lowerNames[idx[i]] < lowerNames[idx[j]]
		})
	}

	out := make([]types.Channel, len(filtered))
	for i, k := range idx {
		out[i] = filtered[k]
	}
	return out
}

// displayLineMap returns a slice mapping display-line index → row index.
// A value of -1 means the line is a blank spacer (the gap before a non-first
// header). View() and SelectByRow() both consume this so the visible layout
// and the click hit-test stay in lock-step.
//
// Thread rows occupy two visual lines (channel name + participants);
// both lines map back to the same row index so a click on either
// row activates the same thread.
func (m *ChannelListModel) displayLineMap() []int {
	var lines []int
	for i, row := range m.rows {
		if row.isHeader && i > 0 {
			lines = append(lines, -1) // blank spacer before non-first headers
		}
		lines = append(lines, i)
		if row.isThread {
			// Second visual line for the participants row, also
			// pointing back to this thread row.
			lines = append(lines, i)
		}
		// User-configured trailing blank lines after every row.
		for k := 0; k < m.itemSpacing; k++ {
			lines = append(lines, -1)
		}
	}
	return lines
}

// ChannelByRow looks up the channel (or header) at the given Y row
// in the sidebar viewport WITHOUT mutating the cursor position
// (m.selected). Used by right-click handling so popping the
// channel context menu doesn't visually shift the sidebar
// highlight off the user's currently active channel.
//
// Returns the same triple as SelectByRow: (channel, isChannel, headerKey).
func (m *ChannelListModel) ChannelByRow(y int) (*types.Channel, bool, string) {
	if y < 0 {
		return nil, false, ""
	}
	targetLine := m.scrollOff + y
	lines := m.displayLineMap()
	if targetLine < 0 || targetLine >= len(lines) {
		return nil, false, ""
	}
	rowIdx := lines[targetLine]
	if rowIdx < 0 {
		return nil, false, ""
	}
	row := m.rows[rowIdx]
	if row.isHeader {
		return nil, false, row.headerKey
	}
	if row.channel != nil {
		return row.channel, true, ""
	}
	return nil, false, ""
}

// SelectByRow selects the row at the given Y position within the sidebar viewport
// (i.e. screen Y minus the sidebar's top border). Returns the selected channel
// (nil for headers), whether a channel was clicked, and the header key if a
// header was clicked.
func (m *ChannelListModel) SelectByRow(y int) (*types.Channel, bool, string) {
	if y < 0 {
		return nil, false, ""
	}
	targetLine := m.scrollOff + y
	lines := m.displayLineMap()
	if targetLine < 0 || targetLine >= len(lines) {
		return nil, false, ""
	}
	rowIdx := lines[targetLine]
	if rowIdx < 0 {
		return nil, false, "" // clicked on a blank spacer
	}
	row := m.rows[rowIdx]
	m.selected = rowIdx
	if row.isHeader {
		return nil, false, row.headerKey
	}
	if row.channel != nil {
		return row.channel, true, ""
	}
	return nil, false, ""
}

func sectionKey(ch types.Channel) string {
	switch {
	case ch.IsFriend:
		return "friends"
	case ch.IsDM:
		return "dm"
	case ch.IsGroup:
		return "group"
	case ch.IsPrivate:
		return "private"
	default:
		return "channels"
	}
}

func sectionLabel(key string) string {
	switch key {
	case "threads":
		return "Threads"
	case "friends":
		return "Friends"
	case "channels":
		return "# Channels"
	case "private":
		return "# Private"
	case "dm":
		return "@ Direct Messages"
	case "group":
		return "Group Chats"
	}
	return key
}

// buildRows constructs the interleaved list of headers and channels
// and refreshes the rowIndexByID fast-lookup map so SelectByID /
// NextUnreadChannel / click hit-tests can land in O(1). When the
// Threads store has at least one active snapshot, a "Threads" group
// is emitted at the very top of the sidebar — above all other
// groups — and the rows for that group occupy two visual lines
// each (handled by displayLineMap and View()).
func (m *ChannelListModel) buildRows() {
	visible := m.visibleChannels()
	m.rows = nil
	// Reset rather than reallocate so we avoid churning the map on
	// every rebuild. Rebuilds are already rare (channel add/remove,
	// hide toggle, sort change) compared with the per-render path
	// that uses this map for lookups.
	if m.rowIndexByID == nil {
		m.rowIndexByID = make(map[string]int, len(visible))
	} else {
		for k := range m.rowIndexByID {
			delete(m.rowIndexByID, k)
		}
	}

	// Threads group: emitted at the top of the sidebar when at least
	// one active thread is tracked. Hidden completely when zero,
	// matching the spec — the user only sees the group when it
	// would carry content.
	if len(m.activeThreads) > 0 {
		m.rows = append(m.rows, sidebarRow{
			isHeader:    true,
			headerKey:   "threads",
			headerLabel: sectionLabel("threads"),
		})
		if !m.collapsed["threads"] {
			for i := range m.activeThreads {
				snap := m.activeThreads[i]
				m.rows = append(m.rows, sidebarRow{
					isThread: true,
					thread:   &snap,
				})
			}
		}
	}

	prevKey := ""
	for i := range visible {
		ch := visible[i]
		key := sectionKey(ch)
		if key != prevKey {
			m.rows = append(m.rows, sidebarRow{
				isHeader:    true,
				headerKey:   key,
				headerLabel: sectionLabel(key),
			})
			prevKey = key
		}
		if !m.collapsed[key] {
			chCopy := ch
			idx := len(m.rows)
			m.rows = append(m.rows, sidebarRow{channel: &chCopy})
			if chCopy.ID != "" {
				m.rowIndexByID[chCopy.ID] = idx
			}
		}
	}

	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// SetThreads replaces the active-threads slice and rebuilds the
// sidebar rows. Called by the model whenever the threads.Store
// reports a change. Pass an empty slice to hide the Threads group.
func (m *ChannelListModel) SetThreads(active []threads.ThreadSnapshot) {
	m.activeThreads = active
	m.rebuild()
}

// SelectedThread returns the snapshot at the cursor when a thread
// row is selected, or nil otherwise.
func (m *ChannelListModel) SelectedThread() *threads.ThreadSnapshot {
	if m.selected < 0 || m.selected >= len(m.rows) {
		return nil
	}
	row := m.rows[m.selected]
	if !row.isThread {
		return nil
	}
	return row.thread
}

// ThreadByRow returns the thread snapshot at the given Y position
// in the sidebar viewport, without mutating the cursor. Used by
// right-click handling so popping the thread context menu doesn't
// shift the highlight off the user's currently active channel.
func (m *ChannelListModel) ThreadByRow(y int) *threads.ThreadSnapshot {
	if y < 0 {
		return nil
	}
	targetLine := m.scrollOff + y
	lines := m.displayLineMap()
	if targetLine < 0 || targetLine >= len(lines) {
		return nil
	}
	rowIdx := lines[targetLine]
	if rowIdx < 0 {
		return nil
	}
	row := m.rows[rowIdx]
	if !row.isThread {
		return nil
	}
	return row.thread
}

// SelectedChannel returns the channel at the current selection, or nil if a header is selected.
func (m *ChannelListModel) SelectedChannel() *types.Channel {
	if m.selected < 0 || m.selected >= len(m.rows) {
		return nil
	}
	row := m.rows[m.selected]
	if row.isHeader {
		return nil
	}
	return row.channel
}

// SelectedRow returns the viewport-relative Y position of the currently
// selected row (0-based from the top of the visible channel list area).
// Returns -1 if nothing is selected.
func (m *ChannelListModel) SelectedRow() int {
	if m.selected < 0 {
		return -1
	}
	lines := m.displayLineMap()
	for lineIdx, rowIdx := range lines {
		if rowIdx == m.selected {
			viewY := lineIdx - m.scrollOff
			if viewY >= 0 {
				return viewY
			}
			return 0
		}
	}
	return -1
}

// SelectByID moves the cursor to the channel with the given ID.
// Lookup is O(1) via rowIndexByID maintained by buildRows.
func (m *ChannelListModel) SelectByID(id string) {
	if id == "" {
		return
	}
	if idx, ok := m.rowIndexByID[id]; ok && idx < len(m.rows) {
		m.selected = idx
	}
}

// NextUnreadChannel returns the next unread channel after the current selection.
func (m *ChannelListModel) NextUnreadChannel() *types.Channel {
	if len(m.rows) == 0 {
		return nil
	}
	n := len(m.rows)
	for i := 1; i <= n; i++ {
		idx := (m.selected + i) % n
		row := m.rows[idx]
		if !row.isHeader && row.channel != nil && m.unread[row.channel.ID] {
			m.selected = idx
			m.ensureVisible()
			return row.channel
		}
	}
	return nil
}

func channelSortOrder(ch types.Channel) int {
	switch {
	// Friends sit at the end of the list when sort is ascending and at
	// the beginning when descending — give them a high order index so the
	// existing asc/desc flip in visibleChannels handles the placement.
	case ch.IsFriend:
		return 99
	case !ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 0
	case ch.IsPrivate && !ch.IsDM && !ch.IsGroup:
		return 1
	case ch.IsDM:
		return 2
	default:
		return 3
	}
}

// ToggleCollapseMsg signals the model to persist collapsed state.
type ToggleCollapseMsg struct{}

// Update handles key events when focused.
func (m ChannelListModel) Update(msg tea.Msg) (ChannelListModel, tea.Cmd) {
	if !m.focused {
		// Mouse wheel still scrolls the sidebar even when not focused, so the
		// user can scroll over it without first clicking to focus.
		if mouse, ok := msg.(tea.MouseMsg); ok {
			switch mouse.Button {
			case tea.MouseButtonWheelUp:
				m.scrollBy(-3)
			case tea.MouseButtonWheelDown:
				m.scrollBy(+3)
			}
		}
		return m, nil
	}

	km := DefaultKeyMap()
	total := len(m.rows)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, km.Up):
			if m.selected > 0 {
				m.selected--
			}
		case key.Matches(msg, km.Down):
			if m.selected < total-1 {
				m.selected++
			}
		case key.Matches(msg, km.PageUp):
			m.selected -= m.height / 2
			if m.selected < 0 {
				m.selected = 0
			}
		case key.Matches(msg, km.PageDown):
			m.selected += m.height / 2
			if m.selected >= total {
				m.selected = total - 1
			}
		case key.Matches(msg, km.Home):
			m.selected = 0
		case key.Matches(msg, km.End):
			m.selected = total - 1

		case key.Matches(msg, km.Enter), key.Matches(msg, km.SidebarCollapse):
			// If a header is selected, toggle collapse.
			if m.selected >= 0 && m.selected < len(m.rows) && m.rows[m.selected].isHeader {
				hk := m.rows[m.selected].headerKey
				m.ToggleCollapse(hk)
				m.buildRows()
				return m, func() tea.Msg { return ToggleCollapseMsg{} }
			}
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollBy(-3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.scrollBy(+3)
			return m, nil
		}
	}

	m.ensureVisible()
	return m, nil
}

// displayLineOfRow returns the display-line index where the given row begins,
// or -1 if the row is not present.
func (m *ChannelListModel) displayLineOfRow(rowIdx int) int {
	for i, r := range m.displayLineMap() {
		if r == rowIdx {
			return i
		}
	}
	return -1
}

// viewHeight returns the inner content height of the sidebar pane.
func (m *ChannelListModel) viewHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// totalDisplayLines returns the total number of rendered display lines.
func (m *ChannelListModel) totalDisplayLines() int {
	return len(m.displayLineMap())
}

// scrollBy adjusts the scroll offset by delta lines and clamps it.
func (m *ChannelListModel) scrollBy(delta int) {
	maxOff := m.totalDisplayLines() - m.viewHeight()
	if maxOff < 0 {
		maxOff = 0
	}
	m.scrollOff += delta
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
	if m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
}

// ensureVisible scrolls so the selected row is inside the visible window.
// Both scrollOff and the display layout are computed in display-line space
// so item spacing > 0 doesn't break the math.
func (m *ChannelListModel) ensureVisible() {
	vh := m.viewHeight()
	selLine := m.displayLineOfRow(m.selected)
	if selLine < 0 {
		return
	}
	if selLine < m.scrollOff {
		m.scrollOff = selLine
	}
	if selLine >= m.scrollOff+vh {
		m.scrollOff = selLine - vh + 1
	}
	maxOff := m.totalDisplayLines() - vh
	if maxOff < 0 {
		maxOff = 0
	}
	if m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the channel list.
func (m ChannelListModel) View() string {
	// SidebarStyle now uses asymmetric padding (1 left, 0 right) so
	// the inner content area is m.width - 3 cells. With a 2-cell
	// row prefix that leaves m.width - 5 cells for the channel /
	// thread name itself.
	maxNameLen := m.width - 5

	// Build display lines from the shared map so click hit-test stays in sync.
	type displayLine struct {
		text string
	}
	var lines []displayLine
	// prevRowIdx tracks the row emitted on the previous loop step so
	// thread rows (which produce two visual lines from the same row
	// index) can render line 1 (channel name) on first encounter and
	// line 2 (participants) on the second. Initialised to -1 so the
	// first iteration is always treated as a fresh row.
	prevRowIdx := -1
	for _, rowIdx := range m.displayLineMap() {
		if rowIdx < 0 {
			lines = append(lines, displayLine{text: ""})
			prevRowIdx = -1
			continue
		}
		row := m.rows[rowIdx]
		if row.isHeader {
			arrow := "▼"
			if m.collapsed[row.headerKey] {
				arrow = "►"
			}
			label := arrow + " " + row.headerLabel
			// Indent group headers one column less than child rows so
			// the header text doesn't line up exactly with the channel
			// names below it. When selected, skip the "> " caret —
			// the highlight colour is enough — so the label stays in
			// the same column as when unselected.
			if rowIdx == m.selected {
				selStyle := lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
				if ColorSelectedChannelBg != "" {
					selStyle = selStyle.Background(ColorSelectedChannelBg)
				}
				lines = append(lines, displayLine{text: selStyle.Render(label)})
			} else {
				lines = append(lines, displayLine{text: SectionHeaderStyle.Render(label)})
			}
		} else if row.isThread && row.thread != nil {
			isSecondLine := prevRowIdx == rowIdx
			lines = append(lines, displayLine{
				text: m.renderThreadItem(*row.thread, rowIdx, maxNameLen, isSecondLine),
			})
		} else if row.channel != nil {
			ch := *row.channel
			isHidden := m.hidden[ch.ID]
			lines = append(lines, displayLine{text: m.renderItem(ch, rowIdx, maxNameLen, isHidden)})
		}
		prevRowIdx = rowIdx
	}

	// Build an optional away-status footer for the selected friend.
	// The footer sits at the very bottom of the sidebar pane,
	// growing UPWARD from the bottom row. It can wrap to up to 3
	// lines based on the current sidebar width; any text beyond
	// that is truncated with an ellipsis.
	//
	// The footer height is computed BEFORE the scroll window so
	// the channel list shrinks to make room and the footer never
	// overlaps content.
	var awayFooterRendered []string
	if m.selected >= 0 && m.selected < len(m.rows) {
		row := m.rows[m.selected]
		if row.channel != nil && row.channel.IsFriend {
			if fs, ok := m.friendStatus[row.channel.ID]; ok && fs.AwayStatus == "away" {
				label := "AWAY"
				if fs.AwayMessage != "" {
					label = "AWAY: " + fs.AwayMessage
				}
				// The usable content width inside the pane is
				// the outer width minus 2 borders minus 2
				// padding cells. Leave 1 extra cell so text
				// doesn't butt against the right border.
				innerW := m.width - 5
				if innerW < 8 {
					innerW = 8
				}
				const maxFooterLines = 3
				awayFooterRendered = wrapAndTruncate(label, innerW, maxFooterLines)
			}
		}
	}
	awayFooterLines := len(awayFooterRendered)

	// Render optional workspace header at the very top of the sidebar.
	var wsHeader string
	if m.workspaceName != "" {
		indicator := ""
		if m.multipleWorkspaces {
			indicator = " ⇅"
		}
		// availableWidth is the inner content width (outer width minus
		// the 2 border columns).
		availableWidth := m.width - 2
		if availableWidth < 1 {
			availableWidth = 1
		}
		wsHeader = lipgloss.NewStyle().
			Foreground(ColorPageHeader).
			Bold(true).
			Width(availableWidth).
			Align(lipgloss.Center).
			Render(m.workspaceName + indicator)
	}
	wsHeaderLines := 0
	if wsHeader != "" {
		wsHeaderLines = 2
	}

	// Options hint takes 1 line when a channel (not header) is selected.
	optionsHintLines := 0
	if m.selected >= 0 && m.selected < len(m.rows) && !m.rows[m.selected].isHeader {
		optionsHintLines = 1
	}

	// Apply scrolling. Reserve space for workspace header, away footer, and options hint.
	viewHeight := m.height - 2 - wsHeaderLines - awayFooterLines - optionsHintLines
	if viewHeight < 1 {
		viewHeight = 1
	}
	start := m.scrollOff
	if start > len(lines) {
		start = len(lines)
	}
	end := start + viewHeight
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	if wsHeader != "" {
		b.WriteString(wsHeader)
		b.WriteString("\n\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(lines[i].text)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Options hint shown when focused and a channel is selected.
	var optionsHint string
	if optionsHintLines > 0 {
		optionsHint = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true).
			Render(" Alt+O: options")
	}

	content := b.String()
	// Append footers pinned at the bottom. Pad the
	// channel list content with exactly enough newlines so the
	// footer's LAST line sits on the very last row inside the
	// border — growing upward from there.
	totalFooterLines := awayFooterLines + optionsHintLines
	if totalFooterLines > 0 {
		renderedLines := strings.Count(content, "\n") + 1
		totalRows := m.height - 1
		gap := totalRows - renderedLines - totalFooterLines
		if gap < 0 {
			gap = 0
		}
		for i := 0; i < gap; i++ {
			content += "\n"
		}
		if awayFooterLines > 0 {
			awayStyle := lipgloss.NewStyle().
				Foreground(ColorMuted).
				Italic(true).
				Bold(true)
			for _, line := range awayFooterRendered {
				content += "\n" + awayStyle.Render(" "+line)
			}
		}
		if optionsHint != "" {
			content += "\n" + optionsHint
		}
	}

	style := SidebarStyle
	if m.focused {
		style = SidebarActiveStyle
	}

	return style.
		Width(m.width).
		Height(m.height).
		Render(content)
}

// wrapAndTruncate word-wraps text to maxWidth columns and returns
// at most maxLines lines. If the wrapped text exceeds maxLines,
// the last line is truncated and an ellipsis appended. Used by
// the sidebar's away-status footer to fit the message into the
// dynamically resizable sidebar width.
func wrapAndTruncate(text string, maxWidth, maxLines int) []string {
	if maxWidth < 4 {
		maxWidth = 4
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, w := range words {
		wLen := len(w)
		if curLen > 0 && curLen+1+wLen > maxWidth {
			// Flush current line.
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
			if len(lines) >= maxLines {
				break
			}
		}
		if curLen > 0 {
			cur.WriteByte(' ')
			curLen++
		}
		// If a single word exceeds the width, hard-break it.
		if wLen > maxWidth {
			remaining := maxWidth - curLen
			if remaining < 1 {
				lines = append(lines, cur.String())
				cur.Reset()
				curLen = 0
				remaining = maxWidth
			}
			cur.WriteString(w[:remaining])
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
			// Drop the rest of the over-long word — it would
			// take too many lines to render fully.
			continue
		}
		cur.WriteString(w)
		curLen += wLen
	}
	if cur.Len() > 0 && len(lines) < maxLines {
		lines = append(lines, cur.String())
	}
	// Truncate to maxLines with ellipsis on the last line.
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	if len(lines) == maxLines && cur.Len() > 0 {
		// Check if there's more text that didn't fit.
		total := 0
		for _, w := range words {
			total += len(w) + 1
		}
		totalChars := 0
		for _, l := range lines {
			totalChars += len(l) + 1
		}
		if totalChars < total {
			last := lines[maxLines-1]
			if len(last) > maxWidth-1 {
				last = last[:maxWidth-2]
			}
			lines[maxLines-1] = last + "…"
		}
	}
	return lines
}

// renderThreadItem renders one of the two visual lines of a Threads-
// group entry. Row 1 shows the channel alias (or "#channel-name" /
// "@friend-name" when no alias is set) on the left and a muted,
// right-aligned "time since last activity" badge on the right.
// Row 2 shows the participants list in muted italic.
//
// The two-column row 1 layout:
//
//	"  Alias            1h"
//	 prefix  name      time
//
// Both columns share the row's selection highlight so the entry
// reads as a single picker target.
func (m ChannelListModel) renderThreadItem(snap threads.ThreadSnapshot, rowIdx int, maxLen int, isSecondLine bool) string {
	// Prefix conventions (matches the regular channel rows so the
	// thread group's left edge lines up with the rest of the
	// sidebar):
	//   selected   line 1 → "> " (cursor in same column as
	//                              regular channels)
	//   selected   line 2 → "    " (participants nested 2 cols
	//                                under the channel name)
	//   unselected line 1 → "  " (matches regular channel
	//                              indent — was previously 4
	//                              cols which pushed the labels
	//                              too far right)
	//   unselected line 2 → "    " (same nest as selected)
	prefix := "  "
	switch {
	case rowIdx == m.selected && !isSecondLine:
		prefix = "> "
	case rowIdx == m.selected:
		prefix = "    "
	case isSecondLine:
		prefix = "    "
	}

	// Inner row width — full sidebar inner area, not just the
	// name budget the caller passes for renderItem. SidebarStyle
	// uses asymmetric padding (1 col left, 0 col right) so the
	// real content area is `m.width - 3` cells. The caller
	// passes `m.width - 5` (the name budget after a 2-col
	// prefix), so we add 2 to recover the full row.
	inner := maxLen + 2
	if inner < 1 {
		inner = 1
	}

	if isSecondLine {
		// Participants row, indented under the channel name.
		partsMax := inner - len(prefix)
		if partsMax < 1 {
			partsMax = 1
		}
		text := joinParticipantNames(snap.Participants, partsMax)
		if text == "" {
			text = "no other participants"
		}
		style := ThreadParticipantsRowStyle
		if rowIdx == m.selected {
			style = lipgloss.NewStyle().
				Foreground(ColorSelectedChannel).
				Italic(true)
			if ColorSelectedChannelBg != "" {
				style = style.Background(ColorSelectedChannelBg)
			}
		}
		return style.Render(prefix + text)
	}

	// Row 1 — name + right-aligned time-since.
	name := threadDisplayName(snap, m.aliases)
	timeStr := formatRelativeTime(snap.LastActivityAt)
	if timeStr == "" && !snap.AddedAt.IsZero() {
		timeStr = formatRelativeTime(snap.AddedAt)
	}

	timeReserve := 0
	if timeStr != "" {
		// One col of breathing room before the time string;
		// time itself ends flush with the right edge of the
		// inner content area.
		timeReserve = len(timeStr) + 1
	}
	nameMax := inner - len(prefix) - timeReserve
	if nameMax < 1 {
		nameMax = 1
	}
	if len(name) > nameMax {
		clip := nameMax - 1
		if clip < 1 {
			clip = 1
		}
		name = name[:clip] + "~"
	}

	var nameStyle lipgloss.Style
	if rowIdx == m.selected {
		nameStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
		if ColorSelectedChannelBg != "" {
			nameStyle = nameStyle.Background(ColorSelectedChannelBg)
		}
	} else if snap.Ref.Source == threads.SourceFriend {
		nameStyle = ThreadFriendChannelRowStyle
	} else {
		nameStyle = ThreadSlackChannelRowStyle
	}

	leftPart := nameStyle.Render(prefix + name)
	if timeStr == "" {
		return leftPart
	}

	// Pad between name and time so the time string sits at the
	// inner right edge — no trailing whitespace before the border.
	used := len(prefix) + len(name)
	spacer := inner - used - len(timeStr)
	if spacer < 1 {
		spacer = 1
	}

	timeStyle := ThreadParticipantsRowStyle
	if rowIdx == m.selected && ColorSelectedChannelBg != "" {
		timeStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Background(ColorSelectedChannelBg).
			Italic(true)
	}
	return leftPart + strings.Repeat(" ", spacer) + timeStyle.Render(timeStr)
}

// threadDisplayName returns the row-1 label for a thread item:
// the channel alias (when one is configured), otherwise the
// transport-prefixed channel name ("#general" for Slack, "@Brian"
// for friend chats). Friend channels never gain a "#" prefix and
// Slack channels never gain an "@" prefix.
func (m ChannelListModel) renderItem(ch types.Channel, rowIdx int, maxLen int, isHidden bool) string {
	prefix := "  "
	if rowIdx == m.selected {
		prefix = "> "
	}

	name := m.displayName(ch)
	if !ch.IsDM && !ch.IsGroup {
		name = fmt.Sprintf("#%s", name)
	}

	if isHidden {
		name = "(hidden) " + name
	}

	if maxLen > 0 && len(name) > maxLen {
		name = name[:maxLen-1] + "~"
	}

	var style lipgloss.Style
	switch {
	case rowIdx == m.selected:
		style = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
		if ColorSelectedChannelBg != "" {
			style = style.Background(ColorSelectedChannelBg)
		}
	case ch.IsFriend:
		// Three-state friend rendering using the dedicated
		// friendStatus map (separate from unread):
		//   green       = online (status "online" or "back")
		//   italic/dim  = away
		//   muted       = offline / unknown
		fs := m.friendStatus[ch.ID]
		switch {
		case fs.Online && fs.AwayStatus == "away":
			style = lipgloss.NewStyle().Foreground(ColorUserAway).Italic(true)
			if ColorUserAwayBg != "" {
				style = style.Background(ColorUserAwayBg)
			}
		case fs.Online:
			style = lipgloss.NewStyle().Foreground(ColorFriendOnline)
			if ColorFriendOnlineBg != "" {
				style = style.Background(ColorFriendOnlineBg)
			}
		default:
			style = lipgloss.NewStyle().Foreground(ColorFriendOffline)
			if ColorFriendOfflineBg != "" {
				style = style.Background(ColorFriendOfflineBg)
			}
		}
		// Unread badge for friends is separate from online.
		if m.unread[ch.ID] {
			name = "* " + name
		}
	case m.unread[ch.ID]:
		name = "* " + name
		style = ChannelUnreadStyle
	case isHidden:
		style = lipgloss.NewStyle().Foreground(ColorMuted)
	default:
		style = lipgloss.NewStyle().Foreground(ColorChannelName)
		if ColorChannelNameBg != "" {
			style = style.Background(ColorChannelNameBg)
		}
	}

	return style.Render(prefix + name)
}
