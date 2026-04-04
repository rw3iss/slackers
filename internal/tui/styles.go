package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	ColorPrimary   = lipgloss.Color("12")  // blue
	ColorSecondary = lipgloss.Color("243") // gray
	ColorAccent    = lipgloss.Color("10")  // green
	ColorError     = lipgloss.Color("9")   // red
	ColorMuted     = lipgloss.Color("240") // dark gray
	ColorHighlight = lipgloss.Color("229") // yellow

	// Sidebar
	SidebarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(0, 1)

	SidebarActiveStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(0, 1)

	ChannelItemStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ChannelSelectedStyle = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	ChannelUnreadStyle   = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	SectionHeaderStyle   = lipgloss.NewStyle().Foreground(ColorMuted).Bold(true).MarginTop(1)

	// Messages
	MessagePaneStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorSecondary).
				Padding(0, 1)

	MessagePaneActiveStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(0, 1)

	UserNameStyle    = lipgloss.NewStyle().Bold(true)
	TimestampStyle   = lipgloss.NewStyle().Foreground(ColorMuted)
	MessageTextStyle = lipgloss.NewStyle()

	// Input
	InputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(0, 1)

	InputActiveStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorAccent).
				Padding(0, 1)

	// Status bar
	StatusBarStyle     = lipgloss.NewStyle().Foreground(ColorMuted)
	StatusConnected    = lipgloss.NewStyle().Foreground(ColorAccent)
	StatusDisconnected = lipgloss.NewStyle().Foreground(ColorError)

	// Help
	HelpStyle = lipgloss.NewStyle().Foreground(ColorMuted)
)

// UserColors assigns a consistent color to a username by hashing.
var userColors = []lipgloss.Color{
	lipgloss.Color("1"), lipgloss.Color("2"), lipgloss.Color("3"),
	lipgloss.Color("4"), lipgloss.Color("5"), lipgloss.Color("6"),
	lipgloss.Color("9"), lipgloss.Color("10"), lipgloss.Color("11"),
	lipgloss.Color("12"), lipgloss.Color("13"), lipgloss.Color("14"),
}

func UserColor(name string) lipgloss.Color {
	h := 0
	for _, c := range name {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return userColors[h%len(userColors)]
}
