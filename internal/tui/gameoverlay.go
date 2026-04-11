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

// GameOverlayCloseMsg hides the game to background.
type GameOverlayCloseMsg struct{}

// GameOverlayQuitMsg fully quits the game (no background).
type GameOverlayQuitMsg struct{}

// gameTickMsg drives the game loop.
type gameTickMsg struct{}

// GameSettings holds user-configurable game parameters.
// Persisted via the plugin settings system.
type GameSettings struct {
	SizeMultiplier float64 // snake: 0.5 to 4.0 (1.0 = default)
	SpeedFactor    float64 // 0.1 to 5.0 (1.0 = normal)
	FullScreen     bool    // if true, board fills the window
	TetrisCols     int     // tetris board columns (default 20)
	TetrisRows     int     // tetris board rows (default 30)
}

func defaultGameSettings() GameSettings {
	return GameSettings{
		SizeMultiplier: 1.0,
		SpeedFactor:    1.0,
		TetrisCols:     20,
		TetrisRows:     30,
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
	// Input throttling: drop rapid key repeats to prevent queue buildup.
	lastInput    time.Time
	inputQueue   int // count of keys processed this throttle window
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

	// Base snake board size at 1.0x scale. The multiplier is always
	// applied to these constants, never to the current board size.
	const snakeBaseW = 60
	const snakeBaseH = 30

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
			w = int(float64(snakeBaseW) * sm)
			h = int(float64(snakeBaseH) * sm)
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
		cols := m.settings.TetrisCols
		rows := m.settings.TetrisRows
		if cols <= 0 {
			cols = 20
		}
		if rows <= 0 {
			rows = 30
		}
		if m.settings.FullScreen {
			cols = maxW - 14 // subtract side panel width
			rows = maxH - 2
		}
		// Clamp to window (board + 14 for side panel, + 2 for border).
		if cols > maxW-14 {
			cols = maxW - 14
		}
		if rows > maxH-2 {
			rows = maxH - 2
		}
		if cols < 6 {
			cols = 6
		}
		if rows < 10 {
			rows = 10
		}
		m.tetris = games.NewTetrisGameSized(cols, rows)
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

		// Settings menu intercepts all input when open (no throttle).
		if m.showSettings {
			return m.updateSettings(key)
		}

		// Throttle game movement keys: max 1 input per 30ms,
		// max 5 queued inputs per window. Control keys (ctrl+*,
		// p, r, esc) bypass throttling.
		isControl := key == "ctrl+q" || key == "ctrl+s" || key == "p" ||
			key == "r" || key == "esc" || key == " "
		if !isControl && !m.paused {
			now := time.Now()
			elapsed := now.Sub(m.lastInput)
			if elapsed < 30*time.Millisecond {
				m.inputQueue++
				if m.inputQueue > 5 {
					return m, nil // drop excess input
				}
			} else {
				m.inputQueue = 0
			}
			m.lastInput = now
		}

		// Universal game controls.
		switch key {
		case "ctrl+q":
			// Hide game to background (pause + keep state).
			// The model handles backgrounding via GameOverlayCloseMsg
			// which now means "hide", not "destroy".
			return m, func() tea.Msg { return GameOverlayCloseMsg{} }
		case "ctrl+s":
			m.showSettings = true
			m.settingSel = 0
			m.settingEditing = false
			m.paused = true
			return m, nil
		case "esc":
			// Escape does nothing in game — only closes settings.
			return m, nil
		case "p", " ":
			// Space and P both toggle pause.
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

// settingItem describes one row in the settings menu.
type settingItem struct {
	key    string // internal key for dispatch
	label  string
	value  string
	desc   string
	toggle bool // true = Enter toggles, not edits
}

func (m GameOverlayModel) buildSettingItems() []settingItem {
	fsLabel := "off"
	if m.settings.FullScreen {
		fsLabel = "on"
	}
	items := []settingItem{
		{key: "fullscreen", label: "Full Screen", value: fsLabel, desc: "Fill the entire window (Enter to toggle)", toggle: true},
	}
	if m.gameName == "snake" {
		sizeVal := fmt.Sprintf("%.1fx", m.settings.SizeMultiplier)
		sizeDesc := "Multiplier: 0.5 to 4.0"
		if m.settings.FullScreen {
			sizeVal = "(full screen)"
			sizeDesc = "Disabled in full screen mode"
		}
		items = append(items, settingItem{key: "size", label: "Board Size", value: sizeVal, desc: sizeDesc})
	}
	if m.gameName == "tetris" {
		colVal := fmt.Sprintf("%d", m.settings.TetrisCols)
		rowVal := fmt.Sprintf("%d", m.settings.TetrisRows)
		if m.settings.FullScreen {
			colVal = "(auto)"
			rowVal = "(auto)"
		}
		items = append(items,
			settingItem{key: "cols", label: "Columns", value: colVal, desc: "Board width in cells (6-60)"},
			settingItem{key: "rows", label: "Rows", value: rowVal, desc: "Board height in cells (10-80)"},
		)
	}
	items = append(items,
		settingItem{key: "speed", label: "Speed", value: fmt.Sprintf("%.1fx", m.settings.SpeedFactor), desc: "Speed factor: 0.1 (slow) to 5.0 (fast)"},
		settingItem{key: "save", label: "[ Save & Restart ]", desc: "Apply settings and restart the game"},
		settingItem{key: "cancel", label: "[ Cancel ]", desc: "Return to game without changes"},
		settingItem{key: "quit", label: "[ Quit Game ]", desc: "Exit the game completely"},
	)
	return items
}

func (m GameOverlayModel) updateSettings(key string) (GameOverlayModel, tea.Cmd) {
	items := m.buildSettingItems()
	maxSel := len(items) - 1

	if m.settingEditing {
		switch key {
		case "enter":
			m.settingEditing = false
			m.applySettingInput(items)
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
		if m.settingSel >= 0 && m.settingSel < len(items) {
			item := items[m.settingSel]
			switch item.key {
			case "fullscreen":
				m.settings.FullScreen = !m.settings.FullScreen
			case "size":
				if !m.settings.FullScreen {
					m.settingEditing = true
					m.settingInput = fmt.Sprintf("%.1f", m.settings.SizeMultiplier)
				}
			case "cols":
				if !m.settings.FullScreen {
					m.settingEditing = true
					m.settingInput = fmt.Sprintf("%d", m.settings.TetrisCols)
				}
			case "rows":
				if !m.settings.FullScreen {
					m.settingEditing = true
					m.settingInput = fmt.Sprintf("%d", m.settings.TetrisRows)
				}
			case "speed":
				m.settingEditing = true
				m.settingInput = fmt.Sprintf("%.1f", m.settings.SpeedFactor)
			case "save":
				m.showSettings = false
				m.initGame()
				m.paused = false
				return m, m.TickCmd()
			case "cancel":
				m.showSettings = false
				m.paused = false
				return m, m.TickCmd()
			case "quit":
				return m, func() tea.Msg { return GameOverlayQuitMsg{} }
			}
		}
	case "esc":
		m.showSettings = false
		m.paused = false
		return m, m.TickCmd()
	}
	return m, nil
}

func (m *GameOverlayModel) applySettingInput(items []settingItem) {
	if m.settingSel < 0 || m.settingSel >= len(items) {
		return
	}
	item := items[m.settingSel]
	switch item.key {
	case "size":
		val, err := strconv.ParseFloat(m.settingInput, 64)
		if err != nil {
			return
		}
		if val < 0.5 {
			val = 0.5
		}
		if val > 4.0 {
			val = 4.0
		}
		m.settings.SizeMultiplier = val
	case "cols":
		val, err := strconv.Atoi(m.settingInput)
		if err != nil {
			return
		}
		if val < 6 {
			val = 6
		}
		if val > 60 {
			val = 60
		}
		m.settings.TetrisCols = val
	case "rows":
		val, err := strconv.Atoi(m.settingInput)
		if err != nil {
			return
		}
		if val < 10 {
			val = 10
		}
		if val > 80 {
			val = 80
		}
		m.settings.TetrisRows = val
	case "speed":
		val, err := strconv.ParseFloat(m.settingInput, 64)
		if err != nil {
			return
		}
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
		footer = "←→: move" + HintSep + "↑: rotate" + HintSep + "↓: soft drop" + HintSep + "Enter: hard drop" + HintSep + "P: pause" + HintSep + "Ctrl+S: settings" + HintSep + "Ctrl+Q: hide"
	} else {
		footer = "↑↓←→/WASD: move" + HintSep + "P: pause" + HintSep + "R: restart" + HintSep + "Ctrl+S: settings" + HintSep + "Ctrl+Q: hide"
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

	items := m.buildSettingItems()
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
