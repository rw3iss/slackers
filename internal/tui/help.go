package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var helpSections = []struct {
	title string
	items []struct{ key, desc string }
}{
	{
		title: "Navigation",
		items: []struct{ key, desc string }{
			{"Tab / Shift-Tab", "Cycle focus between panels"},
			{"Up / Down / k / j", "Navigate channels or scroll messages"},
			{"PgUp / PgDn", "Scroll messages by page"},
			{"Ctrl-U / Ctrl-D", "Half-page scroll"},
			{"Home / End", "Jump to top / bottom of messages"},
		},
	},
	{
		title: "Actions",
		items: []struct{ key, desc string }{
			{"Enter", "Select channel (sidebar) or send message (input)"},
			{"i  or  /", "Focus the message input"},
			{"Esc", "Cancel input / return to sidebar"},
			{"Ctrl-R", "Refresh channel list"},
			{"Ctrl-K", "Search and jump to a channel"},
			{"Ctrl-X", "Hide selected channel from sidebar"},
			{"Ctrl-G", "View and unhide hidden channels"},
			{"Ctrl-O", "Toggle hidden channels visible in sidebar"},
			{"Ctrl-A", "Rename/alias selected channel"},
		},
	},
	{
		title: "App",
		items: []struct{ key, desc string }{
			{"Ctrl-H", "Toggle this help page"},
			{"Ctrl-S", "Open settings"},
			{"Ctrl-Q / Ctrl-C", "Quit"},
		},
	},
}

func renderHelp(width, height int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Width(24)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Slackers Help"))
	b.WriteString("\n\n")

	for _, section := range helpSections {
		b.WriteString(sectionTitleStyle.Render(section.title))
		b.WriteString("\n")
		for _, item := range section.items {
			b.WriteString("  ")
			b.WriteString(keyStyle.Render(item.key))
			b.WriteString(descStyle.Render(item.desc))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Press Esc or Ctrl-H to close"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(70, width-4)).
		MaxHeight(height - 4)

	box := boxStyle.Render(content)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		box)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
