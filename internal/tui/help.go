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
			{"Esc", "Toggle between sidebar and input"},
			{"Up / Down / k / j", "Navigate channels or scroll messages"},
			{"Up / Down (input)", "Browse sent message history"},
			{"PgUp / PgDn", "Scroll messages by page"},
			{"Ctrl-U / Ctrl-D", "Half-page scroll"},
			{"Home / End", "Jump to top / bottom of messages"},
		},
	},
	{
		title: "Messages & Files",
		items: []struct{ key, desc string }{
			{"Enter", "Select channel (sidebar) or send message (input)"},
			{"i  or  /", "Focus the message input"},
			{"Ctrl-F", "Search messages (Tab toggles scope)"},
			{"Ctrl-U", "Attach file to send (opens file browser)"},
			{"f (messages)", "Toggle file select mode"},
			{"Ctrl-Up", "Enter file select mode from anywhere"},
			{"Ctrl-Down", "Exit file select, focus input"},
			{"Ctrl-L", "Browse all files across channels"},
		},
	},
	{
		title: "Channels",
		items: []struct{ key, desc string }{
			{"Ctrl-K", "Search and jump to a channel"},
			{"Ctrl-N", "Jump to next unread channel"},
			{"Ctrl-R", "Refresh channel list"},
			{"Ctrl-X", "Hide selected channel"},
			{"Ctrl-G", "View and unhide hidden channels"},
			{"Ctrl-O", "Toggle hidden channels visible"},
			{"Ctrl-A", "Rename/alias selected channel"},
			{"Enter / Space", "Collapse/expand channel group"},
		},
	},
	{
		title: "Mouse (enable in settings)",
		items: []struct{ key, desc string }{
			{"Click", "Focus panel, select channel, download file"},
			{"Scroll wheel", "Scroll messages or channels"},
			{"Ctrl/Shift+scroll", "Fast scroll (5x)"},
			{"Shift+click", "Select text (bypass mouse capture)"},
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
	boxWidth := min(85, width-4) - 8 // account for border + padding

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Width(boxWidth).
		Align(lipgloss.Center)

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
		Width(min(85, width-4)).
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
