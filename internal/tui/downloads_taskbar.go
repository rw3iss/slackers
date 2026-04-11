package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/downloads"
)

// renderDownloadsButton returns the styled button for the taskbar
// when there are active downloads.
func renderDownloadsButton(mgr *downloads.Manager) string {
	if mgr == nil {
		return ""
	}
	count := mgr.ActiveCount()
	if count <= 0 {
		return ""
	}
	word := "Download"
	if count != 1 {
		word = "Downloads"
	}
	text := fmt.Sprintf(" ⬇ %d %s ", count, word)
	return lipgloss.NewStyle().
		Background(ColorSubtleBg).
		Foreground(ColorAccent).
		Bold(true).
		Render(text)
}

// downloadsButtonClickArea returns the click area for the downloads
// taskbar button. Positioned left of the game taskbar button.
func (m Model) downloadsButtonClickArea() (x0, x1, y int, visible bool) {
	if m.downloadMgr == nil || m.downloadMgr.ActiveCount() <= 0 {
		return 0, 0, 0, false
	}
	btn := renderDownloadsButton(m.downloadMgr)
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}
	// Position left of the game taskbar button (if any).
	paneEnd := m.width - 2
	// Check if game button is present.
	if m.backgroundGame != nil {
		gameBtn := m.renderGameTaskbarButton()
		paneEnd -= lipgloss.Width(gameBtn) + 1
	}
	x := paneEnd - btnW
	if x < 0 {
		x = 0
	}
	return x, x + btnW, 0, true
}
