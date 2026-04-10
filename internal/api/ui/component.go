package ui

// Component is the base interface for all UI elements.
type Component interface {
	ID() string
	Render(width, height int) string
	HandleKey(key string) bool // returns true if key was consumed
	SetSize(width, height int)
}

// SizePolicy controls how a component behaves in a layout.
type SizePolicy struct {
	MinWidth  int
	MaxWidth  int
	MinHeight int
	MaxHeight int
	Grow      bool // true = expand to fill available space
}
