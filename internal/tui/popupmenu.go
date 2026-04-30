package tui

// PopupMenu is a shared, reusable right-click context menu primitive.
// It handles rendering, positioning, clamping, keyboard/mouse
// navigation, and background-overlay compositing. Each concrete
// overlay (MsgOptions, SidebarOptions, ChatOptions, FriendCardOptions)
// embeds a PopupMenu and provides only its own action types,
// item list, and action-dispatch logic.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PopupMenuItem is a single entry in a popup menu. Index is an
// opaque caller-defined value used to dispatch actions.
type PopupMenuItem struct {
	Label string
	Index int
}

// PopupMenu handles the shared rendering, positioning, navigation,
// and hit-testing for right-click context menus.
type PopupMenu struct {
	title    string
	items    []PopupMenuItem
	selected int

	// Anchor position (click coordinates).
	x, y int

	// Optional minimum X (e.g. chat pane left edge). 0 = no constraint.
	minX int

	// Optional explicit bounds for clamping (e.g. chat pane rect).
	hasBounds                              bool
	boundsLeft, boundsTop, boundsRight, boundsBottom int

	// Computed after SetSize.
	finalX, finalY int
	boxW, boxH     int
	width, height  int

	// Optional fixed content width (0 = auto-calculate from items).
	fixedWidth int
}

// NewPopupMenu creates a popup menu anchored at (x, y) with the
// given title and items.
func NewPopupMenu(title string, items []PopupMenuItem, x, y int) PopupMenu {
	return PopupMenu{
		title: title,
		items: items,
		x:     x,
		y:     y,
	}
}

// SetMinX sets the minimum left X for position clamping.
func (m *PopupMenu) SetMinX(minX int) { m.minX = minX }

// SetBounds sets explicit bounds for position clamping.
func (m *PopupMenu) SetBounds(left, top, right, bottom int) {
	m.hasBounds = true
	m.boundsLeft = left
	m.boundsTop = top
	m.boundsRight = right
	m.boundsBottom = bottom
}

// SetFixedWidth forces the popup content to a specific width.
// Pass 0 to auto-calculate from item labels.
func (m *PopupMenu) SetFixedWidth(w int) { m.fixedWidth = w }

// SetSize measures the rendered popup and clamps its position
// within the terminal (or explicit bounds).
func (m *PopupMenu) SetSize(w, h int) {
	m.width = w
	m.height = h
	sample := m.renderBox()
	m.boxH = strings.Count(sample, "\n") + 1
	m.boxW = lipgloss.Width(sample)

	if m.hasBounds {
		// Clamp within explicit bounds.
		m.finalX = m.x
		if m.finalX < m.boundsLeft {
			m.finalX = m.boundsLeft
		}
		if m.finalX+m.boxW > m.boundsRight+1 {
			m.finalX = m.boundsRight + 1 - m.boxW
		}
		if m.finalX < m.boundsLeft {
			m.finalX = m.boundsLeft
		}
		m.finalY = m.y
		if m.finalY+m.boxH > m.boundsBottom {
			m.finalY = m.boundsBottom - m.boxH
		}
		if m.finalY < m.boundsTop {
			m.finalY = m.boundsTop
		}
	} else {
		// Clamp within terminal, respecting optional minX.
		m.finalX = m.x
		if m.finalX < m.minX {
			m.finalX = m.minX
		}
		if m.finalX+m.boxW > m.width {
			m.finalX = m.width - m.boxW - 1
		}
		if m.finalX < m.minX {
			m.finalX = m.minX
		}
		m.finalY = m.y
		if m.finalY+m.boxH > m.height {
			m.finalY = m.height - m.boxH - 1
		}
		if m.finalY < 0 {
			m.finalY = 0
		}
	}
}

// ClickInside returns true if (x, y) is within the rendered popup
// (with a 1-cell buffer so edge clicks still register).
func (m PopupMenu) ClickInside(x, y int) bool {
	const buffer = 1
	return x >= m.finalX-buffer && x < m.finalX+m.boxW+buffer &&
		y >= m.finalY-buffer && y < m.finalY+m.boxH+buffer
}

// Selected returns the currently highlighted item index.
func (m PopupMenu) Selected() int { return m.selected }

// HandleKey processes a key event for navigation. Returns the
// selected item index and whether the user confirmed (Enter).
func (m *PopupMenu) HandleKey(key string) (int, bool) {
	switch key {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case "enter":
		if m.selected >= 0 && m.selected < len(m.items) {
			return m.selected, true
		}
	}
	return m.selected, false
}

// HandleClick processes a left-click and returns the clicked item
// index and true if a valid item was hit.
func (m *PopupMenu) HandleClick(clickX, clickY int) (int, bool) {
	// Each item spans 2 rows (blank line before it). Offset of 3
	// accounts for: border (1) + title line (1) + first blank (1).
	delta := clickY - m.finalY - 3
	row := delta / 2
	if row >= 0 && row < len(m.items) {
		m.selected = row
		return row, true
	}
	return -1, false
}

// renderBox builds the popup content string.
func (m PopupMenu) renderBox() string {
	var b strings.Builder
	b.WriteString(PopupTitleStyle.Render(m.title))
	b.WriteString("\n")
	for i, item := range m.items {
		b.WriteString("\n")
		cursor := "  "
		style := ChannelItemStyle
		if i == m.selected {
			cursor = "> "
			style = ChannelSelectedStyle
		}
		b.WriteString(style.Render(cursor + item.Label))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(PopupDimStyle.Render("↑↓ Enter Esc"))

	contentWidth := m.fixedWidth
	if contentWidth == 0 {
		maxLen := len(m.title)
		for _, it := range m.items {
			if l := len(it.Label) + 2; l > maxLen {
				maxLen = l
			}
		}
		contentWidth = maxLen + 4
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Width(contentWidth)

	return boxStyle.Render(b.String())
}

// View overlays the popup onto bgContent at the clamped anchor
// position, preserving background to the left and right.
func (m PopupMenu) View(bgContent string) string {
	box := m.renderBox()
	boxLines := strings.Split(box, "\n")

	bgLines := strings.Split(bgContent, "\n")
	for len(bgLines) < m.height {
		bgLines = append(bgLines, "")
	}

	for i, line := range boxLines {
		row := m.finalY + i
		if row >= len(bgLines) {
			break
		}
		lineW := lipgloss.Width(line)
		bgLines[row] = ansiTruncatePad(bgLines[row], m.finalX) + line + ansiAfterCells(bgLines[row], m.finalX+lineW)
	}

	return strings.Join(bgLines, "\n")
}

// Update handles both key and mouse events. Returns the updated
// menu and (selectedIndex, confirmed). Callers use confirmed to
// dispatch their own action messages.
func (m PopupMenu) Update(msg tea.Msg) (PopupMenu, int, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		idx, confirmed := m.HandleKey(msg.String())
		return m, idx, confirmed
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			idx, confirmed := m.HandleClick(msg.X, msg.Y)
			return m, idx, confirmed
		}
	}
	return m, m.selected, false
}
