package tui

import (
	"fmt"
	"strconv"
	"strings"

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
	ColorMenuItem        lipgloss.Color
	ColorMenuItemBg      lipgloss.Color
	ColorBorderDefault   lipgloss.Color
	ColorBorderDefaultBg lipgloss.Color
	ColorBorderActive    lipgloss.Color
	ColorBorderActiveBg  lipgloss.Color
	ColorEmote           lipgloss.Color
	ColorEmoteBg         lipgloss.Color
	ColorCodeSnippet     lipgloss.Color
	ColorCodeSnippetBg   lipgloss.Color

	// Shared widget colors — centralised to eliminate inline
	// magic 256-color indices scattered across overlays. Refreshed
	// by rebuildDerivedStyles so theme changes pick them up.
	ColorKeyBindText lipgloss.Color // 229 — keybind text in help / shortcut editor
	ColorDescText    lipgloss.Color // 252 — secondary description text / metadata
	ColorStatusOn    lipgloss.Color // #00ff00 — online / secure / "on" indicator

	// IsDarkTheme is true when the active theme self-identifies as dark.
	IsDarkTheme = true

	// Computed background variants — set by ApplyTheme, cached for
	// zero per-frame cost. Used by reactions, pills, badges, emoji
	// picker, and other elements that need a subtle contrast against
	// the main background without hardcoding palette indices.
	ColorSubtleBg      lipgloss.Color // slight offset from main bg (reactions, pills)
	ColorSubtleBgAlt   lipgloss.Color // a bit more contrast (select bg)
	ColorSubtleBgHover lipgloss.Color // hover/emphasis (selected reaction, emoji cell)
	ColorInvertedFg    lipgloss.Color // fg for inverted elements (pill selected on accent bg)
	ColorOverlayFill   lipgloss.Color // whitespace fill for overlay backgrounds
)

// Footer hint format constants. Every overlay should reference
// these instead of hand-rolling its own Esc-action label so the
// footer voice stays consistent across the app.
const (
	// HintSep is the separator placed between hint items in a
	// footer line. Middot with surrounding spaces gives a clean
	// visual break that reads well in monospaced fonts.
	HintSep = " · "
	// FooterHintClose is the canonical footer suffix for a leaf
	// overlay that's dismissed by pressing Esc. Most modals,
	// lists, and viewers should use this.
	FooterHintClose = "Esc: close"
	// FooterHintBack is used by nested overlays that return to
	// a parent view (e.g. Friends Config sub-pages, Settings
	// sub-pages, theme editor inside theme picker).
	FooterHintBack = "Esc: back"
	// FooterHintCancel is reserved for destructive-action
	// confirmations where the Esc key actually cancels a
	// committed change (not just closes a modal).
	FooterHintCancel = "Esc: cancel"
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

	// Message-pane hot-path styles — rebuilt by ApplyTheme so they
	// always reflect the active palette. Reference these directly
	// inside render loops instead of calling lipgloss.NewStyle()
	// per frame / per message / per reaction.
	MessagePendingStyle         lipgloss.Style // "⏳ pending" badge on unsent friend messages
	MessageHighlightBgStyle     lipgloss.Style // fixed 236 background for flagged/highlight lines
	MessageSelectBgStyle        lipgloss.Style // fixed 237 background used by react-mode selection
	MessageDateSepStyle         lipgloss.Style // date separator row
	MessageFileStyle            lipgloss.Style // idle file attachment row
	MessageFileSelectedStyle    lipgloss.Style // file row when in select-mode and highlighted
	MessageFileUploadingStyle   lipgloss.Style // "uploading…" muted italic
	MessageThreadRuleStyle      lipgloss.Style // thread-box top rule
	MessageReplyLabelStyle      lipgloss.Style // "X replies" label on inline reply rows
	MessageReactionStyle        lipgloss.Style // unselected reaction badge
	MessageReactionSelStyle     lipgloss.Style // selected reaction badge
	MessageHeaderHintStyle      lipgloss.Style // muted italic hint in the message-pane header
	MessageHeaderSecureStyle    lipgloss.Style // fixed "#00ff00" "secure p2p" indicator
	MessageHeaderDateStyle      lipgloss.Style // "Today" style date in the header
	MessageHeaderHighlight      lipgloss.Style // highlight color used for header info banners
	MessageCogStyle             lipgloss.Style // "⚙" cog in friend chat header
	FriendCardPillStyle         lipgloss.Style // styled pill rendered in place of [FRIEND:…] markers
	FriendCardPillSelectedStyle lipgloss.Style // pill highlighted when cursor is on it in select mode
	CodeSnippetStyle            lipgloss.Style // idle styling for inline `code` spans in chat
	CodeSnippetSelectedStyle    lipgloss.Style // highlighted styling when cursor is on a snippet
	EmoteMessageStyle           lipgloss.Style // emote action text in chat (/laugh, /wave)

	// Emoji picker hot-path styles.
	EmojiActiveBgStyle     lipgloss.Style // tab highlight background for the active category
	EmojiActiveIconStyle   lipgloss.Style // bold + themed fg on the active category icon
	EmojiInactiveIconStyle lipgloss.Style // muted fg on inactive category icons
	EmojiCellStyle         lipgloss.Style // plain grid cell background
	EmojiSelectedCellStyle lipgloss.Style // grid cell for the hovered/selected emoji
	EmojiFavCellStyle      lipgloss.Style // grid cell for a favourited emoji
)

