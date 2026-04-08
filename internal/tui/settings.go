package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/backup"
	"github.com/rw3iss/slackers/internal/config"
)

// SettingsSavedMsg is sent when settings have been persisted to disk.
type SettingsSavedMsg struct{}

// SettingsOpenFileBrowserMsg requests the model to open a file browser for settings.
type SettingsOpenFileBrowserMsg struct{ CurrentPath string }

// SettingsModel provides an interactive config editor overlay.
type SettingsModel struct {
	fields       []settingsField
	selected     int
	scrollOffset int
	editing      bool
	input        textinput.Model
	cfg          *config.Config
	width        int
	height       int
	message      string
	version      string
}

type settingsField struct {
	label       string
	key         string
	value       string
	description string
	options     []string // if non-empty, Enter cycles through these instead of opening text input
	isHeader    bool     // if true, this row is a non-selectable group separator
}

// header returns a non-selectable group separator field with the given label.
func header(label string) settingsField {
	return settingsField{label: label, isHeader: true}
}

// nextSelectable returns the next selectable (non-header) field index from
// `from`, walking in direction `dir` (+1 or -1). Returns `from` if there
// are no other selectable rows. Wraps around at the ends.
func (m SettingsModel) nextSelectable(from, dir int) int {
	n := len(m.fields)
	if n == 0 {
		return 0
	}
	idx := from
	for i := 0; i < n; i++ {
		idx += dir
		if idx < 0 {
			idx = n - 1
		}
		if idx >= n {
			idx = 0
		}
		if !m.fields[idx].isHeader {
			return idx
		}
	}
	return from
}

// firstSelectable returns the first non-header field index, or 0.
func (m SettingsModel) firstSelectable() int {
	for i, f := range m.fields {
		if !f.isHeader {
			return i
		}
	}
	return 0
}

// lastSelectable returns the last non-header field index, or len-1.
func (m SettingsModel) lastSelectable() int {
	for i := len(m.fields) - 1; i >= 0; i-- {
		if !m.fields[i].isHeader {
			return i
		}
	}
	return len(m.fields) - 1
}

