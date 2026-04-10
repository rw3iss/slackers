package tui

// Game overlay — takes full keyboard control to run interactive
// games (snake, tetris, etc.). The overlay intercepts all key
// events and routes them to the active game. A periodic tick
// drives the game loop.
//
// The user can exit the game with Ctrl+Q (returns to the normal
// TUI). The status bar shows game controls while active.

import (
	"fmt"
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

// GameOverlayModel manages an active game session.
type GameOverlayModel struct {
	gameName string
	snake    *games.SnakeGame
	paused   bool
	width    int
	height   int
	speed    time.Duration // tick interval
}

// NewGameOverlay creates a game overlay for the given game name.
func NewGameOverlay(name string) GameOverlayModel {
	m := GameOverlayModel{
		gameName: name,
		speed:    150 * time.Millisecond,
	}
	switch name {
	case "snake":
		m.snake = games.NewSnakeGame()
	}
	return m
}

func (m *GameOverlayModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// TickCmd returns the tea.Cmd that schedules the next game tick.
func (m GameOverlayModel) TickCmd() tea.Cmd {
	if m.paused {
		return nil
	}
	return tea.Tick(m.speed, func(time.Time) tea.Msg {
		return gameTickMsg{}
	})
}

func (m GameOverlayModel) Update(msg tea.Msg) (GameOverlayModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		key := v.String()

		// Universal game controls.
		switch key {
		case "ctrl+q":
			return m, func() tea.Msg { return GameOverlayCloseMsg{} }
		case "p", " ":
			m.paused = !m.paused
			if !m.paused {
				return m, m.TickCmd()
			}
			return m, nil
		case "r":
			// Restart.
			if m.snake != nil && m.snake.IsGameOver() {
				m.snake = games.NewSnakeGame()
				m.paused = false
				return m, m.TickCmd()
			}
		}

		// Game-specific controls.
		if m.snake != nil && !m.paused {
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
		return m, nil

	case gameTickMsg:
		if m.paused {
			return m, nil
		}
		if m.snake != nil {
			m.snake.Tick()
			if m.snake.IsGameOver() {
				return m, nil // stop ticking
			}
		}
		return m, m.TickCmd()
	}
	return m, nil
}

func (m GameOverlayModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	scoreStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)

	var b strings.Builder

	// Header.
	title := "Snake"
	if m.gameName == "tetris" {
		title = "Tetris"
	}
	b.WriteString(titleStyle.Render("  " + title))

	if m.snake != nil {
		b.WriteString("  " + scoreStyle.Render(fmt.Sprintf("Score: %d", m.snake.Score())))
		if m.paused {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorHighlight).Render("[PAUSED]"))
		}
	}
	b.WriteString("\n\n")

	// Game render.
	if m.snake != nil {
		frame := m.snake.RenderFrame()
		b.WriteString(frame)
		if m.snake.IsGameOver() {
			b.WriteString("\n\n")
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("  GAME OVER!"))
			b.WriteString("  " + dimStyle.Render("Press 'r' to restart or Ctrl+Q to quit"))
		}
	} else {
		b.WriteString(dimStyle.Render("  Game not available: " + m.gameName))
	}

	// Footer.
	footer := "↑↓←→/WASD: move" + HintSep + "Space/P: pause" + HintSep + "R: restart" + HintSep + "Ctrl+Q: quit game"

	scaffold := OverlayScaffold{
		Title:       title,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 70,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
