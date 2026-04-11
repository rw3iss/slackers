package tui

// Game overlay — takes full keyboard control to run interactive
// games (snake, tetris, etc.). The overlay intercepts all key
// events and routes them to the active game. A periodic tick
// drives the game loop.
//
// Ctrl+Q exits the game. Ctrl+S opens the in-game settings menu
// (board size, speed). Space/P pauses. R restarts after game over.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/plugins/games"
)

// GameOverlayOpenMsg opens the game overlay with the named game.
type GameOverlayOpenMsg struct {
	GameName string
}

// GameOverlayCloseMsg closes the game overlay.
type GameOverlayCloseMsg struct{}

// gameTickMsg drives the game loop.
type gameTickMsg struct{}

// GameSettings holds user-configurable game parameters.
// Persisted via the plugin settings system.
type GameSettings struct {
	SizeMultiplier float64 // 0.5 to 4.0 (1.0 = default 30x15)
	SpeedFactor    float64 // 0.1 to 5.0 (1.0 = normal)
	FullScreen     bool    // if true, board fills the window
}

func defaultGameSettings() GameSettings {
	return GameSettings{
		SizeMultiplier: 1.0,
		SpeedFactor:    1.0,
	}
}

// GameOverlayModel manages an active game session.
type GameOverlayModel struct {
	gameName string
	snake    *games.SnakeGame
	tetris   *games.TetrisGame
	paused   bool
	width    int
	height   int
	settings GameSettings
	// Settings menu state.
	showSettings   bool
	settingSel     int // 0=size, 1=speed, 2=save, 3=cancel
	settingEditing bool
	settingInput   string
}

// NewGameOverlay creates a game overlay for the given game name.
func NewGameOverlay(name string, settings GameSettings) GameOverlayModel {
	m := GameOverlayModel{
		gameName: name,
		settings: settings,
	}
	m.initGame()
	return m
}

func (m *GameOverlayModel) initGame() {
	// Available space inside the overlay: subtract border (2),
	// padding (6), header (2 lines), footer (2 lines), score (1 line).
	maxW := m.width - 10
	maxH := m.height - 10
	if maxW < 10 {
		maxW = 10
	}
	if maxH < 8 {
		maxH = 8
	}

	switch m.gameName {
	case "snake":
		var w, h int
		if m.settings.FullScreen {
			w = maxW
			h = maxH
		} else {
			sm := m.settings.SizeMultiplier
			if sm <= 0 {
				sm = 1.0
			}
			w = int(float64(30) * sm)
			h = int(float64(15) * sm)
		}
		// Clamp to available space.
		if w > maxW {
			w = maxW
		}
		if h > maxH {
			h = maxH
		}
		// Minimums.
		if w < 10 {
			w = 10
		}
		if h < 8 {
			h = 8
		}
		m.snake = games.NewSnakeGameSized(w, h)
		m.tetris = nil
	case "tetris":
		m.tetris = games.NewTetrisGame()
		m.snake = nil
	}
}

func (m *GameOverlayModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m GameOverlayModel) baseSpeed() time.Duration {
	switch m.gameName {
	case "tetris":
		return 500 * time.Millisecond
	default:
		return 150 * time.Millisecond
	}
}

// TickCmd returns the tea.Cmd that schedules the next game tick.
func (m GameOverlayModel) TickCmd() tea.Cmd {
	if m.paused || m.showSettings {
		return nil
	}
	sf := m.settings.SpeedFactor
	if sf <= 0 {
		sf = 1.0
	}
	// Higher speed factor = faster = shorter interval.
	interval := time.Duration(float64(m.baseSpeed()) / sf)
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return gameTickMsg{}
	})
}

func (m GameOverlayModel) isGameOver() bool {
	if m.snake != nil {
		return m.snake.IsGameOver()
	}
	if m.tetris != nil {
		return m.tetris.IsGameOver()
	}
	return false
}

