package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rw3iss/slackers/internal/debug"
)

// MsgOptionsAction represents a chosen action from the options menu.
type MsgOptionsAction int

const (
	MsgActionNone MsgOptionsAction = iota
	MsgActionReact
	MsgActionReply
	MsgActionEdit
	MsgActionDelete
	MsgActionCopy
	MsgActionViewFile
)

// MsgOptionsSelectMsg signals which option the user chose.
type MsgOptionsSelectMsg struct {
	Action    MsgOptionsAction
	MessageID string
	Preview   string
}

// msgOptionsItem is a single menu entry in the popup.
type msgOptionsItem struct {
	label  string
	action MsgOptionsAction
}

// MsgOptionsModel is a small popup menu shown next to a clicked message.
type MsgOptionsModel struct {
	messageID  string
	preview    string
	items      []msgOptionsItem
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
// allowAuthorActions controls whether the author-only "Edit" and "Delete"
// entries are appended to the menu. hasFiles adds a "View File" entry.
func NewMsgOptions(messageID, preview string, x, y, minX int, allowAuthorActions, hasFiles bool) MsgOptionsModel {
	items := []msgOptionsItem{
		{"React", MsgActionReact},
		{"Reply", MsgActionReply},
	}
	if allowAuthorActions {
		items = append(items,
			msgOptionsItem{"Edit", MsgActionEdit},
		)
	}
	// Copy Message is available regardless of author — any user
	// can quote or archive any message they can see. Placed
	// after Edit (when visible) so the author-only actions
	// cluster together at the top.
	items = append(items, msgOptionsItem{"Copy Message", MsgActionCopy})
	if hasFiles {
		items = append(items, msgOptionsItem{"View File", MsgActionViewFile})
	}
	if allowAuthorActions {
		items = append(items, msgOptionsItem{"Delete", MsgActionDelete})
	}
	return MsgOptionsModel{
		messageID: messageID,
		preview:   preview,
		items:     items,
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
	for i, item := range m.items {
		// Blank line before each item.
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

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Width(18)

	return boxStyle.Render(b.String())
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
			if m.selected < len(m.items)-1 {
				m.selected++
			}
		case "enter":
			item := m.items[m.selected]
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
			// Items each span 2 rows (blank line before each).
			delta := msg.Y - m.finalY - 2
			row := delta / 2
			debug.Log("[msgoptions] click at (%d,%d) finalY=%d row=%d items=%d", msg.X, msg.Y, m.finalY, row, len(m.items))
			if row >= 0 && row < len(m.items) {
				m.selected = row
				item := m.items[row]
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
		lineW := lipgloss.Width(line)
		bgLines[row] = ansiTruncatePad(bgLines[row], posX) + line + ansiAfterCells(bgLines[row], posX+lineW)
	}

	return strings.Join(bgLines, "\n")
}

// ansiTruncatePad returns s truncated or padded to exactly visualW
// visible *terminal cells*, preserving ANSI escape sequences and
// accounting for double-width runes (emoji, CJK). This is what the
// popup overlay uses to build a line-by-line background slice: the
// popup's left border is placed at column visualW, so every visible
// column before it needs to be correct to the cell, not the rune.
//
// Earlier versions of this function counted every rune as 1 cell,
// which caused the popup border to drift right of its intended
// column whenever emojis (or any 2-cell rune) appeared on the same
// row — the emoji took 2 cells on screen but was counted as 1, so
// the truncation stopped too late and the padding arithmetic was
// off by one per emoji. The fix is to consult go-runewidth (already
// used transitively by lipgloss) for the actual cell width.
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
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// Zero-width combining mark / variation selector / ZWJ —
			// emit it but don't advance the column counter. It attaches
			// to the previous rune's cell.
			out.WriteRune(r)
			continue
		}
		if visiblePos+w > visualW {
			// Including this rune would overflow the requested width.
			// Stop here and let the padding loop below fill the
			// remaining cell(s) with spaces. This also handles the
			// case where the cursor lands on the second cell of a
			// 2-wide rune — we drop the rune entirely and pad with a
			// space so the overlay border sits cleanly.
			break
		}
		out.WriteRune(r)
		visiblePos += w
	}
	// Reset ANSI before padding to prevent bg color bleed.
	out.WriteString("\x1b[0m")
	for visiblePos < visualW {
		out.WriteRune(' ')
		visiblePos++
	}
	return out.String()
}

// ansiAfterCells returns the portion of s that starts at visual column
// startCol, prepended with an ANSI reset so inherited colour from the
// skipped prefix doesn't leak into the returned segment. Used by popup
// overlay renderers to restore background content to the right of the
// popup box.
func ansiAfterCells(s string, startCol int) string {
	if startCol <= 0 {
		return s
	}
	visPos := 0
	inEsc := false
	for i, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if visPos >= startCol {
			return "\x1b[0m" + s[i:]
		}
		visPos += w
	}
	return ""
}
