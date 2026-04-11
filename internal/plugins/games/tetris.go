package games

import (
	"math/rand"

	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/api/ui"
)

// Tetromino shapes — each is a 4x4 grid of booleans for each rotation.
type Tetromino struct {
	Rotations [4][4][4]bool
	Color     lipgloss.Color
}

var tetrominoes = []Tetromino{
	// I
	{Rotations: [4][4][4]bool{
		{{false, false, false, false}, {true, true, true, true}, {false, false, false, false}, {false, false, false, false}},
		{{false, false, true, false}, {false, false, true, false}, {false, false, true, false}, {false, false, true, false}},
		{{false, false, false, false}, {false, false, false, false}, {true, true, true, true}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, false, false}, {false, true, false, false}, {false, true, false, false}},
	}, Color: lipgloss.Color("#00ffff")},
	// O
	{Rotations: [4][4][4]bool{
		{{false, true, true, false}, {false, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, true, false}, {false, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, true, false}, {false, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, true, false}, {false, true, true, false}, {false, false, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#ffff00")},
	// T
	{Rotations: [4][4][4]bool{
		{{false, true, false, false}, {true, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, true, false}, {false, true, false, false}, {false, false, false, false}},
		{{false, false, false, false}, {true, true, true, false}, {false, true, false, false}, {false, false, false, false}},
		{{false, true, false, false}, {true, true, false, false}, {false, true, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#aa00ff")},
	// S
	{Rotations: [4][4][4]bool{
		{{false, true, true, false}, {true, true, false, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, true, false}, {false, false, true, false}, {false, false, false, false}},
		{{false, false, false, false}, {false, true, true, false}, {true, true, false, false}, {false, false, false, false}},
		{{true, false, false, false}, {true, true, false, false}, {false, true, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#00ff00")},
	// Z
	{Rotations: [4][4][4]bool{
		{{true, true, false, false}, {false, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, false, true, false}, {false, true, true, false}, {false, true, false, false}, {false, false, false, false}},
		{{false, false, false, false}, {true, true, false, false}, {false, true, true, false}, {false, false, false, false}},
		{{false, true, false, false}, {true, true, false, false}, {true, false, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#ff0000")},
	// L
	{Rotations: [4][4][4]bool{
		{{false, false, true, false}, {true, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, false, false}, {false, true, true, false}, {false, false, false, false}},
		{{false, false, false, false}, {true, true, true, false}, {true, false, false, false}, {false, false, false, false}},
		{{true, true, false, false}, {false, true, false, false}, {false, true, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#ff8800")},
	// J
	{Rotations: [4][4][4]bool{
		{{true, false, false, false}, {true, true, true, false}, {false, false, false, false}, {false, false, false, false}},
		{{false, true, true, false}, {false, true, false, false}, {false, true, false, false}, {false, false, false, false}},
		{{false, false, false, false}, {true, true, true, false}, {false, false, true, false}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, false, false}, {true, true, false, false}, {false, false, false, false}},
	}, Color: lipgloss.Color("#0000ff")},
}

const (
	defaultTetrisWidth  = 20
	defaultTetrisHeight = 30
)

// TetrisGame holds the state of a tetris game.
type TetrisGame struct {
	width    int
	height   int
	board    [][]lipgloss.Color // [height][width], empty string = empty cell
	current  int                // index into tetrominoes
	rotation int
	pieceX   int
	pieceY   int
	next     int // next piece index
	score    int
	level    int
	lines    int
	gameOver bool
	canvas   *ui.Canvas
	canvasW  int
	canvasH  int
}

// NewTetrisGame creates a new tetris game with default dimensions.
func NewTetrisGame() *TetrisGame {
	return NewTetrisGameSized(defaultTetrisWidth, defaultTetrisHeight)
}

// NewTetrisGameSized creates a tetris game with custom dimensions.
func NewTetrisGameSized(w, h int) *TetrisGame {
	if w < 6 {
		w = 6
	}
	if h < 10 {
		h = 10
	}
	cw := w + 14 // board(w) + borders(2) + gap(1) + preview panel(11)
	ch := h + 2
	board := make([][]lipgloss.Color, h)
	for i := range board {
		board[i] = make([]lipgloss.Color, w)
	}
	g := &TetrisGame{
		width:   w,
		height:  h,
		board:   board,
		canvas:  ui.NewCanvas("tetris", cw, ch),
		canvasW: cw,
		canvasH: ch,
		level:   1,
		next:    rand.Intn(len(tetrominoes)),
	}
	g.spawnPiece()
	return g
}

func (g *TetrisGame) spawnPiece() {
	g.current = g.next
	g.next = rand.Intn(len(tetrominoes))
	g.rotation = 0
	g.pieceX = g.width/2 - 2
	g.pieceY = 0
	if g.collides(g.pieceX, g.pieceY, g.rotation) {
		g.gameOver = true
	}
}

func (g *TetrisGame) collides(px, py, rot int) bool {
	shape := tetrominoes[g.current].Rotations[rot]
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if !shape[y][x] {
				continue
			}
			bx, by := px+x, py+y
			if bx < 0 || bx >= g.width || by >= g.height {
				return true
			}
			if by >= 0 && g.board[by][bx] != "" {
				return true
			}
		}
	}
	return false
}

func (g *TetrisGame) lockPiece() {
	shape := tetrominoes[g.current].Rotations[g.rotation]
	color := tetrominoes[g.current].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if shape[y][x] {
				by := g.pieceY + y
				bx := g.pieceX + x
				if by >= 0 && by < g.height && bx >= 0 && bx < g.width {
					g.board[by][bx] = color
				}
			}
		}
	}
	g.clearLines()
	g.spawnPiece()
}

func (g *TetrisGame) clearLines() {
	cleared := 0
	for y := g.height - 1; y >= 0; y-- {
		full := true
		for x := 0; x < g.width; x++ {
			if g.board[y][x] == "" {
				full = false
				break
			}
		}
		if full {
			cleared++
			// Shift everything above down.
			for sy := y; sy > 0; sy-- {
				g.board[sy] = g.board[sy-1]
			}
			g.board[0] = make([]lipgloss.Color, g.width)
			y++ // recheck this row
		}
	}
	if cleared > 0 {
		g.lines += cleared
		// Scoring: 1=100, 2=300, 3=500, 4=800
		points := []int{0, 100, 300, 500, 800}
		if cleared > 4 {
			cleared = 4
		}
		g.score += points[cleared] * g.level
		g.level = g.lines/10 + 1
	}
}

// Tick advances the game by dropping the piece one row.
func (g *TetrisGame) Tick() {
	if g.gameOver {
		return
	}
	if g.collides(g.pieceX, g.pieceY+1, g.rotation) {
		g.lockPiece()
	} else {
		g.pieceY++
	}
}

// MoveLeft moves the piece left.
func (g *TetrisGame) MoveLeft() {
	if !g.gameOver && !g.collides(g.pieceX-1, g.pieceY, g.rotation) {
		g.pieceX--
	}
}

// MoveRight moves the piece right.
func (g *TetrisGame) MoveRight() {
	if !g.gameOver && !g.collides(g.pieceX+1, g.pieceY, g.rotation) {
		g.pieceX++
	}
}

// Rotate rotates the piece clockwise.
func (g *TetrisGame) Rotate() {
	if g.gameOver {
		return
	}
	newRot := (g.rotation + 1) % 4
	if !g.collides(g.pieceX, g.pieceY, newRot) {
		g.rotation = newRot
	}
}

// Drop instantly drops the piece to the bottom.
func (g *TetrisGame) Drop() {
	if g.gameOver {
		return
	}
	for !g.collides(g.pieceX, g.pieceY+1, g.rotation) {
		g.pieceY++
	}
	g.lockPiece()
}

// Score returns the current score.
func (g *TetrisGame) Score() int { return g.score }

// Level returns the current level.
func (g *TetrisGame) Level() int { return g.level }

// Lines returns total lines cleared.
func (g *TetrisGame) Lines() int { return g.lines }

// IsGameOver returns whether the game has ended.
func (g *TetrisGame) IsGameOver() bool { return g.gameOver }

// RenderFrame draws the current state to the canvas.
func (g *TetrisGame) RenderFrame() string {
	g.canvas.Clear()
	wallColor := lipgloss.Color("#444444")
	bgColor := lipgloss.Color("#111111")

	// Draw board background + border.
	for y := 0; y < g.height; y++ {
		g.canvas.Set(0, y+1, '│', wallColor, "")
		g.canvas.Set(g.width+1, y+1, '│', wallColor, "")
		for x := 0; x < g.width; x++ {
			color := g.board[y][x]
			if color != "" {
				g.canvas.Set(x+1, y+1, '█', color, "")
			} else {
				g.canvas.Set(x+1, y+1, '·', bgColor, "")
			}
		}
	}
	// Bottom border.
	for x := 0; x <= g.width+1; x++ {
		g.canvas.Set(x, g.height+1, '─', wallColor, "")
	}

	// Draw current piece.
	shape := tetrominoes[g.current].Rotations[g.rotation]
	color := tetrominoes[g.current].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if shape[y][x] {
				by := g.pieceY + y
				bx := g.pieceX + x
				if by >= 0 && by < g.height && bx >= 0 && bx < g.width {
					g.canvas.Set(bx+1, by+1, '█', color, "")
				}
			}
		}
	}

	// Side panel: next piece + stats.
	panelX := g.width + 3
	labelColor := lipgloss.Color("#888888")
	statColor := lipgloss.Color("#cccccc")

	g.canvas.DrawText(panelX, 1, "Next:", labelColor, "")
	nextShape := tetrominoes[g.next].Rotations[0]
	nextColor := tetrominoes[g.next].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if nextShape[y][x] {
				g.canvas.Set(panelX+x, 3+y, '█', nextColor, "")
			}
		}
	}

	// Stats below the preview.
	g.canvas.DrawText(panelX, 8, "Score:", labelColor, "")
	g.canvas.DrawText(panelX, 9, itoa(g.score), statColor, "")
	g.canvas.DrawText(panelX, 11, "Level:", labelColor, "")
	g.canvas.DrawText(panelX, 12, itoa(g.level), statColor, "")
	g.canvas.DrawText(panelX, 14, "Lines:", labelColor, "")
	g.canvas.DrawText(panelX, 15, itoa(g.lines), statColor, "")

	return g.canvas.Render(g.canvasW, g.canvasH)
}
