package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/theme"
)

// All exported style variables are mutated by ApplyTheme so the renderer
// can switch themes at runtime. Components should reference the package
// vars directly (e.g. ColorPrimary, MessagePaneStyle) at render time so
// any theme reapply is picked up immediately.
var (
	// Core palette colors — populated from the active theme.
	ColorPrimary   lipgloss.Color
	ColorSecondary lipgloss.Color
	ColorAccent    lipgloss.Color
	ColorError     lipgloss.Color
	ColorMuted     lipgloss.Color
	ColorHighlight lipgloss.Color

	// Semantic colors — populated from the active theme.
	ColorMessageText   lipgloss.Color
	ColorInfoText      lipgloss.Color
	ColorDayLabel      lipgloss.Color
	ColorTimestamp     lipgloss.Color
	ColorBackground    lipgloss.Color
	ColorPageHeader    lipgloss.Color
	ColorGroupHeader   lipgloss.Color
	ColorStatusMessage lipgloss.Color
	ColorFileButton    lipgloss.Color
	ColorReplyLabel    lipgloss.Color
	ColorSelection     lipgloss.Color
	ColorBorderDefault lipgloss.Color
	ColorBorderActive  lipgloss.Color

	// IsDarkTheme is true when the active theme self-identifies as dark.
	IsDarkTheme = true
)

// Derived styles — rebuilt by ApplyTheme.
var (
	SidebarStyle           lipgloss.Style
	SidebarActiveStyle     lipgloss.Style
	ChannelItemStyle       lipgloss.Style
	ChannelSelectedStyle   lipgloss.Style
	ChannelUnreadStyle     lipgloss.Style
	SectionHeaderStyle     lipgloss.Style
	MessagePaneStyle       lipgloss.Style
	MessagePaneActiveStyle lipgloss.Style
	UserNameStyle          lipgloss.Style
	TimestampStyle         lipgloss.Style
	MessageTextStyle       lipgloss.Style
	InputStyle             lipgloss.Style
	InputActiveStyle       lipgloss.Style
	StatusBarStyle         lipgloss.Style
	StatusConnected        lipgloss.Style
	StatusDisconnected     lipgloss.Style
	HelpStyle              lipgloss.Style
)

// activeTheme tracks the most recently applied theme so the UI can
// display the current selection.
var activeTheme theme.Theme

func init() {
	ApplyTheme(theme.Default())
}

// ActiveTheme returns the most recently applied theme.
func ActiveTheme() theme.Theme {
	return activeTheme
}

// ApplyTheme reassigns every theme-driven color and rebuilds derived
// styles. Safe to call at runtime — subsequent View() calls will pick
// up the new colors automatically.
func ApplyTheme(t theme.Theme) {
	activeTheme = t

	ColorPrimary = lipgloss.Color(t.Get(theme.KeyPrimary))
	ColorSecondary = lipgloss.Color(t.Get(theme.KeySecondary))
	ColorAccent = lipgloss.Color(t.Get(theme.KeyAccent))
	ColorError = lipgloss.Color(t.Get(theme.KeyError))
	ColorMuted = lipgloss.Color(t.Get(theme.KeyMuted))
	ColorHighlight = lipgloss.Color(t.Get(theme.KeyHighlight))

	ColorMessageText = lipgloss.Color(t.Get(theme.KeyMessageText))
	ColorInfoText = lipgloss.Color(t.Get(theme.KeyInfoText))
	ColorDayLabel = lipgloss.Color(t.Get(theme.KeyDayLabel))
	ColorTimestamp = lipgloss.Color(t.Get(theme.KeyTimestamp))
	ColorBackground = lipgloss.Color(t.Get(theme.KeyBackground))
	ColorPageHeader = lipgloss.Color(t.Get(theme.KeyPageHeader))
	ColorGroupHeader = lipgloss.Color(t.Get(theme.KeyGroupHeader))
	ColorStatusMessage = lipgloss.Color(t.Get(theme.KeyStatusMessage))
	ColorFileButton = lipgloss.Color(t.Get(theme.KeyFileButton))
	ColorReplyLabel = lipgloss.Color(t.Get(theme.KeyReplyLabel))
	ColorSelection = lipgloss.Color(t.Get(theme.KeySelection))
	ColorBorderDefault = lipgloss.Color(t.Get(theme.KeyBorderDefault))
	ColorBorderActive = lipgloss.Color(t.Get(theme.KeyBorderActive))

	IsDarkTheme = t.IsDark()

	rebuildDerivedStyles()
}

// rebuildDerivedStyles is split out so it can run from init() and from
// ApplyTheme() with the same effect.
func rebuildDerivedStyles() {
	SidebarStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Padding(0, 1)
	SidebarActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(0, 1)

	ChannelItemStyle = lipgloss.NewStyle().Foreground(ColorMessageText)
	ChannelSelectedStyle = lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	ChannelUnreadStyle = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	SectionHeaderStyle = lipgloss.NewStyle().Foreground(ColorGroupHeader).Bold(true)

	MessagePaneStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Padding(0, 1)
	MessagePaneActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(0, 1)

	UserNameStyle = lipgloss.NewStyle().Bold(true)
	TimestampStyle = lipgloss.NewStyle().Foreground(ColorTimestamp)
	MessageTextStyle = lipgloss.NewStyle().Foreground(ColorMessageText)

	InputStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Padding(0, 1)
	InputActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().Foreground(ColorStatusMessage)
	StatusConnected = lipgloss.NewStyle().Foreground(ColorAccent)
	StatusDisconnected = lipgloss.NewStyle().Foreground(ColorError)

	HelpStyle = lipgloss.NewStyle().Foreground(ColorInfoText)
}

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
