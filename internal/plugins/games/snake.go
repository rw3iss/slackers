package games

import (
	"math/rand"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/api/ui"
)

// Direction for snake movement.
type Direction int

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

// Point is an x,y coordinate.
type Point struct {
	X, Y int
}

// SnakeGame holds the state of a snake game.
type SnakeGame struct {
	width    int
	height   int
	snake    []Point
	dir      Direction
	food     Point
	score    int
	gameOver bool
	canvas   *ui.Canvas
}

const (
	defaultWidth  = 30
	defaultHeight = 15
)

var (
	colorSnakeHead = lipgloss.Color("#00ff00")
	colorSnakeBody = lipgloss.Color("#008800")
	colorFood      = lipgloss.Color("#ff0000")
	colorWall      = lipgloss.Color("#444444")
	colorBG        = lipgloss.Color("#000000")
)

// NewSnakeGame creates a new snake game with default dimensions.
func NewSnakeGame() *SnakeGame {
	return NewSnakeGameSized(defaultWidth, defaultHeight)
}

// NewSnakeGameSized creates a snake game with custom dimensions.
func NewSnakeGameSized(w, h int) *SnakeGame {
	g := &SnakeGame{
		width:  w,
		height: h,
		dir:    DirRight,
		canvas: ui.NewCanvas("snake", w, h),
	}
	// Initial snake in the center.
	cx, cy := w/2, h/2
	g.snake = []Point{
		{cx, cy},
		{cx - 1, cy},
		{cx - 2, cy},
	}
	g.spawnFood()
	return g
}

func (g *SnakeGame) spawnFood() {
	for {
		g.food = Point{
			X: 1 + rand.Intn(g.width-2),
			Y: 1 + rand.Intn(g.height-2),
		}
		// Don't spawn on the snake.
		onSnake := false
		for _, p := range g.snake {
			if p == g.food {
				onSnake = true
				break
			}
		}
		if !onSnake {
			return
		}
	}
}

// Tick advances the game by one step.
func (g *SnakeGame) Tick() {
	if g.gameOver {
		return
	}
	head := g.snake[0]
	var next Point
	switch g.dir {
	case DirUp:
		next = Point{head.X, head.Y - 1}
	case DirDown:
		next = Point{head.X, head.Y + 1}
	case DirLeft:
		next = Point{head.X - 1, head.Y}
	case DirRight:
		next = Point{head.X + 1, head.Y}
	}

	// Wall collision.
	if next.X <= 0 || next.X >= g.width-1 || next.Y <= 0 || next.Y >= g.height-1 {
		g.gameOver = true
		return
	}
	// Self collision.
	for _, p := range g.snake {
		if next == p {
			g.gameOver = true
			return
		}
	}

	g.snake = append([]Point{next}, g.snake...)
	if next == g.food {
		g.score++
		g.spawnFood()
	} else {
		g.snake = g.snake[:len(g.snake)-1]
	}
}

// Score returns the current score.
func (g *SnakeGame) Score() int { return g.score }

// IsGameOver returns whether the game has ended.
func (g *SnakeGame) IsGameOver() bool { return g.gameOver }

// SetDirection changes the snake's direction.
func (g *SnakeGame) SetDirection(d Direction) {
	// Prevent 180-degree turns.
	switch {
	case g.dir == DirUp && d == DirDown:
	case g.dir == DirDown && d == DirUp:
	case g.dir == DirLeft && d == DirRight:
	case g.dir == DirRight && d == DirLeft:
	default:
		g.dir = d
	}
}

// RenderFrame draws the current game state to the canvas and
// returns the rendered string.
func (g *SnakeGame) RenderFrame() string {
	g.canvas.Clear()

	// Draw walls.
	for x := 0; x < g.width; x++ {
		g.canvas.Set(x, 0, '█', colorWall, colorBG)
		g.canvas.Set(x, g.height-1, '█', colorWall, colorBG)
	}
	for y := 0; y < g.height; y++ {
		g.canvas.Set(0, y, '█', colorWall, colorBG)
		g.canvas.Set(g.width-1, y, '█', colorWall, colorBG)
	}

	// Draw food.
	g.canvas.Set(g.food.X, g.food.Y, '●', colorFood, colorBG)

	// Draw snake.
	for i, p := range g.snake {
		color := colorSnakeBody
		ch := rune('█')
		if i == 0 {
			color = colorSnakeHead
		}
		g.canvas.Set(p.X, p.Y, ch, color, colorBG)
	}

	// Score header.
	scoreText := "Score: " + itoa(g.score)
	if g.gameOver {
		scoreText = "GAME OVER — Score: " + itoa(g.score)
	}
	header := scoreText + "\n"
	return header + g.canvas.Render(g.width, g.height)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
