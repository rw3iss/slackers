package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/shortcuts"
)

// ShortcutsEditorOpenMsg signals the model to open the shortcuts editor.
type ShortcutsEditorOpenMsg struct{}

// ShortcutsSavedMsg signals that shortcuts were saved.
type ShortcutsSavedMsg struct{}

// ShortcutsEditorModel provides an overlay to view and edit keyboard
// shortcuts. Renders a filterable, mouse-scrollable list of every
// action in shortcuts.ActionOrder with its current binding. The
// filter textinput at the top lives in m.filter; navigation wraps
// around at both ends of the filtered list.
type ShortcutsEditorModel struct {
	actions   []string
	merged    shortcuts.ShortcutMap
	overrides shortcuts.ShortcutMap
	filter    textinput.Model
	// filtered is the index-into-actions slice of rows currently
	// visible after applying the filter query. Rebuilt on every
	// filter update.
	filtered     []int
	selected     int // index into filtered
	scrollOff    int
	editing      bool   // waiting for key press
	confirming   bool   // confirming conflict resolution
	conflictKey  string // the key that conflicted
	conflictWith string // the action it conflicts with
	message      string
	width        int
	height       int
	version      string
}

// NewShortcutsEditorModel creates a shortcuts editor from the current merged map.
func NewShortcutsEditorModel(merged, overrides shortcuts.ShortcutMap, version string) ShortcutsEditorModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.CharLimit = 48
	ti.Prompt = " / "
	ti.Focus()
	m := ShortcutsEditorModel{
		actions:   shortcuts.ActionOrder,
		merged:    merged,
		overrides: overrides,
		filter:    ti,
		version:   version,
	}
	m.rebuildFiltered()
	return m
}

// rebuildFiltered updates the visible row index slice based on the
// current filter query. Matching is case-insensitive against both
// the action description and the action's key bindings, so typing
// "ctrl+k" narrows to the channel-search row and typing "hide"
// narrows to the hide-channel row.
func (m *ShortcutsEditorModel) rebuildFiltered() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.filtered = m.filtered[:0]
	for i, action := range m.actions {
		if q == "" {
			m.filtered = append(m.filtered, i)
			continue
		}
		desc := strings.ToLower(shortcuts.ActionDescriptions[action])
		keys := strings.ToLower(strings.Join(m.merged[action], " "))
		if strings.Contains(desc, q) || strings.Contains(keys, q) || strings.Contains(action, q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.ensureVisible()
}

// currentAction returns the action name at the currently selected
// row in the filtered list, or "" if the list is empty.
func (m *ShortcutsEditorModel) currentAction() string {
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return ""
	}
	return m.actions[m.filtered[m.selected]]
}

