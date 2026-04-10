package ui

import "strings"

// VBox arranges children vertically.
type VBox struct {
	id       string
	children []Component
	width    int
	height   int
}

func NewVBox(id string) *VBox {
	return &VBox{id: id}
}

func (v *VBox) ID() string { return v.id }

func (v *VBox) Add(child Component) {
	v.children = append(v.children, child)
}

func (v *VBox) Remove(id string) {
	for i, c := range v.children {
		if c.ID() == id {
			v.children = append(v.children[:i], v.children[i+1:]...)
			return
		}
	}
}

func (v *VBox) Children() []Component { return v.children }

func (v *VBox) SetSize(w, h int) {
	v.width = w
	v.height = h
	// Distribute height evenly among children.
	if len(v.children) == 0 {
		return
	}
	childH := h / len(v.children)
	for _, c := range v.children {
		c.SetSize(w, childH)
	}
}

func (v *VBox) Render(width, height int) string {
	var parts []string
	for _, c := range v.children {
		parts = append(parts, c.Render(width, 0))
	}
	return strings.Join(parts, "\n")
}

func (v *VBox) HandleKey(key string) bool {
	for _, c := range v.children {
		if c.HandleKey(key) {
			return true
		}
	}
	return false
}

// HBox arranges children horizontally.
type HBox struct {
	id       string
	children []Component
	width    int
	height   int
}

func NewHBox(id string) *HBox {
	return &HBox{id: id}
}

func (h *HBox) ID() string { return h.id }

func (h *HBox) Add(child Component) {
	h.children = append(h.children, child)
}

func (h *HBox) Remove(id string) {
	for i, c := range h.children {
		if c.ID() == id {
			h.children = append(h.children[:i], h.children[i+1:]...)
			return
		}
	}
}

func (h *HBox) Children() []Component { return h.children }

func (h *HBox) SetSize(w, ht int) {
	h.width = w
	h.height = ht
	if len(h.children) == 0 {
		return
	}
	childW := w / len(h.children)
	for _, c := range h.children {
		c.SetSize(childW, ht)
	}
}

func (h *HBox) Render(width, height int) string {
	if len(h.children) == 0 {
		return ""
	}
	// Render each child, then merge side-by-side.
	childW := width / len(h.children)
	if childW < 1 {
		childW = 1
	}
	var columns [][]string
	maxLines := 0
	for _, c := range h.children {
		rendered := c.Render(childW, height)
		lines := strings.Split(rendered, "\n")
		columns = append(columns, lines)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	var rows []string
	for row := 0; row < maxLines; row++ {
		var parts []string
		for col, lines := range columns {
			w := childW
			if col == len(columns)-1 {
				w = width - childW*(len(columns)-1)
			}
			line := ""
			if row < len(lines) {
				line = lines[row]
			}
			// Pad to column width.
			for len(line) < w {
				line += " "
			}
			parts = append(parts, line)
		}
		rows = append(rows, strings.Join(parts, ""))
	}
	return strings.Join(rows, "\n")
}

func (h *HBox) HandleKey(key string) bool {
	for _, c := range h.children {
		if c.HandleKey(key) {
			return true
		}
	}
	return false
}
