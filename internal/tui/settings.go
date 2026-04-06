package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
)

// SettingsSavedMsg is sent when settings have been persisted to disk.
type SettingsSavedMsg struct{}

// SettingsOpenFileBrowserMsg requests the model to open a file browser for settings.
type SettingsOpenFileBrowserMsg struct{ CurrentPath string }

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
	version  string
}

type settingsField struct {
	label       string
	key         string
	value       string
	description string
	options     []string // if non-empty, Enter cycles through these instead of opening text input
}

// NewSettingsModel creates a settings editor from the current config.
func NewSettingsModel(cfg *config.Config, version string) SettingsModel {
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
				label:       "Auto Update",
				key:         "auto_update",
				value:       autoUpdateValue(cfg.AutoUpdate),
				description: "Automatically update on startup when a new version is available",
				options:     []string{"on", "off"},
			},
			{
				label:       "Away Timeout",
				key:         "away_timeout",
				value:       awayTimeoutValue(cfg.AwayTimeout),
				description: "Seconds of inactivity before 'away' status (0 = disabled)",
			},
			{
				label:       "Mouse",
				key:         "mouse_enabled",
				value:       boolToOnOff(cfg.MouseEnabled),
				description: "Enable mouse click/scroll (restart required, Shift+click to select text)",
				options:     []string{"on", "off"},
			},
			{
				label:       "Notifications",
				key:         "notifications",
				value:       boolToOnOff(cfg.Notifications),
				description: "Terminal bell and desktop notifications",
				options:     []string{"on", "off"},
			},
			{
				label:       "Poll Interval",
				key:         "poll_interval",
				value:       strconv.Itoa(cfg.PollInterval),
				description: "Seconds between new-message checks (1-300)",
			},
			{
				label:       "Priority Channels",
				key:         "poll_priority",
				value:       strconv.Itoa(pollPriorityVal(cfg.PollPriority)),
				description: "Most-recent channels checked every poll (0-10). Higher = more API usage. Slack limit: ~50 req/min.",
			},
			{
				label:       "Input History",
				key:         "input_history_max",
				value:       strconv.Itoa(inputHistMax(cfg.InputHistoryMax)),
				description: "Max sent messages to remember (1-200)",
			},
			{
				label:       "Download Path",
				key:         "download_path",
				value:       downloadPathValue(cfg.DownloadPath),
				description: "File download location (Enter to browse)",
			},
			{
				label:       "Sort By",
				key:         "channel_sort_by",
				value:       channelSortValue(cfg.ChannelSortBy),
				description: "Channel list sorting mode",
				options:     []string{"type", "name", "recent"},
			},
			{
				label:       "Sort Direction",
				key:         "channel_sort_asc",
				value:       boolToDir(cfg.ChannelSortAsc),
				description: "Channel list sorting direction",
				options:     []string{"asc", "desc"},
			},
			{
				label:       "Keyboard Shortcuts",
				key:         "shortcuts",
				value:       "Customize...",
				description: "View and edit all keyboard shortcuts",
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
		cfg:     cfg,
		input:   ti,
		version: version,
	}
}

func channelSortValue(s string) string {
	if s == "" {
		return "type"
	}
	return s
}

func boolToDir(b *bool) string {
	if b == nil || *b {
		return "asc"
	}
	return "desc"
}

func autoUpdateValue(b *bool) string {
	if b == nil || *b {
		return "on"
	}
	return "off"
}

func awayTimeoutValue(n int) string {
	if n <= 0 {
		return "0"
	}
	return strconv.Itoa(n)
}

func pollPriorityVal(n int) int {
	if n <= 0 {
		return 3
	}
	return n
}

func inputHistMax(n int) int {
	if n <= 0 {
		return 20
	}
	return n
}

func downloadPathValue(p string) string {
	if p == "" {
		home, _ := os.UserHomeDir()
		return home + "/Downloads"
	}
	return p
}

func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
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
	case "enter", "tab":
		f := m.fields[m.selected]

		// Token fields can't be edited inline.
		if f.key == "bot_token" || f.key == "app_token" || f.key == "user_token" {
			m.message = "Run 'slackers setup' to change tokens"
			return m, nil
		}

		// Keyboard shortcuts opens the shortcuts editor.
		if f.key == "shortcuts" {
			return m, func() tea.Msg {
				return ShortcutsEditorOpenMsg{}
			}
		}

		// Download path opens a folder browser.
		if f.key == "download_path" {
			return m, func() tea.Msg {
				return SettingsOpenFileBrowserMsg{CurrentPath: f.value}
			}
		}

		// Fields with options: cycle to next option.
		if len(f.options) > 0 {
			return m.cycleOption()
		}

		// Free-text fields: open text input.
		m.editing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
		m.message = ""
	}
	return m, nil
}

