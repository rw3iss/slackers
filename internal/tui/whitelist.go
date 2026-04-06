package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/types"
)

// WhitelistUpdateMsg is sent when the whitelist is modified.
type WhitelistUpdateMsg struct {
	Whitelist []string
}

// WhitelistModel provides an overlay to manage the secure messaging whitelist.
type WhitelistModel struct {
	whitelist []string              // user IDs
	users     map[string]types.User // lookup for display names
	selected  int
	adding    bool // true when the add-user input is active
	addInput  textinput.Model
	width     int
	height    int
}

// NewWhitelistModel creates a new whitelist management overlay.
func NewWhitelistModel(whitelist []string, users map[string]types.User) WhitelistModel {
	ti := textinput.New()
	ti.Placeholder = "User ID or @name"
	ti.CharLimit = 64

	return WhitelistModel{
		whitelist: append([]string{}, whitelist...),
		users:     users,
		addInput:  ti,
	}
}

// SetSize sets the overlay dimensions.
func (m *WhitelistModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the whitelist overlay.
func (m WhitelistModel) Update(msg tea.Msg) (WhitelistModel, tea.Cmd) {
	if m.adding {
		return m.updateAdding(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.whitelist)-1 {
				m.selected++
			}
		case "a":
			m.adding = true
			m.addInput.Reset()
			m.addInput.Focus()
			return m, textinput.Blink
		case "d", "delete", "backspace":
			if len(m.whitelist) > 0 && m.selected < len(m.whitelist) {
				m.whitelist = append(m.whitelist[:m.selected], m.whitelist[m.selected+1:]...)
				if m.selected >= len(m.whitelist) && m.selected > 0 {
					m.selected--
				}
				return m, func() tea.Msg {
					return WhitelistUpdateMsg{Whitelist: m.whitelist}
				}
			}
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.selected > 0 {
				m.selected--
			}
		case tea.MouseButtonWheelDown:
			if m.selected < len(m.whitelist)-1 {
				m.selected++
			}
		}
	}
	return m, nil
}

func (m WhitelistModel) updateAdding(msg tea.Msg) (WhitelistModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.adding = false
			m.addInput.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.addInput.Value())
			if val != "" {
				// Resolve @name to user ID if possible.
				userID := val
				if strings.HasPrefix(val, "@") {
					name := val[1:]
					for id, u := range m.users {
						if strings.EqualFold(u.DisplayName, name) || strings.EqualFold(u.RealName, name) {
							userID = id
							break
						}
					}
				}
				// Avoid duplicates.
				found := false
				for _, id := range m.whitelist {
					if id == userID {
						found = true
						break
					}
				}
				if !found {
					m.whitelist = append(m.whitelist, userID)
				}
			}
			m.adding = false
			m.addInput.Blur()
			return m, func() tea.Msg {
				return WhitelistUpdateMsg{Whitelist: m.whitelist}
			}
		}
	}

	var cmd tea.Cmd
	m.addInput, cmd = m.addInput.Update(msg)
	return m, cmd
}

// View renders the whitelist management overlay.
func (m WhitelistModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	secureStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00ff00"))

	var b strings.Builder

	b.WriteString(titleStyle.Render("Secure Whitelist"))
	b.WriteString("\n\n")

	if len(m.whitelist) == 0 {
		b.WriteString(dimStyle.Render("  No whitelisted users"))
		b.WriteString("\n")
	} else {
		for i, userID := range m.whitelist {
			name := userID
			if u, ok := m.users[userID]; ok {
				name = u.DisplayName
				if u.RealName != "" {
					name = u.RealName + " (" + u.DisplayName + ")"
				}
			}

			prefix := "  "
			if i == m.selected {
				prefix = "> "
			}

			line := prefix + secureStyle.Render("@"+name)
			if i == m.selected {
				b.WriteString(ChannelSelectedStyle.Render(line))
			} else {
				b.WriteString(ChannelItemStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.adding {
		b.WriteString("  Add user: ")
		b.WriteString(m.addInput.View())
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  a: add | d: remove | Esc: close"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(55, m.width-4)).
		MaxHeight(m.height - 4)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
