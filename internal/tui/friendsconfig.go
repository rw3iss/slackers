package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/friends"
)

// FriendsConfigOpenMsg signals that the friends config overlay should open.
type FriendsConfigOpenMsg struct{}

// FriendsConfigCloseMsg signals the friends config overlay should close.
type FriendsConfigCloseMsg struct{}

// Internal page enum for the friends config panel.
type fcPage int

const (
	fcPageMenu fcPage = iota
	fcPageList
	fcPageEditMyInfo
	fcPageShareInfo
	fcPageAddFriend
	fcPageAddFriendJSON
	fcPageExport
	fcPageImport
	fcPageEditFriend
)

// FriendsConfigModel manages the friends configuration overlay.
type FriendsConfigModel struct {
	page         fcPage
	store        *friends.FriendStore
	cfg          *config.Config
	selected     int
	editFriend   *friends.Friend    // friend being edited
	editFields   []settingsField    // reuse settings field pattern
	editSelected int
	editing      bool
	input        textinput.Model
	importPath   string
	importOverwrite bool
	message      string
	jsonInput    textinput.Model    // for paste JSON
	width        int
	height       int

	// My info fields
	myFields   []settingsField
	mySelected int
	myEditing  bool

	// Contact card cache
	contactJSON string
}

var menuItems = []string{
	"Friends List",
	"Edit My Info",
	"Share My Info...",
	"Add a Friend...",
	"Export Friends List",
	"Import Friends List",
}

// NewFriendsConfigModel creates a new friends config overlay.
func NewFriendsConfigModel(store *friends.FriendStore, cfg *config.Config) FriendsConfigModel {
	ti := textinput.New()
	ti.CharLimit = 256

	ji := textinput.New()
	ji.Placeholder = "Paste friend's contact JSON here..."
	ji.CharLimit = 2048

	return FriendsConfigModel{
		page:  fcPageMenu,
		store: store,
		cfg:   cfg,
		input: ti,
		jsonInput: ji,
	}
}

func (m *FriendsConfigModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles all input for the friends config overlay.
func (m FriendsConfigModel) Update(msg tea.Msg) (FriendsConfigModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollUp()
		case tea.MouseButtonWheelDown:
			m.scrollDown()
		}
	}
	return m, nil
}

func (m *FriendsConfigModel) scrollUp() {
	switch m.page {
	case fcPageMenu:
		if m.selected > 0 { m.selected-- }
	case fcPageList:
		if m.selected > 0 { m.selected-- }
	case fcPageEditMyInfo:
		if m.mySelected > 0 { m.mySelected-- }
	case fcPageEditFriend:
		if m.editSelected > 0 { m.editSelected-- }
	}
}

func (m *FriendsConfigModel) scrollDown() {
	switch m.page {
	case fcPageMenu:
		if m.selected < len(menuItems)-1 { m.selected++ }
	case fcPageList:
		count := m.store.Count()
		if m.selected < count-1 { m.selected++ }
	case fcPageEditMyInfo:
		if m.mySelected < len(m.myFields)-1 { m.mySelected++ }
	case fcPageEditFriend:
		if m.editSelected < len(m.editFields)-1 { m.editSelected++ }
	}
}

func (m FriendsConfigModel) handleKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	// If editing a text field, handle input mode.
	if m.editing || m.myEditing {
		return m.handleEditing(msg)
	}

	switch m.page {
	case fcPageMenu:
		return m.handleMenuKey(msg)
	case fcPageList:
		return m.handleListKey(msg)
	case fcPageEditMyInfo:
		return m.handleEditMyInfoKey(msg)
	case fcPageShareInfo:
		return m.handleShareInfoKey(msg)
	case fcPageAddFriend:
		return m.handleAddFriendKey(msg)
	case fcPageAddFriendJSON:
		return m.handleAddFriendJSONKey(msg)
	case fcPageExport:
		return m.handleExportKey(msg)
	case fcPageImport:
		return m.handleImportKey(msg)
	case fcPageEditFriend:
		return m.handleEditFriendKey(msg)
	}
	return m, nil
}

