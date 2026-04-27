package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Cell is a single character cell on a Canvas.
type Cell struct {
	Char rune
	FG   lipgloss.Color
	BG   lipgloss.Color
}

// Canvas provides a character-addressable grid for games and
// other pixel-level rendering. Each cell can have its own
// foreground and background color.
type Canvas struct {
	id     string
	width  int
	height int
	cells  [][]Cell
}

// NewCanvas creates a canvas with the given dimensions.
func NewCanvas(id string, w, h int) *Canvas {
	c := &Canvas{
		id:     id,
		width:  w,
		height: h,
	}
	c.cells = make([][]Cell, h)
	for y := range c.cells {
		c.cells[y] = make([]Cell, w)
		for x := range c.cells[y] {
			c.cells[y][x] = Cell{Char: ' '}
		}
	}
	return c
}

func (c *Canvas) ID() string               { return c.id }
func (c *Canvas) SetSize(w, h int)         { /* canvas has fixed size */ }
func (c *Canvas) HandleKey(key string) bool { return false }

// Set places a character with colors at the given position.
func (c *Canvas) Set(x, y int, ch rune, fg, bg lipgloss.Color) {
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return
	}
	c.cells[y][x] = Cell{Char: ch, FG: fg, BG: bg}
}

// Get returns the cell at the given position.
func (c *Canvas) Get(x, y int) Cell {
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return Cell{Char: ' '}
	}
	return c.cells[y][x]
}

// Clear resets all cells to spaces with no color.
func (c *Canvas) Clear() {
	for y := range c.cells {
		for x := range c.cells[y] {
			c.cells[y][x] = Cell{Char: ' '}
		}
	}
}

// Fill sets all cells to the given character and colors.
func (c *Canvas) Fill(ch rune, fg, bg lipgloss.Color) {
	for y := range c.cells {
		for x := range c.cells[y] {
			c.cells[y][x] = Cell{Char: ch, FG: fg, BG: bg}
		}
	}
}

// DrawRect draws a rectangle outline.
func (c *Canvas) DrawRect(x, y, w, h int, ch rune, fg, bg lipgloss.Color) {
	for dx := 0; dx < w; dx++ {
		c.Set(x+dx, y, ch, fg, bg)
		c.Set(x+dx, y+h-1, ch, fg, bg)
	}
	for dy := 0; dy < h; dy++ {
		c.Set(x, y+dy, ch, fg, bg)
		c.Set(x+w-1, y+dy, ch, fg, bg)
	}
}

// DrawText writes a string horizontally starting at (x, y).
func (c *Canvas) DrawText(x, y int, text string, fg, bg lipgloss.Color) {
	for i, ch := range text {
		c.Set(x+i, y, ch, fg, bg)
	}
}

// Render outputs the canvas as a string. Consecutive cells with
// the same fg/bg colors are batched into a single styled run,
// drastically reducing lipgloss.Style allocations (from ~1200
// per frame to ~100-200 for a typical tetris board).
func (c *Canvas) Render(width, height int) string {
	var b strings.Builder
	for y := 0; y < c.height; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		x := 0
		for x < c.width {
			cell := c.cells[y][x]
			// Scan for a run of consecutive cells with the same colors.
			end := x + 1
			for end < c.width &&
				c.cells[y][end].FG == cell.FG &&
				c.cells[y][end].BG == cell.BG {
				end++
			}
			// Build the run text.
			var run strings.Builder
			for i := x; i < end; i++ {
				run.WriteRune(c.cells[y][i].Char)
			}
			// Style the entire run with one allocation.
			s := lipgloss.NewStyle()
			if cell.FG != "" {
				s = s.Foreground(cell.FG)
			}
			if cell.BG != "" {
				s = s.Background(cell.BG)
			}
			b.WriteString(s.Render(run.String()))
			x = end
		}
	}
	return b.String()
}

// Width returns the canvas width.
func (c *Canvas) Width() int { return c.width }

// Height returns the canvas height.
func (c *Canvas) Height() int { return c.height }
