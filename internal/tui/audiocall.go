package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/audio"
)

// AudioCallCloseMsg closes the call overlay but leaves the call running.
type AudioCallCloseMsg struct{}

// AudioCallEndMsg ends the call entirely.
type AudioCallEndMsg struct{}

// AudioCallAcceptMsg accepts an incoming call.
type AudioCallAcceptMsg struct{}

// AudioCallTimerTickMsg fires once per second to refresh the call duration.
type AudioCallTimerTickMsg struct{}

// AudioMeterTickMsg fires at ~20Hz to refresh meter bars in the effects view.
type AudioMeterTickMsg struct{}

// AudioCallModel is the overlay for ringing/active/effects states of a call.
type AudioCallModel struct {
	call           *ActiveCall
	width          int
	height         int
	showEffects    bool
	effectsTab     int // 0=outgoing, 1=incoming
	effectsSel     int
	monitorMode    bool
	engine         *audio.Engine
	eqSelectedBand int
	profiles       []audio.EffectProfile
	configDir      string
}

// Effects row indices:
// 0        = EQ on/off
// 1-7      = EQ bands 0-6
// 8        = Comp on/off
// 9        = Comp threshold
// 10       = Comp ratio
// 11       = Comp attack
// 12       = Comp release
// 13       = Comp makeup
const effectsRowCount = 14

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

// SetEngine binds the audio engine for effects control.
func (m *AudioCallModel) SetEngine(e *audio.Engine) { m.engine = e }

// SetConfigDir sets the directory for saving effect profiles.
func (m *AudioCallModel) SetConfigDir(dir string) { m.configDir = dir }

// SetProfiles sets the available effect profiles.
func (m *AudioCallModel) SetProfiles(p []audio.EffectProfile) { m.profiles = p }

// activeChain returns the effect chain for the currently selected tab.
func (m *AudioCallModel) activeChain() *audio.EffectChain {
	if m.engine == nil {
		return nil
	}
	if m.effectsTab == 0 {
		return m.engine.OutgoingEffects()
	}
	return m.engine.IncomingEffects()
}

