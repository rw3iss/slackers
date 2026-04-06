package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
			{"Ctrl-D", "Cancel file download"},
			{"Ctrl-\\", "Toggle input mode (normal/edit)"},
			{"Alt-Enter", "New line (normal) or send (edit)"},
			{"Shift-Enter", "Insert new line (both modes)"},
			{"Ctrl-W", "Toggle full screen chat mode"},
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

// HelpModel holds state for the scrollable help overlay.
type HelpModel struct {
	scrollOffset int
	totalLines   int
	visibleLines int
	width        int
	height       int
	version      string
}

// NewHelpModel creates a new help overlay model.
func NewHelpModel(version string) HelpModel {
	return HelpModel{version: version}
}

// SetSize sets the overlay dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Box uses: 2 border + 2 padding + 4 header lines + 3 footer lines = 11
	m.visibleLines = h - 4 - 11
	if m.visibleLines < 3 {
		m.visibleLines = 3
	}
}

// Update handles key and mouse events for the help overlay.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if m.scrollOffset < m.totalLines-m.visibleLines {
				m.scrollOffset++
			}
		case "pgup":
			m.scrollOffset -= m.visibleLines
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		case "pgdown":
			m.scrollOffset += m.visibleLines
			maxScroll := m.totalLines - m.visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
		case "home":
			m.scrollOffset = 0
		case "end":
			maxScroll := m.totalLines - m.visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			m.scrollOffset = maxScroll
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollOffset -= 3
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		case tea.MouseButtonWheelDown:
			m.scrollOffset += 3
			maxScroll := m.totalLines - m.visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
		}
	}
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

	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Width(24)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	versionStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Width(boxWidth).
		Align(lipgloss.Center)

	// Build all lines.
	var lines []string
	for si, section := range helpSections {
		if si > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sectionTitleStyle.Render(section.title))
		for _, item := range section.items {
			lines = append(lines, "  "+keyStyle.Render(item.key)+descStyle.Render(item.desc))
		}
	}

	m.totalLines = len(lines)

	// Apply scroll window.
	maxScroll := m.totalLines - m.visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}

	start := m.scrollOffset
	end := start + m.visibleLines
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[start:end]

	// Build final content.
	var b strings.Builder
	b.WriteString(titleStyle.Render("Slackers Help"))
	b.WriteString("\n")
	b.WriteString(versionStyle.Render("(v" + m.version + ")"))
	b.WriteString("\n\n")

	b.WriteString(strings.Join(visible, "\n"))

	b.WriteString("\n\n")
	if maxScroll > 0 {
		scrollInfo := dimStyle.Render(fmt.Sprintf("  [%d/%d] ", m.scrollOffset+1, maxScroll+1))
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
