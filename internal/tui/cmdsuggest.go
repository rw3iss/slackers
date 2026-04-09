package tui

// Slash-command suggestion popup. Floats directly above the input
// bar while the user is typing a /command. Renders the top N
// fuzzy matches with name + description in two aligned columns.
//
// Up/Down navigates, Tab completes the highlighted entry into the
// input, Enter submits, Esc dismisses. The popup is purely a view
// — the model owns the registry, the input parsing, and the
// dispatch.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/commands"
)

// CmdSuggestModel is the suggestion popup state.
type CmdSuggestModel struct {
	matches  []*commands.Command
	selected int
	width    int  // overall popup width
	maxRows  int  // hard cap on visible suggestions
	visible  bool // whether to render
}

// NewCmdSuggest builds an empty suggestion popup. Call SetMatches
// before View to populate it.
func NewCmdSuggest() CmdSuggestModel {
	return CmdSuggestModel{
		maxRows: 8,
	}
}

// SetMatches replaces the displayed matches and clamps the
// cursor. Calls with an empty slice hide the popup.
func (s *CmdSuggestModel) SetMatches(matches []*commands.Command) {
	s.matches = matches
	if s.selected >= len(matches) {
		s.selected = len(matches) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
	s.visible = len(matches) > 0
}

// Visible reports whether the popup currently has matches to show.
func (s *CmdSuggestModel) Visible() bool { return s.visible && len(s.matches) > 0 }

// Hide explicitly dismisses the popup (used on Esc / send / blur).
func (s *CmdSuggestModel) Hide() {
	s.visible = false
	s.selected = 0
}

// SetWidth tells the popup how wide it can render. Usually set to
// match the input bar width minus the side borders.
func (s *CmdSuggestModel) SetWidth(w int) {
	s.width = w
}

// SetMaxRows caps how many suggestions are visible at once.
func (s *CmdSuggestModel) SetMaxRows(n int) {
	if n < 1 {
		n = 1
	}
	s.maxRows = n
}

// Selected returns the highlighted command, or nil if there are
// no matches.
func (s *CmdSuggestModel) Selected() *commands.Command {
	if s.selected < 0 || s.selected >= len(s.matches) {
		return nil
	}
	return s.matches[s.selected]
}

// Move cycles the selection by delta with wrap-around.
func (s *CmdSuggestModel) Move(delta int) {
	if len(s.matches) == 0 {
		return
	}
	s.selected = (s.selected + delta) % len(s.matches)
	if s.selected < 0 {
		s.selected += len(s.matches)
	}
}

// Height returns the number of terminal rows the popup will
// occupy when rendered (including the rounded border). Used by the
// host to reserve space above the input bar.
func (s *CmdSuggestModel) Height() int {
	if !s.Visible() {
		return 0
	}
	rows := len(s.matches)
	if rows > s.maxRows {
		rows = s.maxRows
	}
	return rows + 2 // top + bottom border
}

// View renders the popup as a bordered box. Each row is "/name"
// + padding + description, with the highlighted row inverted.
func (s *CmdSuggestModel) View() string {
	if !s.Visible() {
		return ""
	}
	maxNameW := 0
	for _, c := range s.matches {
		if l := len(c.FullName()); l > maxNameW {
			maxNameW = l
		}
	}
	// Reserve at least a few cells of gutter so descriptions
	// breathe even when the longest name is short.
	maxNameW += 2

	idle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	desc := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	cursor := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Reverse(true)

	// Window the visible slice around the cursor so the
	// highlighted row stays in view when there are more matches
	// than the popup can show at once.
	start := 0
	end := len(s.matches)
	if end > s.maxRows {
		start = s.selected - s.maxRows/2
		if start < 0 {
			start = 0
		}
		end = start + s.maxRows
		if end > len(s.matches) {
			end = len(s.matches)
			start = end - s.maxRows
		}
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		c := s.matches[i]
		name := c.FullName()
		padded := name + strings.Repeat(" ", maxNameW-len(name))
		var line string
		if i == s.selected {
			line = cursor.Render("▶ "+padded) + "  " + desc.Render(c.Description)
		} else {
			line = "  " + idle.Render(padded) + "  " + desc.Render(c.Description)
		}
		if s.width > 4 && lipgloss.Width(line) > s.width-4 {
			line = ansiTruncatePad(line, s.width-4)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1)
	if s.width > 4 {
		box = box.Width(s.width - 2)
	}
	return box.Render(strings.TrimRight(b.String(), "\n"))
}

// FormatRow is a public helper for rendering a single command line
// in the same style as the popup. Used by the Command List view to
// keep the visual presentation consistent.
func FormatRow(name, description string, width, nameW int) string {
	idle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	desc := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	padded := name + strings.Repeat(" ", nameW-len(name))
	line := "  " + idle.Render(padded) + "  " + desc.Render(description)
	if width > 4 && lipgloss.Width(line) > width-4 {
		line = ansiTruncatePad(line, width-4)
	}
	return line
}

// describe is unused at the moment but exported as a convenience
// for any future host that wants to format an arbitrary command
// pair without going through SetMatches.
func describe(c *commands.Command) string {
	if c == nil {
		return ""
	}
	if c.Usage != "" {
		return fmt.Sprintf("%s — %s", c.Description, c.Usage)
	}
	return c.Description
}
