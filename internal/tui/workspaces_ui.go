package tui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/setup"
	"github.com/rw3iss/slackers/internal/workspace"
)

// =====================================================================
// Messages
// =====================================================================

// WorkspacesOpenMsg requests opening the workspaces list overlay.
type WorkspacesOpenMsg struct{}

// WorkspacesCloseMsg signals the workspaces list should close.
type WorkspacesCloseMsg struct{}

// WorkspaceEditOpenMsg opens the edit overlay for a workspace.
// An empty TeamID means "add new workspace".
type WorkspaceEditOpenMsg struct{ TeamID string }

// WorkspaceEditCloseMsg dismisses the workspace editor.
type WorkspaceEditCloseMsg struct{}

// WorkspaceEditSavedMsg fires after the editor saves a workspace.
type WorkspaceEditSavedMsg struct{ TeamID string }

// =====================================================================
// Workspaces List
// =====================================================================

// WorkspacesModel lists all workspaces and lets the user switch, sign
// in/out, add, edit, or delete them.
type WorkspacesModel struct {
	workspaces    []*workspace.Workspace
	activeID      string
	selected      int
	width, height int
	message       string
	confirmDel    bool
}

// NewWorkspacesModel constructs the list from the workspace map, sorted
// alphabetically by display name, with the active workspace's index
// pre-selected.
func NewWorkspacesModel(wsMap map[string]*workspace.Workspace, activeID string) WorkspacesModel {
	list := make([]*workspace.Workspace, 0, len(wsMap))
	for _, ws := range wsMap {
		list = append(list, ws)
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].DisplayName()) < strings.ToLower(list[j].DisplayName())
	})

	sel := 0
	for i, ws := range list {
		if ws.ID() == activeID {
			sel = i
			break
		}
	}

	return WorkspacesModel{
		workspaces: list,
		activeID:   activeID,
		selected:   sel,
	}
}

// SetSize stores the terminal dimensions for centering.
func (m *WorkspacesModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles input for the workspaces list overlay.
func (m WorkspacesModel) Update(msg tea.Msg) (WorkspacesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Delete confirmation prompt eats the next key.
		if m.confirmDel {
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				if m.selected >= 0 && m.selected < len(m.workspaces) {
					ws := m.workspaces[m.selected]
					teamID := ws.ID()
					m.confirmDel = false
					return m, func() tea.Msg { return WorkspaceRemovedMsg{TeamID: teamID} }
				}
			default:
				m.message = "Delete cancelled"
			}
			m.confirmDel = false
			return m, nil
		}

		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return WorkspacesCloseMsg{} }

		case "up", "k":
			if len(m.workspaces) > 0 {
				m.selected--
				if m.selected < 0 {
					m.selected = len(m.workspaces) - 1
				}
			}

		case "down", "j":
			if len(m.workspaces) > 0 {
				m.selected++
				if m.selected >= len(m.workspaces) {
					m.selected = 0
				}
			}

		case "enter":
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				if ws.SignedIn {
					teamID := ws.ID()
					return m, tea.Sequence(
						func() tea.Msg { return WorkspaceSwitchMsg{TeamID: teamID} },
						func() tea.Msg { return WorkspacesCloseMsg{} },
					)
				}
				// Signed-out workspace — sign it in and close.
				return m, tea.Sequence(
					signInWorkspaceCmd(ws),
					func() tea.Msg { return WorkspacesCloseMsg{} },
				)
			}

		case "a":
			// Add new workspace.
			return m, func() tea.Msg { return WorkspaceEditOpenMsg{} }

		case "e":
			// Edit selected workspace.
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				teamID := m.workspaces[m.selected].ID()
				return m, func() tea.Msg { return WorkspaceEditOpenMsg{TeamID: teamID} }
			}

		case "s":
			// Toggle sign-in / sign-out.
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				if ws.SignedIn {
					teamID := ws.ID()
					m.message = fmt.Sprintf("Signed out of %s", ws.DisplayName())
					return m, func() tea.Msg { return WorkspaceSignOutMsg{TeamID: teamID} }
				}
				m.message = fmt.Sprintf("Signing in to %s…", ws.DisplayName())
				return m, signInWorkspaceCmd(ws)
			}

		case "d", "delete":
			// Delete workspace (with confirmation).
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				m.confirmDel = true
				m.message = fmt.Sprintf("Delete %q? (y/N)", ws.DisplayName())
			}
		}
	}
	return m, nil
}

