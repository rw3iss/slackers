package tui

// OverlayScaffold composes a standard modal overlay: an optional
// bold title, the caller-supplied body, and an optional dimmed
// footer hint, all wrapped in OverlayBox and centred on the screen.
//
// Every modal-style overlay in the codebase repeats the same
// "titleStyle.Render(title) + body + dimStyle.Render(hint) + box
// + lipgloss.Place" pattern; this type collapses it into a single
// line per overlay:
//
//	scaffold := OverlayScaffold{
//	    Title:       "Rename Channel",
//	    Footer:      "Enter: save · Esc: cancel",
//	    Width:       m.width,
//	    Height:      m.height,
//	    MaxBoxWidth: 55,
//	}
//	return scaffold.Render(body)
//
// The scaffold is introduced incrementally — the existing
// `OverlayBox` / `OverlayBoxSized` helpers remain for overlays that
// need custom pre-processing on their content before the box is
// applied (see `about.go`, which centres every content line first).

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// OverlayScaffold describes a modal overlay's chrome. All fields are
// optional; a zero value produces a plain centred OverlayBox around
// the body.
type OverlayScaffold struct {
	// Title is rendered above the body in the primary colour. If
	// empty, no title row is emitted.
	Title string
	// Footer is rendered below the body in a dimmed italic style.
	// If empty, no footer row is emitted.
	Footer string
	// EmptyMessage replaces the body when the body is empty after
	// trimming. Useful for list overlays that want to show
	// "No items" without the caller having to special-case it.
	EmptyMessage string
	// Width and Height are the full screen dimensions used for
	// `lipgloss.Place` centring. They are required for the modal
	// to position correctly.
	Width  int
	Height int
	// BoxWidth / BoxHeight, when > 0, pin the modal box to the
	// given size. Left at zero, the box sizes to its content.
	BoxWidth  int
	BoxHeight int
	// MaxBoxWidth, when > 0, caps the box width at
	// `min(MaxBoxWidth, Width-4)`. A common value is 55 — matches
	// the existing whitelist / rename / friends-config modals.
	MaxBoxWidth int
	// BorderColor picks the border colour. Empty → ColorBorderDefault.
	BorderColor lipgloss.Color
}

// Render assembles the final, centred modal string.
func (s OverlayScaffold) Render(body string) string {
	var b strings.Builder

	if s.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)
		b.WriteString(titleStyle.Render(s.Title))
		b.WriteString("\n\n")
	}

	trimmed := strings.TrimSpace(body)
	if trimmed == "" && s.EmptyMessage != "" {
		emptyStyle := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)
		b.WriteString(emptyStyle.Render("  " + s.EmptyMessage))
	} else {
		b.WriteString(body)
	}

	if s.Footer != "" {
		b.WriteString("\n\n")
		footerStyle := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)
		b.WriteString(footerStyle.Render("  " + s.Footer))
	}

	content := b.String()

	// Pick the right OverlayBox helper based on whether explicit
	// sizing was requested.
	border := s.BorderColor
	if border == "" {
		border = ColorBorderDefault
	}

	boxW := s.BoxWidth
	if s.MaxBoxWidth > 0 {
		maxW := s.MaxBoxWidth
		if s.Width-4 < maxW {
			maxW = s.Width - 4
		}
		if maxW < 10 {
			maxW = 10
		}
		if boxW == 0 || boxW > maxW {
			boxW = maxW
		}
	}

	if boxW > 0 || s.BoxHeight > 0 {
		// Fall through to a manual box render so we can independently
		// set width / height. OverlayBoxSized requires both; we accept
		// either or both being zero.
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(1, 3)
		if boxW > 0 {
			style = style.Width(boxW)
		}
		if s.BoxHeight > 0 {
			style = style.Height(s.BoxHeight)
		}
		box := style.Render(content)
		return lipgloss.Place(s.Width, s.Height, lipgloss.Center, lipgloss.Center, box)
	}

	return OverlayBox(s.Width, s.Height, content, border)
}
