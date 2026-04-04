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

// ChannelListModel represents the sidebar channel list.
type ChannelListModel struct {
	channels    []types.Channel
	selected    int
	scrollOff   int // scroll offset (first visible line)
	unread      map[string]bool
	hidden      map[string]bool
	showHidden  bool // toggle to show hidden channels inline
	aliases     map[string]string
	sortBy      string
	sortAsc     bool
	focused     bool
	width       int
	height      int
}

// NewChannelList creates a new channel list model.
func NewChannelList() ChannelListModel {
	return ChannelListModel{
		unread:  make(map[string]bool),
		hidden:  make(map[string]bool),
		aliases: make(map[string]string),
		sortBy:  SortByType,
		sortAsc: true,
	}
}

func (m *ChannelListModel) SetChannels(channels []types.Channel) {
	m.channels = channels
	visible := m.visibleChannels()
	if m.selected >= len(visible) {
		m.selected = 0
	}
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

func (m *ChannelListModel) SetSort(sortBy string, ascending bool) {
	m.sortBy = sortBy
	m.sortAsc = ascending
}

// ToggleShowHidden toggles whether hidden channels are shown inline.
func (m *ChannelListModel) ToggleShowHidden() {
	m.showHidden = !m.showHidden
	visible := m.visibleChannels()
	if m.selected >= len(visible) {
		m.selected = len(visible) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// ShowingHidden returns whether hidden channels are currently shown.
func (m *ChannelListModel) ShowingHidden() bool {
	return m.showHidden
}

func (m *ChannelListModel) HideChannel(id string) {
	m.hidden[id] = true
	visible := m.visibleChannels()
	if m.selected >= len(visible) && m.selected > 0 {
		m.selected = len(visible) - 1
	}
}

func (m *ChannelListModel) UnhideChannel(id string) {
	delete(m.hidden, id)
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

func (m *ChannelListModel) SelectedChannel() *types.Channel {
	visible := m.visibleChannels()
	if len(visible) == 0 || m.selected < 0 || m.selected >= len(visible) {
		return nil
	}
	return &visible[m.selected]
}

func (m *ChannelListModel) SelectByID(id string) {
	visible := m.visibleChannels()
	for i, ch := range visible {
		if ch.ID == id {
			m.selected = i
			return
		}
	}
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
		// We don't have last-message timestamps in Channel, so fall through to type sort
		fallthrough
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

// Update handles key events when focused.
func (m ChannelListModel) Update(msg tea.Msg) (ChannelListModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	km := DefaultKeyMap()
	visible := m.visibleChannels()
	total := len(visible)

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
		}
	}

	// Keep selection in view
	m.ensureVisible()

	return m, nil
}

// ensureVisible adjusts scrollOff so the selected item is visible.
func (m *ChannelListModel) ensureVisible() {
	viewHeight := m.height - 2 // account for border/padding
	if viewHeight < 1 {
		viewHeight = 1
	}

	// Account for section headers in line count
	// Approximate: selected item's line position = selected + number_of_headers_before_it
	linePos := m.selectedLinePos()

	if linePos < m.scrollOff {
		m.scrollOff = linePos
	}
	if linePos >= m.scrollOff+viewHeight {
		m.scrollOff = linePos - viewHeight + 1
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// selectedLinePos returns the approximate line position of the selected item
// including section headers.
func (m *ChannelListModel) selectedLinePos() int {
	visible := m.visibleChannels()
	linePos := 0
	idx := 0
	prevType := -1

	for _, ch := range visible {
		t := channelSortOrder(ch)
		if t != prevType {
			if linePos > 0 {
				linePos++ // blank line / header
			}
			linePos++ // header line
			prevType = t
		}
		if idx == m.selected {
			return linePos
		}
		linePos++
		idx++
	}
	return linePos
}

// View renders the channel list.
func (m ChannelListModel) View() string {
	visible := m.visibleChannels()

	// Build all lines with section headers
	type lineItem struct {
		text    string
		isHeader bool
	}
	var lines []lineItem

	prevType := -1
	idx := 0
	maxNameLen := m.width - 6

	for _, ch := range visible {
		t := channelSortOrder(ch)
		if t != prevType {
			var header string
			switch t {
			case 0:
				header = "# Channels"
			case 1:
				header = "# Private"
			case 2:
				header = "@ Direct Messages"
			case 3:
				header = "Group Chats"
			}
			lines = append(lines, lineItem{text: SectionHeaderStyle.Render(header), isHeader: true})
			prevType = t
		}

		isHidden := m.hidden[ch.ID]
		name := m.renderItem(ch, idx, maxNameLen, isHidden)
		lines = append(lines, lineItem{text: name})
		idx++
	}

	// Apply scrolling
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

func (m ChannelListModel) renderItem(ch types.Channel, idx int, maxLen int, isHidden bool) string {
	prefix := "  "
	if idx == m.selected {
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
	case idx == m.selected:
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