// View renders the workspaces list as a centered modal.
func (m WorkspacesModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	onStyle := lipgloss.NewStyle().Foreground(ColorFriendOnline)
	offStyle := lipgloss.NewStyle().Foreground(ColorFriendOffline)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Workspaces"))
	b.WriteString("\n\n")

	if len(m.workspaces) == 0 {
		b.WriteString(dimStyle.Render("  (no workspaces configured)"))
		b.WriteString("\n")
	}

	for i, ws := range m.workspaces {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}

		// Online/offline bullet.
		var bullet string
		if ws.SignedIn {
			bullet = onStyle.Render("●")
		} else {
			bullet = offStyle.Render("○")
		}

		// Active workspace star.
		star := ""
		if ws.ID() == m.activeID {
			star = titleStyle.Render(" ★")
		}

		// Status suffix.
		var suffix string
		if !ws.SignedIn {
			suffix = muteStyle.Render("  signed out")
		} else if ws.UnreadCount > 0 {
			suffix = muteStyle.Render(fmt.Sprintf("  (%d unread)", ws.UnreadCount))
		}

		name := ws.DisplayName()
		var line string
		if i == m.selected {
			line = selStyle.Render(marker) + bullet + " " + selStyle.Render(name) + star + suffix
		} else {
			line = textStyle.Render(marker) + bullet + " " + textStyle.Render(name) + star + suffix
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}

	b.WriteString(dimStyle.Render("  ↑/↓: select" + HintSep + "Enter: switch" + HintSep + "a: add" + HintSep + "e: edit" + HintSep + "s: sign in/out" + HintSep + "d: delete" + HintSep + FooterHintClose))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// =====================================================================
// Workspace Editor
// =====================================================================

// wsEditField describes one editable row in the workspace editor.
type wsEditField struct {
	label       string
	key         string
	value       string
	description string
	readOnly    bool
	options     []string // cycle options (e.g. on/off)
	isAction    bool     // action row (copy hash, copy JSON)
}

// WorkspaceEditModel lets the user edit a workspace's settings or
// create a new workspace by pasting a setup hash / entering tokens.
type WorkspaceEditModel struct {
	configDir     string
	workspace     *workspace.Workspace
	isNew         bool
	fields        []wsEditField
	selected      int
	editing       bool
	input         textinput.Model
	guard         DirtyGuard
	status        TimedMessage
	width, height int
}

// NewWorkspaceEditModel creates an editor. If ws is nil, the editor
// is in "add new workspace" mode with empty fields.
func NewWorkspaceEditModel(ws *workspace.Workspace, configDir string) WorkspaceEditModel {
	isNew := ws == nil
	if isNew {
		ws = &workspace.Workspace{
			Config: workspace.WorkspaceConfig{
				AutoSignIn: true,
			},
			ChannelMeta: make(map[string]workspace.ChannelMeta),
		}
	}

	ti := textinput.New()
	ti.CharLimit = 256

	fields := []wsEditField{
		{label: "Name", key: "name", value: ws.Config.Name, description: "Display name for this workspace"},
		{label: "Team ID", key: "team_id", value: ws.Config.TeamID, description: "Slack team ID (from OAuth)", readOnly: !isNew},
		{label: "Bot Token", key: "bot_token", value: ws.Config.BotToken, description: "xoxb-... (required)"},
		{label: "App Token", key: "app_token", value: ws.Config.AppToken, description: "xapp-... (required for real-time)"},
		{label: "User Token", key: "user_token", value: ws.Config.UserToken, description: "xoxp-... (optional, for sending as yourself)"},
		{label: "Auto Sign In", key: "auto_sign_in", value: boolOnOff(ws.Config.AutoSignIn), description: "Auto-connect on app startup", options: []string{"on", "off"}},
	}
	if isNew {
		// Prepend a setup-hash import field for quick setup.
		fields = append([]wsEditField{
			{label: "Setup Hash", key: "setup_hash", value: "", description: "Paste a setup hash or JSON to auto-fill tokens"},
		}, fields...)
	}
	// Action rows at the bottom.
	if !isNew {
		fields = append(fields,
			wsEditField{label: "Copy Setup Hash", key: "copy_hash", isAction: true, description: "Copy compact setup hash to clipboard"},
			wsEditField{label: "Copy Setup JSON", key: "copy_json", isAction: true, description: "Copy setup config as JSON"},
		)
	}

	return WorkspaceEditModel{
		configDir: configDir,
		workspace: ws,
		isNew:     isNew,
		fields:    fields,
		input:     ti,
	}
}

func boolOnOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// SetSize stores the terminal dimensions.
func (m *WorkspaceEditModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles input for the workspace editor.
func (m WorkspaceEditModel) Update(msg tea.Msg) (WorkspaceEditModel, tea.Cmd) {
	// Timed message clear.
	if clearMsg, ok := msg.(TimedMessageClearMsg); ok {
		m.status.HandleClear(clearMsg.ID)
		return m, nil
	}

	// Text input editing mode.
	if m.editing {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				val := strings.TrimSpace(m.input.Value())
				f := &m.fields[m.selected]
				if f.key == "setup_hash" && val != "" {
					// Try to import from hash/JSON.
					cmd := m.importSetupHash(val)
					m.editing = false
					return m, cmd
				}
				f.value = val
				m.guard.MarkDirty()
				m.editing = false
			case "esc":
				m.editing = false
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}

	// Dirty guard prompt.
	if m.guard.IsPrompting() {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			result := m.guard.HandlePrompt(keyMsg.String())
			switch result {
			case PromptConfirm:
				return m, func() tea.Msg { return WorkspaceEditCloseMsg{} }
			case PromptCancel:
				m.status.Clear()
				return m, nil
			}
		}
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			if m.guard.Intercept("esc") {
				m.status.SetImmediate(m.guard.PromptText())
				return m, nil
			}
			return m, func() tea.Msg { return WorkspaceEditCloseMsg{} }

		case "up", "k":
			if m.selected > 0 {
				m.selected--
			} else {
				m.selected = len(m.fields) - 1
			}
		case "down", "j":
			if m.selected < len(m.fields)-1 {
				m.selected++
			} else {
				m.selected = 0
			}

		case "enter", " ":
			f := &m.fields[m.selected]
			if f.readOnly {
				m.status.SetImmediate("This field is read-only")
				return m, nil
			}
			// Cycle options (on/off toggle).
			if len(f.options) > 0 {
				cur := 0
				for i, o := range f.options {
					if o == f.value {
						cur = i
						break
					}
				}
				f.value = f.options[(cur+1)%len(f.options)]
				m.guard.MarkDirty()
				return m, nil
			}
			// Action rows.
			if f.isAction {
				return m, m.handleAction(f.key)
			}
			// Open text input.
			m.editing = true
			m.input.SetValue(f.value)
			m.input.Focus()
			m.input.CursorEnd()
			return m, nil

		case "s":
			return m, m.save()
		}
	}
	return m, nil
}

