package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/shortcuts"
)

// helpEntry maps a shortcut action to its display description.
type helpEntry struct {
	action string // key in ShortcutMap (e.g. "quit")
	desc   string
	extra  string // optional hardcoded key text appended (for non-shortcut keys)
}

// helpSection defines a group of help entries.
type helpSection struct {
	title   string
	entries []helpEntry
}

// helpLayout defines all sections and their entries.
// The action field maps to the ShortcutMap key; the actual binding
// is resolved at generation time so user overrides are reflected.
var helpLayout = []helpSection{
	{
		title: "Navigation",
		entries: []helpEntry{
			{"tab", "Cycle focus (forward)", ""},
			{"shift_tab", "Cycle focus (backward)", ""},
			{"escape", "Toggle between sidebar and input", ""},
			{"up", "Navigate / scroll up", ""},
			{"down", "Navigate / scroll down", ""},
			{"page_up", "Scroll messages by page", ""},
			{"page_down", "Scroll messages by page", ""},
			{"half_page_up", "Half-page scroll (messages focused)", ""},
			{"half_page_down", "Half-page scroll (messages focused)", ""},
			{"home", "Jump to top", ""},
			{"end", "Jump to bottom", ""},
		},
	},
	{
		title: "Messages & Files",
		entries: []helpEntry{
			{"enter", "Select channel or send message", ""},
			{"focus_input", "Focus the message input", ""},
			{"search_messages", "Search messages (Tab toggles scope)", ""},
			{"attach_file", "Attach file to send", ""},
			{"toggle_file_select", "Toggle file select mode (messages)", ""},
			{"enter_file_select", "Enter file select from anywhere", ""},
			{"focus_input_global", "Exit file select, focus input", ""},
			{"files_list", "Browse all files across channels", ""},
			{"cancel_download", "Cancel file download", ""},
			{"toggle_input_mode", "Toggle input mode (normal/edit)", ""},
			{"", "New line (normal) or send (edit)", "Alt-Enter"},
			{"", "Insert new line (both modes)", "Shift-Enter"},
			{"toggle_full_mode", "Toggle full screen chat mode", ""},
		},
	},
	{
		title: "Channels",
		entries: []helpEntry{
			{"search_channels", "Search and jump to a channel", ""},
			{"next_unread", "Jump to next unread channel", ""},
			{"refresh", "Refresh channel list", ""},
			{"hide_channel", "Hide selected channel", ""},
			{"show_hidden", "View and unhide hidden channels", ""},
			{"toggle_hidden", "Toggle hidden channels visible", ""},
			{"rename_group", "Rename/alias selected channel", ""},
			{"sidebar_collapse", "Collapse/expand channel group", ""},
		},
	},
	{
		title: "Mouse (enable in settings)",
		entries: []helpEntry{
			{"", "Focus panel, select channel, download file", "Click"},
			{"", "Scroll messages or channels", "Scroll wheel"},
			{"", "Fast scroll (5x)", "Ctrl/Shift+scroll"},
			{"", "Select text (bypass mouse capture)", "Shift+click"},
		},
	},
	{
		title: "App",
		entries: []helpEntry{
			{"help", "Toggle this help page", ""},
			{"settings", "Open settings", ""},
			{"notifications", "Open the notifications view (unread, reactions, friend requests)", ""},
			{"befriend", "Send friend request to current DM user", ""},
			{"emoji_picker", "Open emoji picker (insert emoji)", ""},
			{"select_message", "Message select mode (also: s in chat, ↑/↓ in chat)", ""},
			{"toggle_theme", "Swap primary ↔ alternate theme", ""},
			{"quit", "Quit", ""},
		},
	},
	{
		title: "Friend Chats (P2P)",
		entries: []helpEntry{
			{"friend_details", "Open friend config for the current friend chat", ""},
			{"share_my_info", "Insert your contact card ([FRIEND:me]) into the input", ""},
			{"", "Click cog in upper-right of friend chat header", "Mouse"},
			{"", "Cancel an in-flight file upload (file select mode)", "c"},
			{"", "Add highlighted folder to favorites (file browser)", "f"},
		},
	},
}

// formatKeys returns a display string for a shortcut's key bindings.
func formatKeys(sm shortcuts.ShortcutMap, action string) string {
	keys := shortcuts.KeysForAction(sm, action)
	if len(keys) == 0 {
		return "???"
	}
	// Format each key for display.
	display := make([]string, len(keys))
	for i, k := range keys {
		display[i] = formatKeyName(k)
	}
	return strings.Join(display, " / ")
}