func (m GameOverlayModel) Update(msg tea.Msg) (GameOverlayModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		key := v.String()

		// Settings menu intercepts all input when open.
		if m.showSettings {
			return m.updateSettings(key)
		}

		// Universal game controls.
		switch key {
		case "ctrl+q":
			return m, func() tea.Msg { return GameOverlayCloseMsg{} }
		case "ctrl+s":
			m.showSettings = true
			m.settingSel = 0
			m.settingEditing = false
			m.paused = true
			return m, nil
		case "p", " ":
			if !m.isGameOver() {
				m.paused = !m.paused
				if !m.paused {
					return m, m.TickCmd()
				}
			}
			return m, nil
		case "r":
			if m.isGameOver() {
				m.initGame()
				m.paused = false
				return m, m.TickCmd()
			}
		}

		// Game-specific controls.
		if !m.paused {
			if m.snake != nil {
				switch key {
				case "up", "w":
					m.snake.SetDirection(games.DirUp)
				case "down", "s":
					m.snake.SetDirection(games.DirDown)
				case "left", "a":
					m.snake.SetDirection(games.DirLeft)
				case "right", "d":
					m.snake.SetDirection(games.DirRight)
				}
			}
			if m.tetris != nil {
				switch key {
				case "left", "a":
					m.tetris.MoveLeft()
				case "right", "d":
					m.tetris.MoveRight()
				case "up", "w":
					m.tetris.Rotate()
				case "down", "s":
					m.tetris.Tick() // soft drop
				case " ":
					// Space is already handled for pause above,
					// but we can use it for hard drop when not pausing.
				case "enter":
					m.tetris.Drop() // hard drop
				}
			}
		}
		return m, nil

	case gameTickMsg:
		if m.paused || m.showSettings {
			return m, nil
		}
		if m.snake != nil {
			m.snake.Tick()
			if m.snake.IsGameOver() {
				return m, nil
			}
		}
		if m.tetris != nil {
			m.tetris.Tick()
			if m.tetris.IsGameOver() {
				return m, nil
			}
		}
		return m, m.TickCmd()
	}
	return m, nil
}

func (m GameOverlayModel) updateSettings(key string) (GameOverlayModel, tea.Cmd) {
	if m.settingEditing {
		switch key {
		case "enter":
			m.settingEditing = false
			m.applySettingInput()
		case "esc":
			m.settingEditing = false
			m.settingInput = ""
		case "backspace":
			if len(m.settingInput) > 0 {
				m.settingInput = m.settingInput[:len(m.settingInput)-1]
			}
		default:
			if len(key) == 1 && (key[0] >= '0' && key[0] <= '9' || key[0] == '.') {
				m.settingInput += key
			}
		}
		return m, nil
	}

	// Menu items: 0=fullscreen, 1=size, 2=speed, 3=save, 4=cancel
	maxSel := 4
	switch key {
	case "up", "k":
		if m.settingSel > 0 {
			m.settingSel--
		}
	case "down", "j":
		if m.settingSel < maxSel {
			m.settingSel++
		}
	case "enter":
		switch m.settingSel {
		case 0: // fullscreen toggle
			m.settings.FullScreen = !m.settings.FullScreen
		case 1: // size
			if !m.settings.FullScreen {
				m.settingEditing = true
				m.settingInput = fmt.Sprintf("%.1f", m.settings.SizeMultiplier)
			}
		case 2: // speed
			m.settingEditing = true
			m.settingInput = fmt.Sprintf("%.1f", m.settings.SpeedFactor)
		case 3: // save
			m.showSettings = false
			m.initGame()
			m.paused = false
			return m, m.TickCmd()
		case 4: // cancel
			m.showSettings = false
			m.paused = false
			return m, m.TickCmd()
		}
	case "esc":
		m.showSettings = false
		m.paused = false
		return m, m.TickCmd()
	}
	return m, nil
}

func (m *GameOverlayModel) applySettingInput() {
	val, err := strconv.ParseFloat(m.settingInput, 64)
	if err != nil {
		return
	}
	switch m.settingSel {
	case 1: // size (index shifted by fullscreen toggle at 0)
		if val < 0.5 {
			val = 0.5
		}
		if val > 4.0 {
			val = 4.0
		}
		m.settings.SizeMultiplier = val
	case 2: // speed
		if val < 0.1 {
			val = 0.1
		}
		if val > 5.0 {
			val = 5.0
		}
		m.settings.SpeedFactor = val
	}
	m.settingInput = ""
}

// Settings returns current settings (for persistence).
func (m GameOverlayModel) Settings() GameSettings {
	return m.settings
}

