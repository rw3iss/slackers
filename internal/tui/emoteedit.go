package tui

// Emote Edit overlay — form for editing or creating an emote.
// Shows a command input at the top and a text input below.
// Validates on save: command not taken by a non-emote command,
// text not empty, variables valid. Save writes to the user's
// emotes.json and dispatches EmoteEditSaveMsg so the model can
// rebuild the command registry.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/emotes"
)

// EmoteEditSaveMsg is dispatched on successful save. The model
// handler rebuilds the command registry so the trie reflects the
// new/updated emote.
type EmoteEditSaveMsg struct {
	Command string
}

// EmoteEditCancelMsg returns to the emotes list without saving.
type EmoteEditCancelMsg struct{}

// EmoteEditModel is the edit/create overlay.
type EmoteEditModel struct {
	commandInput textinput.Model
	textInput    textinput.Model
	original     string // original command name (empty for new)
	store        *emotes.Store
	registry     *commands.Registry
	errMsg       string
	width        int
	height       int
	focusIdx     int // 0=command, 1=text, 2=save, 3=cancel
}

// NewEmoteEdit creates the edit overlay. If existing is non-nil,
// the form is pre-filled with the emote's command and text.
// Otherwise it's a blank "new emote" form.
func NewEmoteEdit(store *emotes.Store, registry *commands.Registry, existing *emotes.Emote) EmoteEditModel {
	ci := textinput.New()
	ci.Placeholder = "command name (no slash)"
	ci.CharLimit = 32
	ci.Prompt = "/ "
	ci.Focus()

	ti := textinput.New()
	ti.Placeholder = "$sender does something..."
	ti.CharLimit = 256
	ti.Width = 60

	original := ""
	if existing != nil {
		ci.SetValue(existing.Command)
		ti.SetValue(existing.Text)
		original = existing.Command
	}

	return EmoteEditModel{
		commandInput: ci,
		textInput:    ti,
		original:     original,
		store:        store,
		registry:     registry,
	}
}

func (m *EmoteEditModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m EmoteEditModel) Update(msg tea.Msg) (EmoteEditModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Route keystrokes based on focus.
		if m.focusIdx == 0 || m.focusIdx == 1 {
			switch v.String() {
			case "esc":
				return m, func() tea.Msg { return EmoteEditCancelMsg{} }
			case "tab", "down":
				m.advanceFocus(1)
				return m, nil
			case "shift+tab", "up":
				m.advanceFocus(-1)
				return m, nil
			case "enter":
				if m.focusIdx == 0 {
					// Enter on command input → advance to text.
					m.advanceFocus(1)
					return m, nil
				}
				// Enter on text input → advance to save.
				m.advanceFocus(1)
				return m, nil
			default:
				if m.focusIdx == 0 {
					var cmd tea.Cmd
					m.commandInput, cmd = m.commandInput.Update(msg)
					m.errMsg = ""
					return m, cmd
				}
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				m.errMsg = ""
				return m, cmd
			}
		}
		// Focus on save/cancel buttons.
		switch v.String() {
		case "esc":
			return m, func() tea.Msg { return EmoteEditCancelMsg{} }
		case "tab", "down":
			m.advanceFocus(1)
			return m, nil
		case "shift+tab", "up":
			m.advanceFocus(-1)
			return m, nil
		case "enter", " ":
			if m.focusIdx == 2 {
				return m, m.save()
			}
			if m.focusIdx == 3 {
				return m, func() tea.Msg { return EmoteEditCancelMsg{} }
			}
		}
	}
	return m, nil
}

func (m *EmoteEditModel) advanceFocus(delta int) {
	m.focusIdx = (m.focusIdx + delta + 4) % 4
	m.commandInput.Blur()
	m.textInput.Blur()
	switch m.focusIdx {
	case 0:
		m.commandInput.Focus()
	case 1:
		m.textInput.Focus()
	}
}

// save validates and persists the emote.
func (m *EmoteEditModel) save() tea.Cmd {
	cmd := strings.ToLower(strings.TrimSpace(m.commandInput.Value()))
	text := strings.TrimSpace(m.textInput.Value())

	// Validate command name.
	if cmd == "" {
		m.errMsg = "Command name cannot be empty"
		return nil
	}
	if strings.ContainsAny(cmd, " \t/") {
		m.errMsg = "Command name cannot contain spaces or slashes"
		return nil
	}
	// Check for conflict with non-emote commands.
	if existing := m.registry.Get(cmd); existing != nil && existing.Kind != commands.KindEmote {
		m.errMsg = "/" + cmd + " is already a built-in command"
		return nil
	}
	// If the command name changed, check the new name isn't taken
	// by another emote (unless it's the same emote we're editing).
	if cmd != m.original {
		if existing := m.registry.Get(cmd); existing != nil && existing.Kind == commands.KindEmote && existing.Name != m.original {
			m.errMsg = "/" + cmd + " is already an emote"
			return nil
		}
	}
	// Validate text.
	if text == "" {
		m.errMsg = "Emote text cannot be empty"
		return nil
	}
	if err := emotes.ValidateTemplate(text); err != nil {
		m.errMsg = err.Error()
		return nil
	}
	// If the command name changed and we were editing an existing
	// emote, delete the old one first so we don't leave a ghost.
	if m.original != "" && cmd != m.original {
		_ = m.store.Delete(m.original)
	}
	// Save the emote.
	e := emotes.Emote{Command: cmd, Text: text}
	if err := m.store.Set(e); err != nil {
		m.errMsg = "Save failed: " + err.Error()
		return nil
	}
	savedCmd := cmd
	return func() tea.Msg { return EmoteEditSaveMsg{Command: savedCmd} }
}

func (m EmoteEditModel) View() string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder

	// Command input.
	prefix := "  "
	if m.focusIdx == 0 {
		prefix = selStyle.Render("▶ ")
	}
	b.WriteString(prefix + labelStyle.Render("Command: ") + m.commandInput.View() + "\n\n")

	// Text input.
	prefix = "  "
	if m.focusIdx == 1 {
		prefix = selStyle.Render("▶ ")
	}
	b.WriteString(prefix + labelStyle.Render("Text:    ") + m.textInput.View() + "\n\n")

	// Variables hint.
	b.WriteString(dimStyle.Render("  Variables: $sender $me $receiver $you $all $text"))
	b.WriteString("\n\n")

	// Error.
	if m.errMsg != "" {
		b.WriteString("  " + errStyle.Render(m.errMsg) + "\n\n")
	}

	// Save button.
	saveLabel := "  Save"
	if m.focusIdx == 2 {
		saveLabel = selStyle.Render("▶ Save")
	}
	b.WriteString(saveLabel + "\n")

	// Cancel button.
	cancelLabel := "  Cancel"
	if m.focusIdx == 3 {
		cancelLabel = selStyle.Render("▶ Cancel")
	}
	b.WriteString(cancelLabel + "\n")

	title := "Edit Emote"
	if m.original == "" {
		title = "New Emote"
	}

	scaffold := OverlayScaffold{
		Title:       title,
		Footer:      "Tab: navigate · Enter: confirm · Esc: cancel",
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 70,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
