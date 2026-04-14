package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/audio"
)

// miniMeterLevel returns the filled column count (0-10) for a dBFS level.
func miniMeterLevel(levelDB float32) int {
	if levelDB < -60 {
		levelDB = -60
	}
	if levelDB > 0 {
		levelDB = 0
	}
	filled := int(((levelDB + 60) / 60) * 10)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return filled
}

// renderMiniMeter renders a 10-column meter where the filled portion
// uses the accent bg (matching the call label) and the empty portion
// uses the theme background (transparent look).
func renderMiniMeter(levelDB float32) string {
	filled := miniMeterLevel(levelDB)
	accentBg := lipgloss.NewStyle().
		Background(ColorAccent).
		Foreground(lipgloss.Color("#ffffff"))
	themeBg := lipgloss.NewStyle().
		Background(ColorBackgroundBg).
		Foreground(ColorMuted)

	filledStr := ""
	emptyStr := ""
	for i := 0; i < 10; i++ {
		if i < filled {
			filledStr += "▮"
		} else {
			emptyStr += "▯"
		}
	}
	return accentBg.Render(filledStr) + themeBg.Render(emptyStr)
}

// renderAudioCallButton returns the styled badge for an active call.
// Meters render with accent bg for level, theme bg for empty, then a
// gap column in theme bg before the call label.
func renderAudioCallButton(call *ActiveCall, engine *audio.Engine, showMic, showPeer bool) string {
	if call == nil || call.State != CallStateActive {
		return ""
	}
	dur := time.Since(call.StartTime)
	min := int(dur.Minutes())
	sec := int(dur.Seconds()) % 60
	mic := "🎤"
	if call.Muted {
		mic = "🔇"
	}

	labelStyle := lipgloss.NewStyle().
		Background(ColorAccent).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)
	gapStyle := lipgloss.NewStyle().
		Background(ColorBackgroundBg)

	var parts []string

	// Meters with gap separator.
	if engine != nil {
		if showMic {
			parts = append(parts, renderMiniMeter(engine.MicLevel))
			parts = append(parts, gapStyle.Render(" "))
		}
		if showPeer {
			parts = append(parts, renderMiniMeter(engine.SpeakerLevel))
			parts = append(parts, gapStyle.Render(" "))
		}
	}

	// Call label.
	label := fmt.Sprintf(" %s 📞 %d:%02d ", mic, min, sec)
	parts = append(parts, labelStyle.Render(label))

	result := ""
	for _, p := range parts {
		result += p
	}
	return result
}

// audioCallButtonClickArea returns the click bounds for the call badge.
func (m Model) audioCallButtonClickArea() (x0, x1, y int, visible bool) {
	if m.activeCall == nil || m.activeCall.State != CallStateActive {
		return 0, 0, 0, false
	}
	btn := renderAudioCallButton(m.activeCall, m.audioEngine, m.audioCallModel.ShowMicMeter(), m.audioCallModel.ShowPeerMeter())
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}
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
