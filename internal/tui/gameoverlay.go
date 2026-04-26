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

// gameTickMsg drives the game loop (gravity, snake movement).
type gameTickMsg struct{}

// gameInputTickMsg drives the fast input polling loop for tetris.
// Movement keys are not executed on KeyMsg — they just record the
// latest key. This tick applies the latest key at a fixed rate,
// preventing OS key-repeat from building up a queue.
type gameInputTickMsg struct{}

// GameSettings holds user-configurable game parameters.
type GameSettings struct {
	SpeedFactor    float64 // 0.1 to 5.0 (1.0 = normal)
	HalveVertical  bool    // double horizontal to compensate for tall terminal chars
	SnakeCols      int     // snake board columns (default 30)
	SnakeRows      int     // snake board rows (default 20)
	TetrisCols     int     // tetris logical columns (default 10)
	TetrisRows     int     // tetris logical rows (default 15)
	BlockScale     int     // tetris block render scale: 1 or 2
	SnakeHighScore int     // persisted high score
	TetrisHighScore int    // persisted high score
}

func defaultGameSettings() GameSettings {
	return GameSettings{
		SpeedFactor:   1.0,
		HalveVertical: true,
		SnakeCols:     30,
		SnakeRows:     20,
		TetrisCols:    30,
		TetrisRows:    40,
		BlockScale:    1,
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
	// Polled input for tetris: KeyMsg just records the latest key;
	// gameInputTickMsg applies it at a fixed rate (~50ms). This
	// prevents OS key-repeat from queuing up dozens of moves.
	heldKey   string    // latest movement key ("left", "right", etc.) or ""
	heldKeyAt time.Time // when heldKey was last refreshed by a KeyMsg
	// Settings menu state.
	showSettings   bool
	settingSel     int // 0=size, 1=speed, 2=save, 3=cancel
	settingEditing bool
	settingInput   string
}

// NewGameOverlay creates a game overlay for the given game name.
func NewGameOverlay(name string, settings GameSettings, w, h int) GameOverlayModel {
	m := GameOverlayModel{
		gameName: name,
		settings: settings,
		width:    w,
		height:   h,
	}
	m.initGame()
	return m
}

func (m *GameOverlayModel) initGame() {
	// Available space inside the overlay — the game window is always
	// full-screen, so we use nearly the entire terminal.
	maxW := m.width - 6 // borders + minimal padding
	maxH := m.height - 6
	if maxW < 10 {
		maxW = 10
	}
	if maxH < 8 {
		maxH = 8
	}

	switch m.gameName {
	case "snake":
		cols := m.settings.SnakeCols
		rows := m.settings.SnakeRows
		if cols <= 0 {
			cols = 30
		}
		if rows <= 0 {
			rows = 20
		}
		// Apply halve-vertical: double horizontal chars per cell.
		hScale := 1
		if m.settings.HalveVertical {
			hScale = 2
		}
		// Clamp logical size so rendered board fits window.
		maxLogicalCols := maxW / hScale
		if cols > maxLogicalCols {
			cols = maxLogicalCols
		}
		if rows > maxH {
			rows = maxH
		}
		if cols < 6 {
			cols = 6
		}
		if rows < 6 {
			rows = 6
		}
		m.snake = games.NewSnakeGameSized(cols, rows)
		m.snake.SetHalveVertical(m.settings.HalveVertical)
		m.snake.SetHScale(hScale)
		m.tetris = nil
	case "tetris":
		cols := m.settings.TetrisCols
		rows := m.settings.TetrisRows
		if cols <= 0 {
			cols = 30
		}
		if rows <= 0 {
			rows = 40
		}
		// Compute render scale factors.
		bs := m.settings.BlockScale
		if bs < 1 {
			bs = 1
		}
		hScale := bs
		vScale := bs
		if m.settings.HalveVertical {
			hScale *= 2 // double horizontal to compensate
		}
		// Clamp logical size so rendered board fits window.
		// Rendered width = cols*hScale + borders(2) + side panel(14).
		// Rendered height = rows*vScale + borders(2).
		maxLogicalCols := (maxW - 16) / hScale
		maxLogicalRows := (maxH - 2) / vScale
		if cols > maxLogicalCols {
			cols = maxLogicalCols
		}
		if rows > maxLogicalRows {
			rows = maxLogicalRows
		}
		if cols < 4 {
			cols = 4
		}
		if rows < 6 {
			rows = 6
		}
		m.tetris = games.NewTetrisGameSized(cols, rows)
		m.tetris.SetRenderScale(hScale, vScale)
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

// InputTickCmd schedules the next fast input poll for tetris movement.
func (m GameOverlayModel) InputTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return gameInputTickMsg{}
	})
}

