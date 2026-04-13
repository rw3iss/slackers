package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AudioCallCloseMsg closes the call overlay but leaves the call running.
type AudioCallCloseMsg struct{}

// AudioCallEndMsg ends the call entirely.
type AudioCallEndMsg struct{}

// AudioCallAcceptMsg accepts an incoming call.
type AudioCallAcceptMsg struct{}

// AudioCallTimerTickMsg fires once per second to refresh the call duration.
type AudioCallTimerTickMsg struct{}

// AudioCallModel is the overlay for ringing/active/effects states of a call.
type AudioCallModel struct {
	call        *ActiveCall
	width       int
	height      int
	showEffects bool
	effectsTab  int // 0=outgoing, 1=incoming
	effectsSel  int
	monitorMode bool
}

// NewAudioCallModel creates a new call overlay bound to the given call.
func NewAudioCallModel(call *ActiveCall) AudioCallModel {
	return AudioCallModel{
		call: call,
	}
}

// SetSize updates the overlay dimensions.
func (m *AudioCallModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events for the audio call overlay.
func (m AudioCallModel) Update(msg tea.Msg) (AudioCallModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	key := keyMsg.String()

	// Ringing state (or nil call — treat as close).
	if m.call == nil || m.call.State == CallStateRinging {
		if m.call != nil && !m.call.Outgoing {
			// Incoming ringing.
			switch key {
			case "enter":
				return m, func() tea.Msg { return AudioCallAcceptMsg{} }
			case "esc":
				return m, func() tea.Msg { return AudioCallEndMsg{} }
			}
		} else {
			// Outgoing ringing (or nil).
			switch key {
			case "esc", "enter":
				return m, func() tea.Msg { return AudioCallEndMsg{} }
			}
		}
		return m, nil
	}

	// Active state.
	if m.call.State == CallStateActive {
		if m.showEffects {
			switch key {
			case "esc":
				m.showEffects = false
				return m, nil
			case "tab":
				m.effectsTab = (m.effectsTab + 1) % 2
				m.effectsSel = 0
				return m, nil
			case "up":
				if m.effectsSel > 0 {
					m.effectsSel--
				}
				return m, nil
			case "down":
				m.effectsSel++
				return m, nil
			}
			return m, nil
		}

		// Main active-call screen.
		switch key {
		case "m":
			m.call.Muted = !m.call.Muted
			return m, nil
		case "e":
			m.showEffects = true
			return m, nil
		case "q":
			return m, func() tea.Msg { return AudioCallEndMsg{} }
		case "esc":
			return m, func() tea.Msg { return AudioCallCloseMsg{} }
		case "enter":
			return m, func() tea.Msg { return AudioCallCloseMsg{} }
		}
	}

	return m, nil
}

// View renders the call overlay as a centered bordered box.
func (m AudioCallModel) View() string {
	if m.call == nil {
		return ""
	}

	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	accentStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	var b strings.Builder
	var title string

	switch {
	case m.call.State == CallStateRinging && m.call.Outgoing:
		title = "Calling..."
		b.WriteString(titleStyle.Render("  Calling "+m.call.PeerName) + "\n\n")
		b.WriteString(dimStyle.Render("  Ringing...") + "\n\n")
		b.WriteString(dimStyle.Render("  Esc: cancel"))

	case m.call.State == CallStateRinging && !m.call.Outgoing:
		title = "Incoming Call"
		b.WriteString(titleStyle.Render("  "+m.call.PeerName+" is calling") + "\n\n")
		b.WriteString(dimStyle.Render("  Enter: accept"+HintSep+"Esc: decline"))

	case m.call.State == CallStateActive && m.showEffects:
		title = "Audio Effects"
		outLabel := "Outgoing"
		inLabel := "Incoming"
		if m.effectsTab == 0 {
			outLabel = accentStyle.Render("[" + outLabel + "]")
			inLabel = dimStyle.Render(" " + inLabel)
		} else {
			outLabel = dimStyle.Render(" " + outLabel)
			inLabel = accentStyle.Render("[" + inLabel + "]")
		}
		b.WriteString("  " + outLabel + " | " + inLabel + "\n\n")
		b.WriteString(dimStyle.Render("  (Effects controls coming\n   in Phase C)") + "\n\n")
		b.WriteString(dimStyle.Render("  " + FooterHintBack + " to call"))

	case m.call.State == CallStateActive:
		title = "Call with " + m.call.PeerName
		dur := time.Since(m.call.StartTime)
		mins := int(dur.Minutes())
		secs := int(dur.Seconds()) % 60
		durationStr := fmt.Sprintf("%d:%02d", mins, secs)

		b.WriteString("  Duration: " + accentStyle.Render(durationStr) + "\n")
		b.WriteString("  Status: " + accentStyle.Render("Connected") + "\n\n")

		micStatus := "on"
		if m.call.Muted {
			micStatus = "off"
		}
		peerStatus := "unmuted"
		if m.call.PeerMuted {
			peerStatus = "muted"
		}
		b.WriteString("  Mic: " + accentStyle.Render(micStatus) + "\n")
		b.WriteString("  Peer: " + accentStyle.Render(peerStatus) + "\n\n")

		b.WriteString(dimStyle.Render("  m: mute"+HintSep+"e: effects") + "\n")
		b.WriteString(dimStyle.Render("  Enter: chat"+HintSep+"q: end call") + "\n")
		b.WriteString(dimStyle.Render("  " + FooterHintClose))

	default:
		title = "Call"
		b.WriteString(dimStyle.Render("  Ending..."))
	}

	titleLine := " " + title + " "
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 2).
		Width(36).
		Render(b.String())

	// Replace top border segment with the title.
	lines := strings.SplitN(box, "\n", 2)
	if len(lines) == 2 {
		topBorder := lines[0]
		titleRendered := lipgloss.NewStyle().Bold(true).Foreground(ColorBorderActive).Render(titleLine)
		// Insert title after the first 2 chars of the border.
		if len(topBorder) > 4 {
			runeTop := []rune(topBorder)
			prefix := string(runeTop[:2])
			suffixStart := 2 + len([]rune(titleLine))
			if suffixStart < len(runeTop) {
				box = prefix + titleRendered + string(runeTop[suffixStart:]) + "\n" + lines[1]
			}
		}
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// audioCallTimerTickCmd returns a command that ticks once per second.
func audioCallTimerTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return AudioCallTimerTickMsg{}
	})
}
