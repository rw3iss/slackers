package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/theme"
)

// All exported style variables are mutated by ApplyTheme so the renderer
// can switch themes at runtime. Components should reference the package
// vars directly (e.g. ColorPrimary, MessagePaneStyle) at render time so
// any theme reapply is picked up immediately.
//
// Each theme key has both a foreground (Color*) and background (Color*Bg)
// slot. The background is empty unless the theme value uses the "fg/bg"
// syntax (e.g. "12/240"); when empty, lipgloss falls through to the
// terminal default.
var (
	// Core palette colors — populated from the active theme.
	ColorPrimary     lipgloss.Color
	ColorPrimaryBg   lipgloss.Color
	ColorSecondary   lipgloss.Color
	ColorSecondaryBg lipgloss.Color
	ColorAccent      lipgloss.Color
	ColorAccentBg    lipgloss.Color
	ColorError       lipgloss.Color
	ColorErrorBg     lipgloss.Color
	ColorMuted       lipgloss.Color
	ColorMutedBg     lipgloss.Color
	ColorHighlight   lipgloss.Color
	ColorHighlightBg lipgloss.Color

	// Semantic colors — populated from the active theme.
	ColorMessageText     lipgloss.Color
	ColorMessageTextBg   lipgloss.Color
	ColorInfoText        lipgloss.Color
	ColorInfoTextBg      lipgloss.Color
	ColorDayLabel        lipgloss.Color
	ColorDayLabelBg      lipgloss.Color
	ColorTimestamp       lipgloss.Color
	ColorTimestampBg     lipgloss.Color
	ColorBackground      lipgloss.Color
	ColorBackgroundBg    lipgloss.Color
	ColorPageHeader      lipgloss.Color
	ColorPageHeaderBg    lipgloss.Color
	ColorGroupHeader     lipgloss.Color
	ColorGroupHeaderBg   lipgloss.Color
	ColorStatusMessage   lipgloss.Color
	ColorStatusMessageBg lipgloss.Color
	ColorFileButton      lipgloss.Color
	ColorFileButtonBg    lipgloss.Color
	ColorReplyLabel      lipgloss.Color
	ColorReplyLabelBg    lipgloss.Color
	ColorSelection       lipgloss.Color
	ColorSelectionBg     lipgloss.Color
	ColorBorderDefault   lipgloss.Color
	ColorBorderDefaultBg lipgloss.Color
	ColorBorderActive    lipgloss.Color
	ColorBorderActiveBg  lipgloss.Color

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

// applyKey reads a theme key and returns its (fg, bg) lipgloss colors.
func applyKey(t theme.Theme, key string) (lipgloss.Color, lipgloss.Color) {
	fg, bg := theme.ParseColor(t.Get(key))
	return lipgloss.Color(fg), lipgloss.Color(bg)
}

// ApplyTheme reassigns every theme-driven color and rebuilds derived
// styles. Safe to call at runtime — subsequent View() calls will pick
// up the new colors automatically.
func ApplyTheme(t theme.Theme) {
	activeTheme = t

	ColorPrimary, ColorPrimaryBg = applyKey(t, theme.KeyPrimary)
	ColorSecondary, ColorSecondaryBg = applyKey(t, theme.KeySecondary)
	ColorAccent, ColorAccentBg = applyKey(t, theme.KeyAccent)
	ColorError, ColorErrorBg = applyKey(t, theme.KeyError)
	ColorMuted, ColorMutedBg = applyKey(t, theme.KeyMuted)
	ColorHighlight, ColorHighlightBg = applyKey(t, theme.KeyHighlight)

	ColorMessageText, ColorMessageTextBg = applyKey(t, theme.KeyMessageText)
	ColorInfoText, ColorInfoTextBg = applyKey(t, theme.KeyInfoText)
	ColorDayLabel, ColorDayLabelBg = applyKey(t, theme.KeyDayLabel)
	ColorTimestamp, ColorTimestampBg = applyKey(t, theme.KeyTimestamp)
	ColorBackground, ColorBackgroundBg = applyKey(t, theme.KeyBackground)
	ColorPageHeader, ColorPageHeaderBg = applyKey(t, theme.KeyPageHeader)
	ColorGroupHeader, ColorGroupHeaderBg = applyKey(t, theme.KeyGroupHeader)
	ColorStatusMessage, ColorStatusMessageBg = applyKey(t, theme.KeyStatusMessage)
	ColorFileButton, ColorFileButtonBg = applyKey(t, theme.KeyFileButton)
	ColorReplyLabel, ColorReplyLabelBg = applyKey(t, theme.KeyReplyLabel)
	ColorSelection, ColorSelectionBg = applyKey(t, theme.KeySelection)
	ColorBorderDefault, ColorBorderDefaultBg = applyKey(t, theme.KeyBorderDefault)
	ColorBorderActive, ColorBorderActiveBg = applyKey(t, theme.KeyBorderActive)

	IsDarkTheme = t.IsDark()

	rebuildDerivedStyles()
}

// rebuildDerivedStyles is split out so it can run from init() and from
// ApplyTheme() with the same effect. Empty bg colors are no-ops in
// lipgloss so we can safely call .Background() unconditionally.
func rebuildDerivedStyles() {
	SidebarStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Background(ColorBackgroundBg).
		Padding(0, 1)
	SidebarActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Background(ColorBackgroundBg).
		Padding(0, 1)

	ChannelItemStyle = lipgloss.NewStyle().Foreground(ColorMessageText)
	ChannelSelectedStyle = lipgloss.NewStyle().
		Foreground(ColorSelection).
		Background(ColorSelectionBg).
		Bold(true)
	ChannelUnreadStyle = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	SectionHeaderStyle = lipgloss.NewStyle().
		Foreground(ColorGroupHeader).
		Background(ColorGroupHeaderBg).
		Bold(true)

	MessagePaneStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Background(ColorBackgroundBg).
		Padding(0, 1)
	MessagePaneActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Background(ColorBackgroundBg).
		Padding(0, 1)

	UserNameStyle = lipgloss.NewStyle().Bold(true)
	TimestampStyle = lipgloss.NewStyle().
		Foreground(ColorTimestamp).
		Background(ColorTimestampBg)
	MessageTextStyle = lipgloss.NewStyle().Foreground(ColorMessageText)

	InputStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderDefault).
		Background(ColorBackgroundBg).
		Padding(0, 1)
	InputActiveStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Background(ColorBackgroundBg).
		Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
		Foreground(ColorStatusMessage).
		Background(ColorStatusMessageBg)
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
