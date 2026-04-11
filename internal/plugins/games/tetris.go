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
	hScale   int // horizontal render scale (chars per cell)
	vScale   int // vertical render scale (rows per cell)
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
		hScale:  1,
		vScale:  1,
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

// SetRenderScale sets the horizontal and vertical character scale
// for rendering. hScale=2 means each cell is 2 chars wide.
// Rebuilds the canvas to the new dimensions.
func (g *TetrisGame) SetRenderScale(h, v int) {
	if h < 1 {
		h = 1
	}
	if v < 1 {
		v = 1
	}
	g.hScale = h
	g.vScale = v
	// Rebuild canvas to fit scaled board + side panel.
	g.canvasW = g.width*h + 2 + 14 // scaled board + borders + panel
	g.canvasH = g.height*v + 2     // scaled board + top/bottom border
	g.canvas = ui.NewCanvas("tetris", g.canvasW, g.canvasH)
}

// setScaledBlock fills a scaled block on the canvas.
func (g *TetrisGame) setScaledBlock(logX, logY int, ch rune, fg, bg lipgloss.Color) {
	sx := logX * g.hScale
	sy := logY * g.vScale
	for dy := 0; dy < g.vScale; dy++ {
		for dx := 0; dx < g.hScale; dx++ {
			g.canvas.Set(sx+dx, sy+dy, ch, fg, bg)
		}
	}
}

// RenderFrame draws the current state to the canvas.
func (g *TetrisGame) RenderFrame() string {
	g.canvas.Clear()
	wallColor := lipgloss.Color("#444444")
	bgColor := lipgloss.Color("#111111")
	hs := g.hScale
	vs := g.vScale

	// Draw board background + border.
	boardRenderW := g.width * hs
	boardRenderH := g.height * vs
	// Left border.
	for ry := 0; ry < boardRenderH; ry++ {
		g.canvas.Set(0, ry+vs, '│', wallColor, "")
	}
	// Right border.
	for ry := 0; ry < boardRenderH; ry++ {
		g.canvas.Set(boardRenderW+1, ry+vs, '│', wallColor, "")
	}
	// Board cells.
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			color := g.board[y][x]
			ch := rune('·')
			fc := bgColor
			if color != "" {
				ch = '█'
				fc = color
			}
			// Render scaled cell.
			for dy := 0; dy < vs; dy++ {
				for dx := 0; dx < hs; dx++ {
					g.canvas.Set(x*hs+1+dx, y*vs+vs+dy, ch, fc, "")
				}
			}
		}
	}
	// Bottom border.
	for rx := 0; rx <= boardRenderW+1; rx++ {
		g.canvas.Set(rx, boardRenderH+vs, '─', wallColor, "")
	}

	// Draw current piece (scaled).
	shape := tetrominoes[g.current].Rotations[g.rotation]
	color := tetrominoes[g.current].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if shape[y][x] {
				by := g.pieceY + y
				bx := g.pieceX + x
				if by >= 0 && by < g.height && bx >= 0 && bx < g.width {
					for dy := 0; dy < vs; dy++ {
						for dx := 0; dx < hs; dx++ {
							g.canvas.Set(bx*hs+1+dx, by*vs+vs+dy, '█', color, "")
						}
					}
				}
			}
		}
	}

	// Side panel: next piece + stats.
	panelX := boardRenderW + 3
	labelColor := lipgloss.Color("#888888")
	statColor := lipgloss.Color("#cccccc")

	g.canvas.DrawText(panelX, vs, "Next:", labelColor, "")
	nextShape := tetrominoes[g.next].Rotations[0]
	nextColor := tetrominoes[g.next].Color
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if nextShape[y][x] {
				for dy := 0; dy < vs; dy++ {
					for dx := 0; dx < hs; dx++ {
						g.canvas.Set(panelX+x*hs+dx, vs+2+y*vs+dy, '█', nextColor, "")
					}
				}
			}
		}
	}

	// Stats below the preview.
	statsY := vs + 2 + 4*vs + 1
	g.canvas.DrawText(panelX, statsY, "Score:", labelColor, "")
	g.canvas.DrawText(panelX, statsY+1, itoa(g.score), statColor, "")
	g.canvas.DrawText(panelX, statsY+3, "Level:", labelColor, "")
	g.canvas.DrawText(panelX, statsY+4, itoa(g.level), statColor, "")
	g.canvas.DrawText(panelX, statsY+6, "Lines:", labelColor, "")
	g.canvas.DrawText(panelX, statsY+7, itoa(g.lines), statColor, "")

	return g.canvas.Render(g.canvasW, g.canvasH)
}
