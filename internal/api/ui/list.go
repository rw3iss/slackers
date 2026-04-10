package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ListItem is one entry in a List component.
type ListItem struct {
	Label string
	Value any
}

// List is a scrollable, selectable list component.
type List struct {
	id       string
	items    []ListItem
	selected int
	width    int
	height   int
	style    lipgloss.Style
	selStyle lipgloss.Style
	onSelect func(item ListItem) // callback when Enter is pressed
}

// NewList creates a new list component.
func NewList(id string) *List {
	return &List{
		id:       id,
		style:    lipgloss.NewStyle(),
		selStyle: lipgloss.NewStyle().Bold(true),
	}
}

func (l *List) ID() string       { return l.id }
func (l *List) SetSize(w, h int) { l.width = w; l.height = h }

// SetItems replaces the list contents.
func (l *List) SetItems(items []ListItem) {
	l.items = items
	if l.selected >= len(items) {
		l.selected = len(items) - 1
	}
	if l.selected < 0 {
		l.selected = 0
	}
}

// SetStyles sets the normal and selected item styles.
func (l *List) SetStyles(normal, selected lipgloss.Style) {
	l.style = normal
	l.selStyle = selected
}

// OnSelect sets a callback for when the user presses Enter.
func (l *List) OnSelect(fn func(item ListItem)) {
	l.onSelect = fn
}

// Selected returns the currently selected index.
func (l *List) Selected() int { return l.selected }

// SelectedItem returns the currently selected item, or nil.
func (l *List) SelectedItem() *ListItem {
	if l.selected < 0 || l.selected >= len(l.items) {
		return nil
	}
	return &l.items[l.selected]
}

func (l *List) HandleKey(key string) bool {
	switch key {
	case "up":
		if l.selected > 0 {
			l.selected--
		}
		return true
	case "down":
		if l.selected < len(l.items)-1 {
			l.selected++
		}
		return true
	case "enter":
		if l.onSelect != nil && l.selected >= 0 && l.selected < len(l.items) {
			l.onSelect(l.items[l.selected])
		}
		return true
	}
	return false
}

func (l *List) Render(width, height int) string {
	if len(l.items) == 0 {
		return ""
	}
	visible := height
	if visible <= 0 || visible > len(l.items) {
		visible = len(l.items)
	}
	// Window around selected.
	start := 0
	if l.selected >= visible {
		start = l.selected - visible + 1
	}
	end := start + visible
	if end > len(l.items) {
		end = len(l.items)
	}

	var lines []string
	for i := start; i < end; i++ {
		item := l.items[i]
		cursor := "  "
		style := l.style
		if i == l.selected {
			cursor = "> "
			style = l.selStyle
		}
		lines = append(lines, cursor+style.Render(item.Label))
	}
	return strings.Join(lines, "\n")
}
