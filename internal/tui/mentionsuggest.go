package tui

// @mention autocomplete popup. Floats above the input bar while
// the user is typing an @ trigger. Renders the top N matches with
// name + subtitle (Slack user / friend) in two aligned columns.
//
// Up/Down navigates, Tab/Enter inserts the wire-format token
// (<@U12345> or <@slacker:abc>) into the input at the trigger
// position, replacing whatever query the user typed after `@`.
// Esc dismisses and leaves the input untouched.
//
// Mirrors CmdSuggestModel — see cmdsuggest.go for the parallel
// pattern.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MentionEntry describes one candidate in the suggestion list.
// Source is presentation only ("user" / "friend") — the canonical
// id is what gets inserted into the input on Tab/Enter.
type MentionEntry struct {
	// ID is the canonical mention identifier — Slack "U..." or
	// "slacker:<SlackerID>". Inserted into the input on selection
	// as <@ID>.
	ID string
	// DisplayName is the rendered label shown in the list, also
	// used as the prefix-match key when filtering.
	DisplayName string
	// Subtitle is a short descriptor shown to the right of the
	// name — typically "user" / "friend" / "you".
	Subtitle string
	// IsFriend marks the entry as a P2P friend so the renderer
	// can colour it with the friend-online palette colour for a
	// quick visual disambiguation from Slack workspace users.
	IsFriend bool
}

// MentionSuggestModel is the state for the @mention popup.
type MentionSuggestModel struct {
	matches  []MentionEntry
	selected int
	width    int
	maxRows  int
	visible  bool
	// triggerStart is the byte index in the input value where the
	// `@` character lives. Set by the model's refreshMentionSuggest
	// helper when the popup is opened, used by the input to
	// compute the replacement region on Tab/Enter completion.
	triggerStart int
	// triggerEnd is one past the last byte of the in-progress
	// query (i.e. the cursor position when the popup was last
	// refreshed). Replacement spans [triggerStart, triggerEnd).
	triggerEnd int
}

// NewMentionSuggest builds an empty popup. Call SetMatches before
// View to populate it.
func NewMentionSuggest() MentionSuggestModel {
	return MentionSuggestModel{maxRows: 8}
}

// SetMatches replaces the displayed candidate list and clamps the
// cursor. An empty slice hides the popup.
func (s *MentionSuggestModel) SetMatches(matches []MentionEntry) {
	s.matches = matches
	if s.selected >= len(matches) {
		s.selected = len(matches) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
	s.visible = len(matches) > 0
}

// SetTrigger records the input-value byte range covered by the
// `@<query>` the user is typing so the input can splice the
// completion in cleanly on Tab/Enter.
func (s *MentionSuggestModel) SetTrigger(start, end int) {
	s.triggerStart = start
	s.triggerEnd = end
}

// TriggerStart returns the byte index of the `@` in the input.
func (s *MentionSuggestModel) TriggerStart() int { return s.triggerStart }

// TriggerEnd returns the byte index just past the in-progress query.
func (s *MentionSuggestModel) TriggerEnd() int { return s.triggerEnd }

// Visible reports whether the popup currently has anything to show.
func (s *MentionSuggestModel) Visible() bool { return s.visible && len(s.matches) > 0 }

// Hide explicitly dismisses the popup.
func (s *MentionSuggestModel) Hide() {
	s.visible = false
	s.selected = 0
}

// SetWidth tells the popup how wide it can render.
func (s *MentionSuggestModel) SetWidth(w int) { s.width = w }

// SetMaxRows caps how many suggestions are visible at once.
func (s *MentionSuggestModel) SetMaxRows(n int) {
	if n < 1 {
		n = 1
	}
	s.maxRows = n
}

// Selected returns the highlighted entry, or nil if there are no
// matches. The pointer is into the popup's slice — callers should
// dereference rather than retain the pointer past the next mutation.
func (s *MentionSuggestModel) Selected() *MentionEntry {
	if s.selected < 0 || s.selected >= len(s.matches) {
		return nil
	}
	e := s.matches[s.selected]
	return &e
}

// Move cycles the selection by delta with wrap-around.
func (s *MentionSuggestModel) Move(delta int) {
	n := len(s.matches)
	if n == 0 {
		return
	}
	s.selected = (s.selected + delta) % n
	if s.selected < 0 {
		s.selected += n
	}
}

// Height returns the number of terminal rows the popup will occupy
// when rendered (including the rounded border). Used by the host
// to reserve space above the input bar.
func (s *MentionSuggestModel) Height() int {
	if !s.Visible() {
		return 0
	}
	rows := len(s.matches)
	if rows > s.maxRows {
		rows = s.maxRows
	}
	return rows + 2 // top + bottom border
}

// View renders the popup as a bordered box with two-column rows
// (name + subtitle). The highlighted row is inverted; friend
// entries get the online-friend colour for the name to
// disambiguate them from Slack users.
func (s *MentionSuggestModel) View() string {
	if !s.Visible() {
		return ""
	}

	type row struct {
		name     string
		subtitle string
		friend   bool
	}
	rows := make([]row, len(s.matches))
	for i, m := range s.matches {
		rows[i] = row{name: "@" + m.DisplayName, subtitle: m.Subtitle, friend: m.IsFriend}
	}

	maxNameW := 0
	for _, r := range rows {
		if l := lipgloss.Width(r.name); l > maxNameW {
			maxNameW = l
		}
	}
	maxNameW += 2

	idleSlack := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	idleFriend := lipgloss.NewStyle().Foreground(ColorFriendOnline).Bold(true)
	desc := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	cursor := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Reverse(true)

	// Window the visible slice around the cursor.
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
		padded := r.name + strings.Repeat(" ", maxNameW-lipgloss.Width(r.name))
		var line string
		if i == s.selected {
			line = cursor.Render("▶ "+padded) + "  " + desc.Render(r.subtitle)
		} else {
			nameStyle := idleSlack
			if r.friend {
				nameStyle = idleFriend
			}
			line = "  " + nameStyle.Render(padded) + "  " + desc.Render(r.subtitle)
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

// rankMentionMatches returns up to `limit` candidates whose
// DisplayName starts with `query` (case-insensitive). The query is
// the text typed after the `@` trigger, with the leading `@`
// already stripped. An empty query returns the full pool.
func rankMentionMatches(query string, pool []MentionEntry, limit int) []MentionEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]MentionEntry, 0, limit)
	for _, e := range pool {
		if q != "" && !strings.HasPrefix(strings.ToLower(e.DisplayName), q) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out
}
