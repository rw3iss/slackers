package tui

import (
	"fmt"
	"strings"

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
			{"befriend", "Send friend request to current DM user", ""},
			{"emoji_picker", "Open emoji picker (insert emoji)", ""},
			{"react_message", "React to a message (messages focused)", ""},
			{"quit", "Quit", ""},
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
	lines        []string // cached rendered lines
	width        int
	height       int
	version      string
}

// NewHelpModel creates a new help overlay model.
func NewHelpModel(version string) HelpModel {
	return HelpModel{version: version}
}

// BuildLines generates the help content lines from the current shortcut map.
func (m *HelpModel) BuildLines(sm shortcuts.ShortcutMap) {
	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Width(24)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var lines []string
	for si, section := range helpLayout {
		if si > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sectionTitleStyle.Render(section.title))
		for _, entry := range section.entries {
			var keyText string
			if entry.action != "" {
				keyText = formatKeys(sm, entry.action)
			} else {
				keyText = entry.extra
			}
			lines = append(lines, "  "+keyStyle.Render(keyText)+descStyle.Render(entry.desc))
		}
	}

	m.lines = lines
	m.totalLines = len(lines)
}

// SetSize sets the overlay dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.visibleLines = h - 4 - 11
	if m.visibleLines < 3 {
		m.visibleLines = 3
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
		switch msg.String() {
		case "up", "k":
			m.scrollOffset--
		case "down", "j":
			m.scrollOffset++
		case "pgup":
			m.scrollOffset -= m.visibleLines
		case "pgdown":
			m.scrollOffset += m.visibleLines
		case "home":
			m.scrollOffset = 0
		case "end":
			m.scrollOffset = m.maxScroll()
		}
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

// View renders the scrollable help overlay.
func (m *HelpModel) View() string {
	boxWidth := min(85, m.width-4) - 8

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

	b.WriteString(strings.Join(visible, "\n"))

	b.WriteString("\n\n")
	if m.maxScroll() > 0 {
		scrollInfo := dimStyle.Render(fmt.Sprintf("  [%d/%d] ", m.scrollOffset+1, m.maxScroll()+1))
		b.WriteString(scrollInfo)
	}
	b.WriteString(dimStyle.Render("Arrow keys/scroll to navigate | Esc or Ctrl-H to close"))

	content := b.String()

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(85, m.width-4)).
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
