package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/theme"
)

// =====================================================================
// Messages
// =====================================================================

// ThemePickerOpenMsg requests opening the theme list overlay. ForAlt=true
// makes the picker store its result as the alternate theme instead of the
// primary one.
type ThemePickerOpenMsg struct{ ForAlt bool }

// ThemePickerCloseMsg signals the picker should close.
type ThemePickerCloseMsg struct{}

// ThemeAppliedMsg fires after a theme is applied so the model can save
// the choice to config. ForAlt is true when the picker was opened to
// select the alternate theme rather than the primary one.
type ThemeAppliedMsg struct {
	Name   string
	ForAlt bool
}

// ThemeEditorOpenMsg opens the editor for the named theme.
type ThemeEditorOpenMsg struct{ Theme theme.Theme }

// ThemeEditorCloseMsg dismisses the editor.
type ThemeEditorCloseMsg struct{}

// ThemeEditorSavedMsg fires after the editor saves a theme to disk so the
// model can refresh any caches that depend on theme colors.
type ThemeEditorSavedMsg struct{}

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

// ThemeColorPreviewMsg fires as the picker cursor moves so the editor (and
// the rest of the app) can show a live preview of the would-be selection.
type ThemeColorPreviewMsg struct {
	Key   string
	Color string
}

// ThemeColorPickerCloseMsg dismisses the color picker without selection.
type ThemeColorPickerCloseMsg struct{}

// ThemeImportBrowseMsg asks the model to open the file browser for an
// import-theme operation.
type ThemeImportBrowseMsg struct{}

// ThemeImportFileMsg carries the filesystem path the user picked from the
// file browser. The picker handles validation, conflicts, and saving.
type ThemeImportFileMsg struct{ Path string }

// =====================================================================
// Theme List (picker)
// =====================================================================

// ThemePickerModel lists all available themes. Live-applies the
// highlighted theme as the user navigates so the change is previewed
// immediately. selected == -1 represents the virtual "Import..." row.
type ThemePickerModel struct {
	themes        []theme.Theme
	selected      int
	width, height int
	original      theme.Theme // for cancel-revert
	confirmDelete bool        // showing the delete confirmation prompt
	message       string
	forAlt        bool // true when picking the alternate theme

	// Import conflict prompt state.
	importPending     *theme.Theme // parsed but not yet saved
	importPendingPath string       // source path of the pending import
	importHasConflict bool         // true while waiting for o/a/n response
}

