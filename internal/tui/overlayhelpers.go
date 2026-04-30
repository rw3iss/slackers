package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// OverlayBox wraps content in a rounded border, pads the inside by
// the standard (1, 3) scheme used across the project, and centres
// the resulting block on screen. Every modal-style overlay should
// use this instead of hand-rolling its own `lipgloss.Place` call,
// which eliminates ~18 duplicated centring blocks across the
// codebase.
//
// Passing an empty borderColor uses the active theme's muted border
// colour — this is the normal case for most overlays. Callers that
// want an emphasised border (e.g. the friends config while editing)
// can pass ColorBorderActive explicitly.
func OverlayBox(width, height int, content string, borderColor lipgloss.Color) string {
	if borderColor == "" {
		borderColor = ColorBorderDefault
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 3).
		Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// OverlayBoxSized is the same as OverlayBox but accepts explicit
// box width/height constraints. Used by overlays that want to pin
// their modal to a specific size regardless of content (e.g. the
// settings panel's fixed layout).
func OverlayBoxSized(screenW, screenH, boxW, boxH int, content string, borderColor lipgloss.Color) string {
	if borderColor == "" {
		borderColor = ColorBorderDefault
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 3).
		Width(boxW).
		Height(boxH).
		Render(content)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

// ansiTruncatePad returns s truncated or padded to exactly visualW
// visible *terminal cells*, preserving ANSI escape sequences and
// accounting for double-width runes (emoji, CJK). This is what the
// popup overlay uses to build a line-by-line background slice: the
// popup's left border is placed at column visualW, so every visible
// column before it needs to be correct to the cell, not the rune.
//
// Earlier versions of this function counted every rune as 1 cell,
// which caused the popup border to drift right of its intended
// column whenever emojis (or any 2-cell rune) appeared on the same
// row — the emoji took 2 cells on screen but was counted as 1, so
// the truncation stopped too late and the padding arithmetic was
// off by one per emoji. The fix is to consult go-runewidth (already
// used transitively by lipgloss) for the actual cell width.
func ansiTruncatePad(s string, visualW int) string {
	if visualW <= 0 {
		return ""
	}
	var out strings.Builder
	visiblePos := 0
	inEsc := false

	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			out.WriteRune(r)
			continue
		}
		if inEsc {
			out.WriteRune(r)
			// CSI sequences end with a letter (typically 'm' for SGR).
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// Zero-width combining mark / variation selector / ZWJ —
			// emit it but don't advance the column counter. It attaches
			// to the previous rune's cell.
			out.WriteRune(r)
			continue
		}
		if visiblePos+w > visualW {
			// Including this rune would overflow the requested width.
			// Stop here and let the padding loop below fill the
			// remaining cell(s) with spaces. This also handles the
			// case where the cursor lands on the second cell of a
			// 2-wide rune — we drop the rune entirely and pad with a
			// space so the overlay border sits cleanly.
			break
		}
		out.WriteRune(r)
		visiblePos += w
	}
	// Reset ANSI before padding to prevent bg color bleed.
	out.WriteString("\x1b[0m")
	for visiblePos < visualW {
		out.WriteRune(' ')
		visiblePos++
	}
	return out.String()
}

// ansiAfterCells returns the portion of s that starts at visual column
// startCol, prepended with an ANSI reset so inherited colour from the
// skipped prefix doesn't leak into the returned segment. Used by popup
// overlay renderers to restore background content to the right of the
// popup box.
func ansiAfterCells(s string, startCol int) string {
	if startCol <= 0 {
		return s
	}
	visPos := 0
	inEsc := false
	for i, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if visPos >= startCol {
			return "\x1b[0m" + s[i:]
		}
		visPos += w
	}
	return ""
}
