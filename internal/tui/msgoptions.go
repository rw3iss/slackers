package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/debug"
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
	messageID  string
	preview    string
	selected   int
	x, y       int // anchor position (click coords)
	minX       int // minimum X (e.g. chat history left edge)
	finalX     int // computed render position after clamping
	finalY     int
	boxW, boxH int
	width      int
	height     int
}

// NewMsgOptions creates an options popup at the given position.
// minX is the minimum left X (e.g. chat history left edge) — popup never starts left of this.
func NewMsgOptions(messageID, preview string, x, y, minX int) MsgOptionsModel {
	return MsgOptionsModel{
		messageID: messageID,
		preview:   preview,
		x:         x,
		y:         y,
		minX:      minX,
	}
}

func (m *MsgOptionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Render a sample box to measure exact dimensions.
	sample := m.renderBox()
	m.boxH = strings.Count(sample, "\n") + 1
	m.boxW = lipgloss.Width(sample)

	// X: start at minX (chat history left edge), or further right if click was further right.
	m.finalX = m.x
	if m.finalX < m.minX {
		m.finalX = m.minX
	}
	// Clamp right edge if it would overflow.
	if m.finalX+m.boxW > m.width {
		m.finalX = m.width - m.boxW - 1
	}
	if m.finalX < m.minX {
		m.finalX = m.minX
	}

	// Y: clamp to fit on screen.
	m.finalY = m.y
	if m.finalY+m.boxH > m.height {
		m.finalY = m.height - m.boxH - 1
	}
	if m.finalY < 0 {
		m.finalY = 0
	}
	debug.Log("[msgoptions] anchor=(%d,%d) minX=%d final=(%d,%d) box=(%d,%d) screen=(%d,%d)",
		m.x, m.y, m.minX, m.finalX, m.finalY, m.boxW, m.boxH, m.width, m.height)
}

// ClickInside returns true if the click is within the popup box (with a 1-cell buffer).
// Lenient buffer allows edge clicks on the border to still count as "inside".
func (m MsgOptionsModel) ClickInside(x, y int) bool {
	const buffer = 1
	return x >= m.finalX-buffer && x < m.finalX+m.boxW+buffer &&
		y >= m.finalY-buffer && y < m.finalY+m.boxH+buffer
}

// renderBox renders the popup contents (used by both View and SetSize).
func (m MsgOptionsModel) renderBox() string {
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

	return boxStyle.Render(b.String())
}

var msgOptionsItems = []struct {
	label  string
	action MsgOptionsAction
}{
	{"React", MsgActionReact},
	{"Reply", MsgActionReply},
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
			// Box layout from top: border(1) + title(1) + items + blank(1) + hint(1) + border(1)
			// Items start at finalY + 2.
			row := msg.Y - m.finalY - 2
			debug.Log("[msgoptions] click at (%d,%d) finalY=%d row=%d items=%d", msg.X, msg.Y, m.finalY, row, len(msgOptionsItems))
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

// ansiTruncatePad returns s truncated or padded to exactly visualW visible columns,
// preserving ANSI escape sequences.
func ansiTruncatePad(s string, visualW int) string {
	if visualW <= 0 {
		return ""
	}
	var out strings.Builder
	visiblePos := 0
	inEsc := false

	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			out.WriteRune(r)
			continue
		}
		if inEsc {
			out.WriteRune(r)
			// CSI sequences end with a letter (typically 'm' for SGR).
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if visiblePos >= visualW {
			break
		}
		out.WriteRune(r)
		visiblePos++ // assume single-cell ASCII; close enough for chat content
	}
	// Reset ANSI before padding to prevent bg color bleed.
	out.WriteString("\x1b[0m")
	for visiblePos < visualW {
		out.WriteRune(' ')
		visiblePos++
	}
	return out.String()
}
