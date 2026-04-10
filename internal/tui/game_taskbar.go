package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderGameTaskbarButton returns the styled button shown in the
// top-right of the message pane when a game is paused in the
// background. Clicking it (or typing /games) restores the game.
func (m Model) renderGameTaskbarButton() string {
	if m.backgroundGame == nil {
		return ""
	}
	name := strings.ToUpper(m.backgroundGame.gameName[:1]) + m.backgroundGame.gameName[1:]
	text := " 🎮 " + name + " (paused) "
	return lipgloss.NewStyle().
		Background(ColorSubtleBg).
		Foreground(ColorAccent).
		Bold(true).
		Render(text)
}

// gameTaskbarClickArea returns the click area for the background
// game taskbar button.
func (m Model) gameTaskbarClickArea() (x0, x1, y int, visible bool) {
	if m.backgroundGame == nil {
		return 0, 0, 0, false
	}
	btn := m.renderGameTaskbarButton()
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}
	paneEnd := m.width - 2
	x := paneEnd - btnW
	if x < 0 {
		x = 0
	}
	return x, x + btnW, 0, true
}
