package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/theme"
)

// =====================================================================
// Messages
// =====================================================================

// ThemePickerOpenMsg requests opening the theme list overlay.
type ThemePickerOpenMsg struct{}

// ThemePickerCloseMsg signals the picker should close.
type ThemePickerCloseMsg struct{}

// ThemeAppliedMsg fires after a theme is applied so the model can save
// the choice to config.
type ThemeAppliedMsg struct{ Name string }

// ThemeEditorOpenMsg opens the editor for the named theme.
type ThemeEditorOpenMsg struct{ Theme theme.Theme }

// ThemeEditorCloseMsg dismisses the editor.
type ThemeEditorCloseMsg struct{}

// ThemeColorPickerOpenMsg opens the color picker for editing a single key.
type ThemeColorPickerOpenMsg struct {
	Key     string
	Initial string
}

// ThemeColorPickedMsg returns from the color picker with the chosen color.
type ThemeColorPickedMsg struct {
	Key   string
	Color string
}

// ThemeColorPickerCloseMsg dismisses the color picker without selection.
type ThemeColorPickerCloseMsg struct{}

// =====================================================================
// Theme List (picker)
// =====================================================================

// ThemePickerModel lists all available themes. Live-applies the
// highlighted theme as the user navigates so the change is previewed
// immediately.
type ThemePickerModel struct {
	themes        []theme.Theme
	selected      int
	width, height int
	original      theme.Theme // for cancel-revert
	confirmDelete bool        // showing the delete confirmation prompt
	message       string
}

// NewThemePicker constructs a fresh picker, restoring the position of
// the active theme so reopens are stable.
func NewThemePicker() ThemePickerModel {
	all := theme.LoadAll()
	active := ActiveTheme()
	idx := 0
	for i, t := range all {
		if strings.EqualFold(t.Name, active.Name) {
			idx = i
			break
		}
	}
	return ThemePickerModel{
		themes:   all,
		selected: idx,
		original: active,
	}
}