// SetSize sets the overlay dimensions.
func (m *ShortcutsEditorModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// IsCapturing returns true when the editor is waiting for a key press (editing or confirming).
// The model should suppress all other shortcuts and signal handling during this state.
func (m *ShortcutsEditorModel) IsCapturing() bool {
	return m.editing || m.confirming
}

// Update handles key + mouse events in the shortcuts editor.
func (m ShortcutsEditorModel) Update(msg tea.Msg) (ShortcutsEditorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirming {
			if msg.String() == "esc" {
				m.confirming = false
				m.editing = false
				m.message = "Cancelled"
				return m, nil
			}
			if msg.String() == "enter" || msg.String() == "y" {
				action := m.currentAction()
				if action == "" {
					m.confirming = false
					m.editing = false
					return m, nil
				}
				m.merged[action] = []string{m.conflictKey}
				if m.overrides == nil {
					m.overrides = make(shortcuts.ShortcutMap)
				}
				m.overrides[action] = []string{m.conflictKey}

				if conflictKeys, ok := m.merged[m.conflictWith]; ok {
					var remaining []string
					for _, k := range conflictKeys {
						if k != m.conflictKey {
							remaining = append(remaining, k)
						}
					}
					m.merged[m.conflictWith] = remaining
					m.overrides[m.conflictWith] = remaining
				}

				conflictDesc := shortcuts.ActionDescriptions[m.conflictWith]
				m.message = fmt.Sprintf("Set '%s'. Cleared from: %s", m.conflictKey, conflictDesc)
				m.confirming = false
				m.editing = false

				overridesCopy := m.overrides
				return m, func() tea.Msg {
					_ = shortcuts.Save(shortcuts.UserConfigPath(), overridesCopy)
					return ShortcutsSavedMsg{}
				}
			}
			return m, nil
		}

		if m.editing {
			if msg.String() == "esc" {
				m.editing = false
				m.message = ""
				return m, nil
			}

			action := m.currentAction()
			if action == "" {
				m.editing = false
				return m, nil
			}
			newKey := msg.String()

			for a, keys := range m.merged {
				if a == action {
					continue
				}
				for _, k := range keys {
					if k == newKey {
						desc := shortcuts.ActionDescriptions[a]
						m.confirming = true
						m.conflictKey = newKey
						m.conflictWith = a
						m.message = fmt.Sprintf("'%s' is used by '%s'. Press Enter/Y to reassign, Esc to cancel", newKey, desc)
						return m, nil
					}
				}
			}

			m.merged[action] = []string{newKey}
			if m.overrides == nil {
				m.overrides = make(shortcuts.ShortcutMap)
			}
			m.overrides[action] = []string{newKey}
			m.editing = false
			m.message = fmt.Sprintf("Set %s to '%s'", action, newKey)

			overridesCopy := m.overrides
			m.rebuildFiltered()
			return m, func() tea.Msg {
				_ = shortcuts.Save(shortcuts.UserConfigPath(), overridesCopy)
				return ShortcutsSavedMsg{}
			}
		}

		// Navigation mode: arrow keys / pgup / pgdn / enter / r
		// all bypass the filter input. Anything else is routed to
		// the filter input so typing narrows the list in-place.
		switch msg.String() {
		case "up":
			if len(m.filtered) == 0 {
				return m, nil
			}
			if m.selected == 0 {
				// Wrap to bottom.
				m.selected = len(m.filtered) - 1
			} else {
				m.selected--
			}
			m.ensureVisible()
			return m, nil
		case "down":
			if len(m.filtered) == 0 {
				return m, nil
			}
			if m.selected >= len(m.filtered)-1 {
				// Wrap to top.
				m.selected = 0
			} else {
				m.selected++
			}
			m.ensureVisible()
			return m, nil
		case "pgup":
			m.selected -= 10
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
			return m, nil
		case "pgdown":
			m.selected += 10
			if m.selected >= len(m.filtered) {
				m.selected = len(m.filtered) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
			return m, nil
		case "home":
			m.selected = 0
			m.ensureVisible()
			return m, nil
		case "end":
			if len(m.filtered) > 0 {
				m.selected = len(m.filtered) - 1
			}
			m.ensureVisible()
			return m, nil
		case "enter":
			if m.currentAction() == "" {
				return m, nil
			}
			m.editing = true
			m.message = "Press any key combination... (Esc to cancel)"
			return m, nil
		case "ctrl+r":
			// Reset selected action to default (moved off 'r' so
			// the user can type "r" in the filter input without
			// wiping a binding).
			action := m.currentAction()
			if action == "" {
				return m, nil
			}
			defaults := shortcuts.DefaultShortcuts()
			if defKeys, ok := defaults[action]; ok {
				m.merged[action] = defKeys
			}
			delete(m.overrides, action)
			m.message = fmt.Sprintf("Reset '%s' to default", action)
			overridesCopy := m.overrides
			m.rebuildFiltered()
			return m, func() tea.Msg {
				_ = shortcuts.Save(shortcuts.UserConfigPath(), overridesCopy)
				return ShortcutsSavedMsg{}
			}
		}

		// Any other keystroke goes into the filter input.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildFiltered()
		return m, cmd

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if len(m.filtered) == 0 {
				return m, nil
			}
			if m.selected > 0 {
				m.selected--
			}
			if m.scrollOff > 0 && m.selected <= m.scrollOff {
				m.scrollOff--
			}
			m.ensureVisible()
		case tea.MouseButtonWheelDown:
			if len(m.filtered) == 0 {
				return m, nil
			}
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			m.ensureVisible()
		}
	}
	return m, nil
}