// formatKeyName converts a shortcut key string to a readable display name.
func formatKeyName(k string) string {
	k = strings.ReplaceAll(k, "ctrl+", "Ctrl-")
	k = strings.ReplaceAll(k, "shift+", "Shift-")
	k = strings.ReplaceAll(k, "alt+", "Alt-")
	switch k {
	case "tab":
		return "Tab"
	case "Shift-tab":
		return "Shift-Tab"
	case "enter":
		return "Enter"
	case "esc":
		return "Esc"
	case "pgup":
		return "PgUp"
	case "pgdown":
		return "PgDn"
	case "home":
		return "Home"
	case "end":
		return "End"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case " ":
		return "Space"
	}
	return k
}

// HelpModel holds state for the scrollable help overlay.
type HelpModel struct {
	scrollOffset int
	totalLines   int
	visibleLines int
	rawEntries   []helpRow // logical rows with raw text for filtering
	lines        []string  // current filtered+highlighted rendered lines
	width        int
	height       int
	version      string
	search       textinput.Model
	sm           shortcuts.ShortcutMap
}

// helpRow is one logical row in the help layout — either a section
// title or a key/desc entry. raw is the plain text used for filter
// matching (case-insensitive). rendered is the styled version (with
// search-match highlight applied) used for display.
type helpRow struct {
	title bool
	raw   string
	key   string // formatted shortcut keys (entries only)
	desc  string // description text (entries only)
}

// NewHelpModel creates a new help overlay model.
func NewHelpModel(version string) HelpModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter / highlight..."
	ti.CharLimit = 64
	ti.Prompt = " / "
	ti.Focus()
	return HelpModel{version: version, search: ti}
}

// BuildLines generates the help content rows from the current
// shortcut map. The rendered lines slice is then produced by
// rebuildLines based on the current search query.
func (m *HelpModel) BuildLines(sm shortcuts.ShortcutMap) {
	m.sm = sm
	m.rawEntries = nil
	for si, section := range helpLayout {
		if si > 0 {
			m.rawEntries = append(m.rawEntries, helpRow{}) // blank row
		}
		m.rawEntries = append(m.rawEntries, helpRow{title: true, raw: section.title})
		for _, entry := range section.entries {
			var keyText string
			if entry.action != "" {
				keyText = formatKeys(sm, entry.action)
			} else {
				keyText = entry.extra
			}
			m.rawEntries = append(m.rawEntries, helpRow{
				raw:  keyText + "  " + entry.desc,
				key:  keyText,
				desc: entry.desc,
			})
		}
	}
	m.rebuildLines()
}

// rebuildLines applies the current search filter + highlight to the
// raw row list and updates m.lines / m.totalLines. When the query is
// empty, every row is rendered as-is. When non-empty, only rows whose
// raw text contains the query (case-insensitive) are kept, and the
// matching substring is rendered with the contrast highlight style.
func (m *HelpModel) rebuildLines() {
	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	keyColor := lipgloss.Color("229")
	descColor := lipgloss.Color("252")
	keyStyle := lipgloss.NewStyle().Foreground(keyColor).Width(24)
	descStyle := lipgloss.NewStyle().Foreground(descColor)

	// Highlight color is computed from the description text colour so
	// matches always appear "more contrasted" than the surrounding
	// help text. brighterColor brightens by a fixed delta and falls
	// back to darkening when the channel is already at the top.
	hlFg := brighterColor(descColor)
	hlStyle := lipgloss.NewStyle().Foreground(hlFg).Bold(true).Underline(true)

	q := strings.ToLower(strings.TrimSpace(m.search.Value()))

	var lines []string
	for _, row := range m.rawEntries {
		switch {
		case row.raw == "" && !row.title:
			// blank row separator — only emit when not filtering or
			// surrounding lines also pass through
			if q == "" {
				lines = append(lines, "")
			}
		case row.title:
			if q == "" || strings.Contains(strings.ToLower(row.raw), q) {
				lines = append(lines, sectionTitleStyle.Render(highlightMatches(row.raw, q, hlStyle)))
			}
		default:
			matches := q != "" && strings.Contains(strings.ToLower(row.raw), q)
			if q != "" && !matches {
				continue
			}
			keyRendered := keyStyle.Render(highlightMatches(row.key, q, hlStyle))
			descRendered := descStyle.Render(highlightMatches(row.desc, q, hlStyle))
			lines = append(lines, "  "+keyRendered+descRendered)
		}
	}

	m.lines = lines
	m.totalLines = len(lines)
}

// highlightMatches wraps every case-insensitive occurrence of needle
// inside haystack with the highlight style. Returns the original
// haystack untouched when needle is empty.
func highlightMatches(haystack, needle string, hl lipgloss.Style) string {
	if needle == "" || haystack == "" {
		return haystack
	}
	lower := strings.ToLower(haystack)
	var b strings.Builder
	idx := 0
	for {
		i := strings.Index(lower[idx:], needle)
		if i < 0 {
			b.WriteString(haystack[idx:])
			break
		}
		i += idx
		b.WriteString(haystack[idx:i])
		b.WriteString(hl.Render(haystack[i : i+len(needle)]))
		idx = i + len(needle)
	}
	return b.String()
}