func (m GameOverlayModel) View() string {
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	scoreStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)

	var b strings.Builder

	// Header with score.
	if m.snake != nil {
		b.WriteString("  " + scoreStyle.Render(fmt.Sprintf("Score: %d", m.snake.Score())))
	}
	if m.tetris != nil {
		b.WriteString("  " + scoreStyle.Render(fmt.Sprintf("Score: %d  Level: %d  Lines: %d",
			m.tetris.Score(), m.tetris.Level(), m.tetris.Lines())))
	}
	if m.paused && !m.showSettings {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorHighlight).Render("[PAUSED]"))
	}
	b.WriteString("\n\n")

	// Settings overlay or game render.
	if m.showSettings {
		b.WriteString(m.renderSettings())
	} else if m.snake != nil {
		b.WriteString(m.snake.RenderFrame())
		if m.snake.IsGameOver() {
			b.WriteString("\n\n")
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("  GAME OVER!"))
			b.WriteString("  " + dimStyle.Render("R: restart · Ctrl+Q: quit"))
		}
	} else if m.tetris != nil {
		b.WriteString(m.tetris.RenderFrame())
		if m.tetris.IsGameOver() {
			b.WriteString("\n\n")
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("  GAME OVER!"))
			b.WriteString("  " + dimStyle.Render("R: restart · Ctrl+Q: quit"))
		}
	} else {
		b.WriteString(dimStyle.Render("  Game not available: " + m.gameName))
	}

	// Footer.
	title := strings.ToUpper(m.gameName[:1]) + m.gameName[1:]
	var footer string
	if m.showSettings {
		footer = "↑↓: select" + HintSep + "Enter: edit/apply" + HintSep + "Esc: cancel"
	} else if m.gameName == "tetris" {
		footer = "←→: move" + HintSep + "↑: rotate" + HintSep + "↓: soft drop" + HintSep + "Enter: hard drop" + HintSep + "P: pause" + HintSep + "Ctrl+S: settings" + HintSep + "Ctrl+Q: quit"
	} else {
		footer = "↑↓←→/WASD: move" + HintSep + "P: pause" + HintSep + "R: restart" + HintSep + "Ctrl+S: settings" + HintSep + "Ctrl+Q: quit"
	}

	maxBoxW := 70
	if m.settings.FullScreen {
		maxBoxW = m.width - 2
	} else {
		// Scale box width based on board size.
		sm := m.settings.SizeMultiplier
		if sm > 1.0 {
			maxBoxW = int(float64(70) * sm)
		}
		if maxBoxW > m.width-2 {
			maxBoxW = m.width - 2
		}
	}
	scaffold := OverlayScaffold{
		Title:       title,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: maxBoxW,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}

func (m GameOverlayModel) renderSettings() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)

	var b strings.Builder
	b.WriteString(titleStyle.Render("  Game Settings"))
	b.WriteString("\n\n")

	fsLabel := "off"
	if m.settings.FullScreen {
		fsLabel = "on"
	}
	sizeVal := fmt.Sprintf("%.1fx", m.settings.SizeMultiplier)
	sizeDesc := "Multiplier: 0.5 to 4.0 (1.0 = default 30x15)"
	if m.settings.FullScreen {
		sizeVal = "(full screen)"
		sizeDesc = "Disabled in full screen mode"
	}
	items := []struct {
		label string
		value string
		desc  string
	}{
		{"Full Screen", fsLabel, "Fill the entire window (Enter to toggle)"},
		{"Board Size", sizeVal, sizeDesc},
		{"Speed", fmt.Sprintf("%.1fx", m.settings.SpeedFactor), "Speed factor: 0.1 (slow) to 5.0 (fast)"},
		{"[ Save & Restart ]", "", "Apply settings and restart the game"},
		{"[ Cancel ]", "", "Return to game without changes"},
	}

	for i, item := range items {
		cursor := "  "
		style := itemStyle
		if i == m.settingSel {
			cursor = selStyle.Render("> ")
			style = selStyle
		}

		if item.value != "" {
			val := valStyle.Render(item.value)
			if m.settingEditing && i == m.settingSel {
				val = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true).Render(m.settingInput + "▌")
			}
			b.WriteString(cursor + style.Render(item.label+": ") + val)
		} else {
			b.WriteString(cursor + style.Render(item.label))
		}
		b.WriteString("\n")
		if i == m.settingSel {
			b.WriteString("    " + dimStyle.Render(item.desc) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