// Update handles key events for the audio call overlay.
func (m AudioCallModel) Update(msg tea.Msg) (AudioCallModel, tea.Cmd) {
	switch msg := msg.(type) {
	case AudioMeterTickMsg:
		if m.monitorMode && m.showEffects {
			return m, audioMeterTickCmd()
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m AudioCallModel) handleKey(msg tea.KeyMsg) (AudioCallModel, tea.Cmd) {
	key := msg.String()

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
			return m.handleEffectsKey(key)
		}

		// Main active-call screen.
		switch key {
		case "m":
			m.call.Muted = !m.call.Muted
			return m, nil
		case "e":
			m.showEffects = true
			var cmd tea.Cmd
			if m.monitorMode {
				cmd = audioMeterTickCmd()
			}
			return m, cmd
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

func (m AudioCallModel) handleEffectsKey(key string) (AudioCallModel, tea.Cmd) {
	chain := m.activeChain()

	switch key {
	case "esc":
		m.showEffects = false
		return m, nil
	case "tab":
		m.effectsTab = (m.effectsTab + 1) % 2
		m.effectsSel = 0
		return m, nil
	case "up", "k":
		if m.effectsSel > 0 {
			m.effectsSel--
		} else {
			m.effectsSel = effectsRowCount - 1
		}
		return m, nil
	case "down", "j":
		m.effectsSel++
		if m.effectsSel >= effectsRowCount {
			m.effectsSel = 0
		}
		return m, nil
	case "v":
		m.monitorMode = !m.monitorMode
		if m.monitorMode {
			return m, audioMeterTickCmd()
		}
		return m, nil
	case "p":
		// Save current settings to the active profile.
		m.saveCurrentProfile()
		return m, nil
	case "left", "h":
		if chain != nil {
			m.adjustParam(chain, m.effectsSel, -1)
		}
		return m, nil
	case "right", "l":
		if chain != nil {
			m.adjustParam(chain, m.effectsSel, +1)
		}
		return m, nil
	}
	return m, nil
}

func (m *AudioCallModel) adjustParam(chain *audio.EffectChain, row, dir int) {
	switch {
	case row == 0:
		// EQ on/off toggle
		chain.EQEnabled = !chain.EQEnabled
	case row >= 1 && row <= 7:
		// EQ band gain ±0.5 dB
		band := row - 1
		chain.EQ.Bands[band].SetGain(chain.EQ.Bands[band].Gain + float32(dir)*0.5)
	case row == 8:
		// Comp on/off toggle
		chain.CompEnabled = !chain.CompEnabled
	case row == 9:
		// Threshold ±1 dB
		chain.Comp.Threshold += float32(dir) * 1.0
	case row == 10:
		// Ratio ±0.5
		chain.Comp.Ratio += float32(dir) * 0.5
		if chain.Comp.Ratio < 1 {
			chain.Comp.Ratio = 1
		}
	case row == 11:
		// Attack ±5 ms
		chain.Comp.AttackMs += float32(dir) * 5.0
		if chain.Comp.AttackMs < 0.1 {
			chain.Comp.AttackMs = 0.1
		}
	case row == 12:
		// Release ±5 ms
		chain.Comp.ReleaseMs += float32(dir) * 5.0
		if chain.Comp.ReleaseMs < 1 {
			chain.Comp.ReleaseMs = 1
		}
	case row == 13:
		// Makeup ±0.5 dB
		chain.Comp.MakeupGain += float32(dir) * 0.5
	}
}

func (m *AudioCallModel) saveCurrentProfile() {
	if m.engine == nil || len(m.profiles) == 0 {
		return
	}
	outCfg := audio.ChainToConfig(m.engine.OutgoingEffects())
	inCfg := audio.ChainToConfig(m.engine.IncomingEffects())
	m.profiles[0].Outgoing = outCfg
	m.profiles[0].Incoming = inCfg
	if m.configDir != "" {
		_ = audio.SaveProfiles(m.configDir, m.profiles)
	}
}

// View renders the call overlay as a centered bordered box.
func (m AudioCallModel) View() string {
	if m.call == nil {
		return ""
	}

	if m.call.State == CallStateActive && m.showEffects {
		return m.viewEffects()
	}

	return m.viewMain()
}

func (m AudioCallModel) viewMain() string {
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

	box := m.renderBox(title, b.String(), 36)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m AudioCallModel) viewEffects() string {
	chain := m.activeChain()
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	accentStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection)
	errStyle := lipgloss.NewStyle().Foreground(ColorError)

	var tabLabel string
	if m.effectsTab == 0 {
		tabLabel = "Outgoing (your mic)"
	} else {
		tabLabel = "Incoming (their audio)"
	}

	title := "Audio Effects"

	var b strings.Builder

	// Tab bar
	outLabel := "Outgoing"
	inLabel := "Incoming"
	if m.effectsTab == 0 {
		outLabel = accentStyle.Render("[" + outLabel + "]")
		inLabel = dimStyle.Render(" " + inLabel)
	} else {
		outLabel = dimStyle.Render(" " + outLabel)
		inLabel = accentStyle.Render("[" + inLabel + "]")
	}
	b.WriteString("  " + outLabel + " | " + inLabel + "\n")
	b.WriteString("  " + dimStyle.Render(tabLabel) + "\n\n")

	if chain == nil {
		b.WriteString(dimStyle.Render("  No audio engine active") + "\n")
	} else {
		// ── EQ section ──
		eqStatus := "OFF"
		eqStatusStyle := errStyle
		if chain.EQEnabled {
			eqStatus = "ON"
			eqStatusStyle = accentStyle
		}
		cursor := "  "
		if m.effectsSel == 0 {
			cursor = selStyle.Render("> ")
		}
		b.WriteString(cursor + dimStyle.Render("── 7-Band Equalizer ──") + " " + eqStatusStyle.Render("["+eqStatus+"]") + "\n\n")

		// Band labels row
		b.WriteString("  ")
		for i, band := range chain.EQ.Bands {
			label := fmt.Sprintf("%-7s", band.Label)
			if m.effectsSel == i+1 {
				b.WriteString(selStyle.Render(label))
			} else {
				b.WriteString(dimStyle.Render(label))
			}
		}
		b.WriteString("\n")

		// Band gain values row
		b.WriteString("  ")
		for i, band := range chain.EQ.Bands {
			val := fmt.Sprintf("%+5.1f  ", band.Gain)
			if m.effectsSel == i+1 {
				b.WriteString(selStyle.Render(val))
			} else {
				b.WriteString(accentStyle.Render(val))
			}
		}
		b.WriteString("\n")

		// Band visual indicators row
		b.WriteString("  ")
		for i, band := range chain.EQ.Bands {
			// Simple visual: bar height from -12 to +12
			var indicator string
			if band.Gain > 1 {
				indicator = "  ▲    "
			} else if band.Gain < -1 {
				indicator = "  ▼    "
			} else {
				indicator = "  ●    "
			}
			if m.effectsSel == i+1 {
				b.WriteString(selStyle.Render(indicator))
			} else {
				b.WriteString(dimStyle.Render(indicator))
			}
		}
		b.WriteString("\n")

		// Selected band detail
		if m.effectsSel >= 1 && m.effectsSel <= 7 {
			band := chain.EQ.Bands[m.effectsSel-1]
			b.WriteString("\n  " + selStyle.Render(fmt.Sprintf("  %s: %+.1f dB  (←/→ adjust ±0.5 dB)", band.Label, band.Gain)) + "\n")
		}
		b.WriteString("\n")

		// ── Compressor section ──
		compStatus := "OFF"
		compStatusStyle := errStyle
		if chain.CompEnabled {
			compStatus = "ON"
			compStatusStyle = accentStyle
		}
		cursor = "  "
		if m.effectsSel == 8 {
			cursor = selStyle.Render("> ")
		}
		b.WriteString(cursor + dimStyle.Render("── Compressor ──") + " " + compStatusStyle.Render("["+compStatus+"]") + "\n\n")

		// Compressor parameters
		type compParam struct {
			label string
			value string
			row   int
		}
		params := []compParam{
			{"Threshold", fmt.Sprintf("%.1f dB", chain.Comp.Threshold), 9},
			{"Ratio", fmt.Sprintf("%.1f:1", chain.Comp.Ratio), 10},
			{"Attack", fmt.Sprintf("%.1f ms", chain.Comp.AttackMs), 11},
			{"Release", fmt.Sprintf("%.1f ms", chain.Comp.ReleaseMs), 12},
			{"Makeup", fmt.Sprintf("%.1f dB", chain.Comp.MakeupGain), 13},
		}

		// Render in two columns where possible
		for i := 0; i < len(params); i += 2 {
			cursor = "  "
			if m.effectsSel == params[i].row {
				cursor = selStyle.Render("> ")
			}
			left := fmt.Sprintf("%-12s %10s", params[i].label+":", params[i].value)
			if m.effectsSel == params[i].row {
				left = selStyle.Render(left)
			} else {
				left = accentStyle.Render(left)
			}

			right := ""
			if i+1 < len(params) {
				rightCursor := "    "
				if m.effectsSel == params[i+1].row {
					rightCursor = selStyle.Render("  > ")
				}
				rv := fmt.Sprintf("%-12s %10s", params[i+1].label+":", params[i+1].value)
				if m.effectsSel == params[i+1].row {
					rv = selStyle.Render(rv)
				} else {
					rv = accentStyle.Render(rv)
				}
				right = rightCursor + rv
			}

			b.WriteString(cursor + left + right + "\n")
		}
		b.WriteString("\n")

		// Meters
		if m.monitorMode {
			inputBar := audio.MeterBar(chain.Comp.InputLevel, 30, 60)
			grBar := audio.GainReductionBar(chain.Comp.GainReduction, 30, 24)
			outputBar := audio.MeterBar(chain.Comp.OutputLevel, 30, 60)

			b.WriteString("  " + dimStyle.Render("Input  ") + " " + accentStyle.Render(inputBar) + fmt.Sprintf("  %5.1f dB", chain.Comp.InputLevel) + "\n")
			b.WriteString("  " + dimStyle.Render("GR     ") + " " + errStyle.Render(grBar) + fmt.Sprintf("  %5.1f dB", chain.Comp.GainReduction) + "\n")
			b.WriteString("  " + dimStyle.Render("Output ") + " " + accentStyle.Render(outputBar) + fmt.Sprintf("  %5.1f dB", chain.Comp.OutputLevel) + "\n")
		} else {
			b.WriteString("  " + dimStyle.Render("(press v to enable live meters)") + "\n")
		}
	}

	b.WriteString("\n")

	// Footer hints
	monLabel := "v: meters on"
	if m.monitorMode {
		monLabel = "v: meters off"
	}
	b.WriteString(dimStyle.Render("  Tab: switch chain" + HintSep + "p: save profile" + HintSep + monLabel) + "\n")
	b.WriteString(dimStyle.Render("  ←/→: adjust" + HintSep + "↑/↓: select" + HintSep + FooterHintBack + " to call"))

	box := m.renderBox(title, b.String(), 80)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m AudioCallModel) renderBox(title, content string, width int) string {
	titleLine := " " + title + " "
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 2).
		Width(width).
		Render(content)

	// Replace top border segment with the title.
	lines := strings.SplitN(box, "\n", 2)
	if len(lines) == 2 {
		topBorder := lines[0]
		titleRendered := lipgloss.NewStyle().Bold(true).Foreground(ColorBorderActive).Render(titleLine)
		if len(topBorder) > 4 {
			runeTop := []rune(topBorder)
			prefix := string(runeTop[:2])
			suffixStart := 2 + len([]rune(titleLine))
			if suffixStart < len(runeTop) {
				box = prefix + titleRendered + string(runeTop[suffixStart:]) + "\n" + lines[1]
			}
		}
	}

	return box
}

// audioCallTimerTickCmd returns a command that ticks once per second.
func audioCallTimerTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return AudioCallTimerTickMsg{}
	})
}

// audioMeterTickCmd returns a command that ticks at ~20Hz for meter updates.
func audioMeterTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return AudioMeterTickMsg{}
	})
}