// brighterColor returns a colour that is visually more contrasted
// than the input. For #rrggbb hex inputs each channel is brightened
// by a fixed delta; if any channel would clip past 255 the function
// instead darkens by the same delta. Non-hex colours fall back to
// the theme's ColorHighlight.
func brighterColor(c lipgloss.Color) lipgloss.Color {
	s := string(c)
	if len(s) == 7 && s[0] == '#' {
		r, err1 := strconv.ParseInt(s[1:3], 16, 0)
		g, err2 := strconv.ParseInt(s[3:5], 16, 0)
		b, err3 := strconv.ParseInt(s[5:7], 16, 0)
		if err1 == nil && err2 == nil && err3 == nil {
			return shiftHex(r, g, b, 60)
		}
	}
	// Fall back to the theme's highlight colour for unparseable
	// inputs (named colours, 256-colour indices, etc).
	return ColorHighlight
}

func shiftHex(r, g, b, delta int64) lipgloss.Color {
	nr, ng, nb := r+delta, g+delta, b+delta
	if nr > 255 || ng > 255 || nb > 255 {
		nr, ng, nb = r-delta, g-delta, b-delta
	}
	clamp := func(v int64) int64 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", clamp(nr), clamp(ng), clamp(nb)))
}

// SetSize sets the overlay dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Reserve 2 extra rows for the search input + spacer below it.
	m.visibleLines = h - 4 - 8
	if m.visibleLines < 3 {
		m.visibleLines = 3
	}
	// Cap the search input width to fit inside the box.
	boxW := min(95, w-4) - 8
	if boxW > 4 {
		m.search.Width = boxW - 4
	}
}

func (m *HelpModel) maxScroll() int {
	ms := m.totalLines - m.visibleLines
	if ms < 0 {
		return 0
	}
	return ms
}

func (m *HelpModel) clampScroll() {
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	if m.scrollOffset > m.maxScroll() {
		m.scrollOffset = m.maxScroll()
	}
}

// Update handles key and mouse events for the help overlay.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Navigation keys are handled here directly so they keep
		// working while the search input has focus.
		switch msg.String() {
		case "up":
			m.scrollOffset--
			m.clampScroll()
			return m, nil
		case "down":
			m.scrollOffset++
			m.clampScroll()
			return m, nil
		case "pgup":
			m.scrollOffset -= m.visibleLines
			m.clampScroll()
			return m, nil
		case "pgdown":
			m.scrollOffset += m.visibleLines
			m.clampScroll()
			return m, nil
		case "home":
			m.scrollOffset = 0
			return m, nil
		case "end":
			m.scrollOffset = m.maxScroll()
			return m, nil
		}
		// Otherwise route the key to the search input (typing,
		// backspace, left/right cursor, etc).
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		// Re-render lines whenever the query may have changed.
		m.rebuildLines()
		m.clampScroll()
		return m, cmd
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollOffset -= 3
		case tea.MouseButtonWheelDown:
			m.scrollOffset += 3
		}
	}
	m.clampScroll()
	return m, nil
}

// SearchValue returns the current filter query.
func (m HelpModel) SearchValue() string {
	return m.search.Value()
}

// ClearSearch resets the search input.
func (m *HelpModel) ClearSearch() {
	m.search.SetValue("")
	m.rebuildLines()
}

// View renders the scrollable help overlay.
func (m *HelpModel) View() string {
	boxWidth := min(95, m.width-4) - 8

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Width(boxWidth).
		Align(lipgloss.Center)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	versionStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Width(boxWidth).
		Align(lipgloss.Center)

	// Apply scroll window.
	m.clampScroll()

	start := m.scrollOffset
	end := start + m.visibleLines
	if end > len(m.lines) {
		end = len(m.lines)
	}
	if start > len(m.lines) {
		start = len(m.lines)
	}
	visible := m.lines[start:end]

	// Build final content.
	var b strings.Builder
	b.WriteString(titleStyle.Render("Slackers Help"))
	b.WriteString("\n")
	b.WriteString(versionStyle.Render("(v" + m.version + ")"))
	b.WriteString("\n\n")

	// Search bar.
	b.WriteString(m.search.View())
	b.WriteString("\n\n")

	b.WriteString(strings.Join(visible, "\n"))

	b.WriteString("\n\n\n")
	b.WriteString(dimStyle.Render("  Type to filter · ↑/↓/PgUp/PgDn to scroll · Esc or Ctrl-H to close"))

	content := b.String()

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(95, m.width-4)).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
