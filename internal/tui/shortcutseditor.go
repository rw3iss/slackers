package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/shortcuts"
)

// ShortcutsEditorOpenMsg signals the model to open the shortcuts editor.
type ShortcutsEditorOpenMsg struct{}

// ShortcutsSavedMsg signals that shortcuts were saved.
type ShortcutsSavedMsg struct{}

// ShortcutsEditorModel provides an overlay to view and edit keyboard shortcuts.
type ShortcutsEditorModel struct {
	actions      []string
	merged       shortcuts.ShortcutMap
	overrides    shortcuts.ShortcutMap
	selected     int
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
	return ShortcutsEditorModel{
		actions:   shortcuts.ActionOrder,
		merged:    merged,
		overrides: overrides,
		version:   version,
	}
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

// Update handles key events in the shortcuts editor.
func (m ShortcutsEditorModel) Update(msg tea.Msg) (ShortcutsEditorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirming {
			// Confirming a conflict resolution.
			if msg.String() == "esc" {
				m.confirming = false
				m.editing = false
				m.message = "Cancelled"
				return m, nil
			}
			if msg.String() == "enter" || msg.String() == "y" {
				// Resolve the conflict: clear the other action and set this one.
				action := m.actions[m.selected]
				m.merged[action] = []string{m.conflictKey}
				if m.overrides == nil {
					m.overrides = make(shortcuts.ShortcutMap)
				}
				m.overrides[action] = []string{m.conflictKey}

				// Clear the conflicting action.
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

				return m, func() tea.Msg {
					_ = shortcuts.Save(shortcuts.UserConfigPath(), m.overrides)
					return ShortcutsSavedMsg{}
				}
			}
			return m, nil
		}

		if m.editing {
			// Capture mode: waiting for any key press.
			if msg.String() == "esc" {
				m.editing = false
				m.message = ""
				return m, nil
			}

			action := m.actions[m.selected]
			newKey := msg.String()

			// Check for conflicts.
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

			// No conflict — set directly.
			m.merged[action] = []string{newKey}
			if m.overrides == nil {
				m.overrides = make(shortcuts.ShortcutMap)
			}
			m.overrides[action] = []string{newKey}
			m.editing = false
			m.message = fmt.Sprintf("Set %s to '%s'", action, newKey)

			return m, func() tea.Msg {
				_ = shortcuts.Save(shortcuts.UserConfigPath(), m.overrides)
				return ShortcutsSavedMsg{}
			}
		}

		// Navigation mode.
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.ensureVisible()
			}
		case "down", "j":
			if m.selected < len(m.actions)-1 {
				m.selected++
				m.ensureVisible()
			}
		case "enter":
			m.editing = true
			m.message = "Press any key combination... (Esc to cancel)"
		case "r":
			// Reset selected action to default.
			action := m.actions[m.selected]
			defaults := shortcuts.DefaultShortcuts()
			if defKeys, ok := defaults[action]; ok {
				m.merged[action] = defKeys
			}
			delete(m.overrides, action)
			m.message = fmt.Sprintf("Reset '%s' to default", action)
			return m, func() tea.Msg {
				_ = shortcuts.Save(shortcuts.UserConfigPath(), m.overrides)
				return ShortcutsSavedMsg{}
			}
		case "pgup":
			m.selected -= 10
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
		case "pgdown":
			m.selected += 10
			if m.selected >= len(m.actions) {
				m.selected = len(m.actions) - 1
			}
			m.ensureVisible()
		}
	}
	return m, nil
}

func (m *ShortcutsEditorModel) ensureVisible() {
	viewHeight := m.height - 12
	if viewHeight < 5 {
		viewHeight = 5
	}
	if m.selected < m.scrollOff {
		m.scrollOff = m.selected
	}
	if m.selected >= m.scrollOff+viewHeight {
		m.scrollOff = m.selected - viewHeight + 1
	}
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
		Foreground(lipgloss.Color("229")).
		Width(24)

	selectedKeyStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Width(24)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

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

	viewHeight := m.height - 12
	if viewHeight < 5 {
		viewHeight = 5
	}
	end := m.scrollOff + viewHeight
	if end > len(m.actions) {
		end = len(m.actions)
	}

	defaults := shortcuts.DefaultShortcuts()

	for i := m.scrollOff; i < end; i++ {
		action := m.actions[i]
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
		b.WriteString(dimStyle.Render("  Enter: rebind | r: reset to default | Esc: close"))
	}

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth + 8).
		MaxHeight(m.height - 2)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}

// suppress unused import
var _ = key.WithKeys
