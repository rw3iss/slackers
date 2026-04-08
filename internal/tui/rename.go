package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RenameChannelMsg is sent when the user renames a channel.
type RenameChannelMsg struct {
	ChannelID string
	Alias     string
}

// RenameModel provides an overlay to set a display alias for a channel.
type RenameModel struct {
	channelID   string
	channelName string
	input       textinput.Model
	width       int
	height      int
}

// NewRenameModel creates a rename overlay for the given channel.
func NewRenameModel(channelID, currentName, currentAlias string) RenameModel {
	ti := textinput.New()
	ti.Placeholder = "Enter alias..."
	ti.CharLimit = 64
	ti.Focus()

	if currentAlias != "" {
		ti.SetValue(currentAlias)
	}

	return RenameModel{
		channelID:   channelID,
		channelName: currentName,
		input:       ti,
	}
}

// SetSize sets the overlay dimensions.
func (m *RenameModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the rename overlay.
func (m RenameModel) Update(msg tea.Msg) (RenameModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			alias := strings.TrimSpace(m.input.Value())
			return m, func() tea.Msg {
				return RenameChannelMsg{
					ChannelID: m.channelID,
					Alias:     alias,
				}
			}
		case "esc":
			// Signal cancel by returning empty alias with special marker
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the rename overlay.
func (m RenameModel) View() string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	b.WriteString(labelStyle.Render("  Current: "))
	b.WriteString(m.channelName)
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("  Alias:   "))
	b.WriteString(m.input.View())

	scaffold := OverlayScaffold{
		Title:       "Rename Channel",
		Footer:      "Enter: save | Esc: cancel | Clear to remove alias",
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 55,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
