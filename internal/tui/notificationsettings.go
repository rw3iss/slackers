package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
)

// NotificationSettingsOpenMsg signals that the notification settings overlay should open.
type NotificationSettingsOpenMsg struct{}

// notifSettingsRow is a single toggle row in the notification settings panel.
type notifSettingsRow struct {
	label       string
	description string
	enabled     bool
	key         string // used to map back to NotificationPrefs fields
}

// NotificationSettingsModel is the overlay for per-type notification preferences.
type NotificationSettingsModel struct {
	rows     []notifSettingsRow
	selected int
	cfg      *config.Config
	width    int
	height   int
	message  string
}

// NewNotificationSettingsModel builds the overlay from the current config.
func NewNotificationSettingsModel(cfg *config.Config) NotificationSettingsModel {
	p := cfg.NotifPrefs
	return NotificationSettingsModel{
		cfg: cfg,
		rows: []notifSettingsRow{
			{
				key:         "new_messages",
				label:       "New Messages",
				description: "Unread messages in channels and DMs",
				enabled:     p.NewMessages,
			},
			{
				key:         "reactions",
				label:       "Reactions",
				description: "Emoji reactions added to your messages",
				enabled:     p.Reactions,
			},
			{
				key:         "friend_requests",
				label:       "Friend Requests",
				description: "Incoming P2P friend requests",
				enabled:     p.FriendRequests,
			},
			{
				key:         "file_shares",
				label:       "File Shares",
				description: "P2P file offers from friends",
				enabled:     p.FileShares,
			},
			{
				key:         "audio_calls",
				label:       "Audio Calls",
				description: "Incoming audio call requests from friends",
				enabled:     p.AudioCalls,
			},
		},
	}
}

// SetSize sets the overlay dimensions.
func (m *NotificationSettingsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles keyboard input for the notification settings panel.
func (m NotificationSettingsModel) Update(msg tea.Msg) (NotificationSettingsModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.rows)-1 {
			m.selected++
		}
	case "home":
		m.selected = 0
	case "end":
		m.selected = len(m.rows) - 1
	case "enter", " ", "tab":
		// Toggle the selected row.
		m.rows[m.selected].enabled = !m.rows[m.selected].enabled
		m.applyToConfig()
		cfg := m.cfg
		return m, func() tea.Msg {
			if err := config.Save(cfg); err != nil {
				return ErrMsg{Err: err}
			}
			return SettingsSavedMsg{}
		}
	}
	return m, nil
}

// applyToConfig writes the current toggle states back to NotifPrefs.
func (m *NotificationSettingsModel) applyToConfig() {
	for _, r := range m.rows {
		switch r.key {
		case "new_messages":
			m.cfg.NotifPrefs.NewMessages = r.enabled
		case "reactions":
			m.cfg.NotifPrefs.Reactions = r.enabled
		case "friend_requests":
			m.cfg.NotifPrefs.FriendRequests = r.enabled
		case "file_shares":
			m.cfg.NotifPrefs.FileShares = r.enabled
		case "audio_calls":
			m.cfg.NotifPrefs.AudioCalls = r.enabled
		}
	}
}

// View renders the notification settings overlay.
func (m NotificationSettingsModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPageHeader).
		Underline(true)

	labelStyle := lipgloss.NewStyle().
		Width(22).
		Foreground(ColorMenuItem)

	selectedLabelStyle := lipgloss.NewStyle().
		Width(22).
		Bold(true).
		Foreground(ColorPrimary)

	onStyle := lipgloss.NewStyle().
		Foreground(ColorStatusOn).
		Bold(true)

	offStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Notification Settings"))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render("Toggle each notification type"))
	b.WriteString("\n\n")

	for i, row := range m.rows {
		cursor := "  "
		ls := labelStyle
		if i == m.selected {
			cursor = "> "
			ls = selectedLabelStyle
		}

		b.WriteString(cursor)
		b.WriteString(ls.Render(row.label))

		if row.enabled {
			b.WriteString(onStyle.Render(" [on] "))
			b.WriteString(offStyle.Render("  off "))
		} else {
			b.WriteString(onStyle.Render(""))
			b.WriteString(offStyle.Render("  on  "))
			b.WriteString(offStyle.Render("[off]"))
		}
		b.WriteString("\n")

		if i == m.selected {
			b.WriteString("    ")
			b.WriteString(descStyle.Render(row.description))
			b.WriteString("\n")
		}
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
	}

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Space/Enter: toggle · ↑↓: navigate · Esc: back"))

	boxH := m.height - 5
	if boxH < 8 {
		boxH = 8
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(60, m.width-4)).
		Height(boxH).
		MaxHeight(boxH + 4).
		Render(b.String())

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(ColorOverlayFill),
	)
}