func (m *ShortcutsEditorModel) ensureVisible() {
	viewHeight := m.viewHeight()
	if viewHeight < 1 {
		viewHeight = 1
	}
	if m.selected < m.scrollOff {
		m.scrollOff = m.selected
	}
	if m.selected >= m.scrollOff+viewHeight {
		m.scrollOff = m.selected - viewHeight + 1
	}
	maxOff := len(m.filtered) - viewHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// viewHeight returns the number of shortcut rows that fit in the
// visible window (screen height minus title / filter / footer).
func (m ShortcutsEditorModel) viewHeight() int {
	// Reservation breakdown inside the box (height = m.height - 4):
	//   title(1) + blank(1) + filter(1) + blank(1)            = 4
	//   blank + message + blank(3) + footer(1)                = 4
	// Effective row capacity:
	vh := (m.height - 4) - 8
	if vh < 5 {
		vh = 5
	}
	return vh
}

// Overrides returns the current user overrides.
func (m *ShortcutsEditorModel) Overrides() shortcuts.ShortcutMap {
	return m.overrides
}

// Merged returns the current merged shortcut map.
func (m *ShortcutsEditorModel) Merged() shortcuts.ShortcutMap {
	return m.merged
}

// View renders the shortcuts editor overlay.
func (m ShortcutsEditorModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	verStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	keyStyle := lipgloss.NewStyle().
		Foreground(ColorKeyBindText).
		Width(24)

	selectedKeyStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Width(24)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorDescText)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	isOverridden := lipgloss.NewStyle().
		Foreground(ColorAccent)

	boxWidth := m.width - 8
	if boxWidth > 85 {
		boxWidth = 85
	}

	var b strings.Builder

	titleLeft := titleStyle.Render("Keyboard Shortcuts")
	titleRight := verStyle.Render("slackers v" + m.version)
	gap := boxWidth - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(titleLeft + strings.Repeat(" ", gap) + titleRight)
	b.WriteString("\n\n")

	// Filter input row.
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	viewHeight := m.viewHeight()
	end := m.scrollOff + viewHeight
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	defaults := shortcuts.DefaultShortcuts()

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  No matches"))
		b.WriteString("\n")
	}

	for i := m.scrollOff; i < end; i++ {
		action := m.actions[m.filtered[i]]
		keys := m.merged[action]
		desc := shortcuts.ActionDescriptions[action]
		keysStr := strings.Join(keys, ", ")
		if len(keys) == 0 {
			keysStr = "(none)"
		}

		cursor := "  "
		ks := keyStyle
		if i == m.selected {
			cursor = "> "
			ks = selectedKeyStyle
			if m.editing {
				keysStr = "[ press key... ]"
			} else if m.confirming {
				keysStr = "[ confirm? ]"
			}
		}

		suffix := ""
		if _, ok := m.overrides[action]; ok {
			defKeys := strings.Join(defaults[action], ", ")
			if defKeys != keysStr {
				suffix = isOverridden.Render(" *")
			}
		}

		b.WriteString(cursor)
		b.WriteString(ks.Render(keysStr + suffix))
		b.WriteString(descStyle.Render(desc))
		b.WriteString("\n")
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
	}

	b.WriteString("\n\n")
	if m.editing {
		b.WriteString(dimStyle.Render("  Press any key to set | Esc: cancel"))
	} else if m.confirming {
		b.WriteString(dimStyle.Render("  Enter/Y: confirm reassign | Esc: cancel"))
	} else {
		b.WriteString(dimStyle.Render("  Type to filter | ↑/↓ nav (wraps) | Enter: rebind | Ctrl-R: reset | Esc: close"))
	}

	content := b.String()

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth + 8).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}

// suppress unused import
var _ = key.WithKeys
