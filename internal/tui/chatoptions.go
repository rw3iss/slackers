package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatOptionsAction represents a chosen action from the chat-pane context menu.
type ChatOptionsAction int

const (
	ChatActionNone ChatOptionsAction = iota
	ChatActionViewContact
	ChatActionBrowseShared
	ChatActionViewFiles
	ChatActionSendFile
	ChatActionAudioCall
	ChatActionRename
)

// ChatOptionsSelectMsg is emitted when the user picks an entry from the
// chat-pane right-click menu.
type ChatOptionsSelectMsg struct {
	Action    ChatOptionsAction
	ChannelID string
	UserID    string
}

type chatOptionsItem struct {
	label  string
	action ChatOptionsAction
}

// ChatOptionsModel is the right-click context menu for the chat/messages pane.
type ChatOptionsModel struct {
	channelID  string
	userID     string
	items      []chatOptionsItem
	selected   int
	x, y       int
	chatLeft   int // left boundary of the chat pane (inclusive)
	chatRight  int // right boundary of the chat pane (inclusive)
	chatTop    int
	chatBottom int
	finalX     int
	finalY     int
	boxW, boxH int
	width      int
	height     int
}

// NewChatOptions builds a context menu anchored at (x, y).
func NewChatOptions(channelID, userID string, items []chatOptionsItem, x, y int) ChatOptionsModel {
	return ChatOptionsModel{
		channelID: channelID,
		userID:    userID,
		items:     items,
		x:         x,
		y:         y,
	}
}

// SetBounds records the visible chat pane boundaries so SetSize can clamp
// the popup inside them.
func (m *ChatOptionsModel) SetBounds(chatLeft, chatTop, chatRight, chatBottom int) {
	m.chatLeft = chatLeft
	m.chatTop = chatTop
	m.chatRight = chatRight
	m.chatBottom = chatBottom
}

// SetSize computes the final render position, clamped so the popup never
// overhangs the chat pane or the terminal edges.
func (m *ChatOptionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	sample := m.renderBox()
	m.boxH = strings.Count(sample, "\n") + 1
	m.boxW = lipgloss.Width(sample)

	// Horizontal: prefer click position, clamp within chat pane.
	m.finalX = m.x
	if m.finalX < m.chatLeft {
		m.finalX = m.chatLeft
	}
	if m.finalX+m.boxW > m.chatRight+1 {
		m.finalX = m.chatRight + 1 - m.boxW
	}
	if m.finalX < m.chatLeft {
		m.finalX = m.chatLeft
	}

	// Vertical: prefer click position, clamp within chat pane.
	m.finalY = m.y
	if m.finalY+m.boxH > m.chatBottom {
		m.finalY = m.chatBottom - m.boxH
	}
	if m.finalY < m.chatTop {
		m.finalY = m.chatTop
	}
}

// ClickInside returns true when (x, y) falls within the rendered popup.
func (m ChatOptionsModel) ClickInside(x, y int) bool {
	const buffer = 1
	return x >= m.finalX-buffer && x < m.finalX+m.boxW+buffer &&
		y >= m.finalY-buffer && y < m.finalY+m.boxH+buffer
}

func (m ChatOptionsModel) renderBox() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Chat"))
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

	maxLen := len("Chat")
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

func (m ChatOptionsModel) Update(msg tea.Msg) (ChatOptionsModel, tea.Cmd) {
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
				return ChatOptionsSelectMsg{
					Action:    item.action,
					ChannelID: m.channelID,
					UserID:    m.userID,
				}
			}
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			// Each item spans 2 rows (blank line before it). Offset of 3
			// accounts for: border (1) + title line (1) + blank gap (1).
			delta := msg.Y - m.finalY - 3
			row := delta / 2
			if row >= 0 && row < len(m.items) {
				m.selected = row
				item := m.items[row]
				return m, func() tea.Msg {
					return ChatOptionsSelectMsg{
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

// View overlays the popup onto bgContent at the clamped anchor position,
// preserving background content to the left and right of the popup.
func (m ChatOptionsModel) View(bgContent string) string {
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
		lineW := lipgloss.Width(line)
		bgLines[row] = ansiTruncatePad(bgLines[row], posX) + line + ansiAfterCells(bgLines[row], posX+lineW)
	}

	return strings.Join(bgLines, "\n")
}
