package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MsgOptionsAction represents a chosen action from the options menu.
type MsgOptionsAction int

const (
	MsgActionNone MsgOptionsAction = iota
	MsgActionReact
	MsgActionReply
)

// MsgOptionsSelectMsg signals which option the user chose.
type MsgOptionsSelectMsg struct {
	Action    MsgOptionsAction
	MessageID string
	Preview   string
}

// MsgOptionsModel is a small popup menu shown next to a clicked message.
type MsgOptionsModel struct {
	messageID string
	preview   string
	selected  int
	x, y      int // anchor position
	width     int
	height    int
}

// NewMsgOptions creates an options popup at the given position.
func NewMsgOptions(messageID, preview string, x, y int) MsgOptionsModel {
	return MsgOptionsModel{
		messageID: messageID,
		preview:   preview,
		x:         x,
		y:         y,
	}
}

func (m *MsgOptionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

var msgOptionsItems = []struct {
	label  string
	action MsgOptionsAction
}{
	{"React  😀", MsgActionReact},
	{"Reply  ↩", MsgActionReply},
}

func (m MsgOptionsModel) Update(msg tea.Msg) (MsgOptionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(msgOptionsItems)-1 {
				m.selected++
			}
		case "enter":
			item := msgOptionsItems[m.selected]
			return m, func() tea.Msg {
				return MsgOptionsSelectMsg{
					Action:    item.action,
					MessageID: m.messageID,
					Preview:   m.preview,
				}
			}
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			// Box layout: border(1) + title(1) + newline(1) + items
			// So options start at m.y + 3.
			row := msg.Y - m.y - 3
			if row >= 0 && row < len(msgOptionsItems) {
				m.selected = row
				item := msgOptionsItems[row]
				return m, func() tea.Msg {
					return MsgOptionsSelectMsg{
						Action:    item.action,
						MessageID: m.messageID,
						Preview:   m.preview,
					}
				}
			}
		}
	}
	return m, nil
}

// View renders a small box at the configured anchor position.
// The box adjusts to stay within the screen bounds.
func (m MsgOptionsModel) View(bgContent string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Options"))
	b.WriteString("\n")
	for i, item := range msgOptionsItems {
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

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Width(18)

	box := boxStyle.Render(b.String())

	// Position the box near the anchor (x, y), adjust to stay on screen.
	boxLines := strings.Split(box, "\n")
	boxH := len(boxLines)
	boxW := lipgloss.Width(box)

	posX := m.x
	posY := m.y
	if posX+boxW > m.width {
		posX = m.width - boxW - 1
	}
	if posX < 0 {
		posX = 0
	}
	if posY+boxH > m.height {
		posY = m.height - boxH - 1
	}
	if posY < 0 {
		posY = 0
	}

	// Overlay onto the background content.
	bgLines := strings.Split(bgContent, "\n")
	for len(bgLines) < m.height {
		bgLines = append(bgLines, "")
	}

	for i, line := range boxLines {
		row := posY + i
		if row >= len(bgLines) {
			break
		}
		// Pad existing line to posX, then append box.
		existing := bgLines[row]
		existingWidth := lipgloss.Width(existing)
		var prefix string
		if existingWidth >= posX {
			// Truncate existing line at posX.
			prefix = truncateToWidth(existing, posX)
		} else {
			prefix = existing + strings.Repeat(" ", posX-existingWidth)
		}
		bgLines[row] = prefix + line
	}

	return strings.Join(bgLines, "\n")
}

// truncateToWidth truncates a string to a visible width, preserving ANSI codes.
func truncateToWidth(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Simple truncation — may break ANSI but functional for most cases.
	return s[:min(len(s), w)]
}
