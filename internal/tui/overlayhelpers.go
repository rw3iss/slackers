package tui

import (
	"github.com/charmbracelet/lipgloss"
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
