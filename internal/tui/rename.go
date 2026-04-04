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
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Rename Channel"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("  Current: "))
	b.WriteString(m.channelName)
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("  Alias:   "))
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Enter: save | Esc: cancel | Clear to remove alias"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(55, m.width-4))

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