// activeTheme tracks the most recently applied theme so the UI can
// display the current selection.
var activeTheme theme.Theme

// keyAttrs caches the bold/italic flags for each theme key, refreshed by
// ApplyTheme. Lookup with KeyBold(theme.KeyMessageText) etc.
var keyAttrs = map[string][2]bool{}

// KeyBold returns whether the given theme key has its bold attribute set.
func KeyBold(key string) bool {
	a := keyAttrs[key]
	return a[0]
}

// KeyItalic returns whether the given theme key has its italic attribute set.
func KeyItalic(key string) bool {
	a := keyAttrs[key]
	return a[1]
}

// styleFromKey returns a lipgloss.Style with foreground, background, bold,
// and italic populated from the named theme key.
func styleFromKey(key string) lipgloss.Style {
	t := activeTheme
	fgStr, bgStr, bold, italic := theme.ParseColorFull(t.Get(key))
	s := lipgloss.NewStyle()
	if fgStr != "" {
		s = s.Foreground(lipgloss.Color(fgStr))
	}
	if bgStr != "" {
		s = s.Background(lipgloss.Color(bgStr))
	}
	if bold {
		s = s.Bold(true)
	}
	if italic {
		s = s.Italic(true)
	}
	return s
}

func init() {
	ApplyTheme(theme.Default())
}

// ActiveTheme returns the most recently applied theme.
func ActiveTheme() theme.Theme {
	return activeTheme
}