func (m FriendsConfigModel) handleEditing(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editing = false
		m.myEditing = false
		m.input.Blur()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		if m.myEditing {
			m.applyMyField(val)
			m.myEditing = false
		} else {
			m.applyEditField(val)
			m.editing = false
		}
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- Menu page ---

func (m FriendsConfigModel) handleMenuKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 { m.selected-- }
	case "down", "j":
		if m.selected < len(menuItems)-1 { m.selected++ }
	case "enter":
		switch m.selected {
		case 0: // Friends List
			m.page = fcPageList
			m.selected = 0
		case 1: // Edit My Info
			m.page = fcPageEditMyInfo
			m.buildMyFields()
			m.mySelected = 0
		case 2: // Share My Info
			m.page = fcPageShareInfo
			m.buildContactJSON()
		case 3: // Add a Friend
			m.page = fcPageAddFriend
			m.buildAddFriendFields()
			m.editSelected = 0
		case 4: // Export
			m.doExport()
		case 5: // Import
			m.page = fcPageImport
			m.importPath = ""
			m.importOverwrite = false
		}
	case "esc":
		return m, func() tea.Msg { return FriendsConfigCloseMsg{} }
	}
	return m, nil
}

// --- Friends List page ---

func (m FriendsConfigModel) handleListKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	all := m.store.All()
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 { m.selected-- }
	case "down", "j":
		if m.selected < len(all)-1 { m.selected++ }
	case "pgup":
		m.selected -= 5
		if m.selected < 0 { m.selected = 0 }
	case "pgdown":
		m.selected += 5
		if m.selected >= len(all) { m.selected = len(all) - 1 }
	case "enter":
		if m.selected < len(all) {
			f := all[m.selected]
			m.editFriend = &f
			m.buildEditFriendFields()
			m.editSelected = 0
			m.page = fcPageEditFriend
		}
	case "d":
		if m.selected < len(all) {
			m.store.Remove(all[m.selected].UserID)
			_ = m.store.Save()
			m.message = "Removed " + all[m.selected].Name
			if m.selected >= m.store.Count() && m.selected > 0 {
				m.selected--
			}
		}
	case "esc":
		m.page = fcPageMenu
		m.selected = 0
	}
	return m, nil
}

// --- Edit My Info page ---

func (m *FriendsConfigModel) buildMyFields() {
	m.myFields = []settingsField{
		{label: "Name", key: "my_name", value: m.cfg.MyName, description: "Your display name in the friends network"},
		{label: "Email", key: "my_email", value: m.cfg.MyEmail, description: "Optional — used for friend uniqueness verification"},
		{label: "P2P Endpoint", key: "p2p_address", value: m.cfg.P2PAddress, description: "Public IP/hostname for P2P connections (leave empty for auto)"},
		{label: "P2P Port", key: "p2p_port", value: strconv.Itoa(p2pPort(m.cfg.P2PPort)), description: "Local port for P2P connections (default 9900)"},
		{label: "Secure Mode", key: "secure_mode", value: boolStr(m.cfg.SecureMode), description: "Enable E2E encrypted messaging", options: []string{"on", "off"}},
		{label: "Slacker ID", key: "slacker_id", value: m.cfg.SlackerID, description: "Your unique Slackers identifier (auto-generated, read-only)"},
	}
}

func p2pPort(p int) int {
	if p <= 0 { return 9900 }
	return p
}

func boolStr(b bool) string {
	if b { return "on" }
	return "off"
}

func (m FriendsConfigModel) handleEditMyInfoKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.mySelected > 0 { m.mySelected-- }
	case "down", "j":
		if m.mySelected < len(m.myFields)-1 { m.mySelected++ }
	case "enter":
		f := m.myFields[m.mySelected]
		if f.key == "slacker_id" {
			m.message = "Slacker ID is read-only"
			return m, nil
		}
		if len(f.options) > 0 {
			m.cycleMyOption()
			return m, nil
		}
		m.myEditing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "esc":
		m.page = fcPageMenu
		m.selected = 1
	}
	return m, nil
}

func (m *FriendsConfigModel) cycleMyOption() {
	f := &m.myFields[m.mySelected]
	for i, opt := range f.options {
		if opt == f.value {
			f.value = f.options[(i+1)%len(f.options)]
			m.applyMyField(f.value)
			return
		}
	}
	if len(f.options) > 0 {
		f.value = f.options[0]
		m.applyMyField(f.value)
	}
}

