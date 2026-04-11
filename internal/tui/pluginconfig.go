package tui

// Plugin Config overlay — shows a plugin's info and editable
// settings. Opened from the Plugin Manager (Enter on a plugin)
// or via /plugin config <name>.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/plugins"
)

// PluginConfigOpenMsg opens the plugin config for a specific plugin.
type PluginConfigOpenMsg struct{ Name string }

// PluginConfigCloseMsg closes the config overlay.
type PluginConfigCloseMsg struct{}

// PluginConfigSavedMsg signals that plugin config was saved.
type PluginConfigSavedMsg struct{ Name string }

// PluginConfigModel manages the config screen for one plugin.
type PluginConfigModel struct {
	pluginName string
	info       *plugins.PluginInfo
	fields     []plugins.ConfigField
	plugin     plugins.Plugin
	selected   int
	editing    bool
	input      textinput.Model
	width      int
	height     int
	message    string
}

// NewPluginConfigModel creates a config screen for the named plugin.
func NewPluginConfigModel(name string, info *plugins.PluginInfo, p plugins.Plugin) PluginConfigModel {
	ti := textinput.New()
	ti.CharLimit = 128

	var fields []plugins.ConfigField
	if p != nil {
		fields = p.ConfigFields()
	}

	return PluginConfigModel{
		pluginName: name,
		info:       info,
		fields:     fields,
		plugin:     p,
		input:      ti,
	}
}

func (m *PluginConfigModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m PluginConfigModel) Update(msg tea.Msg) (PluginConfigModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		if m.editing {
			switch v.String() {
			case "enter":
				val := strings.TrimSpace(m.input.Value())
				if m.selected < len(m.fields) && m.plugin != nil {
					m.plugin.SetConfig(m.fields[m.selected].Key, val)
					m.fields[m.selected].Value = val
					m.message = "Saved: " + m.fields[m.selected].Label
				}
				m.editing = false
				m.input.Blur()
			case "esc":
				m.editing = false
				m.input.Blur()
			default:
				m.input, _ = m.input.Update(msg)
			}
			return m, nil
		}

		// Total items: fields + back button.
		maxSel := len(m.fields)

		switch v.String() {
		case "esc":
			return m, func() tea.Msg { return PluginConfigCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < maxSel {
				m.selected++
			}
		case "enter":
			if m.selected < len(m.fields) {
				f := m.fields[m.selected]
				m.editing = true
				m.input.SetValue(f.Value)
				m.input.Focus()
				m.input.CursorEnd()
			} else {
				// Back button.
				return m, func() tea.Msg { return PluginConfigCloseMsg{} }
			}
		}
	}
	return m, nil
}

func (m PluginConfigModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	labelStyle := lipgloss.NewStyle().Width(16).Foreground(ColorDescText)
	selLabelStyle := lipgloss.NewStyle().Width(16).Bold(true).Foreground(ColorPrimary)
	valueStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	descStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	selItemStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Plugin: " + m.pluginName))
	b.WriteString("\n\n")

	// Plugin info.
	if m.info != nil {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  Version: %s  Author: %s  Status: %s",
			m.info.Version, m.info.Author, m.info.State)))
		b.WriteString("\n")
		if m.info.Description != "" {
			b.WriteString(dimStyle.Render("  " + m.info.Description))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(m.fields) == 0 {
		b.WriteString(dimStyle.Render("  No configurable settings for this plugin."))
		b.WriteString("\n\n")
	} else {
		b.WriteString(titleStyle.Render("  Settings"))
		b.WriteString("\n\n")

		for i, f := range m.fields {
			cursor := "  "
			ls := labelStyle
			if i == m.selected {
				cursor = "> "
				ls = selLabelStyle
			}
			b.WriteString(cursor)
			b.WriteString(ls.Render(f.Label))

			if m.editing && i == m.selected {
				b.WriteString(m.input.View())
			} else {
				val := f.Value
				if val == "" {
					val = "(not set)"
				}
				b.WriteString(valueStyle.Render(val))
			}
			b.WriteString("\n")
			if i == m.selected {
				b.WriteString("    " + descStyle.Render(f.Description) + "\n")
			}
			b.WriteString("\n")
		}
	}

	// Back button.
	if m.selected == len(m.fields) {
		b.WriteString(selItemStyle.Render("> [ Back ]"))
	} else {
		b.WriteString(itemStyle.Render("  [ Back ]"))
	}
	b.WriteString("\n")

	if m.message != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorHighlight).Render("  "+m.message))
	}

	footer := "↑↓: navigate" + HintSep + "Enter: edit/save" + HintSep + FooterHintBack
	scaffold := OverlayScaffold{
		Title:       "Plugin Config — " + m.pluginName,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 70,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