// NewSettingsModel creates a settings editor from the current config.
func NewSettingsModel(cfg *config.Config, version string) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 64

	return SettingsModel{
		fields: []settingsField{
			// ───── Appearance ─────
			header("Appearance"),
			{
				label:       "Theme",
				key:         "theme",
				value:       themeValueLabel(cfg.Theme),
				description: "Choose a color theme (live preview)",
			},
			{
				label:       "Alt Theme",
				key:         "alt_theme",
				value:       themeValueLabel(cfg.AltTheme),
				description: "Secondary theme — toggle with Ctrl-Y at any time",
			},
			{
				label:       "Sidebar Width",
				key:         "sidebar_width",
				value:       strconv.Itoa(cfg.SidebarWidth),
				description: "Width of the channel sidebar in characters (10-80)",
			},
			{
				label:       "Sidebar Item Spacing",
				key:         "sidebar_item_spacing",
				value:       strconv.Itoa(cfg.SidebarItemSpacing),
				description: "Empty lines between sidebar items (0-2)",
				options:     []string{"0", "1", "2"},
			},
			{
				label:       "Message Item Spacing",
				key:         "message_item_spacing",
				value:       strconv.Itoa(cfg.MessageItemSpacing),
				description: "Vertical spacing between chat messages (0=compact, 1=relaxed, 2=comfortable)",
				options:     []string{"0", "1", "2"},
			},
			{
				label:       "Timestamp Format",
				key:         "timestamp_format",
				value:       cfg.TimestampFormat,
				description: "Go time format for message timestamps (e.g. 15:04, 3:04 PM)",
			},
			{
				label:       "Reply Format",
				key:         "reply_format",
				value:       replyFormatVal(cfg.ReplyFormat),
				description: "How replies appear in chat (inline = nested below, inside = enter to view)",
				options:     []string{"inline", "inside"},
			},

			// ───── Behavior ─────
			header("Behavior"),
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
				label:       "Input History",
				key:         "input_history_max",
				value:       strconv.Itoa(inputHistMax(cfg.InputHistoryMax)),
				description: "Max sent messages to remember (1-200)",
			},
			{
				label:       "Notification Timeout",
				key:         "notification_timeout",
				value:       strconv.Itoa(notificationTimeoutValue(cfg.NotificationTimeout)),
				description: "Seconds before status, warning, and notification messages auto-clear (default 3)",
			},

			// ───── Channels ─────
			header("Channels"),
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
				label:       "Poll Interval",
				key:         "poll_interval",
				value:       strconv.Itoa(cfg.PollInterval),
				description: "Seconds between current-channel polls (1-300)",
			},
			{
				label:       "Bg Poll Interval",
				key:         "poll_interval_bg",
				value:       strconv.Itoa(bgPollVal(cfg.PollIntervalBg)),
				description: "Seconds between background channel checks (5-600, default 30)",
			},
			{
				label:       "Priority Channels",
				key:         "poll_priority",
				value:       strconv.Itoa(pollPriorityVal(cfg.PollPriority)),
				description: "Extra channels polled when socket is disconnected (0-10)",
			},

			// ───── Files ─────
			header("Files"),
			{
				label:       "Download Path",
				key:         "download_path",
				value:       downloadPathValue(cfg.DownloadPath),
				description: "File download location (Enter to browse)",
			},

			// ───── Friends & Secure Messaging ─────
			header("Friends & Secure Messaging"),
			{
				label:       "Friends Config",
				key:         "friends_config",
				value:       "Manage...",
				description: "Manage friends, profile, and P2P connections",
			},
			{
				label:       "Secure Mode",
				key:         "secure_mode",
				value:       boolToOnOff(cfg.SecureMode),
				description: "E2E encrypted P2P messaging with whitelisted peers (restart required)",
				options:     []string{"on", "off"},
			},
			{
				label:       "P2P Port",
				key:         "p2p_port",
				value:       strconv.Itoa(p2pPortVal(cfg.P2PPort)),
				description: "Local port for P2P connections (default 9900)",
			},
			{
				label:       "Secure Whitelist",
				key:         "whitelist",
				value:       "Manage...",
				description: "Manage users allowed for E2E encrypted messaging",
			},

			// ───── Customization ─────
			header("Customization"),
			{
				label:       "Keyboard Shortcuts",
				key:         "shortcuts",
				value:       "Customize...",
				description: "View and edit all keyboard shortcuts",
			},

			// ───── Account ─────
			header("Account"),
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

			// ───── Backup ─────
			header("Backup"),
			{
				label:       "Export Settings",
				key:         "export_settings",
				value:       "Export...",
				description: "Save all settings, themes, friends and history to a zip in ~/Downloads",
			},
			{
				label:       "Import Settings",
				key:         "import_settings",
				value:       "Import...",
				description: "Restore from a slackers export archive",
			},

			// ───── Info ─────
			header("Info"),
			{
				label:       "About",
				key:         "about",
				value:       "View...",
				description: "Show version, credits, and project links",
			},
		},
		cfg:     cfg,
		input:   ti,
		version: version,
	}.withInitialSelection()
}