// cycleOption advances the current field to the next option and saves.
func (m SettingsModel) cycleOption() (SettingsModel, tea.Cmd) {
	f := &m.fields[m.selected]
	current := f.value
	nextIdx := 0
	for i, opt := range f.options {
		if opt == current {
			nextIdx = (i + 1) % len(f.options)
			break
		}
	}
	f.value = f.options[nextIdx]
	cmd := m.applyField(f.key, f.value)
	return m, cmd
}

func (m SettingsModel) updateEditing(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		newVal := m.input.Value()
		m.fields[m.selected].value = newVal
		m.editing = false
		m.input.Blur()
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

	case "auto_update":
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "on" {
			b := true
			m.cfg.AutoUpdate = &b
		} else {
			b := false
			m.cfg.AutoUpdate = &b
		}
		m.message = "Auto update: " + value

	case "away_timeout":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			m.message = "Must be 0 (disabled) or positive seconds"
			m.fields[m.selected].value = awayTimeoutValue(m.cfg.AwayTimeout)
			return nil
		}
		m.cfg.AwayTimeout = n
		if n == 0 {
			m.message = "Away detection disabled"
		} else {
			m.message = fmt.Sprintf("Away after %ds of inactivity", n)
		}

	case "mouse_enabled":
		v := strings.ToLower(strings.TrimSpace(value))
		m.cfg.MouseEnabled = (v == "on")
		if m.cfg.MouseEnabled {
			m.message = "Mouse enabled (restart to apply)"
		} else {
			m.message = "Mouse disabled (restart to apply)"
		}

	case "notifications":
		v := strings.ToLower(strings.TrimSpace(value))
		m.cfg.Notifications = (v == "on")
		if m.cfg.Notifications {
			m.message = "Notifications enabled"
		} else {
			m.message = "Notifications disabled"
		}

	case "input_history_max":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 200 {
			m.message = "Must be 1-200"
			m.fields[m.selected].value = strconv.Itoa(inputHistMax(m.cfg.InputHistoryMax))
			return nil
		}
		m.cfg.InputHistoryMax = n
		m.message = "History size updated"

	case "poll_priority":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 10 {
			m.message = "Must be 0-10"
			m.fields[m.selected].value = strconv.Itoa(pollPriorityVal(m.cfg.PollPriority))
			return nil
		}
		m.cfg.PollPriority = n
		m.message = fmt.Sprintf("Priority channels: %d", n)

	case "poll_interval":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 300 {
			m.message = "Poll interval must be 1-300 seconds"
			m.fields[m.selected].value = strconv.Itoa(m.cfg.PollInterval)
			return nil
		}
		m.cfg.PollInterval = n
		m.message = "Poll interval updated"

	case "channel_sort_by":
		m.cfg.ChannelSortBy = value
		m.message = "Sort: " + value

	case "channel_sort_asc":
		if value == "asc" {
			b := true
			m.cfg.ChannelSortAsc = &b
		} else {
			b := false
			m.cfg.ChannelSortAsc = &b
		}
		m.message = "Direction: " + value
	}

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

	optionActiveStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	optionInactiveStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	var b strings.Builder

	verStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	// Content width = box width - border(2) - padding(6)
	contentWidth := min(65, m.width-4) - 8
	// Version aligns with value column: cursor(2) + label(20) = 22 chars from left
	verText := "slackers v" + m.version
	verPad := contentWidth - lipgloss.Width(titleStyle.Render("Settings")) - len(verText)
	if verPad < 1 {
		verPad = 1
	}
	b.WriteString(titleStyle.Render("Settings") + strings.Repeat(" ", verPad) + verStyle.Render(verText))
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
		} else if len(f.options) > 0 && i == m.selected {
			// Show all options with the active one highlighted.
			for j, opt := range f.options {
				if j > 0 {
					b.WriteString("  ")
				}
				if opt == f.value {
					b.WriteString(optionActiveStyle.Render("[" + opt + "]"))
				} else {
					b.WriteString(optionInactiveStyle.Render(" " + opt + " "))
				}
			}
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
		f := m.fields[m.selected]
		if len(f.options) > 0 {
			b.WriteString(dimStyle.Render("  Enter/Tab: cycle | Esc/Ctrl-S: close"))
		} else {
			b.WriteString(dimStyle.Render("  Enter: edit | Esc/Ctrl-S: close"))
		}
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
