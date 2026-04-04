package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// Sort modes for channels.
const (
	SortByType    = "type"    // default: group by type (public, private, DM, group)
	SortByName    = "name"    // alphabetical by display name
	SortByRecent  = "recent"  // most recent message first (falls back to type)
)

// sidebarRow represents either a section header or a channel in the sidebar.
type sidebarRow struct {
	isHeader    bool
	headerKey   string // "channels", "private", "dm", "group"
	headerLabel string
	channel     *types.Channel
}

// ChannelListModel represents the sidebar channel list.
type ChannelListModel struct {
	channels    []types.Channel
	rows        []sidebarRow // computed: headers + channels interleaved
	selected    int
	scrollOff   int
	unread      map[string]bool
	hidden      map[string]bool
	showHidden  bool
	aliases     map[string]string
	latestTS    map[string]string
	collapsed   map[string]bool // headerKey -> collapsed
	sortBy      string
	sortAsc     bool
	focused     bool
	width       int
	height      int
}

// NewChannelList creates a new channel list model.
func NewChannelList() ChannelListModel {
	return ChannelListModel{
		unread:    make(map[string]bool),
		hidden:    make(map[string]bool),
		aliases:   make(map[string]string),
		latestTS:  make(map[string]string),
		collapsed: make(map[string]bool),
		sortBy:    SortByType,
		sortAsc:   true,
	}
}

func (m *ChannelListModel) SetChannels(channels []types.Channel) {
	m.channels = channels
	m.buildRows()
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
	m.buildRows()
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
}

// ToggleShowHidden toggles whether hidden channels are shown inline.
func (m *ChannelListModel) ToggleShowHidden() {
	m.showHidden = !m.showHidden
	m.buildRows()
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

func (m *ChannelListModel) ClearUnread(channelID string) {
	delete(m.unread, channelID)
}

func (m *ChannelListModel) displayName(ch types.Channel) string {
	if alias, ok := m.aliases[ch.ID]; ok && alias != "" {
		return alias
	}
	return ch.Name
}

// visibleChannels returns channels in display order, respecting hide/sort.
func (m *ChannelListModel) visibleChannels() []types.Channel {
	var filtered []types.Channel
	for _, ch := range m.channels {
		if m.hidden[ch.ID] && !m.showHidden {
			continue
		}
		filtered = append(filtered, ch)
	}

	switch m.sortBy {
	case SortByName:
		sort.SliceStable(filtered, func(i, j int) bool {
			a := strings.ToLower(m.displayName(filtered[i]))
			b := strings.ToLower(m.displayName(filtered[j]))
			if m.sortAsc {
				return a < b
			}
			return a > b
		})
	case SortByRecent:
		sort.SliceStable(filtered, func(i, j int) bool {
			tsI := m.latestTS[filtered[i].ID]
			tsJ := m.latestTS[filtered[j].ID]
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
		sort.SliceStable(filtered, func(i, j int) bool {
			oi := channelSortOrder(filtered[i])
			oj := channelSortOrder(filtered[j])
			if oi != oj {
				if m.sortAsc {
					return oi < oj
				}
				return oi > oj
			}
			a := strings.ToLower(m.displayName(filtered[i]))
			b := strings.ToLower(m.displayName(filtered[j]))
			return a < b
		})
	}

	return filtered
}

func sectionKey(ch types.Channel) string {
	switch {
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

// buildRows constructs the interleaved list of headers and channels.
func (m *ChannelListModel) buildRows() {
	visible := m.visibleChannels()
	m.rows = nil

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
			m.rows = append(m.rows, sidebarRow{channel: &chCopy})
		}
	}

	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
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

// SelectByID moves the cursor to the channel with the given ID.
func (m *ChannelListModel) SelectByID(id string) {
	for i, row := range m.rows {
		if !row.isHeader && row.channel != nil && row.channel.ID == id {
			m.selected = i
			return
		}
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

		case key.Matches(msg, km.Enter), msg.String() == " ":
			// If a header is selected, toggle collapse.
			if m.selected >= 0 && m.selected < len(m.rows) && m.rows[m.selected].isHeader {
				hk := m.rows[m.selected].headerKey
				m.ToggleCollapse(hk)
				m.buildRows()
				return m, func() tea.Msg { return ToggleCollapseMsg{} }
			}
		}
	}

	m.ensureVisible()
	return m, nil
}

func (m *ChannelListModel) ensureVisible() {
	viewHeight := m.height - 2
	if viewHeight < 1 {
		viewHeight = 1
	}
	if m.selected < m.scrollOff {
		m.scrollOff = m.selected
	}
	if m.selected >= m.scrollOff+viewHeight {
		m.scrollOff = m.selected - viewHeight + 1
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the channel list.
func (m ChannelListModel) View() string {
	maxNameLen := m.width - 6

	// Build display lines from rows.
	type displayLine struct {
		text string
	}
	var lines []displayLine

	for i, row := range m.rows {
		if row.isHeader {
			// Add blank line before headers (except the first).
			if i > 0 {
				lines = append(lines, displayLine{text: ""})
			}
			arrow := "▼"
			if m.collapsed[row.headerKey] {
				arrow = "►"
			}
			label := arrow + " " + row.headerLabel
			if i == m.selected {
				lines = append(lines, displayLine{text: ChannelSelectedStyle.Render("> " + label)})
			} else {
				lines = append(lines, displayLine{text: SectionHeaderStyle.Render("  " + label)})
			}
		} else if row.channel != nil {
			ch := *row.channel
			isHidden := m.hidden[ch.ID]
			lines = append(lines, displayLine{text: m.renderItem(ch, i, maxNameLen, isHidden)})
		}
	}

	// Apply scrolling.
	viewHeight := m.height - 2
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
	for i := start; i < end; i++ {
		b.WriteString(lines[i].text)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	content := b.String()

	style := SidebarStyle
	if m.focused {
		style = SidebarActiveStyle
	}

	return style.
		Width(m.width).
		Height(m.height).
		Render(content)
}

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
		style = ChannelSelectedStyle
	case m.unread[ch.ID]:
		name = "* " + name
		style = ChannelUnreadStyle
	case isHidden:
		style = lipgloss.NewStyle().Foreground(ColorMuted)
	default:
		style = ChannelItemStyle
	}

	return style.Render(prefix + name)
}
