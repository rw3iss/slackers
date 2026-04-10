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
	tetrisWidth  = 10
	tetrisHeight = 20
)

// TetrisGame holds the state of a tetris game.
type TetrisGame struct {
	board    [tetrisHeight][tetrisWidth]lipgloss.Color // empty string = empty cell
	current  int                                       // index into tetrominoes
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

// NewTetrisGame creates a new tetris game.
func NewTetrisGame() *TetrisGame {
	cw := tetrisWidth + 8 // board + border + preview
	ch := tetrisHeight + 2
	g := &TetrisGame{
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
	g.pieceX = tetrisWidth/2 - 2
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
			if bx < 0 || bx >= tetrisWidth || by >= tetrisHeight {
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
				if by >= 0 && by < tetrisHeight && bx >= 0 && bx < tetrisWidth {
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
	for y := tetrisHeight - 1; y >= 0; y-- {
		full := true
		for x := 0; x < tetrisWidth; x++ {
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
			g.board[0] = [tetrisWidth]lipgloss.Color{}
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
	for y := 0; y < tetrisHeight; y++ {
		g.canvas.Set(0, y+1, '│', wallColor, "")
		g.canvas.Set(tetrisWidth+1, y+1, '│', wallColor, "")
		for x := 0; x < tetrisWidth; x++ {
			color := g.board[y][x]
			if color != "" {
				g.canvas.Set(x+1, y+1, '█', color, "")
			} else {
				g.canvas.Set(x+1, y+1, '·', bgColor, "")
			}
		}
	}
	// Bottom border.
	for x := 0; x <= tetrisWidth+1; x++ {
		g.canvas.Set(x, tetrisHeight+1, '─', wallColor, "")
	}

	// Draw current piece.
	shape := tetrominoes[g.current].Rotations[g.rotation]
	color := tetrominoes[g.current].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if shape[y][x] {
				by := g.pieceY + y
				bx := g.pieceX + x
				if by >= 0 && by < tetrisHeight && bx >= 0 && bx < tetrisWidth {
					g.canvas.Set(bx+1, by+1, '█', color, "")
				}
			}
		}
	}

	// Draw next piece preview.
	previewX := tetrisWidth + 3
	g.canvas.DrawText(previewX, 1, "Next:", lipgloss.Color("#888888"), "")
	nextShape := tetrominoes[g.next].Rotations[0]
	nextColor := tetrominoes[g.next].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if nextShape[y][x] {
				g.canvas.Set(previewX+x, 3+y, '█', nextColor, "")
			}
		}
	}

	// Score info.
	header := "Score: " + itoa(g.score) + "  Level: " + itoa(g.level) + "  Lines: " + itoa(g.lines)
	if g.gameOver {
		header = "GAME OVER — " + header
	}
	return header + "\n" + g.canvas.Render(g.canvasW, g.canvasH)
}