// NewThemePicker constructs a fresh picker, restoring the position of
// the active theme so reopens are stable. forAlt=true makes the picker
// stamp its result with ForAlt so the model writes it to the alt slot.
func NewThemePicker(forAlt bool) ThemePickerModel {
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
		forAlt:   forAlt,
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
		// Import-conflict prompt eats the next key.
		if m.importHasConflict {
			switch strings.ToLower(msg.String()) {
			case "o":
				// Overwrite the existing theme.
				if m.importPending != nil {
					if _, err := theme.Save(*m.importPending); err != nil {
						m.message = "Import failed: " + err.Error()
					} else {
						m.message = "Imported (overwrote): " + m.importPending.Name
						m.reload()
						// Move selection onto the imported theme.
						for i, t := range m.themes {
							if strings.EqualFold(t.Name, m.importPending.Name) {
								m.selected = i
								break
							}
						}
						m.applyAtCursor()
					}
				}
			case "a":
				// Add alongside — append a numeric suffix until unique.
				if m.importPending != nil {
					base := m.importPending.Name
					counter := 2
					for {
						candidate := fmt.Sprintf("%s %d", base, counter)
						if _, exists := theme.FindByName(candidate); !exists {
							m.importPending.Name = candidate
							break
						}
						counter++
						if counter > 1000 {
							m.message = "Could not find a unique name"
							m.importPending = nil
							m.importHasConflict = false
							return m, nil
						}
					}
					if _, err := theme.Save(*m.importPending); err != nil {
						m.message = "Import failed: " + err.Error()
					} else {
						m.message = "Imported as: " + m.importPending.Name
						m.reload()
						for i, t := range m.themes {
							if strings.EqualFold(t.Name, m.importPending.Name) {
								m.selected = i
								break
							}
						}
						m.applyAtCursor()
					}
				}
			default:
				m.message = "Import cancelled"
			}
			m.importHasConflict = false
			m.importPending = nil
			m.importPendingPath = ""
			return m, nil
		}
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
			if m.selected > -1 {
				m.selected--
				m.applyAtCursor()
			}
		case "down", "j":
			if m.selected < len(m.themes)-1 {
				m.selected++
				m.applyAtCursor()
			}
		case "i":
			// 'i' shortcut to import a theme file.
			return m, func() tea.Msg { return ThemeImportBrowseMsg{} }
		case "enter":
			// Import row?
			if m.selected == -1 {
				return m, func() tea.Msg { return ThemeImportBrowseMsg{} }
			}
			// Confirm selection (already applied) and persist.
			if m.selected >= 0 && m.selected < len(m.themes) {
				name := m.themes[m.selected].Name
				forAlt := m.forAlt
				return m, tea.Sequence(
					func() tea.Msg { return ThemeAppliedMsg{Name: name, ForAlt: forAlt} },
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
// Also re-captures the active theme as the new "original" so a subsequent
// Esc-cancel doesn't revert edits the editor just persisted.
func (m *ThemePickerModel) Refresh() {
	m.reload()
	m.original = ActiveTheme()
}

// BeginImport tries to import the theme file at path. If the file is
// invalid an error message is set. If the theme conflicts with an
// existing theme by name, the picker enters its conflict-prompt state
// (the user is asked whether to overwrite or add alongside). If there
// is no conflict the file is saved immediately and the list refreshed.
func (m *ThemePickerModel) BeginImport(path string) {
	t, err := theme.Load(path)
	if err != nil {
		m.message = "Invalid theme: " + err.Error()
		return
	}
	if t.Name == "" {
		m.message = "Theme file has no name"
		return
	}
	if t.Colors == nil {
		m.message = "Theme file has no colors"
		return
	}
	if _, exists := theme.FindByName(t.Name); exists {
		m.importPending = &t
		m.importPendingPath = path
		m.importHasConflict = true
		m.message = fmt.Sprintf("Theme %q already exists — [o]verwrite, [a]dd alongside, Esc cancel", t.Name)
		return
	}
	if _, err := theme.Save(t); err != nil {
		m.message = "Save failed: " + err.Error()
		return
	}
	m.message = "Imported: " + t.Name
	m.reload()
	for i, th := range m.themes {
		if strings.EqualFold(th.Name, t.Name) {
			m.selected = i
			break
		}
	}
	m.applyAtCursor()
}

// View renders the picker as a centered modal.
func (m ThemePickerModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	textStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Themes"))
	b.WriteString("\n\n")
	// Import row at the top.
	{
		marker := "  "
		if m.selected == -1 {
			marker = "> "
		}
		label := marker + "Import…"
		if m.selected == -1 {
			label = selStyle.Render(label)
		} else {
			label = textStyle.Render(label)
		}
		b.WriteString(label + muteStyle.Render("  Browse for a theme JSON to add"))
		b.WriteString("\n\n")
	}
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
		var line string
		if i == m.selected {
			line = selStyle.Render(marker+t.DisplayName()) + tag
		} else {
			line = textStyle.Render(marker+t.DisplayName()) + tag
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  ↑/↓: preview" + HintSep + "Enter: apply" + HintSep + "e: edit" + HintSep + "c: clone" + HintSep + "d: delete" + HintSep + "i: import" + HintSep + FooterHintClose))

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

	// Active picker preview state. While the color picker is open, the
	// editor records the key being edited and its original value so that
	// EndPreview(false) can revert if the user cancels.
	previewKey      string
	previewOriginal string
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

// BeginPreview records the current value of `key` so EndPreview(false) can
// revert. Called when the color picker opens.
func (m *ThemeEditorModel) BeginPreview(key string) {
	m.previewKey = key
	m.previewOriginal = m.current.Get(key)
}

// PreviewColor temporarily applies a color value to the working theme without
// committing it. Call repeatedly as the picker cursor moves.
func (m *ThemeEditorModel) PreviewColor(value string) {
	if m.previewKey == "" {
		return
	}
	if m.current.Colors == nil {
		m.current.Colors = map[string]string{}
	}
	m.current.Colors[m.previewKey] = value
	ApplyTheme(m.current)
}

// EndPreview finalizes the preview. If commit is true the current value
// stays and the editor is marked dirty. If false the original value is
// restored.
func (m *ThemeEditorModel) EndPreview(commit bool) {
	if m.previewKey == "" {
		return
	}
	if commit {
		m.dirty = true
	} else {
		if m.current.Colors == nil {
			m.current.Colors = map[string]string{}
		}
		m.current.Colors[m.previewKey] = m.previewOriginal
		ApplyTheme(m.current)
	}
	m.previewKey = ""
	m.previewOriginal = ""
}

// PreviewOriginal returns the value the picker should restore on a reset.
func (m *ThemeEditorModel) PreviewOriginal() string {
	return m.previewOriginal
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
			// 0 = name, 1..len = color keys, len+1 = Export action row.
			if m.selected < len(theme.AllKeys)+1 {
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
			// Export action row.
			if m.selected == len(theme.AllKeys)+1 {
				path, err := exportThemeToDownloads(m.current)
				if err != nil {
					m.message = "Export failed: " + err.Error()
				} else {
					m.message = "Exported to " + path
				}
				return m, nil
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
			// Preserve the working color map on the saved struct, then re-apply
			// it so the rest of the app instantly reflects the persisted theme.
			saved.Colors = m.current.Colors
			m.current = saved
			m.original = saved
			m.dirty = false
			m.message = "Saved"
			ApplyTheme(m.current)
			return m, func() tea.Msg { return ThemeEditorSavedMsg{} }
		}
	}
	return m, nil
}

// exportThemeToDownloads writes a theme to ~/Downloads/<name>.json so the
// user can share it with others. Returns the absolute path on success.
func exportThemeToDownloads(t theme.Theme) (string, error) {
	dir := backupDownloadsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := theme.SanitizeFilename(t.Name)
	if name == "" {
		name = "theme"
	}
	path := filepath.Join(dir, name+".json")
	exported := theme.Theme{
		Name:   t.Name,
		Mode:   t.Mode,
		Colors: t.Colors,
	}
	data, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(path)
	return abs, nil
}

// backupDownloadsDir returns ~/Downloads (or the user's home as a fallback).
func backupDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.TempDir()
	}
	dl := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dl); err == nil && info.IsDir() {
		return dl
	}
	return home
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
	textStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)

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
		var line string
		switch {
		case m.selected == 0 && !m.editingName:
			line = selStyle.Render(marker + label + value)
		case m.editingName:
			line = textStyle.Render(marker+label) + value
		default:
			line = textStyle.Render(marker + label + value)
		}
		b.WriteString(line)
		b.WriteString("\n\n")
	}

	const valueCellW = 14
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
		// Render the value in a fixed-width centered cell using the actual
		// fg + bg colors of the value, so the editor shows a real preview.
		// If the value has no explicit background, fall back to the theme's
		// general background so the cell looks the way it will in context.
		fg, bg, bold, italic := theme.ParseColorFull(val)
		cellStyle := lipgloss.NewStyle().
			Width(valueCellW).
			Align(lipgloss.Center)
		if fg != "" {
			cellStyle = cellStyle.Foreground(lipgloss.Color(fg))
		}
		bgColor := bg
		if bgColor == "" {
			bgColor = m.current.GetBg(theme.KeyBackground)
		}
		if bgColor != "" {
			cellStyle = cellStyle.Background(lipgloss.Color(bgColor))
		}
		if bold {
			cellStyle = cellStyle.Bold(true)
		}
		if italic {
			cellStyle = cellStyle.Italic(true)
		}
		if fg == "" && bg == "" {
			cellStyle = cellStyle.Foreground(ColorMuted)
		}
		// Pad the key label to a fixed width for alignment.
		label := padRight(key, 16)
		labelStr := marker + label
		if isSel {
			labelStr = selStyle.Render(labelStr)
		} else {
			labelStr = textStyle.Render(labelStr)
		}
		line := labelStr + " " + cellStyle.Render(valLabel)
		// Description in muted text after the value.
		desc := theme.KeyDescription(key)
		if desc != "" {
			line += "  " + muteStyle.Render(desc)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Export action row at the bottom.
	{
		exportIdx := len(theme.AllKeys) + 1
		marker := "  "
		isSel := m.selected == exportIdx
		if isSel {
			marker = "> "
		}
		label := padRight("Export", 16)
		labelStr := marker + label
		if isSel {
			labelStr = selStyle.Render(labelStr)
		} else {
			labelStr = textStyle.Render(labelStr)
		}
		hint := muteStyle.Render("  Save this theme to ~/Downloads as a shareable JSON")
		b.WriteString("\n" + labelStr + hint)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	if m.editingName {
		b.WriteString(dimStyle.Render("  Type name" + HintSep + "Enter: accept" + HintSep + FooterHintCancel))
	} else {
		b.WriteString(dimStyle.Render("  ↑/↓: navigate" + HintSep + "Enter: edit" + HintSep + "s: save" + HintSep + FooterHintBack))
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
// colors. The user can pick BOTH a foreground and a background plus toggle
// bold and italic attributes, then accept to commit the combined value
// back to the theme editor.
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
	// Working fg/bg values (raw color strings, "" = default) and attributes.
	fg     string
	bg     string
	bold   bool
	italic bool
	// Initial values captured at construction time, for the reset key
	// and so the parent can revert if the user cancels.
	initialFg     string
	initialBg     string
	initialBold   bool
	initialItalic bool
}

// NewThemeColorPicker constructs a new color picker for the given key
// and the initial combined value (e.g. "12" or "12/240+bi").
func NewThemeColorPicker(key, initial string) ThemeColorPickerModel {
	fg, bg, bold, italic := theme.ParseColorFull(initial)
	cursorR, cursorC := 0, 0
	if n, err := strconv.Atoi(fg); err == nil && n >= 0 && n < 256 {
		cursorR = n / 16
		cursorC = n % 16
	}
	ti := textinput.New()
	ti.CharLimit = 32
	ti.SetValue(initial)
	return ThemeColorPickerModel{
		key:           key,
		cursorR:       cursorR,
		cursorC:       cursorC,
		cellW:         4,
		cellH:         2,
		manualInput:   ti,
		slot:          pickerSlotFg,
		fg:            fg,
		bg:            bg,
		bold:          bold,
		italic:        italic,
		initialFg:     fg,
		initialBg:     bg,
		initialBold:   bold,
		initialItalic: italic,
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

// commit emits the combined fg/bg+attrs value back to the editor.
func (m *ThemeColorPickerModel) commit() tea.Cmd {
	k := m.key
	value := theme.JoinColorFull(m.fg, m.bg, m.bold, m.italic)
	return func() tea.Msg { return ThemeColorPickedMsg{Key: k, Color: value} }
}

// previewValue returns the (fg, bg) pair the picker would emit right now —
// the currently-committed values with the cursor cell substituted into the
// active slot. This is what the bottom preview shows and what we send via
// ThemeColorPreviewMsg as the cursor moves.
func (m *ThemeColorPickerModel) previewValue() (string, string) {
	fg, bg := m.fg, m.bg
	cursorVal := strconv.Itoa(m.currentIndex())
	if m.slot == pickerSlotFg {
		fg = cursorVal
	} else {
		bg = cursorVal
	}
	return fg, bg
}

// emitPreview returns a command that sends the current preview value back
// to the editor, so the rest of the app updates live as the cursor moves.
func (m *ThemeColorPickerModel) emitPreview() tea.Cmd {
	fg, bg := m.previewValue()
	value := theme.JoinColorFull(fg, bg, m.bold, m.italic)
	k := m.key
	return func() tea.Msg { return ThemeColorPreviewMsg{Key: k, Color: value} }
}

// applyCursor stores the cursor cell's color into the active slot.
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
				return m, m.emitPreview()
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

	changed := false
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ThemeColorPickerCloseMsg{} }
		case "left", "h":
			if m.cursorC > 0 {
				m.cursorC--
				changed = true
			}
		case "right", "l":
			if m.cursorC < 15 {
				m.cursorC++
				changed = true
			}
		case "up", "k":
			if m.cursorR > 0 {
				m.cursorR--
				changed = true
			}
		case "down", "j":
			if m.cursorR < 15 {
				m.cursorR++
				changed = true
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
			changed = true
		case "enter":
			// Enter: assign the cursor cell to the active slot AND accept.
			m.applyCursor()
			return m, m.commit()
		case "a":
			// Accept without re-assigning (use whatever fg/bg are already set).
			return m, m.commit()
		case "f":
			m.slot = pickerSlotFg
			changed = true
		case "b":
			m.slot = pickerSlotBg
			changed = true
		case "tab", "t":
			if m.slot == pickerSlotFg {
				m.slot = pickerSlotBg
			} else {
				m.slot = pickerSlotFg
			}
			changed = true
		case "ctrl+b", "alt+b":
			m.bold = !m.bold
			changed = true
		case "alt+i":
			// Note: ctrl+i is identical to Tab in terminals (ASCII 9), so
			// we use alt+i for italic instead.
			m.italic = !m.italic
			changed = true
		case "x":
			// Clear the active slot.
			if m.slot == pickerSlotFg {
				m.fg = ""
			} else {
				m.bg = ""
			}
			changed = true
		case "r":
			// Reset everything to the values the picker opened with.
			m.fg = m.initialFg
			m.bg = m.initialBg
			m.bold = m.initialBold
			m.italic = m.initialItalic
			changed = true
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
					changed = true
				}
			}
		}
	}
	if changed {
		return m, m.emitPreview()
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
	mode := "FG"
	if m.slot == pickerSlotBg {
		mode = "BG"
	}
	b.WriteString(titleStyle.Render("Pick a color: " + m.key + "  [" + mode + "]"))
	b.WriteString("\n\n")

	// Build a 16x16 grid. Cell rendering depends on which slot is active:
	//   FG mode: each cell shows its number IN that color, on the currently
	//            selected background (or default if none) — so you preview
	//            how the candidate fg looks against your chosen bg.
	//   BG mode: each cell shows its number ON that color as the background,
	//            with text in the currently selected fg (or auto-contrast).
	// The cursor cell uses Reverse + Bold (no border, no extra row).
	for r := 0; r < 16; r++ {
		for c := 0; c < 16; c++ {
			idx := r*16 + c
			cellStyle := lipgloss.NewStyle().
				Width(m.cellW).
				Align(lipgloss.Center)
			if m.slot == pickerSlotFg {
				// Render number in this color, on the chosen bg if any.
				cellStyle = cellStyle.Foreground(lipgloss.Color(strconv.Itoa(idx)))
				if m.bg != "" {
					cellStyle = cellStyle.Background(lipgloss.Color(m.bg))
				}
			} else {
				// Render number on this color, with text in the chosen fg.
				cellStyle = cellStyle.Background(lipgloss.Color(strconv.Itoa(idx)))
				if m.fg != "" {
					cellStyle = cellStyle.Foreground(lipgloss.Color(m.fg))
				} else {
					cellStyle = cellStyle.Foreground(lipgloss.Color(contrastFor(idx)))
				}
			}
			if r == m.cursorR && c == m.cursorC {
				// Single-row highlight that doesn't change cell dimensions.
				cellStyle = cellStyle.Bold(true).Reverse(true)
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
		// Live preview: substitute the cursor cell into the active slot so
		// the sample updates as the user moves around the grid.
		previewFg, previewBg := m.previewValue()

		fgLabel := mutedStyle.Render("  FG")
		bgLabel := mutedStyle.Render("  BG")
		if m.slot == pickerSlotFg {
			fgLabel = activeSlotStyle.Render("▶ FG")
		} else {
			bgLabel = activeSlotStyle.Render("▶ BG")
		}
		fgVal := previewFg
		if fgVal == "" {
			fgVal = "(default)"
		}
		bgVal := previewBg
		if bgVal == "" {
			bgVal = "(default)"
		}
		previewStyle := lipgloss.NewStyle().Padding(0, 2)
		if previewFg != "" {
			previewStyle = previewStyle.Foreground(lipgloss.Color(previewFg))
		}
		if previewBg != "" {
			previewStyle = previewStyle.Background(lipgloss.Color(previewBg))
		} else {
			previewStyle = previewStyle.Background(lipgloss.Color("236"))
		}
		if m.bold {
			previewStyle = previewStyle.Bold(true)
		}
		if m.italic {
			previewStyle = previewStyle.Italic(true)
		}
		preview := previewStyle.Render(" sample text ")
		// Bold / Italic indicators.
		boldOn := mutedStyle.Render("[B]")
		italicOn := mutedStyle.Render("[I]")
		if m.bold {
			boldOn = activeSlotStyle.Render("[B]")
		}
		if m.italic {
			italicOn = activeSlotStyle.Render("[I]")
		}
		b.WriteString(fmt.Sprintf("  %s: %-12s   %s: %-12s   %s %s   %s\n",
			fgLabel, fgVal, bgLabel, bgVal, boldOn, italicOn, preview))
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Arrows/mouse" + HintSep + "Space: assign+toggle" + HintSep + "Enter: accept" + HintSep + "Tab/f/b: slot" + HintSep + "Alt-B: bold" + HintSep + "Alt-I: italic" + HintSep + "x: clear" + HintSep + "r: reset" + HintSep + "m: manual" + HintSep + FooterHintCancel))

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