func (m *FriendsConfigModel) applyMyField(val string) {
	f := &m.myFields[m.mySelected]
	f.value = val
	switch f.key {
	case "my_name":
		m.cfg.MyName = val
	case "my_email":
		m.cfg.MyEmail = val
	case "p2p_address":
		m.cfg.P2PAddress = val
	case "p2p_port":
		n, err := strconv.Atoi(val)
		if err == nil && n >= 1024 && n <= 65535 {
			m.cfg.P2PPort = n
		} else {
			m.message = "Port must be 1024-65535"
			return
		}
	case "secure_mode":
		m.cfg.SecureMode = (val == "on")
	}
	go config.Save(m.cfg)
	m.message = "Saved"
}

// --- Share My Info page ---

func (m *FriendsConfigModel) buildContactJSON() {
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		"", // public key filled at runtime if available
		"", // multiaddr filled at runtime
		m.cfg.P2PAddress,
		p2pPort(m.cfg.P2PPort),
	)
	data, _ := json.MarshalIndent(card, "", "  ")
	m.contactJSON = string(data)
}

func (m FriendsConfigModel) handleShareInfoKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	if msg.String() == "esc" {
		m.page = fcPageMenu
		m.selected = 2
	}
	return m, nil
}

// --- Add a Friend page ---

func (m *FriendsConfigModel) buildAddFriendFields() {
	m.editFields = []settingsField{
		{label: "Name", key: "name", value: "", description: "Friend's display name"},
		{label: "Slacker ID", key: "slacker_id", value: "", description: "Friend's Slacker ID (from their contact card)"},
		{label: "Email", key: "email", value: "", description: "Friend's email (optional)"},
		{label: "Public Key", key: "public_key", value: "", description: "Friend's X25519 public key (base64)"},
		{label: "Endpoint", key: "endpoint", value: "", description: "Friend's IP or hostname"},
		{label: "Port", key: "port", value: "9900", description: "Friend's P2P port"},
		{label: "Multiaddr", key: "multiaddr", value: "", description: "Full libp2p multiaddr (optional, overrides endpoint+port)"},
	}
}

func (m FriendsConfigModel) handleAddFriendKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.editSelected > 0 { m.editSelected-- }
	case "down", "j":
		if m.editSelected < len(m.editFields)-1 { m.editSelected++ }
	case "enter":
		m.editing = true
		f := m.editFields[m.editSelected]
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "ctrl+j": // paste JSON mode
		m.page = fcPageAddFriendJSON
		m.jsonInput.Reset()
		m.jsonInput.Focus()
		return m, textinput.Blink
	case "ctrl+s": // save/add friend
		return m.doAddFriend()
	case "esc":
		m.page = fcPageMenu
		m.selected = 3
	}
	return m, nil
}

func (m *FriendsConfigModel) applyEditField(val string) {
	if m.page == fcPageEditFriend && m.editFriend != nil {
		m.applyFriendEditField(val)
		return
	}
	f := &m.editFields[m.editSelected]
	f.value = val
}

func (m FriendsConfigModel) doAddFriend() (FriendsConfigModel, tea.Cmd) {
	f := friends.Friend{}
	for _, field := range m.editFields {
		switch field.key {
		case "name":
			f.Name = field.value
		case "slacker_id":
			f.SlackerID = field.value
		case "email":
			f.Email = field.value
		case "public_key":
			f.PublicKey = field.value
		case "endpoint":
			f.Endpoint = field.value
		case "port":
			n, _ := strconv.Atoi(field.value)
			f.Port = n
		case "multiaddr":
			f.Multiaddr = field.value
		}
	}
	if f.Name == "" {
		m.message = "Name is required"
		return m, nil
	}

	conflict := m.store.FindConflict(f)
	if conflict != "" {
		m.message = "Conflict with existing friend: " + conflict
		return m, nil
	}

	if err := m.store.Add(f); err != nil {
		m.message = "Error: " + err.Error()
		return m, nil
	}
	_ = m.store.Save()
	m.message = f.Name + " added!"
	m.page = fcPageMenu
	m.selected = 0
	return m, nil
}

// --- Add Friend JSON page ---

