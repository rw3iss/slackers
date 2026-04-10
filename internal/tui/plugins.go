package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/plugins"
)

// PluginsOpenMsg opens the plugins management overlay.
type PluginsOpenMsg struct{}

// PluginsCloseMsg closes the plugins overlay.
type PluginsCloseMsg struct{}

// PluginToggleMsg toggles a plugin's enabled/disabled state.
type PluginToggleMsg struct{ Name string }

// PluginUninstallMsg requests uninstalling a plugin.
type PluginUninstallMsg struct{ Name string }

// PluginsModel is the overlay state.
type PluginsModel struct {
	plugins          []plugins.PluginInfo
	selected         int
	width            int
	height           int
	message          string // status message
	confirmUninstall string // plugin name pending confirmation
}

// NewPluginsModel creates the overlay with the current plugin list.
func NewPluginsModel(list []plugins.PluginInfo) PluginsModel {
	return PluginsModel{
		plugins: list,
	}
}

func (m *PluginsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m PluginsModel) Update(msg tea.Msg) (PluginsModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Uninstall confirmation.
		if m.confirmUninstall != "" {
			switch v.String() {
			case "y", "Y", "enter":
				name := m.confirmUninstall
				m.confirmUninstall = ""
				return m, func() tea.Msg { return PluginUninstallMsg{Name: name} }
			default:
				m.confirmUninstall = ""
				m.message = "Uninstall cancelled"
				return m, nil
			}
		}

		switch v.String() {
		case "esc":
			return m, func() tea.Msg { return PluginsCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.plugins)-1 {
				m.selected++
			}
		case "e", "enter":
			// Toggle enable/disable.
			if m.selected >= 0 && m.selected < len(m.plugins) {
				name := m.plugins[m.selected].Name
				return m, func() tea.Msg { return PluginToggleMsg{Name: name} }
			}
		case "d":
			// Uninstall — prompt confirmation.
			if m.selected >= 0 && m.selected < len(m.plugins) {
				m.confirmUninstall = m.plugins[m.selected].Name
				m.message = fmt.Sprintf("Uninstall %s? y/Enter=confirm, any key=cancel", m.confirmUninstall)
			}
		}
	}
	return m, nil
}

// Refresh updates the plugin list (after enable/disable/uninstall).
func (m *PluginsModel) Refresh(list []plugins.PluginInfo) {
	m.plugins = list
	if m.selected >= len(m.plugins) {
		m.selected = len(m.plugins) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m PluginsModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	enabledStyle := lipgloss.NewStyle().Foreground(ColorStatusOn)
	disabledStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	descStyle := lipgloss.NewStyle().Foreground(ColorDescText)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Plugins"))
	b.WriteString("\n\n")

	if len(m.plugins) == 0 {
		b.WriteString(dimStyle.Render("  No plugins installed."))
		b.WriteString("\n")
	} else {
		// Table header.
		header := fmt.Sprintf("  %-20s %-10s %-15s %s", "Name", "Version", "Author", "Status")
		b.WriteString(dimStyle.Render(header))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  " + strings.Repeat("─", 65)))
		b.WriteString("\n")

		for i, p := range m.plugins {
			cursor := "  "
			if i == m.selected {
				cursor = selStyle.Render("> ")
			}

			name := p.Name
			if len(name) > 20 {
				name = name[:17] + "…"
			}
			version := p.Version
			author := p.Author
			if len(author) > 15 {
				author = author[:12] + "…"
			}

			var statusStr string
			if p.State == plugins.StateDisabled {
				statusStr = disabledStyle.Render("disabled")
			} else if p.State == plugins.StateRunning {
				statusStr = enabledStyle.Render("running")
			} else {
				statusStr = enabledStyle.Render("enabled")
			}

			row := fmt.Sprintf("%-20s %-10s %-15s ", name, version, author)
			if i == m.selected {
				row = selStyle.Render(row)
			}
			b.WriteString(cursor + row + statusStr + "\n")

			// Show description for selected plugin.
			if i == m.selected && p.Description != "" {
				b.WriteString("    " + descStyle.Render(p.Description) + "\n")
			}
		}
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}

	footer := "↑↓: navigate" + HintSep + "e/Enter: enable/disable" + HintSep + "d: uninstall" + HintSep + FooterHintClose
	scaffold := OverlayScaffold{
		Title:       "Plugin Manager",
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 80,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
