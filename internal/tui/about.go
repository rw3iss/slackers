package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AboutOpenMsg signals that the About overlay should be displayed.
type AboutOpenMsg struct{}

// AboutCloseMsg signals that the About overlay should be dismissed.
type AboutCloseMsg struct{}

// AboutModel renders an "About" overlay with version + credits.
type AboutModel struct {
	width   int
	height  int
	version string
}

// NewAboutModel constructs a new About overlay for the given version.
func NewAboutModel(version string) AboutModel {
	return AboutModel{version: version}
}

// SetSize sets the screen dimensions.
func (m *AboutModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles input. Esc / Enter / q dismisses the overlay.
func (m AboutModel) Update(msg tea.Msg) (AboutModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "enter", "q":
			return m, func() tea.Msg { return AboutCloseMsg{} }
		}
	}
	return m, nil
}

// View renders the About panel centered on the screen.
func (m AboutModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	linkStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	hintStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Slackers %s", m.version)))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("Designed by Ryan Weiss "))
	b.WriteString(linkStyle.Render("(https://ryanweiss.net)"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Developed by Claude "))
	b.WriteString(linkStyle.Render("(https://claude.ai)"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("GitHub: "))
	b.WriteString(linkStyle.Render("https://github.com/rw3iss/slackers"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Donate: "))
	b.WriteString(linkStyle.Render("https://buymeacoffee.com/ttv1xp6yaj"))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("Esc to close"))

	// Center each line within the box and apply a rounded border.
	contentLines := strings.Split(b.String(), "\n")
	maxW := 0
	for _, line := range contentLines {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	centered := make([]string, len(contentLines))
	for i, line := range contentLines {
		pad := (maxW - lipgloss.Width(line)) / 2
		centered[i] = strings.Repeat(" ", pad) + line
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 4).
		Render(strings.Join(centered, "\n"))

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