func (m FriendsConfigModel) handleAddFriendJSONKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.page = fcPageAddFriend
		m.jsonInput.Blur()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.jsonInput.Value())
		if val == "" {
			return m, nil
		}
		var card friends.ContactCard
		if err := json.Unmarshal([]byte(val), &card); err != nil {
			m.message = "Invalid JSON: " + err.Error()
			return m, nil
		}
		// Fill the add-friend fields from the parsed card.
		m.fillFieldsFromCard(card)
		m.page = fcPageAddFriend
		m.jsonInput.Blur()
		m.message = "Parsed contact card — review and save"
		return m, nil
	}
	var cmd tea.Cmd
	m.jsonInput, cmd = m.jsonInput.Update(msg)
	return m, cmd
}

func (m *FriendsConfigModel) fillFieldsFromCard(card friends.ContactCard) {
	for i, f := range m.editFields {
		switch f.key {
		case "name":
			if card.Name != "" { m.editFields[i].value = card.Name }
		case "slacker_id":
			if card.SlackerID != "" { m.editFields[i].value = card.SlackerID }
		case "email":
			if card.Email != "" { m.editFields[i].value = card.Email }
		case "public_key":
			if card.PublicKey != "" { m.editFields[i].value = card.PublicKey }
		case "endpoint":
			if card.Endpoint != "" { m.editFields[i].value = card.Endpoint }
		case "port":
			if card.Port > 0 { m.editFields[i].value = strconv.Itoa(card.Port) }
		case "multiaddr":
			if card.Multiaddr != "" { m.editFields[i].value = card.Multiaddr }
		}
	}
}

// --- Export page ---

func (m *FriendsConfigModel) doExport() {
	data, err := m.store.ExportJSON()
	if err != nil {
		m.message = "Export error: " + err.Error()
		return
	}
	dlPath := m.cfg.DownloadPath
	if dlPath == "" {
		home, _ := os.UserHomeDir()
		dlPath = filepath.Join(home, "Downloads")
	}
	outPath := filepath.Join(dlPath, "slackers-friends.json")
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		m.message = "Write error: " + err.Error()
		return
	}
	m.message = fmt.Sprintf("Exported %d friends to %s", m.store.Count(), outPath)
}

func (m FriendsConfigModel) handleExportKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	m.page = fcPageMenu
	m.selected = 4
	return m, nil
}

// --- Import page ---

