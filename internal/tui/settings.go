package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
)

// SettingsSavedMsg is sent when settings have been persisted to disk.
type SettingsSavedMsg struct{}

// SettingsModel provides an interactive config editor overlay.
type SettingsModel struct {
	fields   []settingsField
	selected int
	editing  bool
	input    textinput.Model
	cfg      *config.Config
	width    int
	height   int
	message  string
}

type settingsField struct {
	label       string
	key         string
	value       string
	description string
}

// NewSettingsModel creates a settings editor from the current config.
func NewSettingsModel(cfg *config.Config) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 64

	return SettingsModel{
		fields: []settingsField{
			{
				label:       "Sidebar Width",
				key:         "sidebar_width",
				value:       strconv.Itoa(cfg.SidebarWidth),
				description: "Width of the channel sidebar in characters (10-80)",
			},
			{
				label:       "Timestamp Format",
				key:         "timestamp_format",
				value:       cfg.TimestampFormat,
				description: "Go time format for message timestamps (e.g. 15:04, 3:04 PM)",
			},
			{
				label:       "Bot Token",
				key:         "bot_token",
				value:       maskToken(cfg.BotToken),
				description: "Slack bot token (xoxb-...)",
			},
			{
				label:       "App Token",
				key:         "app_token",
				value:       maskToken(cfg.AppToken),
				description: "Slack app-level token (xapp-...)",
			},
			{
				label:       "User Token",
				key:         "user_token",
				value:       maskToken(cfg.UserToken),
				description: "Slack user token (xoxp-...) for sending as yourself",
			},
		},
		cfg:   cfg,
		input: ti,
	}
}

func maskToken(t string) string {
	if t == "" {
		return "(not set)"
	}
	if len(t) <= 12 {
		return t[:4] + "..."
	}
	return t[:12] + "..."
}

// SetSize sets the overlay dimensions.
func (m *SettingsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the settings overlay.
func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		return m.updateNavigating(msg)
	}
	return m, nil
}

func (m SettingsModel) updateNavigating(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.fields)-1 {
			m.selected++
		}
	case "enter":
		f := m.fields[m.selected]
		// Don't allow editing masked token fields inline
		if f.key == "bot_token" || f.key == "app_token" || f.key == "user_token" {
			m.message = "Run 'slackers setup' to change tokens"
			return m, nil
		}
		m.editing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
		m.message = ""
	}
	return m, nil
}

func (m SettingsModel) updateEditing(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		newVal := m.input.Value()
		m.fields[m.selected].value = newVal
		m.editing = false
		m.input.Blur()

		// Apply to config
		cmd := m.applyField(m.fields[m.selected].key, newVal)
		return m, cmd

	case "esc":
		m.editing = false
		m.input.Blur()
		m.message = ""
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *SettingsModel) applyField(key, value string) tea.Cmd {
	switch key {
	case "sidebar_width":
		n, err := strconv.Atoi(value)
		if err != nil || n < 10 || n > 80 {
			m.message = "Sidebar width must be 10-80"
			m.fields[m.selected].value = strconv.Itoa(m.cfg.SidebarWidth)
			return nil
		}
		m.cfg.SidebarWidth = n
		m.message = "Sidebar width updated"

	case "timestamp_format":
		m.cfg.TimestampFormat = value
		m.message = "Timestamp format updated"
	}

	// Save to disk
	cfg := m.cfg
	return func() tea.Msg {
		if err := config.Save(cfg); err != nil {
			return ErrMsg{Err: err}
		}
		return SettingsSavedMsg{}
	}
}

// Config returns the current config.
func (m *SettingsModel) Config() *config.Config {
	return m.cfg
}

// View renders the settings overlay.
func (m SettingsModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Width(20).
		Foreground(lipgloss.Color("252"))

	valueStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	selectedLabelStyle := lipgloss.NewStyle().
		Width(20).
		Bold(true).
		Foreground(ColorPrimary)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Settings"))
	b.WriteString("\n\n")

	for i, f := range m.fields {
		cursor := "  "
		ls := labelStyle
		if i == m.selected {
			cursor = "> "
			ls = selectedLabelStyle
		}

		b.WriteString(cursor)
		b.WriteString(ls.Render(f.label))

		if m.editing && i == m.selected {
			b.WriteString(m.input.View())
		} else {
			b.WriteString(valueStyle.Render(f.value))
		}
		b.WriteString("\n")

		if i == m.selected {
			b.WriteString("    ")
			b.WriteString(descStyle.Render(f.description))
			b.WriteString("\n")
		}
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
	}

	b.WriteString("\n\n")
	if m.editing {
		b.WriteString(dimStyle.Render("  Enter: save | Esc: cancel"))
	} else {
		b.WriteString(dimStyle.Render("  Enter: edit | Esc/Ctrl-S: close"))
	}

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(65, m.width-4)).
		MaxHeight(m.height - 4)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// settingsFieldCount returns the number of editable fields.
func settingsFieldCount() int {
	return 5
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
