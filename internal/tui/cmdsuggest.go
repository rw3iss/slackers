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
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/commands"
)

// CmdSuggestModel is the suggestion popup state.
//
// The popup has two modes:
//
//  1. Command mode — renders matches[] (slice of *Command). The
//     user is still typing the command name; Tab completes into
//     the input, Enter runs the highlighted command directly.
//
//  2. Arg mode — renders argMatches[] (slice of argCandidate).
//     The user has typed `/cmd <space> …` and we're offering
//     sub-argument completions based on the command's
//     ArgSpec.Kind. Tab completes the selected arg into the
//     input (keeping the command prefix intact), Enter runs.
//
// Which mode is active is determined by refreshCmdSuggest on
// every keystroke. Only one of matches / argMatches is populated
// at a time.
type CmdSuggestModel struct {
	matches    []*commands.Command
	argMatches []argCandidate
	argMode    bool   // true → render argMatches instead of matches
	argPrefix  string // "/cmd " — what to prepend when Tab-completing an arg
	selected   int
	width      int  // overall popup width
	maxRows    int  // hard cap on visible suggestions
	visible    bool // whether to render
}

// NewCmdSuggest builds an empty suggestion popup. Call SetMatches
// before View to populate it.
func NewCmdSuggest() CmdSuggestModel {
	return CmdSuggestModel{
		maxRows: 8,
	}
}

// SetMatches replaces the displayed matches and clamps the
// cursor. Calls with an empty slice hide the popup. Always
// switches the popup back into command mode; callers that want
// arg mode should use SetArgMatches instead.
func (s *CmdSuggestModel) SetMatches(matches []*commands.Command) {
	s.matches = matches
	s.argMatches = nil
	s.argMode = false
	s.argPrefix = ""
	if s.selected >= len(matches) {
		s.selected = len(matches) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
	s.visible = len(matches) > 0
}

// SetArgMatches puts the popup into arg-completion mode,
// rendering the given candidate list instead of commands.
// prefix is the "/cmd " string that should be prepended to the
// highlighted candidate when the user Tab-completes it into the
// input bar.
func (s *CmdSuggestModel) SetArgMatches(matches []argCandidate, prefix string) {
	s.argMatches = matches
	s.matches = nil
	s.argMode = true
	s.argPrefix = prefix
	if s.selected >= len(matches) {
		s.selected = len(matches) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
	s.visible = len(matches) > 0
}

// SelectedArg returns the currently highlighted arg candidate,
// or nil if the popup isn't in arg mode or has no matches.
func (s *CmdSuggestModel) SelectedArg() *argCandidate {
	if !s.argMode || s.selected < 0 || s.selected >= len(s.argMatches) {
		return nil
	}
	c := s.argMatches[s.selected]
	return &c
}

// ArgPrefix returns the "/cmd " string that should be prepended
// to a Tab-completed arg value.
func (s *CmdSuggestModel) ArgPrefix() string { return s.argPrefix }

// InArgMode reports whether the popup is currently showing arg
// completions instead of command completions.
func (s *CmdSuggestModel) InArgMode() bool { return s.argMode }

// Visible reports whether the popup currently has anything to
// show in either command or arg mode.
func (s *CmdSuggestModel) Visible() bool { return s.visible && s.currentLen() > 0 }

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
// no matches or the popup is in arg mode.
func (s *CmdSuggestModel) Selected() *commands.Command {
	if s.argMode {
		return nil
	}
	if s.selected < 0 || s.selected >= len(s.matches) {
		return nil
	}
	return s.matches[s.selected]
}

// currentLen returns the length of whichever candidate slice is
// active (commands or arg candidates).
func (s *CmdSuggestModel) currentLen() int {
	if s.argMode {
		return len(s.argMatches)
	}
	return len(s.matches)
}

// Move cycles the selection by delta with wrap-around. Works
// against whichever candidate slice is active (commands or args).
func (s *CmdSuggestModel) Move(delta int) {
	n := s.currentLen()
	if n == 0 {
		return
	}
	s.selected = (s.selected + delta) % n
	if s.selected < 0 {
		s.selected += n
	}
}

// Height returns the number of terminal rows the popup will
// occupy when rendered (including the rounded border). Used by the
// host to reserve space above the input bar.
func (s *CmdSuggestModel) Height() int {
	if !s.Visible() {
		return 0
	}
	rows := s.currentLen()
	if rows > s.maxRows {
		rows = s.maxRows
	}
	return rows + 2 // top + bottom border
}

// View renders the popup as a bordered box. Each row is "name"
// + padding + description, with the highlighted row inverted.
// Command mode shows `/cmd`; arg mode shows the raw arg name
// plus a per-kind description.
func (s *CmdSuggestModel) View() string {
	if !s.Visible() {
		return ""
	}

	// Build the (name, description) rows for whichever mode is
	// active so the rendering loop is uniform.
	type row struct {
		name string
		desc string
	}
	var rows []row
	if s.argMode {
		rows = make([]row, len(s.argMatches))
		for i, c := range s.argMatches {
			rows[i] = row{name: c.Name, desc: c.Description}
		}
	} else {
		rows = make([]row, len(s.matches))
		for i, c := range s.matches {
			rows[i] = row{name: c.FullName(), desc: c.Description}
		}
	}

	maxNameW := 0
	for _, r := range rows {
		if l := len(r.name); l > maxNameW {
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
	end := len(rows)
	if end > s.maxRows {
		start = s.selected - s.maxRows/2
		if start < 0 {
			start = 0
		}
		end = start + s.maxRows
		if end > len(rows) {
			end = len(rows)
			start = end - s.maxRows
		}
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		r := rows[i]
		padded := r.name + strings.Repeat(" ", maxNameW-len(r.name))
		var line string
		if i == s.selected {
			line = cursor.Render("▶ "+padded) + "  " + desc.Render(r.desc)
		} else {
			line = "  " + idle.Render(padded) + "  " + desc.Render(r.desc)
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
