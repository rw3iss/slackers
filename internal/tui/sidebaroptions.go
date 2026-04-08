package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SidebarOptionsAction represents a chosen action from the sidebar
// channel context menu.
type SidebarOptionsAction int

const (
	SidebarActionNone SidebarOptionsAction = iota
	SidebarActionHide
	SidebarActionRename
	SidebarActionInvite
	SidebarActionViewContact
)

// SidebarOptionsSelectMsg signals which option the user chose from
// the sidebar channel context menu. ChannelID is the sidebar
// channel id (e.g. "CXXXXXX" for a Slack channel or "friend:slacker:..."
// for a friend). UserID is filled in for DM / friend entries so the
// invite / contact-info actions can route to the right person.
type SidebarOptionsSelectMsg struct {
	Action    SidebarOptionsAction
	ChannelID string
	UserID    string
}

type sidebarOptionsItem struct {
	label  string
	action SidebarOptionsAction
}

// SidebarOptionsModel is a popup menu rendered next to a right-clicked
// channel entry in the sidebar.
type SidebarOptionsModel struct {
	channelID  string
	userID     string
	items      []sidebarOptionsItem
	selected   int
	x, y       int // anchor position (click coords)
	finalX     int // computed render position after clamping
	finalY     int
	boxW, boxH int
	width      int
	height     int
}

// NewSidebarOptions builds a popup for the given channel. The caller
// decides which items to include based on channel type and friend
// status — this function just takes them pre-assembled.
func NewSidebarOptions(channelID, userID string, items []sidebarOptionsItem, x, y int) SidebarOptionsModel {
	return SidebarOptionsModel{
		channelID: channelID,
		userID:    userID,
		items:     items,
		x:         x,
		y:         y,
	}
}

// SetSize measures the rendered popup against the terminal dimensions
// and clamps its top-left anchor so the box stays fully visible.
func (m *SidebarOptionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	sample := m.renderBox()
	m.boxH = strings.Count(sample, "\n") + 1
	m.boxW = lipgloss.Width(sample)

	m.finalX = m.x
	if m.finalX < 0 {
		m.finalX = 0
	}
	if m.finalX+m.boxW > m.width {
		m.finalX = m.width - m.boxW - 1
	}
	if m.finalX < 0 {
		m.finalX = 0
	}

	m.finalY = m.y
	if m.finalY+m.boxH > m.height {
		m.finalY = m.height - m.boxH - 1
	}
	if m.finalY < 0 {
		m.finalY = 0
	}
}

// ClickInside reports whether (x,y) is inside the popup box (with a
// 1-cell buffer so edge clicks still register as "inside").
func (m SidebarOptionsModel) ClickInside(x, y int) bool {
	const buffer = 1
	return x >= m.finalX-buffer && x < m.finalX+m.boxW+buffer &&
		y >= m.finalY-buffer && y < m.finalY+m.boxH+buffer
}

func (m SidebarOptionsModel) renderBox() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Channel"))
	b.WriteString("\n")
	for i, item := range m.items {
		b.WriteString("\n")
		cursor := "  "
		style := ChannelItemStyle
		if i == m.selected {
			cursor = "> "
			style = ChannelSelectedStyle
		}
		b.WriteString(style.Render(cursor + item.label))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑↓ Enter Esc"))

	// Box wide enough to fit the longest item plus cursor + padding.
	maxLen := len("Channel")
	for _, it := range m.items {
		if l := len(it.label) + 2; l > maxLen {
			maxLen = l
		}
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Width(maxLen + 4)

	return boxStyle.Render(b.String())
}

func (m SidebarOptionsModel) Update(msg tea.Msg) (SidebarOptionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
			}
		case "enter":
			if m.selected < 0 || m.selected >= len(m.items) {
				return m, nil
			}
			item := m.items[m.selected]
			return m, func() tea.Msg {
				return SidebarOptionsSelectMsg{
					Action:    item.action,
					ChannelID: m.channelID,
					UserID:    m.userID,
				}
			}
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			// Items each span 2 rows (blank line before each). The
			// -3 offset (vs. -2 in MsgOptionsModel) shifts the hit
			// area down by 1 row so clicks on an item label land
			// on that item instead of the one above it.
			delta := msg.Y - m.finalY - 3
			row := delta / 2
			if row >= 0 && row < len(m.items) {
				m.selected = row
				item := m.items[row]
				return m, func() tea.Msg {
					return SidebarOptionsSelectMsg{
						Action:    item.action,
						ChannelID: m.channelID,
						UserID:    m.userID,
					}
				}
			}
		}
	}
	return m, nil
}

// View overlays the popup onto bgContent at its clamped anchor.
func (m SidebarOptionsModel) View(bgContent string) string {
	box := m.renderBox()
	boxLines := strings.Split(box, "\n")
	posX := m.finalX
	posY := m.finalY

	bgLines := strings.Split(bgContent, "\n")
	for len(bgLines) < m.height {
		bgLines = append(bgLines, "")
	}

	for i, line := range boxLines {
		row := posY + i
		if row >= len(bgLines) {
			break
		}
		bgLines[row] = ansiTruncatePad(bgLines[row], posX) + line
	}

	return strings.Join(bgLines, "\n")
}
