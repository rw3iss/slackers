package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Label is a single-line text component.
type Label struct {
	id    string
	text  string
	style lipgloss.Style
}

func NewLabel(id, text string) *Label {
	return &Label{id: id, text: text}
}

func (l *Label) ID() string               { return l.id }
func (l *Label) SetSize(w, h int)         {}
func (l *Label) HandleKey(key string) bool { return false }
func (l *Label) SetStyle(s lipgloss.Style) { l.style = s }
func (l *Label) SetText(text string)       { l.text = text }
func (l *Label) Render(width, height int) string {
	return l.style.Render(l.text)
}

// Paragraph is a multi-line text block that word-wraps.
type Paragraph struct {
	id    string
	text  string
	style lipgloss.Style
	width int
}

func NewParagraph(id, text string) *Paragraph {
	return &Paragraph{id: id, text: text}
}

func (p *Paragraph) ID() string               { return p.id }
func (p *Paragraph) SetSize(w, h int)         { p.width = w }
func (p *Paragraph) HandleKey(key string) bool { return false }
func (p *Paragraph) SetStyle(s lipgloss.Style) { p.style = s }
func (p *Paragraph) SetText(text string)       { p.text = text }
func (p *Paragraph) Render(width, height int) string {
	w := width
	if p.width > 0 {
		w = p.width
	}
	if w <= 0 {
		return p.style.Render(p.text)
	}
	// Simple word wrap.
	words := strings.Fields(p.text)
	var lines []string
	var current string
	for _, word := range words {
		if current == "" {
			current = word
		} else if len(current)+1+len(word) <= w {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return p.style.Render(strings.Join(lines, "\n"))
}