// withInitialSelection moves the cursor to the first non-header field.
func (m SettingsModel) withInitialSelection() SettingsModel {
	m.selected = m.firstSelectable()
	return m
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

func p2pPortVal(n int) int {
	if n <= 0 {
		return 9900
	}
	return n
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

// notificationTimeoutValue returns the configured notification timeout
// or the global default (3 seconds) when unset / invalid.
func notificationTimeoutValue(n int) int {
	if n <= 0 {
		return 3
	}
	return n
}

func pollPriorityVal(n int) int {
	if n <= 0 {
		return 3
	}
	return n
}

func bgPollVal(n int) int {
	if n < 5 {
		return 30
	}
	return n
}

func replyFormatVal(s string) string {
	if s == "" {
		return "inline"
	}
	return s
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

func themeValueLabel(name string) string {
	if name == "" {
		return "Default"
	}
	return name
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

// Update handles key and mouse events in the settings overlay.
func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		return m.updateNavigating(msg)
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.selected = m.nextSelectable(m.selected, -1)
		case tea.MouseButtonWheelDown:
			m.selected = m.nextSelectable(m.selected, +1)
		}
	}
	return m, nil
}

func (m SettingsModel) updateNavigating(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.selected = m.nextSelectable(m.selected, -1)
	case "down", "j":
		m.selected = m.nextSelectable(m.selected, +1)
	case "pgup":
		for i := 0; i < 5; i++ {
			m.selected = m.nextSelectable(m.selected, -1)
		}
	case "pgdown":
		for i := 0; i < 5; i++ {
			m.selected = m.nextSelectable(m.selected, +1)
		}
	case "home":
		m.selected = m.firstSelectable()
	case "end":
		m.selected = m.lastSelectable()
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

		// Whitelist opens the whitelist manager.
		if f.key == "whitelist" {
			return m, func() tea.Msg {
				return WhitelistOpenMsg{}
			}
		}

		// Friends config opens the friends manager.
		if f.key == "friends_config" {
			return m, func() tea.Msg {
				return FriendsConfigOpenMsg{}
			}
		}

		// About opens the about overlay.
		if f.key == "about" {
			return m, func() tea.Msg {
				return AboutOpenMsg{}
			}
		}

		// Theme opens the theme picker.
		if f.key == "theme" {
			return m, func() tea.Msg {
				return ThemePickerOpenMsg{}
			}
		}

		// Alt Theme opens the same picker but in "alt" mode.
		if f.key == "alt_theme" {
			return m, func() tea.Msg {
				return ThemePickerOpenMsg{ForAlt: true}
			}
		}

		// Export settings to a zip in ~/Downloads.
		if f.key == "export_settings" {
			path, err := backup.Export(filepath.Join(backup.DefaultExportDir(), backup.DefaultExportName()))
			if err != nil {
				m.message = "Export failed: " + err.Error()
			} else {
				m.message = "Exported to " + path
			}
			return m, nil
		}

		// Import settings — handled through a confirmation prompt overlay.
		if f.key == "import_settings" {
			m.message = "Run 'slackers import <archive.zip>' from the command line"
			return m, nil
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

	case "sidebar_item_spacing":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 2 {
			m.message = "Sidebar item spacing must be 0-2"
			m.fields[m.selected].value = strconv.Itoa(m.cfg.SidebarItemSpacing)
			return nil
		}
		m.cfg.SidebarItemSpacing = n
		m.message = "Sidebar item spacing updated"

	case "message_item_spacing":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 2 {
			m.message = "Message item spacing must be 0-2"
			m.fields[m.selected].value = strconv.Itoa(m.cfg.MessageItemSpacing)
			return nil
		}
		m.cfg.MessageItemSpacing = n
		m.message = "Message item spacing updated"

	case "timestamp_format":
		m.cfg.TimestampFormat = value
		m.message = "Timestamp format updated"

	case "secure_mode":
		v := strings.ToLower(strings.TrimSpace(value))
		m.cfg.SecureMode = (v == "on")
		if m.cfg.SecureMode {
			m.message = "Secure mode enabled (restart to activate)"
		} else {
			m.message = "Secure mode disabled (restart to deactivate)"
		}

	case "p2p_port":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1024 || n > 65535 {
			m.message = "Port must be 1024-65535"
			m.fields[m.selected].value = strconv.Itoa(p2pPortVal(m.cfg.P2PPort))
			return nil
		}
		m.cfg.P2PPort = n
		m.message = fmt.Sprintf("P2P port: %d (restart to apply)", n)

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

	case "notification_timeout":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			m.message = "Must be a positive number of seconds"
			m.fields[m.selected].value = strconv.Itoa(notificationTimeoutValue(m.cfg.NotificationTimeout))
			return nil
		}
		m.cfg.NotificationTimeout = n
		m.message = fmt.Sprintf("Notifications clear after %ds", n)

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

	case "poll_interval_bg":
		n, err := strconv.Atoi(value)
		if err != nil || n < 5 || n > 600 {
			m.message = "Background poll must be 5-600 seconds"
			m.fields[m.selected].value = strconv.Itoa(bgPollVal(m.cfg.PollIntervalBg))
			return nil
		}
		m.cfg.PollIntervalBg = n
		m.message = fmt.Sprintf("Background poll interval: %ds", n)

	case "reply_format":
		m.cfg.ReplyFormat = value
		m.message = "Reply format: " + value

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
		Width(24).
		Foreground(ColorMenuItem)

	valueStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	selectedLabelStyle := lipgloss.NewStyle().
		Width(24).
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
	contentWidth := min(85, m.width-4) - 8
	// Version aligns with value column: cursor(2) + label(20) = 22 chars from left
	verText := "slackers v" + m.version
	verPad := contentWidth - lipgloss.Width(titleStyle.Render("Settings")) - len(verText)
	if verPad < 1 {
		verPad = 1
	}
	// Build the header (title row + blank) into a separate buffer so we can
	// reserve room for it and the footer when computing the field window.
	var headerBuf strings.Builder
	headerBuf.WriteString(titleStyle.Render("Settings") + strings.Repeat(" ", verPad) + verStyle.Render(verText))
	headerBuf.WriteString("\n\n")

	// Height is inner content height; lipgloss adds 4 (border + padding),
	// so the rendered box is m.height - 1 (one row of breathing room at the
	// top). MaxHeight pins it so navigation can't grow the box past this.
	boxHeightCalc := m.height - 5
	if boxHeightCalc < 6 {
		boxHeightCalc = 6
	}
	innerH := boxHeightCalc - 4
	if innerH < 1 {
		innerH = 1
	}
	headerLineCount := strings.Count(headerBuf.String(), "\n")
	// Footer reserves: blank line + footer hint + optional message line.
	footerLineCount := 2
	if m.message != "" {
		footerLineCount += 2
	}
	// Calculate visible field window. Each field is normally 1 line, the
	// selected field takes 2 (includes description), and group headers
	// can add a leading blank line. Reserve 4 extra rows so navigating
	// through the list (which changes which row has the description and
	// how many headers are visible) can never push the body past the
	// inner box height and grow the box.
	available := innerH - headerLineCount - footerLineCount - 4
	if available < 3 {
		available = 3
	}
	visibleFields := available
	if visibleFields > len(m.fields) {
		visibleFields = len(m.fields)
	}

	// Auto-scroll to keep selected in view.
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	}
	if m.selected >= m.scrollOffset+visibleFields {
		m.scrollOffset = m.selected - visibleFields + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	end := m.scrollOffset + visibleFields
	if end > len(m.fields) {
		end = len(m.fields)
	}

	groupHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPageHeader).
		Underline(true)

	// Build the field list into its own buffer so we can clip just the body
	// without losing the footer.
	bodyBuf := &b
	bodyBuf.Reset()

	for i := m.scrollOffset; i < end; i++ {
		f := m.fields[i]

		// Group separator: blank line above (skip for the first visible row),
		// then a bold underlined label, no value, no description.
		if f.isHeader {
			if i > m.scrollOffset {
				b.WriteString("\n")
			}
			b.WriteString("  ")
			b.WriteString(groupHeaderStyle.Render(f.label))
			b.WriteString("\n")
			continue
		}

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

	// bodyBuf currently holds the rendered field list. Append the footer
	// directly: blank line + optional message + blank line + hint.
	if m.message != "" {
		bodyBuf.WriteString("\n")
		bodyBuf.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
	}
	bodyBuf.WriteString("\n\n")
	if m.editing {
		bodyBuf.WriteString(dimStyle.Render("  Enter: save | Esc: cancel"))
	} else {
		f := m.fields[m.selected]
		if len(f.options) > 0 {
			bodyBuf.WriteString(dimStyle.Render("  Enter/Tab: cycle | Esc/Ctrl-S: close"))
		} else {
			bodyBuf.WriteString(dimStyle.Render("  Enter: edit | Esc/Ctrl-S: close"))
		}
	}

	// Final content = header + body. Lipgloss pads to Height for us.
	content := headerBuf.String() + bodyBuf.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(85, m.width-4)).
		Height(boxHeightCalc).
		MaxHeight(boxHeightCalc + 4)

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