// importSetupHash tries to decode a setup hash or JSON and fill in
// the token fields.
func (m *WorkspaceEditModel) importSetupHash(input string) tea.Cmd {
	cfg, err := setup.ParseAny(input)
	if err != nil {
		m.status.SetImmediate("Invalid setup hash: " + err.Error())
		return nil
	}
	// Fill in token fields from the decoded config.
	for i := range m.fields {
		switch m.fields[i].key {
		case "bot_token":
			// Setup hash doesn't include bot token directly — it has
			// client_id/secret for OAuth. But if a user_token was included, set it.
		case "app_token":
			if cfg.AppToken != "" {
				m.fields[i].value = cfg.AppToken
			}
		case "user_token":
			if cfg.UserToken != "" {
				m.fields[i].value = cfg.UserToken
			}
		}
	}
	// Store client ID/secret on the workspace config for OAuth flow.
	m.workspace.Config.ClientID = cfg.ClientID
	m.workspace.Config.ClientSecret = cfg.ClientSecret
	m.guard.MarkDirty()
	m.status.SetImmediate("Setup hash imported — fill in remaining tokens and save")
	return nil
}

// save persists the workspace config to disk.
func (m *WorkspaceEditModel) save() tea.Cmd {
	// Apply field values back to workspace config.
	for _, f := range m.fields {
		switch f.key {
		case "name":
			m.workspace.Config.Name = f.value
		case "team_id":
			m.workspace.Config.TeamID = f.value
		case "bot_token":
			m.workspace.Config.BotToken = f.value
		case "app_token":
			m.workspace.Config.AppToken = f.value
		case "user_token":
			m.workspace.Config.UserToken = f.value
		case "auto_sign_in":
			m.workspace.Config.AutoSignIn = f.value == "on"
		}
	}

	if m.workspace.Config.TeamID == "" {
		m.status.SetImmediate("Team ID is required")
		return nil
	}

	if err := workspace.Save(m.configDir, m.workspace); err != nil {
		m.status.SetImmediate("Save failed: " + err.Error())
		return nil
	}

	m.guard.MarkClean()
	m.isNew = false
	teamID := m.workspace.Config.TeamID
	cmd := m.status.Set("Saved", 3*time.Second)
	return tea.Batch(cmd, func() tea.Msg {
		return WorkspaceEditSavedMsg{TeamID: teamID}
	})
}