// SetSize stores the screen dimensions.
func (m *ThemePickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// reload re-reads the theme list from disk and clamps the selection.
func (m *ThemePickerModel) reload() {
	m.themes = theme.LoadAll()
	if m.selected >= len(m.themes) {
		m.selected = len(m.themes) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// applyAtCursor applies the highlighted theme to the renderer.
func (m *ThemePickerModel) applyAtCursor() {
	if m.selected < 0 || m.selected >= len(m.themes) {
		return
	}
	ApplyTheme(m.themes[m.selected])
}

// Update handles input.
func (m ThemePickerModel) Update(msg tea.Msg) (ThemePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Confirm-delete prompt eats the next key.
		if m.confirmDelete {
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				if m.selected >= 0 && m.selected < len(m.themes) {
					t := m.themes[m.selected]
					if err := theme.Delete(t); err != nil {
						m.message = "Delete failed: " + err.Error()
					} else {
						m.message = "Deleted " + t.Name
						m.reload()
						m.applyAtCursor()
					}
				}
			default:
				m.message = "Delete cancelled"
			}
			m.confirmDelete = false
			return m, nil
		}
		switch msg.String() {
		case "esc":
			// Revert to the original theme on cancel.
			ApplyTheme(m.original)
			return m, func() tea.Msg { return ThemePickerCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.applyAtCursor()
			}
		case "down", "j":
			if m.selected < len(m.themes)-1 {
				m.selected++
				m.applyAtCursor()
			}
		case "enter":
			// Confirm selection (already applied) and persist.
			if m.selected >= 0 && m.selected < len(m.themes) {
				name := m.themes[m.selected].Name
				return m, tea.Batch(
					func() tea.Msg { return ThemeAppliedMsg{Name: name} },
					func() tea.Msg { return ThemePickerCloseMsg{} },
				)
			}
		case "e":
			// Edit theme.
			if m.selected >= 0 && m.selected < len(m.themes) {
				t := m.themes[m.selected]
				return m, func() tea.Msg { return ThemeEditorOpenMsg{Theme: t} }
			}
		case "c":
			// Clone theme.
			if m.selected >= 0 && m.selected < len(m.themes) {
				src := m.themes[m.selected]
				cloned, err := theme.Clone(src)
				if err != nil {
					m.message = "Clone failed: " + err.Error()
				} else {
					m.message = "Cloned to " + cloned.Name
					m.reload()
					// Move selection onto the new clone.
					for i, t := range m.themes {
						if t.Name == cloned.Name {
							m.selected = i
							break
						}
					}
					m.applyAtCursor()
				}
			}
		case "d", "delete":
			// Delete user theme (not built-ins).
			if m.selected >= 0 && m.selected < len(m.themes) {
				if m.themes[m.selected].Builtin {
					m.message = "Cannot delete built-in themes"
				} else {
					m.confirmDelete = true
					m.message = fmt.Sprintf("Delete %q? (y/N)", m.themes[m.selected].Name)
				}
			}
		}
	}
	return m, nil
}

// Refresh reloads the theme list (used after the editor saves changes).
func (m *ThemePickerModel) Refresh() {
	m.reload()
}

// View renders the picker as a centered modal.
func (m ThemePickerModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Themes"))
	b.WriteString("\n\n")
	if len(m.themes) == 0 {
		b.WriteString(dimStyle.Render("  (no themes found)"))
		b.WriteString("\n")
	}
	for i, t := range m.themes {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		tag := ""
		if t.Builtin {
			tag = muteStyle.Render(" [built-in]")
		}
		line := marker + t.Name + tag
		if i == m.selected {
			line = selStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  ↑/↓ preview · Enter apply · e edit · c clone · d delete · Esc cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// =====================================================================
// Theme Editor
// =====================================================================

// ThemeEditorModel lets the user edit one theme's properties.
type ThemeEditorModel struct {
	original      theme.Theme // baseline for cancel-revert
	current       theme.Theme // working copy (with edits)
	width, height int
	selected      int // 0 = name field, 1..len(AllKeys) = color keys
	editingName   bool
	nameInput     textinput.Model
	confirmCancel bool
	dirty         bool
	message       string
}

// NewThemeEditor constructs an editor for a copy of the given theme.
func NewThemeEditor(t theme.Theme) ThemeEditorModel {
	working := theme.Theme{
		Name:    t.Name,
		Mode:    t.Mode,
		Colors:  copyMap(t.Colors),
		Path:    t.Path,
		Builtin: t.Builtin,
	}
	ti := textinput.New()
	ti.CharLimit = 48
	ti.SetValue(working.Name)
	return ThemeEditorModel{
		original:  t,
		current:   working,
		nameInput: ti,
	}
}

// SetSize stores the screen dimensions.
func (m *ThemeEditorModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetColor updates the working theme's color and re-applies it live so
// the editor preview reflects the change immediately.
func (m *ThemeEditorModel) SetColor(key, value string) {
	if m.current.Colors == nil {
		m.current.Colors = map[string]string{}
	}
	m.current.Colors[key] = value
	m.dirty = true
	ApplyTheme(m.current)
}

// Update handles editor input.
func (m ThemeEditorModel) Update(msg tea.Msg) (ThemeEditorModel, tea.Cmd) {
	if m.editingName {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				newName := strings.TrimSpace(m.nameInput.Value())
				if newName == "" {
					m.message = "Name cannot be empty"
				} else {
					m.current.Name = newName
					m.dirty = true
					m.message = ""
				}
				m.editingName = false
			case "esc":
				m.nameInput.SetValue(m.current.Name)
				m.editingName = false
			default:
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}

	if m.confirmCancel {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch strings.ToLower(keyMsg.String()) {
			case "y", "enter":
				ApplyTheme(m.original)
				return m, func() tea.Msg { return ThemeEditorCloseMsg{} }
			default:
				m.confirmCancel = false
				m.message = ""
			}
		}
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			if m.dirty {
				m.confirmCancel = true
				m.message = "Discard changes? (y/N)"
				return m, nil
			}
			ApplyTheme(m.original)
			return m, func() tea.Msg { return ThemeEditorCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(theme.AllKeys) { // 0..len = name + keys
				m.selected++
			}
		case "enter", " ":
			if m.selected == 0 {
				m.editingName = true
				m.nameInput.SetValue(m.current.Name)
				m.nameInput.Focus()
				m.nameInput.CursorEnd()
				return m, nil
			}
			keyIdx := m.selected - 1
			if keyIdx >= 0 && keyIdx < len(theme.AllKeys) {
				key := theme.AllKeys[keyIdx]
				return m, func() tea.Msg {
					return ThemeColorPickerOpenMsg{Key: key, Initial: m.current.Get(key)}
				}
			}
		case "s":
			// Save: persist the working theme.
			if strings.TrimSpace(m.current.Name) == "" {
				m.message = "Name cannot be empty"
				return m, nil
			}
			// Built-ins cannot overwrite themselves — saving creates a user copy
			// under the same name, which the loader will prefer next session.
			saved, err := saveTheme(m.current, m.original)
			if err != nil {
				m.message = "Save failed: " + err.Error()
				return m, nil
			}
			m.current = saved
			m.original = saved
			m.dirty = false
			m.message = "Saved"
			return m, nil
		}
	}
	return m, nil
}

// saveTheme persists the working theme. If the name changed, the old
// user theme file is removed (built-ins are left in place).
func saveTheme(working, original theme.Theme) (theme.Theme, error) {
	if working.Name != original.Name && !original.Builtin {
		// Use Rename, which checks for filename collisions.
		return theme.Rename(working, working.Name)
	}
	path, err := theme.Save(working)
	if err != nil {
		return working, err
	}
	working.Path = path
	working.Builtin = false
	return working, nil
}

// View renders the editor.
func (m ThemeEditorModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Theme Editor"))
	b.WriteString("\n\n")

	// Name field at the top.
	{
		marker := "  "
		if m.selected == 0 {
			marker = "> "
		}
		label := "Name: "
		var value string
		if m.editingName {
			value = m.nameInput.View()
		} else {
			value = m.current.Name
		}
		line := marker + label + value
		if m.selected == 0 && !m.editingName {
			line = selStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n\n")
	}

	for i, key := range theme.AllKeys {
		marker := "  "
		isSel := m.selected == i+1
		if isSel {
			marker = "> "
		}
		val := m.current.Get(key)
		valLabel := val
		if val == "" {
			valLabel = "(default)"
		}
		// Render the value in its actual color so the user gets a live preview.
		valStyle := lipgloss.NewStyle()
		if val != "" {
			valStyle = valStyle.Foreground(lipgloss.Color(val)).Bold(true)
		} else {
			valStyle = valStyle.Foreground(ColorMuted)
		}
		// Pad the key label to a fixed width for alignment.
		label := padRight(key, 16)
		line := marker + label + " " + valStyle.Render(valLabel)
		if isSel {
			line = selStyle.Render(marker+label) + " " + valStyle.Render(valLabel)
		}
		// Description in muted text after the value.
		desc := theme.KeyDescription(key)
		if desc != "" {
			line += "  " + muteStyle.Render(desc)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	if m.editingName {
		b.WriteString(dimStyle.Render("  Type name · Enter accept · Esc cancel"))
	} else {
		b.WriteString(dimStyle.Render("  ↑/↓ navigate · Enter edit · s save · Esc back"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// =====================================================================
// Color Picker
// =====================================================================

// pickerSlot identifies which color slot (fg or bg) the picker is currently editing.
type pickerSlot int

const (
	pickerSlotFg pickerSlot = iota
	pickerSlotBg
)

// ThemeColorPickerModel renders a 16x16 grid of the 256 standard terminal
// colors. The user can pick BOTH a foreground and a background, then accept
// to commit the combined "fg/bg" value back to the theme editor.
type ThemeColorPickerModel struct {
	key           string
	width, height int
	cursorR       int
	cursorC       int
	gridStartX    int
	gridStartY    int
	cellW         int
	cellH         int
	manualEntry   bool
	manualInput   textinput.Model

	// Active slot the next pick / clear targets.
	slot pickerSlot
	// Working fg/bg values (raw color strings, "" = default).
	fg string
	bg string
}

// NewThemeColorPicker constructs a new color picker for the given key
// and the initial combined value (e.g. "12" or "12/240").
func NewThemeColorPicker(key, initial string) ThemeColorPickerModel {
	fg, bg := theme.ParseColor(initial)
	cursorR, cursorC := 0, 0
	if n, err := strconv.Atoi(fg); err == nil && n >= 0 && n < 256 {
		cursorR = n / 16
		cursorC = n % 16
	}
	ti := textinput.New()
	ti.CharLimit = 32
	ti.SetValue(initial)
	return ThemeColorPickerModel{
		key:         key,
		cursorR:     cursorR,
		cursorC:     cursorC,
		cellW:       4,
		cellH:       2,
		manualInput: ti,
		slot:        pickerSlotFg,
		fg:          fg,
		bg:          bg,
	}
}

// SetSize stores screen dimensions.
func (m *ThemeColorPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// currentIndex returns the 0..255 color index of the current cursor cell.
func (m *ThemeColorPickerModel) currentIndex() int {
	return m.cursorR*16 + m.cursorC
}

// commit emits the combined fg/bg value back to the editor.
func (m *ThemeColorPickerModel) commit() tea.Cmd {
	k := m.key
	value := theme.JoinColor(m.fg, m.bg)
	return func() tea.Msg { return ThemeColorPickedMsg{Key: k, Color: value} }
}

// applyCursor stores the cursor cell's color into the active slot.
// Returns true if the active slot changed (so the editor preview can blink).
func (m *ThemeColorPickerModel) applyCursor() {
	val := strconv.Itoa(m.currentIndex())
	if m.slot == pickerSlotFg {
		m.fg = val
	} else {
		m.bg = val
	}
}

// Update handles input.
func (m ThemeColorPickerModel) Update(msg tea.Msg) (ThemeColorPickerModel, tea.Cmd) {
	if m.manualEntry {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				val := strings.TrimSpace(m.manualInput.Value())
				m.fg, m.bg = theme.ParseColor(val)
				m.manualEntry = false
				return m, nil
			case "esc":
				m.manualEntry = false
				return m, nil
			default:
				var cmd tea.Cmd
				m.manualInput, cmd = m.manualInput.Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ThemeColorPickerCloseMsg{} }
		case "left", "h":
			if m.cursorC > 0 {
				m.cursorC--
			}
		case "right", "l":
			if m.cursorC < 15 {
				m.cursorC++
			}
		case "up", "k":
			if m.cursorR > 0 {
				m.cursorR--
			}
		case "down", "j":
			if m.cursorR < 15 {
				m.cursorR++
			}
		case " ":
			// Space: assign the cursor cell to the active slot, then auto-flip
			// to the other slot for a smooth fg→bg flow on first use.
			m.applyCursor()
			if m.slot == pickerSlotFg {
				m.slot = pickerSlotBg
			} else {
				m.slot = pickerSlotFg
			}
		case "enter":
			// Enter: assign the cursor cell to the active slot AND accept.
			m.applyCursor()
			return m, m.commit()
		case "a":
			// Accept without re-assigning (use whatever fg/bg are already set).
			return m, m.commit()
		case "f":
			m.slot = pickerSlotFg
		case "b":
			m.slot = pickerSlotBg
		case "t":
			if m.slot == pickerSlotFg {
				m.slot = pickerSlotBg
			} else {
				m.slot = pickerSlotFg
			}
		case "x":
			// Clear the active slot.
			if m.slot == pickerSlotFg {
				m.fg = ""
			} else {
				m.bg = ""
			}
		case "m":
			m.manualEntry = true
			m.manualInput.Focus()
			m.manualInput.CursorEnd()
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if msg.X >= m.gridStartX && msg.Y >= m.gridStartY && m.cellW > 0 && m.cellH > 0 {
				dx := msg.X - m.gridStartX
				dy := msg.Y - m.gridStartY
				col := dx / m.cellW
				row := dy / m.cellH
				if col >= 0 && col < 16 && row >= 0 && row < 16 {
					m.cursorC = col
					m.cursorR = row
					m.applyCursor()
					// Click does NOT auto-commit so the user can pick fg, click 'b'
					// or 't', click again for bg, then Enter / 'a' to accept.
				}
			}
		}
	}
	return m, nil
}

// View renders the color picker grid.
func (m *ThemeColorPickerModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	activeSlotStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Pick a color: " + m.key))
	b.WriteString("\n\n")

	// Build a 16x16 grid. Each cell shows " NNN " on a background of color N
	// so the user sees the actual color sample.
	for r := 0; r < 16; r++ {
		for c := 0; c < 16; c++ {
			idx := r*16 + c
			cellStyle := lipgloss.NewStyle().
				Background(lipgloss.Color(strconv.Itoa(idx))).
				Foreground(lipgloss.Color(contrastFor(idx))).
				Width(m.cellW).
				Align(lipgloss.Center)
			if r == m.cursorR && c == m.cursorC {
				cellStyle = cellStyle.Bold(true).
					Border(lipgloss.NormalBorder(), false, false, true, false).
					BorderForeground(ColorPrimary)
			}
			b.WriteString(cellStyle.Render(strconv.Itoa(idx)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.manualEntry {
		b.WriteString("  Manual (fg or fg/bg): ")
		b.WriteString(m.manualInput.View())
		b.WriteString("\n")
	} else {
		// Two slots side by side, with the active one bolded.
		fgLabel := "FG"
		bgLabel := "BG"
		if m.slot == pickerSlotFg {
			fgLabel = activeSlotStyle.Render("▶ FG")
		} else {
			fgLabel = mutedStyle.Render("  FG")
		}
		if m.slot == pickerSlotBg {
			bgLabel = activeSlotStyle.Render("▶ BG")
		} else {
			bgLabel = mutedStyle.Render("  BG")
		}
		fgVal := m.fg
		if fgVal == "" {
			fgVal = "(default)"
		}
		bgVal := m.bg
		if bgVal == "" {
			bgVal = "(default)"
		}
		// Live preview rendered with the actual fg+bg pair.
		previewStyle := lipgloss.NewStyle().Padding(0, 2)
		if m.fg != "" {
			previewStyle = previewStyle.Foreground(lipgloss.Color(m.fg))
		}
		if m.bg != "" {
			previewStyle = previewStyle.Background(lipgloss.Color(m.bg))
		} else {
			previewStyle = previewStyle.Background(lipgloss.Color("236"))
		}
		preview := previewStyle.Render(" sample text ")
		b.WriteString(fmt.Sprintf("  %s: %-12s   %s: %-12s   %s\n",
			fgLabel, fgVal, bgLabel, bgVal, preview))
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Arrows/mouse move · Space assign+toggle · Enter accept · f/b/t slot · x clear · m manual · Esc cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())

	// Compute the grid's screen position so mouse hit-testing matches.
	rendered := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	boxW := lipgloss.Width(box)
	boxH := strings.Count(box, "\n") + 1
	boxX := (m.width - boxW) / 2
	boxY := (m.height - boxH) / 2
	if boxX < 0 {
		boxX = 0
	}
	if boxY < 0 {
		boxY = 0
	}
	// Inside the box: border (1) + padding-top (1) + title (1) + blank (1) = 4 rows
	// before the grid. Left: border (1) + padding-left (3) = 4 cols.
	m.gridStartY = boxY + 4
	m.gridStartX = boxX + 4
	return rendered
}

// contrastFor picks black or white text for legibility on a 256 background.
func contrastFor(idx int) string {
	// Standard 16 ANSI colors.
	if idx < 16 {
		// Bright/dark heuristic.
		switch idx {
		case 0, 1, 2, 4, 5, 6, 8, 12, 13:
			return "15"
		}
		return "0"
	}
	// 6x6x6 cube (16..231).
	if idx >= 16 && idx <= 231 {
		i := idx - 16
		r := i / 36
		g := (i / 6) % 6
		bl := i % 6
		brightness := r*30 + g*59 + bl*11
		if brightness > 200 {
			return "0"
		}
		return "15"
	}
	// Grayscale ramp 232..255.
	if idx >= 232 {
		if idx-232 > 12 {
			return "0"
		}
		return "15"
	}
	return "15"
}

// =====================================================================
// Helpers
// =====================================================================

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
