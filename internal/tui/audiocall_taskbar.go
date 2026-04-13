package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderAudioCallButton returns the styled badge for an active call.
// Returns "" if no call is active.
func renderAudioCallButton(call *ActiveCall) string {
	if call == nil || call.State != CallStateActive {
		return ""
	}
	dur := time.Since(call.StartTime)
	min := int(dur.Minutes())
	sec := int(dur.Seconds()) % 60
	text := fmt.Sprintf(" 📞 %d:%02d ", min, sec)
	return lipgloss.NewStyle().
		Background(ColorAccent).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		Render(text)
}

// audioCallButtonClickArea returns the click bounds for the call badge.
func (m Model) audioCallButtonClickArea() (x0, x1, y int, visible bool) {
	if m.activeCall == nil || m.activeCall.State != CallStateActive {
		return 0, 0, 0, false
	}
	btn := renderAudioCallButton(m.activeCall)
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}
	// Position right of downloads button, left of game button.
	paneEnd := m.width - 2
	if m.backgroundGame != nil {
		paneEnd -= lipgloss.Width(m.renderGameTaskbarButton()) + 1
	}
	if m.downloadMgr != nil && m.downloadMgr.ActiveCount() > 0 {
		paneEnd -= lipgloss.Width(renderDownloadsButton(m.downloadMgr)) + 1
	}
	x := paneEnd - btnW
	if x < 0 {
		x = 0
	}
	return x, x + btnW, 0, true
}