// handleAction handles action row activations (copy hash, copy JSON).
func (m *WorkspaceEditModel) handleAction(key string) tea.Cmd {
	switch key {
	case "copy_hash":
		cfg := setup.Config{
			ClientID:     m.workspace.Config.ClientID,
			ClientSecret: m.workspace.Config.ClientSecret,
			AppToken:     m.workspace.Config.AppToken,
		}
		hash, err := setup.Encode(cfg)
		if err != nil {
			m.status.SetImmediate("Encode failed: " + err.Error())
			return nil
		}
		m.status.SetImmediate("Setup hash copied!")
		return copyToClipboardCmd(hash)
	case "copy_json":
		cfg := setup.Config{
			ClientID:     m.workspace.Config.ClientID,
			ClientSecret: m.workspace.Config.ClientSecret,
			AppToken:     m.workspace.Config.AppToken,
			TeamID:       m.workspace.Config.TeamID,
		}
		data, _ := json.Marshal(cfg)
		m.status.SetImmediate("Setup JSON copied!")
		return copyToClipboardCmd(string(data))
	}
	return nil
}

// View renders the workspace editor as a centered modal.
func (m WorkspaceEditModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	valueStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	var b strings.Builder
	if m.isNew {
		b.WriteString(titleStyle.Render("Add Workspace"))
	} else {
		name := m.workspace.DisplayName()
		b.WriteString(titleStyle.Render("Edit Workspace: " + name))
	}
	b.WriteString("\n\n")

	for i, f := range m.fields {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}

		label := padRight(f.label, 14)
		var line string

		if m.editing && i == m.selected {
			// Show text input.
			if i == m.selected {
				line = selStyle.Render(marker+label) + " " + m.input.View()
			}
		} else if f.isAction {
			// Action row.
			if i == m.selected {
				line = selStyle.Render(marker + f.label)
			} else {
				line = textStyle.Render(marker + f.label)
			}
			line += "  " + muteStyle.Render(f.description)
		} else {
			// Normal field.
			val := f.value
			if val == "" {
				val = "(empty)"
			}
			// Mask tokens.
			if strings.Contains(f.key, "token") && len(val) > 12 {
				val = val[:8] + "..." + val[len(val)-4:]
			}

			labelStr := marker + label
			if i == m.selected {
				labelStr = selStyle.Render(labelStr)
			} else {
				labelStr = textStyle.Render(labelStr)
			}

			valStr := valueStyle.Render(val)
			if f.readOnly {
				valStr = muteStyle.Render(val + " (read-only)")
			}
			line = labelStr + " " + valStr
			if f.description != "" && !f.readOnly {
				line += "  " + muteStyle.Render(f.description)
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.status.Text() != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.status.Text()))
		b.WriteString("\n\n")
	}
	if m.editing {
		b.WriteString(dimStyle.Render("  Enter: accept" + HintSep + FooterHintCancel))
	} else if m.guard.IsPrompting() {
		b.WriteString(dimStyle.Render("  y: discard & leave" + HintSep + "n/Esc: stay"))
	} else {
		b.WriteString(dimStyle.Render("  ↑/↓: navigate" + HintSep + "Enter: edit" + HintSep + "s: save" + HintSep + FooterHintBack))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// =====================================================================
// Clipboard helper
// =====================================================================

// copyToClipboardCmd tries to copy text to the system clipboard using
// xclip, xsel, or wl-copy.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch {
		case commandExists("wl-copy"):
			cmd = exec.Command("wl-copy", text)
		case commandExists("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(text)
		case commandExists("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(text)
		default:
			return nil
		}
		_ = cmd.Run()
		return nil
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