// applyHeldKey executes the current held movement key on the tetris game.
func (m *GameOverlayModel) applyHeldKey() {
	if m.tetris == nil {
		return
	}
	switch m.heldKey {
	case "left", "a":
		m.tetris.MoveLeft()
	case "right", "d":
		m.tetris.MoveRight()
	case "up", "w":
		m.tetris.Rotate()
	case "down", "s":
		m.tetris.Tick()
	}
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
			m.heldKey = ""
			return m, nil
		case "esc":
			// Escape does nothing in game — only closes settings.
			return m, nil
		case "p", " ":
			// Space and P both toggle pause.
			if !m.isGameOver() {
				m.paused = !m.paused
				m.heldKey = ""
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
				// Movement keys are NOT executed here. We just
				// record the latest key; gameInputTickMsg applies
				// it at a fixed 50ms rate. This prevents OS
				// key-repeat events from building a movement queue.
				switch key {
				case "left", "a", "right", "d", "up", "w", "down", "s":
					wasEmpty := m.heldKey == ""
					m.heldKey = key
					m.heldKeyAt = time.Now()
					if wasEmpty {
						// First press: apply immediately for
						// responsiveness, then start polling.
						m.applyHeldKey()
						return m, m.InputTickCmd()
					}
				case "enter":
					// Hard drop always executes (one-shot).
					m.tetris.Drop()
				}
			}
		}
		return m, nil

	case gameInputTickMsg:
		// Fast input poll for tetris: apply the held key if it's
		// still fresh (key repeat events keep refreshing heldKeyAt).
		// Once the key is released, repeat events stop and the
		// held key goes stale after ~120ms, ending the poll loop.
		if m.tetris == nil || m.paused || m.showSettings || m.isGameOver() {
			m.heldKey = ""
			return m, nil
		}
		if m.heldKey == "" {
			return m, nil
		}
		if time.Since(m.heldKeyAt) > 120*time.Millisecond {
			// Key was released — stop polling.
			m.heldKey = ""
			return m, nil
		}
		m.applyHeldKey()
		return m, m.InputTickCmd()

	case gameTickMsg:
		if m.paused || m.showSettings {
			return m, nil
		}
		if m.snake != nil {
			m.snake.Tick()
			if m.snake.IsGameOver() {
				if m.snake.Score() > m.settings.SnakeHighScore {
					m.settings.SnakeHighScore = m.snake.Score()
				}
				return m, nil
			}
		}
		if m.tetris != nil {
			m.tetris.Tick()
			if m.tetris.IsGameOver() {
				if m.tetris.Score() > m.settings.TetrisHighScore {
					m.settings.TetrisHighScore = m.tetris.Score()
				}
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
	var items []settingItem
	if m.gameName == "snake" {
		items = append(items,
			settingItem{key: "cols", label: "Columns", value: fmt.Sprintf("%d", m.settings.SnakeCols), desc: "Board width in cells (6-100)"},
			settingItem{key: "rows", label: "Rows", value: fmt.Sprintf("%d", m.settings.SnakeRows), desc: "Board height in cells (6-60)"},
		)
	}
	if m.gameName == "tetris" {
		bsVal := fmt.Sprintf("%d", m.settings.BlockScale)
		if m.settings.BlockScale < 1 {
			bsVal = "1"
		}
		items = append(items,
			settingItem{key: "cols", label: "Columns", value: fmt.Sprintf("%d", m.settings.TetrisCols), desc: "Board width in logical cells (4-60)"},
			settingItem{key: "rows", label: "Rows", value: fmt.Sprintf("%d", m.settings.TetrisRows), desc: "Board height in logical cells (6-80)"},
			settingItem{key: "blockscale", label: "Block Scale", value: bsVal, desc: "←/→ to change: 1 = normal, 2 = double", toggle: true},
		)
	}
	hvLabel := "off"
	if m.settings.HalveVertical {
		hvLabel = "on"
	}
	items = append(items,
		settingItem{key: "halvevert", label: "Halve Vertical", value: hvLabel,
			desc: "Doubles horizontal scale to compensate for tall terminal chars", toggle: true},
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
	case "left", "right":
		if m.settingSel >= 0 && m.settingSel < len(items) {
			item := items[m.settingSel]
			if item.key == "blockscale" {
				if key == "left" && m.settings.BlockScale > 1 {
					m.settings.BlockScale = 1
				} else if key == "right" && m.settings.BlockScale < 2 {
					m.settings.BlockScale = 2
				}
			} else if item.key == "halvevert" {
				m.settings.HalveVertical = !m.settings.HalveVertical
			}
		}
	case "enter":
		if m.settingSel >= 0 && m.settingSel < len(items) {
			item := items[m.settingSel]
			switch item.key {
			case "cols":
				m.settingEditing = true
				if m.gameName == "snake" {
					m.settingInput = fmt.Sprintf("%d", m.settings.SnakeCols)
				} else {
					m.settingInput = fmt.Sprintf("%d", m.settings.TetrisCols)
				}
			case "rows":
				m.settingEditing = true
				if m.gameName == "snake" {
					m.settingInput = fmt.Sprintf("%d", m.settings.SnakeRows)
				} else {
					m.settingInput = fmt.Sprintf("%d", m.settings.TetrisRows)
				}
			case "blockscale":
				// Toggle between 1 and 2.
				if m.settings.BlockScale >= 2 {
					m.settings.BlockScale = 1
				} else {
					m.settings.BlockScale = 2
				}
			case "halvevert":
				m.settings.HalveVertical = !m.settings.HalveVertical
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
	case "cols":
		val, err := strconv.Atoi(m.settingInput)
		if err != nil {
			return
		}
		if val < 4 {
			val = 4
		}
		if val > 100 {
			val = 100
		}
		if m.gameName == "snake" {
			m.settings.SnakeCols = val
		} else {
			m.settings.TetrisCols = val
		}
	case "rows":
		val, err := strconv.Atoi(m.settingInput)
		if err != nil {
			return
		}
		if val < 6 {
			val = 6
		}
		if val > 80 {
			val = 80
		}
		if m.gameName == "snake" {
			m.settings.SnakeRows = val
		} else {
			m.settings.TetrisRows = val
		}
	case "blockscale":
		val, err := strconv.Atoi(m.settingInput)
		if err != nil {
			return
		}
		if val < 1 {
			val = 1
		}
		if val > 2 {
			val = 2
		}
		m.settings.BlockScale = val
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

// centerFrame horizontally and vertically centers the game frame.
func (m GameOverlayModel) centerFrame(frame string) string {
	lines := strings.Split(frame, "\n")
	if len(lines) == 0 {
		return frame
	}

	// Horizontal centering.
	maxW := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > maxW {
			maxW = w
		}
	}
	contentW := m.width - 10
	hPad := ""
	if contentW > maxW {
		hPad = strings.Repeat(" ", (contentW-maxW)/2)
	}

	// Vertical centering: available height minus header (score line
	// + blank) and footer (2 lines), then center the frame.
	contentH := m.height - 12 // overlay chrome + header + footer
	vPad := 0
	if contentH > len(lines) {
		vPad = (contentH - len(lines)) / 2
	}

	var centered strings.Builder
	for i := 0; i < vPad; i++ {
		centered.WriteString("\n")
	}
	for i, line := range lines {
		if i > 0 {
			centered.WriteString("\n")
		}
		centered.WriteString(hPad)
		centered.WriteString(line)
	}
	return centered.String()
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
		hi := m.settings.SnakeHighScore
		b.WriteString("  " + scoreStyle.Render(fmt.Sprintf("Score: %d", m.snake.Score())))
		if hi > 0 {
			b.WriteString("  " + dimStyle.Render(fmt.Sprintf("Best: %d", hi)))
		}
	}
	if m.tetris != nil {
		hi := m.settings.TetrisHighScore
		b.WriteString("  " + scoreStyle.Render(fmt.Sprintf("Score: %d  Level: %d  Lines: %d",
			m.tetris.Score(), m.tetris.Level(), m.tetris.Lines())))
		if hi > 0 {
			b.WriteString("  " + dimStyle.Render(fmt.Sprintf("Best: %d", hi)))
		}
	}
	if m.paused && !m.showSettings {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorHighlight).Render("[PAUSED]"))
	}
	b.WriteString("\n\n")

	// Settings overlay or game render.
	if m.showSettings {
		b.WriteString(m.renderSettings())
	} else if m.snake != nil {
		b.WriteString(m.centerFrame(m.snake.RenderFrame()))
		if m.snake.IsGameOver() {
			b.WriteString("\n\n")
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("  GAME OVER!"))
			b.WriteString("  " + dimStyle.Render("R: restart · Ctrl+Q: quit"))
		}
	} else if m.tetris != nil {
		b.WriteString(m.centerFrame(m.tetris.RenderFrame()))
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

	// Game window is always full-screen.
	scaffold := OverlayScaffold{
		Title:       title,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		BoxWidth:    m.width - 2,
		BoxHeight:   m.height - 2,
		MaxBoxWidth: m.width - 2,
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
			var val string
			if item.key == "blockscale" && i == m.settingSel {
				// Horizontal selector: ◀ 1  2 ▶
				opt1Style := dimStyle
				opt2Style := dimStyle
				if m.settings.BlockScale <= 1 {
					opt1Style = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
				} else {
					opt2Style = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
				}
				val = "◀ " + opt1Style.Render("1") + "  " + opt2Style.Render("2") + " ▶"
			} else if m.settingEditing && i == m.settingSel {
				val = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true).Render(m.settingInput + "▌")
			} else {
				val = valStyle.Render(item.value)
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