func (m FriendsConfigModel) handleImportKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.page = fcPageMenu
		m.selected = 5
	case "tab":
		m.importOverwrite = !m.importOverwrite
	case "enter":
		if m.importPath == "" {
			m.editing = true
			m.input.SetValue("")
			m.input.Placeholder = "Path to friends JSON file..."
			m.input.Focus()
			return m, textinput.Blink
		}
		return m.doImport()
	}
	if m.editing {
		switch msg.String() {
		case "enter":
			m.importPath = strings.TrimSpace(m.input.Value())
			m.editing = false
			m.input.Blur()
			return m, nil
		case "esc":
			m.editing = false
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m FriendsConfigModel) doImport() (FriendsConfigModel, tea.Cmd) {
	data, err := os.ReadFile(m.importPath)
	if err != nil {
		m.message = "Read error: " + err.Error()
		return m, nil
	}
	incoming, err := friends.ImportJSON(data)
	if err != nil {
		m.message = "Parse error: " + err.Error()
		return m, nil
	}
	added, skipped, overwritten := m.store.Import(incoming, m.importOverwrite)
	_ = m.store.Save()
	m.message = fmt.Sprintf("Imported: %d added, %d skipped, %d overwritten", added, skipped, overwritten)
	m.page = fcPageMenu
	m.selected = 5
	return m, nil
}

// --- Edit Friend page ---

func (m *FriendsConfigModel) buildEditFriendFields() {
	if m.editFriend == nil { return }
	f := m.editFriend
	m.editFields = []settingsField{
		{label: "Name", key: "name", value: f.Name, description: "Display name"},
		{label: "Email", key: "email", value: f.Email, description: "Email address"},
		{label: "Slacker ID", key: "slacker_id", value: f.SlackerID, description: "Unique identifier (read-only)"},
		{label: "Public Key", key: "public_key", value: f.PublicKey, description: "X25519 public key (base64)"},
		{label: "Endpoint", key: "endpoint", value: f.Endpoint, description: "IP/hostname"},
		{label: "Port", key: "port", value: strconv.Itoa(f.Port), description: "P2P port"},
		{label: "Multiaddr", key: "multiaddr", value: f.Multiaddr, description: "Full libp2p multiaddr"},
		{label: "Connection", key: "conn_type", value: f.ConnectionType, description: "Connection type (p2p/e2e)"},
		{label: "Added", key: "added_at", value: time.Unix(f.AddedAt, 0).Format("2006-01-02 15:04"), description: "Date added (read-only)"},
		{label: "Last Online", key: "last_online", value: formatLastOnline(f.LastOnline), description: "Last seen online (read-only)"},
		{label: "Status", key: "status", value: onlineLabel(f.Online), description: "Current connection status"},
	}
}

func formatLastOnline(ts int64) string {
	if ts == 0 { return "never" }
	t := time.Unix(ts, 0)
	dur := time.Since(t)
	switch {
	case dur < time.Minute:
		return "just now"
	case dur < time.Hour:
		return fmt.Sprintf("%dm ago", int(dur.Minutes()))
	case dur < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(dur.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

func onlineLabel(on bool) string {
	if on { return "online" }
	return "offline"
}

func (m FriendsConfigModel) handleEditFriendKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.editSelected > 0 { m.editSelected-- }
	case "down", "j":
		if m.editSelected < len(m.editFields)-1 { m.editSelected++ }
	case "enter":
		f := m.editFields[m.editSelected]
		// Read-only fields.
		if f.key == "slacker_id" || f.key == "added_at" || f.key == "last_online" || f.key == "status" {
			m.message = "This field is read-only"
			return m, nil
		}
		m.editing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "ctrl+s": // save
		return m.saveFriendEdit()
	case "esc":
		m.page = fcPageList
		m.editFriend = nil
	}
	return m, nil
}

func (m *FriendsConfigModel) applyFriendEditField(val string) {
	f := &m.editFields[m.editSelected]
	f.value = val
}

func (m FriendsConfigModel) saveFriendEdit() (FriendsConfigModel, tea.Cmd) {
	if m.editFriend == nil { return m, nil }
	for _, f := range m.editFields {
		switch f.key {
		case "name":
			m.editFriend.Name = f.value
		case "email":
			m.editFriend.Email = f.value
		case "public_key":
			m.editFriend.PublicKey = f.value
		case "endpoint":
			m.editFriend.Endpoint = f.value
		case "port":
			n, _ := strconv.Atoi(f.value)
			m.editFriend.Port = n
		case "multiaddr":
			m.editFriend.Multiaddr = f.value
		case "conn_type":
			m.editFriend.ConnectionType = f.value
		}
	}
	_ = m.store.Update(*m.editFriend)
	_ = m.store.Save()
	m.message = m.editFriend.Name + " updated"
	m.page = fcPageList
	m.editFriend = nil
	return m, nil
}

// --- View ---

func (m FriendsConfigModel) View() string {
	switch m.page {
	case fcPageMenu:
		return m.viewMenu()
	case fcPageList:
		return m.viewList()
	case fcPageEditMyInfo:
		return m.viewEditFields("Edit My Info", m.myFields, m.mySelected, m.myEditing,
			"Enter: edit | Esc: back")
	case fcPageShareInfo:
		return m.viewShareInfo()
	case fcPageAddFriend:
		return m.viewEditFields("Add a Friend", m.editFields, m.editSelected, m.editing,
			"Enter: edit field | Ctrl-J: paste JSON | Ctrl-S: save | Esc: back")
	case fcPageAddFriendJSON:
		return m.viewAddFriendJSON()
	case fcPageImport:
		return m.viewImport()
	case fcPageEditFriend:
		title := "Edit Friend"
		if m.editFriend != nil { title = "Edit: " + m.editFriend.Name }
		return m.viewEditFields(title, m.editFields, m.editSelected, m.editing,
			"Enter: edit | Ctrl-S: save | Esc: back")
	}
	return ""
}

func (m FriendsConfigModel) viewMenu() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Friends Config"))
	b.WriteString("\n\n")

	for i, item := range menuItems {
		cursor := "  "
		if i == m.selected { cursor = "> " }
		style := ChannelItemStyle
		if i == m.selected { style = ChannelSelectedStyle }
		b.WriteString(style.Render(cursor + item))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Enter: select | Esc: close"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Friends enable private P2P chat outside Slack."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  See 'slackers help friends' for setup guide."))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	onStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
	offStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Friends List"))
	b.WriteString("\n\n")

	all := m.store.All()
	if len(all) == 0 {
		b.WriteString(dimStyle.Render("  No friends yet. Use 'Add a Friend' to get started."))
	} else {
		maxVisible := m.height - 14
		if maxVisible < 3 { maxVisible = 3 }
		if maxVisible > len(all) { maxVisible = len(all) }
		start := 0
		if m.selected >= maxVisible { start = m.selected - maxVisible + 1 }
		end := start + maxVisible
		if end > len(all) { end = len(all) }

		for i := start; i < end; i++ {
			f := all[i]
			cursor := "  "
			if i == m.selected { cursor = "> " }

			status := offStyle.Render("  offline")
			if f.Online {
				status = onStyle.Render("  online")
			} else if f.LastOnline > 0 {
				status = offStyle.Render("  " + formatLastOnline(f.LastOnline))
			}

			name := f.Name
			if name == "" { name = f.UserID }
			style := ChannelItemStyle
			if i == m.selected { style = ChannelSelectedStyle }
			b.WriteString(style.Render(cursor + name))
			b.WriteString(status)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("  Enter: edit | d: remove | Esc: back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewEditFields(title string, fields []settingsField, selected int, editing bool, hint string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Width(16).Foreground(lipgloss.Color("252"))
	selLabelStyle := lipgloss.NewStyle().Width(16).Bold(true).Foreground(ColorPrimary)
	valueStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	descStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	maxVisible := m.height - 14
	if maxVisible < 3 { maxVisible = 3 }
	if maxVisible > len(fields) { maxVisible = len(fields) }
	scrollOff := 0
	if selected >= maxVisible { scrollOff = selected - maxVisible + 1 }
	end := scrollOff + maxVisible
	if end > len(fields) { end = len(fields) }

	for i := scrollOff; i < end; i++ {
		f := fields[i]
		cursor := "  "
		ls := labelStyle
		if i == selected {
			cursor = "> "
			ls = selLabelStyle
		}
		b.WriteString(cursor)
		b.WriteString(ls.Render(f.label))

		if editing && i == selected {
			b.WriteString(m.input.View())
		} else {
			val := f.value
			if val == "" { val = "(empty)" }
			b.WriteString(valueStyle.Render(val))
		}
		b.WriteString("\n")

		if i == selected {
			b.WriteString("    ")
			b.WriteString(descStyle.Render(f.description))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("  " + hint))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewShareInfo() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	codeStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	var b strings.Builder
	b.WriteString(titleStyle.Render("My Contact Card"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Share this JSON with friends so they can add you:"))
	b.WriteString("\n\n")
	b.WriteString(codeStyle.Render(m.contactJSON))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Copy the above JSON and send it to your friend."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  They can paste it in: Friends Config > Add a Friend > Ctrl-J"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Esc: back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewAddFriendJSON() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Paste Contact Card JSON"))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(m.jsonInput.View())
	b.WriteString("\n\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("  Enter: parse | Esc: back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewImport() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Import Friends"))
	b.WriteString("\n\n")

	if m.importPath == "" {
		b.WriteString("  Press Enter to set the file path.\n")
	} else {
		b.WriteString(fmt.Sprintf("  File: %s\n", m.importPath))
	}

	b.WriteString("\n")
	overwriteLabel := "[ ] Overwrite conflicts"
	if m.importOverwrite { overwriteLabel = "[x] Overwrite conflicts" }
	b.WriteString("  " + overwriteLabel + "\n")
	b.WriteString(dimStyle.Render("    If enabled, matching entries will be replaced.\n    If disabled, conflicts are skipped."))
	b.WriteString("\n\n")

	if m.importPath != "" {
		b.WriteString("  Press Enter to import.\n\n")
	}

	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("  Tab: toggle overwrite | Enter: set path/import | Esc: back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) renderBox(content string) string {
	boxHeight := m.height - 4
	if boxHeight < 10 { boxHeight = 10 }

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(70, m.width-4)).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