// applyKey reads a theme key, caches its bold/italic flags, and returns
// its (fg, bg) lipgloss colors.
func applyKey(t theme.Theme, key string) (lipgloss.Color, lipgloss.Color) {
	fg, bg, bold, italic := theme.ParseColorFull(t.Get(key))
	keyAttrs[key] = [2]bool{bold, italic}
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
	ColorMenuItem, ColorMenuItemBg = applyKey(t, theme.KeyMenuItem)
	ColorBorderDefault, ColorBorderDefaultBg = applyKey(t, theme.KeyBorderDefault)
	ColorBorderActive, ColorBorderActiveBg = applyKey(t, theme.KeyBorderActive)
	ColorEmote, ColorEmoteBg = applyKey(t, theme.KeyEmote)
	ColorCodeSnippet, ColorCodeSnippetBg = applyKey(t, theme.KeyCodeSnippet)

	// Shared widget colors. These are not yet theme keys —
	// they're fixed 256-color indices chosen to read well on
	// both dark and light themes. They're here (rather than as
	// const) so a future theme extension can override them
	// without touching callers.
	ColorKeyBindText = lipgloss.Color("229")
	ColorDescText = lipgloss.Color("252")
	ColorStatusOn = lipgloss.Color("#00ff00")

	IsDarkTheme = t.IsDark()

	// Compute subtle background variants from the theme's main
	// background color. These replace hardcoded "236"/"237"/"240"
	// palette entries so reactions, pills, and badges blend with any
	// theme. Cached here so the per-frame cost is zero.
	if IsDarkTheme {
		ColorSubtleBg = lipgloss.Color("236")    // slightly lighter than typical dark bg
		ColorSubtleBgAlt = lipgloss.Color("237")  // a bit more contrast
		ColorSubtleBgHover = lipgloss.Color("240") // hover/selected emphasis
	} else {
		ColorSubtleBg = lipgloss.Color("254")    // slightly darker than typical light bg
		ColorSubtleBgAlt = lipgloss.Color("253")  // a bit more contrast
		ColorSubtleBgHover = lipgloss.Color("250") // hover/selected emphasis
	}
	// Foreground for inverted elements (pill selected, code selected).
	// Must contrast against ColorAccent background.
	if IsDarkTheme {
		ColorInvertedFg = lipgloss.Color("0")
	} else {
		ColorInvertedFg = lipgloss.Color("255")
	}
	// Whitespace fill for overlay backgrounds — matches the main theme bg.
	ColorOverlayFill = ColorBackgroundBg

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

	ChannelItemStyle = styleFromKey(theme.KeyMessageText)
	ChannelSelectedStyle = styleFromKey(theme.KeySelection).Bold(true)
	ChannelUnreadStyle = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	SectionHeaderStyle = styleFromKey(theme.KeyGroupHeader).Bold(true)

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
	TimestampStyle = styleFromKey(theme.KeyTimestamp)
	MessageTextStyle = styleFromKey(theme.KeyMessageText)

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

	StatusBarStyle = styleFromKey(theme.KeyStatusMessage)
	StatusConnected = lipgloss.NewStyle().Foreground(ColorAccent)
	StatusDisconnected = lipgloss.NewStyle().Foreground(ColorError)

	HelpStyle = styleFromKey(theme.KeyInfoText)

	// Message-pane hot-path styles. Kept at the bottom of ApplyTheme
	// so they pick up any palette change before the next render.
	MessagePendingStyle = lipgloss.NewStyle().
		Foreground(ColorHighlight).
		Italic(true)
	MessageHighlightBgStyle = lipgloss.NewStyle().Background(ColorSubtleBg)
	MessageSelectBgStyle = lipgloss.NewStyle().Background(ColorSubtleBgAlt)
	MessageDateSepStyle = lipgloss.NewStyle().Foreground(ColorDayLabel).Bold(true)
	MessageFileStyle = lipgloss.NewStyle().Foreground(ColorFileButton)
	MessageFileSelectedStyle = lipgloss.NewStyle().
		Foreground(ColorFileButton).
		Bold(true).
		Background(ColorSubtleBg)
	MessageFileUploadingStyle = lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	MessageThreadRuleStyle = lipgloss.NewStyle().Foreground(ColorPrimary)
	MessageReplyLabelStyle = lipgloss.NewStyle().Foreground(ColorReplyLabel).Italic(true)
	MessageReactionStyle = lipgloss.NewStyle().
		Background(ColorSubtleBg).
		Foreground(ColorDescText).
		Padding(0, 1)
	MessageReactionSelStyle = lipgloss.NewStyle().
		Background(ColorSubtleBgHover).
		Foreground(ColorPrimary).
		Bold(true).
		Padding(0, 1)
	MessageHeaderHintStyle = lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	MessageHeaderSecureStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
	MessageHeaderDateStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	MessageHeaderHighlight = lipgloss.NewStyle().Foreground(ColorHighlight)
	MessageCogStyle = lipgloss.NewStyle().Foreground(ColorHighlight)
	FriendCardPillStyle = lipgloss.NewStyle().
		Foreground(ColorAccent).
		Background(ColorSubtleBg).
		Bold(true).
		Padding(0, 1)
	// FriendCardPillSelectedStyle is the highlighted version of the
	// pill rendered when the user has cycled the in-message
	// selection cursor onto a contact card. Inverts the foreground
	// onto a brighter background so the cursor location is
	// unambiguous against neighbouring pills and message text.
	FriendCardPillSelectedStyle = lipgloss.NewStyle().
		Foreground(ColorInvertedFg).
		Background(ColorAccent).
		Bold(true).
		Padding(0, 1)
	// EmoteMessageStyle is used for emote action text in the
	// chat pane (/laugh, /wave). Italic + the theme's emote
	// color (purple-ish by default).
	EmoteMessageStyle = lipgloss.NewStyle().
		Foreground(ColorEmote).
		Italic(true)

	// CodeSnippetStyle is the idle rendering for inline `code`
	// spans inside chat message bodies. Italic + accent fg so it
	// reads as "this is a code-ish thing" without adding any
	// background fill (that would clash with the message pane's
	// own background when the cursor isn't on the snippet).
	csFg := ColorCodeSnippet
	if csFg == "" {
		csFg = ColorAccent
	}
	csBg := ColorCodeSnippetBg
	csStyle := lipgloss.NewStyle().Foreground(csFg)
	if csBg != "" {
		csStyle = csStyle.Background(csBg)
	}
	CodeSnippetStyle = csStyle

	// CodeSnippetSelectedStyle is the highlighted form when the
	// user has arrow-navigated the select-mode cursor onto a
	// snippet. Inverts fg/bg.
	CodeSnippetSelectedStyle = lipgloss.NewStyle().
		Foreground(ColorInvertedFg).
		Background(csFg).
		Bold(true)

	// Emoji picker styles.
	EmojiActiveBgStyle = lipgloss.NewStyle().Background(ColorSubtleBg)
	EmojiActiveIconStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Background(ColorSubtleBg)
	EmojiInactiveIconStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	EmojiCellStyle = lipgloss.NewStyle()
	EmojiSelectedCellStyle = lipgloss.NewStyle().Background(ColorSubtleBgHover)
	EmojiFavCellStyle = lipgloss.NewStyle().Background(ColorSubtleBg)
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

// bgSGR returns the ANSI SGR sequence that sets the given background color.
// Supports 256-color indices ("12") and truecolor hex strings ("#ff8800").
// Returns "" if the color is empty (no override).
func bgSGR(c lipgloss.Color) string {
	s := string(c)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		r, _ := strconv.ParseInt(s[1:3], 16, 32)
		g, _ := strconv.ParseInt(s[3:5], 16, 32)
		b, _ := strconv.ParseInt(s[5:7], 16, 32)
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	}
	// Anything else: assume a 256-color palette index.
	return fmt.Sprintf("\x1b[48;5;%sm", s)
}

// ApplyBackgroundReset post-processes a fully rendered TUI string so that
// every ANSI reset is followed by an SGR that re-asserts the theme's
// configured background color. This makes the background "stick" through
// the gaps between styled runs (e.g. between a styled timestamp and the
// next plain message text), instead of clearing back to the terminal's
// default. Returns s unchanged when no background is configured.
func ApplyBackgroundReset(s string) string {
	bg := bgSGR(ColorBackgroundBg)
	if bg == "" {
		return s
	}
	const reset = "\x1b[0m"
	return bg + strings.ReplaceAll(s, reset, reset+bg)
}
