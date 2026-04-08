package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// If FriendID is set, the overlay opens directly on the Edit Friend page
// pre-populated with that friend.
type FriendsConfigOpenMsg struct {
	FriendID string
}

// FriendKeyRotateRequestMsg asks the model to initiate a key rotation
// with the given friend. The model owns the P2P node and secure
// manager, so it does the actual send and tracks pending state.
type FriendKeyRotateRequestMsg struct {
	FriendUserID string
}

// FriendChatHistoryClearedMsg signals that the user wiped the on-disk
// chat history for a friend from the Friend Config Edit pane. The
// model uses it to drop the in-memory cache and refresh the live
// message view if that friend's chat is currently open.
type FriendChatHistoryClearedMsg struct {
	FriendUserID string
}

// FriendAddedHandshakeMsg fires after a new friend has been saved via
// the Add a Friend page. The model attempts ConnectToPeer + sends a
// MsgTypeFriendRequest to complete the handshake. On success any
// pending friend-request notification for that peer is cleared.
type FriendAddedHandshakeMsg struct {
	UserID    string
	Name      string
	Multiaddr string
}

// FriendTestConnectionMsg asks the model to verify (or reestablish) a
// P2P connection to an existing friend using their currently-stored
// multiaddr. The model:
//   - errors if multiaddr or public key is missing,
//   - calls ConnectToPeer + IsConnected,
//   - on success: marks the friend online and shows a confirmation,
//   - on failure: shows an error in the status bar AND in the
//     friends-config message area.
//
// AlsoHandshake is true when this fires from a save where the
// public key or multiaddr changed — in that case the model also
// sends a friend-request handshake so a stale or new identity
// re-pairs cleanly.
type FriendTestConnectionMsg struct {
	FriendUserID  string
	AlsoHandshake bool
}

// FriendsConfigCloseMsg signals the friends config overlay should close.
type FriendsConfigCloseMsg struct{}

// FriendImportBrowseMsg requests the model to open the file browser to
// pick a contact card JSON for the Add Friend page.
type FriendImportBrowseMsg struct{}

// FriendImportFileMsg carries the path of a contact card JSON file the
// user picked from the file browser. Handled by the friends config to
// pre-fill the Add Friend form.
type FriendImportFileMsg struct{ Path string }

// FriendsImportBrowseMsg requests the model to open the file browser to
// pick a friends-list JSON for the Import Friends List page.
type FriendsImportBrowseMsg struct{}

// FriendsImportFileMsg carries the path the user picked for the friends
// list import.
type FriendsImportFileMsg struct{ Path string }

// fcMessageClearMsg clears the friends-config status message after a
// timeout. The ID is matched against the model's current message ID so
// older timers don't clobber newer messages.
type fcMessageClearMsg struct{ ID int64 }

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
	page            fcPage
	store           *friends.FriendStore
	cfg             *config.Config
	selected        int
	editFriend      *friends.Friend // friend being edited
	editFields      []settingsField // reuse settings field pattern
	editSelected    int
	editing         bool
	input           textinput.Model
	importPath      string
	importOverwrite bool
	message         string
	messageID       int64           // increments on every status message; timer matches against this
	jsonInput       textinput.Model // for paste JSON
	width           int
	height          int

	// My info fields
	myFields   []settingsField
	mySelected int
	myEditing  bool

	// Contact card cache
	contactJSON     string // pretty-printed JSON for "Copy Profile as JSON"
	contactJSONLine string // single-line JSON for the json import command
	contactHash     string // SLF2.* compact hash
	contactCmd      string // legacy alias = contactCmdHash
	contactCmdHash  string // "slackers import-friend SLF2.<...>"
	contactCmdJSON  string // "slackers import-friend '<one-line-json>'"

	// Share My Info navigation: 0 = JSON block, 1 = Export-to-file button.
	shareInfoSelected int

	// Import page navigation: 0 = file path row, 1 = overwrite toggle, 2 = Import button.
	importSelected int

	// pendingClearHistory is the friend UserID for which a "Clear
	// Chat History" confirmation is currently shown. Empty when no
	// prompt is active. The next y/Enter completes the deletion;
	// any other key cancels.
	pendingClearHistory string

	// pendingDeleteFriend is the friend UserID currently awaiting a
	// delete confirmation in the Friends List page. Empty when no
	// prompt is active. The next y/Enter completes the removal; any
	// other key cancels.
	pendingDeleteFriend string

	// editFriendStandalone is true when fcPageEditFriend was opened directly
	// (e.g. via the friend details shortcut from a friend chat). In that
	// mode, Esc closes the overlay rather than returning to the friends
	// list, and the page renders an extra "Friends Config Menu" navigation
	// row at the bottom of the field list.
	editFriendStandalone bool

	// Local identity / endpoint info populated from the parent model so
	// the Share My Info card includes the actual public key and a
	// connectable multiaddr instead of empty placeholders.
	myPublicKey string
	myMultiaddr string

	// peerMultiaddrLookup looks up a connected friend's current
	// multiaddr from the live P2P node. Set by the parent model.
	peerMultiaddrLookup peerMultiaddrLookupFn

	// friendHistory is the persistent chat history store. Used by
	// the Clear Chat History action.
	friendHistory *friends.ChatHistoryStore
}

// SetIdentity wires the local node's public key and multiaddr into the
// friends-config model so it can build a real shareable contact card.
func (m *FriendsConfigModel) SetIdentity(pubKey, multiaddr string) {
	m.myPublicKey = pubKey
	m.myMultiaddr = multiaddr
}

// peerMultiaddrLookup is set by the parent model so the friends-config
// page can ask the live P2P node for a connected friend's current
// multiaddr. Returns "" when the peer isn't online.
type peerMultiaddrLookupFn func(friendUserID string) string

// SetPeerMultiaddrLookup wires the lookup callback. May be nil; in that
// case the auto-fill behaviour is skipped.
func (m *FriendsConfigModel) SetPeerMultiaddrLookup(fn peerMultiaddrLookupFn) {
	m.peerMultiaddrLookup = fn
}

// SetFriendHistory wires the chat history store so the friend edit
// page can show the history file path in the Clear Chat History
// confirmation prompt and actually clear it.
func (m *FriendsConfigModel) SetFriendHistory(h *friends.ChatHistoryStore) {
	m.friendHistory = h
}

var menuItems = []string{
	"Friends List",
	"Edit My Info",
	"Share My Info...",
	"Add a Friend...",
	"Export Friends List",
	"Import Friends List",
	"Share Format: <toggle>",
}

// shareFormatMode returns "json" or "hash" — the active value of
// cfg.ShareMyInfoFormat. The empty string is treated as "json" since
// that is the new-user default.
func shareFormatMode(cfg *config.Config) string {
	if cfg != nil && cfg.ShareMyInfoFormat == "hash" {
		return "hash"
	}
	return "json"
}

// shareFormatLabel returns the display string for the Share Format
// menu row, including the active option and a short reminder of what
// it means. The label is rebuilt every render so toggling reflects
// immediately without needing to mutate menuItems in place.
func shareFormatLabel(cfg *config.Config) string {
	if shareFormatMode(cfg) == "json" {
		return "Share Format: JSON  (full profile, plain text on the wire)"
	}
	return "Share Format: Hash  (compact and obfuscated, fewer fields)"
}

// NewFriendsConfigModel creates a new friends config overlay.
func NewFriendsConfigModel(store *friends.FriendStore, cfg *config.Config) FriendsConfigModel {
	ti := textinput.New()
	ti.CharLimit = 256

	ji := textinput.New()
	ji.Placeholder = "Paste friend's contact JSON here..."
	ji.CharLimit = 2048

	return FriendsConfigModel{
		page:      fcPageMenu,
		store:     store,
		cfg:       cfg,
		input:     ti,
		jsonInput: ji,
	}
}

func (m *FriendsConfigModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// OpenEditFriend opens the overlay directly on the Edit Friend page,
// pre-loaded with the friend identified by userID. If the friend is not
// found, the overlay opens on the menu page instead.
func (m *FriendsConfigModel) OpenEditFriend(userID string) {
	if m.store == nil {
		return
	}
	f := m.store.Get(userID)
	if f == nil {
		return
	}
	cp := *f
	m.editFriend = &cp
	m.tryAutoFillMultiaddr()
	m.buildEditFriendFields()
	m.editSelected = 0
	m.page = fcPageEditFriend
	m.editFriendStandalone = true
}

// tryAutoFillMultiaddr fills the friend's Multiaddr field from the
// live P2P peerstore if it's empty and the friend is currently online.
// Persists the new value to the friend store on success.
func (m *FriendsConfigModel) tryAutoFillMultiaddr() {
	if m.editFriend == nil || m.editFriend.Multiaddr != "" {
		return
	}
	if m.peerMultiaddrLookup == nil {
		return
	}
	addr := m.peerMultiaddrLookup(m.editFriend.UserID)
	if addr == "" {
		return
	}
	m.editFriend.Multiaddr = addr
	if m.store != nil {
		_ = m.store.Update(*m.editFriend)
		_ = m.store.Save()
	}
}

// setStatusMessage sets a status message and returns a tea.Cmd that clears
// the message after the given timeout. Each call increments messageID so
// stale timers from previous messages don't clobber a newer message.
// Pass ttl == 0 to use the user's configured global notification timeout.
func (m *FriendsConfigModel) setStatusMessage(s string, ttl time.Duration) tea.Cmd {
	m.messageID++
	m.message = s
	id := m.messageID
	if ttl <= 0 {
		ttl = m.cfg.NotificationTTL()
	}
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return fcMessageClearMsg{ID: id}
	})
}

// clearStatusMessage immediately clears the status message and bumps the
// ID so any in-flight clear timers become no-ops.
func (m *FriendsConfigModel) clearStatusMessage() {
	m.messageID++
	m.message = ""
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
	case fcMessageClearMsg:
		// Only clear if the timer matches the current message ID — newer
		// messages have already incremented messageID and shouldn't be
		// clobbered by a stale timer.
		if msg.ID == m.messageID {
			m.message = ""
		}
	}
	return m, nil
}

func (m *FriendsConfigModel) scrollUp() {
	switch m.page {
	case fcPageMenu:
		if m.selected > 0 {
			m.selected--
		}
	case fcPageList:
		if m.selected > 0 {
			m.selected--
		}
	case fcPageEditMyInfo:
		if m.mySelected > 0 {
			m.mySelected--
		}
	case fcPageEditFriend:
		if m.editSelected > 0 {
			m.editSelected--
		}
	}
}

func (m *FriendsConfigModel) scrollDown() {
	switch m.page {
	case fcPageMenu:
		if m.selected < len(menuItems)-1 {
			m.selected++
		}
	case fcPageList:
		count := m.store.Count()
		if m.selected < count-1 {
			m.selected++
		}
	case fcPageEditMyInfo:
		if m.mySelected < len(m.myFields)-1 {
			m.mySelected++
		}
	case fcPageEditFriend:
		if m.editSelected < len(m.editFields)-1 {
			m.editSelected++
		}
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
		var cmd tea.Cmd
		if m.myEditing {
			m.applyMyField(val)
			m.myEditing = false
		} else {
			m.applyEditField(val)
			m.editing = false
		}
		// Whatever applyMyField / applyEditField left in m.message
		// (success or validation error) gets wrapped with the global
		// status timeout so it auto-clears.
		if m.message != "" {
			cmd = m.setStatusMessage(m.message, 0)
		}
		m.input.Blur()
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- Menu page ---

func (m FriendsConfigModel) handleMenuKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = len(menuItems) - 1
		}
	case "down", "j":
		if m.selected < len(menuItems)-1 {
			m.selected++
		} else {
			m.selected = 0
		}
	case "enter":
		switch m.selected {
		case 0: // Friends List
			m.page = fcPageList
			m.selected = 0
		case 1: // Edit My Info
			m.page = fcPageEditMyInfo
			// Auto-detect public IP on entry if the field is empty.
			if m.cfg != nil && m.cfg.P2PAddress == "" {
				if ip, err := detectPublicIP(); err == nil && ip != "" {
					m.cfg.P2PAddress = ip
					config.SaveDebounced(m.cfg)
				}
			}
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
			cmd := m.doExport()
			return m, cmd
		case 5: // Import
			m.page = fcPageImport
			m.importPath = ""
			m.importOverwrite = false
			m.importSelected = 0
		case 6: // Share Format toggle
			if m.cfg != nil {
				if m.cfg.ShareMyInfoFormat == "json" {
					m.cfg.ShareMyInfoFormat = "hash"
				} else {
					m.cfg.ShareMyInfoFormat = "json"
				}
				config.SaveDebounced(m.cfg)
			}
			return m, m.setStatusMessage("Share format set to "+strings.ToUpper(shareFormatMode(m.cfg)), 0)
		}
	case "esc":
		return m, func() tea.Msg { return FriendsConfigCloseMsg{} }
	}
	return m, nil
}

// --- Friends List page ---

func (m FriendsConfigModel) handleListKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	all := m.store.All()
	// Active delete-confirmation prompt: y/Enter confirms removal,
	// any other key cancels.
	if m.pendingDeleteFriend != "" {
		s := msg.String()
		uid := m.pendingDeleteFriend
		m.pendingDeleteFriend = ""
		if s == "y" || s == "Y" || s == "enter" {
			var name string
			for _, f := range all {
				if f.UserID == uid {
					name = f.Name
					break
				}
			}
			m.store.Remove(uid)
			_ = m.store.Save()
			if m.selected >= m.store.Count() && m.selected > 0 {
				m.selected--
			}
			return m, m.setStatusMessage("Removed "+name, 0)
		}
		return m, m.setStatusMessage("Delete cancelled", 0)
	}
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		} else if len(all) > 0 {
			m.selected = len(all) - 1
		}
	case "down", "j":
		if m.selected < len(all)-1 {
			m.selected++
		} else {
			m.selected = 0
		}
	case "pgup":
		m.selected -= 5
		if m.selected < 0 {
			m.selected = 0
		}
	case "pgdown":
		m.selected += 5
		if m.selected >= len(all) {
			m.selected = len(all) - 1
		}
	case "enter":
		if m.selected < len(all) {
			f := all[m.selected]
			m.editFriend = &f
			m.tryAutoFillMultiaddr()
			m.buildEditFriendFields()
			m.editSelected = 0
			m.page = fcPageEditFriend
		}
	case "d":
		if m.selected < len(all) {
			f := all[m.selected]
			m.pendingDeleteFriend = f.UserID
			return m, m.setStatusMessage("Delete "+f.Name+"? y=confirm, any other key=cancel", 0)
		}
	case "esc":
		m.page = fcPageMenu
		m.selected = 0
		m.clearStatusMessage()
	}
	return m, nil
}

// --- Edit My Info page ---

func (m *FriendsConfigModel) buildMyFields() {
	m.myFields = []settingsField{
		{label: "Name", key: "my_name", value: m.cfg.MyName, description: "Your display name in the friends network"},
		{label: "Email", key: "my_email", value: m.cfg.MyEmail, description: "Optional — used for friend uniqueness verification"},
		{label: "P2P Endpoint", key: "p2p_address", value: m.cfg.P2PAddress, description: "Your Public IP/hostname for P2P connections. Press 'r' to refresh."},
		{label: "P2P Port", key: "p2p_port", value: strconv.Itoa(p2pPort(m.cfg.P2PPort)), description: "Local port for P2P connections (default 9900)"},
		{label: "Secure Mode", key: "secure_mode", value: boolStr(m.cfg.SecureMode), description: "Master switch for the P2P/E2E layer. When ON, Slackers starts the libp2p node, generates your X25519 identity, exchanges messages with friends end-to-end encrypted (ChaCha20-Poly1305), and serves files over P2P. When OFF, all friend chats are disabled.", options: []string{"on", "off"}},
		{label: "Auto-accept", key: "auto_accept_friends", value: boolStr(m.cfg.AutoAcceptFriendRequests), description: "When ON, incoming friend requests are accepted automatically — the handshake runs in the background and the new friend is added without prompting. When OFF, requests show as a notification and a confirmation modal you can accept or reject.", options: []string{"on", "off"}},
		{label: "Ping Interval", key: "friend_ping_seconds", value: strconv.Itoa(friendPingSeconds(m.cfg.FriendPingSeconds)), description: "How often (in seconds) the app polls friend connection state, re-fires profile-sync / request-pending pings on reconnect, and retries any messages still flagged Pending. Smaller = more responsive but more wake-ups. Minimum 2s, default 5s."},
		{label: "Slacker ID", key: "slacker_id", value: m.cfg.SlackerID, description: "Your unique Slackers identifier (auto-generated, read-only)"},
	}
}

func friendPingSeconds(v int) int {
	if v < 2 {
		return 5
	}
	return v
}

func p2pPort(p int) int {
	if p <= 0 {
		return 9900
	}
	return p
}

func boolStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m FriendsConfigModel) handleEditMyInfoKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.mySelected > 0 {
			m.mySelected--
		} else if len(m.myFields) > 0 {
			m.mySelected = len(m.myFields) - 1
		}
	case "down", "j":
		if m.mySelected < len(m.myFields)-1 {
			m.mySelected++
		} else {
			m.mySelected = 0
		}
	case "r", "R":
		// Re-detect the public IP and apply.
		f := m.myFields[m.mySelected]
		if f.key == "p2p_address" {
			ip, err := detectPublicIP()
			if err != nil || ip == "" {
				cmd := m.setStatusMessage("Could not auto-detect public IP", 0)
				return m, cmd
			}
			m.applyMyField(ip)
			m.buildMyFields()
			cmd := m.setStatusMessage("Detected "+ip, 0)
			return m, cmd
		}
	case "left", "right", "tab":
		// Cycle option fields without entering text edit.
		f := m.myFields[m.mySelected]
		if len(f.options) > 0 {
			m.cycleMyOption()
			cmd := m.setStatusMessage("Saved", 0)
			return m, cmd
		}
	case "enter":
		f := m.myFields[m.mySelected]
		if f.key == "slacker_id" {
			// Read-only — silently ignore.
			return m, nil
		}
		if len(f.options) > 0 {
			m.cycleMyOption()
			cmd := m.setStatusMessage("Saved", 0)
			return m, cmd
		}
		m.myEditing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "esc":
		m.page = fcPageMenu
		m.selected = 1
		m.clearStatusMessage()
	}
	return m, nil
}

// detectPublicIP queries an HTTP service for the local public IPv4
// address. Uses a short timeout and best-effort fallback.
func detectPublicIP() (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
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
	case "auto_accept_friends":
		m.cfg.AutoAcceptFriendRequests = (val == "on")
	case "friend_ping_seconds":
		n, err := strconv.Atoi(val)
		if err != nil || n < 2 {
			m.message = "Ping interval must be an integer ≥ 2"
			return
		}
		m.cfg.FriendPingSeconds = n
	}
	config.SaveDebounced(m.cfg)
	m.message = "Saved"
}

// --- Share My Info page ---

func (m *FriendsConfigModel) buildContactJSON() {
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		m.myPublicKey,
		m.myMultiaddr,
	)
	data, _ := json.MarshalIndent(card, "", "  ")
	m.contactJSON = string(data)
	oneLine, _ := json.Marshal(card)
	m.contactJSONLine = string(oneLine)
	// Build the JSON import command. We single-quote the JSON for
	// shell safety; profile values never contain single quotes
	// because Name/Email come from the user via plain text input
	// and the other fields are base64 / multiaddr / numeric.
	m.contactCmdJSON = "slackers import-friend '" + m.contactJSONLine + "'"
	if hash, err := friends.EncodeContactCard(card); err == nil {
		m.contactHash = hash
		m.contactCmdHash = "slackers import-friend " + hash
		m.contactCmd = m.contactCmdHash
	} else {
		m.contactHash = ""
		m.contactCmdHash = ""
		m.contactCmd = ""
	}
}

// shareInfoOption describes a single row in the redesigned Share My
// Info screen. Each row gets a label, a short description shown to
// the user, and a value-fetcher that returns the string that will
// be copied (or in the case of Export, the path written).
type shareInfoOption struct {
	label    string
	desc     string
	getValue func(m *FriendsConfigModel) string
	// onEnter is called instead of the default copy-to-clipboard
	// flow when set. Used by the Export option.
	onEnter func(m *FriendsConfigModel) tea.Cmd
}

// shareInfoOptions returns the ordered list of options shown in the
// Share My Info screen. Built fresh on every access so the values
// stay in sync with the latest contact card cache.
func shareInfoOptions() []shareInfoOption {
	return []shareInfoOption{
		{
			label:    "Copy Profile as JSON",
			desc:     "Copies your full contact card as plain-text JSON. Includes every profile field.",
			getValue: func(m *FriendsConfigModel) string { return m.contactJSON },
		},
		{
			label:    "Copy Profile as Hash",
			desc:     "Copies a compact SLF2 hash. More secure (obfuscated, fewer fields), but missing optional profile data.",
			getValue: func(m *FriendsConfigModel) string { return m.contactHash },
		},
		{
			label:    "Copy One-line import command (JSON)",
			desc:     "A ready-to-run shell command that imports your full profile (all fields, plain text).",
			getValue: func(m *FriendsConfigModel) string { return m.contactCmdJSON },
		},
		{
			label:    "Copy One-line import command (Hash)",
			desc:     "A ready-to-run shell command using the SLF2 hash. Smaller and more secure, fewer fields.",
			getValue: func(m *FriendsConfigModel) string { return m.contactCmdHash },
		},
		{
			label:    "Copy Public Key",
			desc:     "Just your X25519 public key. Useful for manually updating a friend's key field if it has drifted.",
			getValue: func(m *FriendsConfigModel) string { return m.myPublicKey },
		},
		{
			label:    "Copy Multiaddr",
			desc:     "Just your libp2p multiaddr. Useful for manually editing a friend's endpoint field.",
			getValue: func(m *FriendsConfigModel) string { return m.myMultiaddr },
		},
		{
			label:    "Export Profile to File",
			desc:     "Writes your contact card JSON to a file in your Downloads folder. The file can be re-imported later.",
			getValue: func(m *FriendsConfigModel) string { return m.contactJSON },
			onEnter: func(m *FriendsConfigModel) tea.Cmd {
				path, err := exportContactCardToDownloads(m.contactJSON, m.cfg)
				if err != nil {
					return m.setStatusMessage("Export failed: "+err.Error(), 0)
				}
				return m.setStatusMessage("Exported to "+path, 0)
			},
		},
	}
}

func (m FriendsConfigModel) handleShareInfoKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	opts := shareInfoOptions()
	switch msg.String() {
	case "esc":
		m.page = fcPageMenu
		m.selected = 2
		m.shareInfoSelected = 0
		m.clearStatusMessage()
		return m, nil
	case "up", "k":
		if m.shareInfoSelected > 0 {
			m.shareInfoSelected--
		} else {
			m.shareInfoSelected = len(opts) - 1
		}
		return m, nil
	case "down", "j":
		if m.shareInfoSelected < len(opts)-1 {
			m.shareInfoSelected++
		} else {
			m.shareInfoSelected = 0
		}
		return m, nil
	case "enter":
		if m.shareInfoSelected < 0 || m.shareInfoSelected >= len(opts) {
			return m, nil
		}
		opt := opts[m.shareInfoSelected]
		if opt.onEnter != nil {
			return m, opt.onEnter(&m)
		}
		val := opt.getValue(&m)
		if val == "" {
			return m, m.setStatusMessage(opt.label+": value unavailable", 0)
		}
		if copyToClipboard(val) {
			return m, m.setStatusMessage(opt.label+": copied to clipboard!", 0)
		}
		return m, m.setStatusMessage(opt.label+": could not copy", 0)
	}
	return m, nil
}

// exportContactCardToDownloads writes the contact card JSON to a file in
// the user's Downloads folder, named after the user (or "contact-card").
func exportContactCardToDownloads(jsonStr string, cfg *config.Config) (string, error) {
	dir := backupDownloadsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "contact-card"
	if cfg != nil && cfg.MyName != "" {
		name = sanitizeFilename(cfg.MyName)
	}
	if name == "" {
		name = "contact-card"
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, []byte(jsonStr), 0o644); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(path)
	return abs, nil
}

// sanitizeFilename returns a filesystem-safe lowercase version of s.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// copyToClipboard attempts to copy text to the system clipboard.
func copyToClipboard(text string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default: // linux/bsd
		// Try xclip first, fall back to xsel.
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			return false
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() == nil
}

// readFromClipboard returns the current clipboard text and ok=true on
// success. Falls back through the same set of system tools as
// copyToClipboard but in their "read" mode.
func readFromClipboard() (string, bool) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "windows":
		cmd = exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard")
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--output")
		} else if _, err := exec.LookPath("wl-paste"); err == nil {
			cmd = exec.Command("wl-paste")
		} else {
			return "", false
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// --- Add a Friend page ---

// advanceSelection moves the editFields selection by delta (+1 or -1)
// with wrap-around, skipping any "_separator" rows so they don't act
// as a stop. Returns the new selection index.
func (m *FriendsConfigModel) advanceSelection(cur, delta int) int {
	n := len(m.editFields)
	if n == 0 {
		return 0
	}
	for i := 0; i < n; i++ {
		cur = (cur + delta + n) % n
		if m.editFields[cur].key != "_separator" {
			return cur
		}
	}
	return cur
}

func (m *FriendsConfigModel) buildAddFriendFields() {
	m.editFields = []settingsField{
		{label: "Import File", key: "import_file", value: "", description: "Press Enter to browse for a contact card JSON, or Ctrl-J to paste from clipboard"},
		{label: "", key: "_separator", value: "", description: ""},
		{label: "Name", key: "name", value: "", description: "Friend's display name shown in your sidebar"},
		{label: "Slacker ID", key: "slacker_id", value: "", description: "Their Slacker ID — copied from their Share My Info contact card"},
		{label: "Email", key: "email", value: "", description: "Optional — used for friend uniqueness checks"},
		{label: "Public Key", key: "public_key", value: "", description: "Their X25519 public key (base64). Comes from their contact card. Used to derive the per-pair encryption key."},
		{label: "Multiaddr", key: "multiaddr", value: "", description: "Full libp2p address: /ip4/<ip>/tcp/<port>/p2p/<peerID>. Comes from their contact card."},
	}
}

func (m FriendsConfigModel) handleAddFriendKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.editSelected = m.advanceSelection(m.editSelected, -1)
	case "down", "j":
		m.editSelected = m.advanceSelection(m.editSelected, 1)
	case "enter":
		f := m.editFields[m.editSelected]
		if f.key == "_separator" {
			return m, nil
		}
		// Selecting the virtual "Import File" row opens the file browser.
		if f.key == "import_file" {
			return m, func() tea.Msg { return FriendImportBrowseMsg{} }
		}
		m.editing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "ctrl+j":
		// Read clipboard text and accept either a contact card JSON
		// blob or the SLF1.<...> hash form.
		text, ok := readFromClipboard()
		if !ok || strings.TrimSpace(text) == "" {
			cmd := m.setStatusMessage("Clipboard is empty or unavailable", 0)
			return m, cmd
		}
		card, err := friends.ParseAnyContactCard(text)
		if err != nil {
			cmd := m.setStatusMessage("Clipboard is not a valid contact card or hash", 0)
			return m, cmd
		}
		m.fillFieldsFromCard(card)
		cmd := m.setStatusMessage("Imported contact card from clipboard", 0)
		return m, cmd
	case "ctrl+s":
		return m.doAddFriend()
	case "esc":
		m.page = fcPageMenu
		m.selected = 3
		m.clearStatusMessage()
	}
	return m, nil
}

// LoadContactCardFile parses a contact card JSON file and pre-fills the
// Add Friend form. Called by the model when the user picks a file via
// the file browser.
func (m *FriendsConfigModel) LoadContactCardFile(path string) tea.Cmd {
	data, err := os.ReadFile(path)
	if err != nil {
		return m.setStatusMessage("Read failed: "+err.Error(), 0)
	}
	card, err := friends.ParseAnyContactCard(string(data))
	if err != nil {
		return m.setStatusMessage("Invalid contact card", 0)
	}
	m.fillFieldsFromCard(card)
	// Make sure the import row reflects the path.
	for i, f := range m.editFields {
		if f.key == "import_file" {
			m.editFields[i].value = path
			break
		}
	}
	return m.setStatusMessage("Imported "+filepath.Base(path), 0)
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
		case "multiaddr":
			f.Multiaddr = field.value
		}
	}
	if f.Name == "" {
		return m, m.setStatusMessage("Name is required", 0)
	}

	conflict := m.store.FindConflict(f)
	if conflict != "" {
		return m, m.setStatusMessage("Conflict with existing friend: "+conflict, 0)
	}

	if err := m.store.Add(f); err != nil {
		return m, m.setStatusMessage("Error: "+err.Error(), 0)
	}
	_ = m.store.Save()
	addedCmd := m.setStatusMessage(f.Name+" added!", 0)
	m.page = fcPageMenu
	m.selected = 0
	// Look up the just-added friend's UserID for the handshake (Add()
	// generates one from SlackerID when not provided).
	uid := f.UserID
	if uid == "" && f.SlackerID != "" {
		uid = "slacker:" + f.SlackerID
	}
	if uid == "" || f.Multiaddr == "" {
		return m, addedCmd
	}
	added := f
	added.UserID = uid
	return m, tea.Batch(addedCmd, func() tea.Msg {
		return FriendAddedHandshakeMsg{
			UserID:    added.UserID,
			Name:      added.Name,
			Multiaddr: added.Multiaddr,
		}
	})
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
		card, err := friends.ParseAnyContactCard(val)
		if err != nil {
			return m, m.setStatusMessage("Invalid contact card: "+err.Error(), 0)
		}
		// Fill the add-friend fields from the parsed card.
		m.fillFieldsFromCard(card)
		m.page = fcPageAddFriend
		m.jsonInput.Blur()
		return m, m.setStatusMessage("Parsed contact card — review and save", 0)
	}
	var cmd tea.Cmd
	m.jsonInput, cmd = m.jsonInput.Update(msg)
	return m, cmd
}

func (m *FriendsConfigModel) fillFieldsFromCard(card friends.ContactCard) {
	for i, f := range m.editFields {
		switch f.key {
		case "name":
			if card.Name != "" {
				m.editFields[i].value = card.Name
			}
		case "slacker_id":
			if card.SlackerID != "" {
				m.editFields[i].value = card.SlackerID
			}
		case "email":
			if card.Email != "" {
				m.editFields[i].value = card.Email
			}
		case "public_key":
			if card.PublicKey != "" {
				m.editFields[i].value = card.PublicKey
			}
		case "multiaddr":
			if card.Multiaddr != "" {
				m.editFields[i].value = card.Multiaddr
			}
		}
	}
}

// --- Export page ---

func (m *FriendsConfigModel) doExport() tea.Cmd {
	data, err := m.store.ExportJSON()
	if err != nil {
		return m.setStatusMessage("Export error: "+err.Error(), 0)
	}
	dlPath := m.cfg.DownloadPath
	if dlPath == "" {
		home, _ := os.UserHomeDir()
		dlPath = filepath.Join(home, "Downloads")
	}
	outPath := filepath.Join(dlPath, "slackers-friends.json")
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return m.setStatusMessage("Write error: "+err.Error(), 0)
	}
	return m.setStatusMessage(
		fmt.Sprintf("Exported %d friends to %s", m.store.Count(), outPath),
		0,
	)
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
		m.clearStatusMessage()
		return m, nil
	case "up", "k":
		if m.importSelected > 0 {
			m.importSelected--
		} else {
			m.importSelected = 2
		}
		return m, nil
	case "down", "j", "tab":
		if m.importSelected < 2 {
			m.importSelected++
		} else {
			m.importSelected = 0
		}
		return m, nil
	case "shift+tab":
		if m.importSelected > 0 {
			m.importSelected--
		} else {
			m.importSelected = 2
		}
		return m, nil
	case "enter":
		switch m.importSelected {
		case 0:
			// Open file browser to pick the friends JSON.
			return m, func() tea.Msg { return FriendsImportBrowseMsg{} }
		case 1:
			m.importOverwrite = !m.importOverwrite
		case 2:
			if m.importPath == "" {
				return m, m.setStatusMessage("Select a file first", 0)
			}
			return m.doImport()
		}
	case " ":
		if m.importSelected == 1 {
			m.importOverwrite = !m.importOverwrite
		}
	}
	return m, nil
}

// LoadFriendsImportFile records the path the user picked from the file
// browser. The user still has to navigate to the Import button and confirm.
func (m *FriendsConfigModel) LoadFriendsImportFile(path string) tea.Cmd {
	m.importPath = path
	m.importSelected = 1
	return m.setStatusMessage("Selected "+filepath.Base(path), 0)
}

func (m FriendsConfigModel) doImport() (FriendsConfigModel, tea.Cmd) {
	data, err := os.ReadFile(m.importPath)
	if err != nil {
		return m, m.setStatusMessage("Read error: "+err.Error(), 0)
	}
	incoming, err := friends.ImportJSON(data)
	if err != nil {
		return m, m.setStatusMessage("Parse error: "+err.Error(), 0)
	}
	added, skipped, overwritten := m.store.Import(incoming, m.importOverwrite)
	_ = m.store.Save()
	cmd := m.setStatusMessage(
		fmt.Sprintf("Imported: %d added, %d skipped, %d overwritten", added, skipped, overwritten),
		0,
	)
	m.page = fcPageMenu
	m.selected = 5
	return m, cmd
}

// --- Edit Friend page ---

func (m *FriendsConfigModel) buildEditFriendFields() {
	if m.editFriend == nil {
		return
	}
	f := m.editFriend
	m.editFields = []settingsField{
		{label: "Name", key: "name", value: f.Name, description: "Display name shown in your sidebar"},
		{label: "Email", key: "email", value: f.Email, description: "Optional — used for friend uniqueness checks"},
		{label: "Slacker ID", key: "slacker_id", value: f.SlackerID, description: "Their unique Slacker identifier (read-only)"},
		{label: "Public Key", key: "public_key", value: f.PublicKey, description: "X25519 public key. Press Enter to rotate — both sides will get a fresh key (friend must be online)."},
		{label: "Multiaddr", key: "multiaddr", value: f.Multiaddr, description: "Full libp2p address: /ip4/<ip>/tcp/<port>/p2p/<peerID>. Press Enter to edit. They send you this in their contact card."},
		{label: "Connection", key: "conn_type", value: connTypeOrDefault(f.ConnectionType), description: "p2p = direct libp2p stream (LAN/WAN with port forward, fastest, requires both online). e2e = encrypted via Slack DMs (slower, works through any firewall, leaves an audit trail in Slack).", options: []string{"p2p", "e2e"}},
		{label: "Added", key: "added_at", value: time.Unix(f.AddedAt, 0).Format("2006-01-02 15:04"), description: "Date added (read-only)"},
		{label: "Last Online", key: "last_online", value: formatLastOnline(f.LastOnline), description: "Last seen online (read-only)"},
		{label: "Status", key: "status", value: onlineLabel(f.Online), description: "Current connection status"},
	}
}

func formatLastOnline(ts int64) string {
	if ts == 0 {
		return "never"
	}
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
	if on {
		return "online"
	}
	return "offline"
}

// connTypeOrDefault returns the stored connection type or "p2p" when
// nothing has been chosen yet so the cycling field always shows a
// concrete value.
func connTypeOrDefault(s string) string {
	if s == "p2p" || s == "e2e" {
		return s
	}
	return "p2p"
}

func (m FriendsConfigModel) handleEditFriendKey(msg tea.KeyMsg) (FriendsConfigModel, tea.Cmd) {
	// Virtual row layout (after the field rows):
	//   len(editFields)     → [ Test Connection ]
	//   len(editFields)+1   → [ Clear Chat History ]
	//   len(editFields)+2   → [ Friends Config Menu ]   (standalone only)
	maxIdx := len(m.editFields) + 1
	if m.editFriendStandalone {
		maxIdx = len(m.editFields) + 2
	}
	// Active confirmation prompt: y/Enter confirms the destructive
	// action, anything else cancels it.
	if m.pendingClearHistory != "" {
		s := msg.String()
		uid := m.pendingClearHistory
		m.pendingClearHistory = ""
		if s == "y" || s == "Y" || s == "enter" {
			return m, m.doClearChatHistory(uid)
		}
		return m, m.setStatusMessage("Clear cancelled", 0)
	}
	switch msg.String() {
	case "up", "k":
		if m.editSelected > 0 {
			m.editSelected--
		} else {
			m.editSelected = maxIdx
		}
	case "down", "j":
		if m.editSelected < maxIdx {
			m.editSelected++
		} else {
			m.editSelected = 0
		}
	case "left", "right", "tab":
		// Cycle option fields (Connection) without entering text edit.
		if m.editSelected < len(m.editFields) {
			f := m.editFields[m.editSelected]
			if len(f.options) > 0 {
				m.cycleEditOption()
				return m, nil
			}
		}
	case "enter":
		// Test Connection virtual row (both modes).
		if m.editSelected == len(m.editFields) {
			return m, m.runTestConnection()
		}
		// Clear Chat History virtual row (both modes).
		if m.editSelected == len(m.editFields)+1 {
			return m, m.requestClearChatHistory()
		}
		// Standalone "Friends Config Menu" navigation row.
		if m.editFriendStandalone && m.editSelected == len(m.editFields)+2 {
			m.page = fcPageMenu
			m.editFriend = nil
			m.editFriendStandalone = false
			m.editSelected = 0
			return m, nil
		}
		f := m.editFields[m.editSelected]
		// Read-only fields: silently ignore (no message — the user can
		// see the field and the bottom hint says read-only).
		if f.key == "slacker_id" || f.key == "added_at" || f.key == "last_online" || f.key == "status" {
			return m, nil
		}
		// Cycling option fields: cycle on Enter.
		if len(f.options) > 0 {
			m.cycleEditOption()
			return m, nil
		}
		// Public Key: hand off to the model to run a P2P key rotation.
		if f.key == "public_key" && m.editFriend != nil {
			friendID := m.editFriend.UserID
			return m, func() tea.Msg {
				return FriendKeyRotateRequestMsg{FriendUserID: friendID}
			}
		}
		m.editing = true
		m.input.SetValue(f.value)
		m.input.Focus()
		m.input.CursorEnd()
	case "ctrl+j":
		// Read clipboard, parse as JSON or SLF1/SLF2 hash, and merge
		// any non-empty fields into this friend's empty slots.
		text, ok := readFromClipboard()
		if !ok || strings.TrimSpace(text) == "" {
			return m, m.setStatusMessage("Clipboard is empty or unavailable", 0)
		}
		card, err := friends.ParseAnyContactCard(text)
		if err != nil {
			return m, m.setStatusMessage("Clipboard is not a valid contact card or hash", 0)
		}
		filled := m.mergeCardIntoEditFriend(card)
		if filled == 0 {
			return m, m.setStatusMessage("Nothing to merge — every field already has a value", 0)
		}
		return m, m.setStatusMessage(fmt.Sprintf("Merged %d field(s) from clipboard", filled), 0)
	case "ctrl+s": // save
		return m.saveFriendEdit()
	case "esc":
		if m.editFriendStandalone {
			// Opened directly via the friend details shortcut: close
			// the entire overlay rather than fall back to the friends
			// list page.
			m.editFriendStandalone = false
			m.editFriend = nil
			return m, func() tea.Msg { return FriendsConfigCloseMsg{} }
		}
		m.page = fcPageList
		m.editFriend = nil
	}
	return m, nil
}

func (m *FriendsConfigModel) applyFriendEditField(val string) {
	f := &m.editFields[m.editSelected]
	f.value = val
}

// requestClearChatHistory shows a confirmation prompt naming the
// on-disk path that will be deleted, so the user has a chance to
// back it up before pressing 'y' to proceed.
func (m *FriendsConfigModel) requestClearChatHistory() tea.Cmd {
	if m.editFriend == nil {
		return m.setStatusMessage("No friend selected", 0)
	}
	if m.friendHistory == nil {
		return m.setStatusMessage("History store unavailable", 0)
	}
	uid := m.editFriend.UserID
	path := m.friendHistory.FilePath(uid)
	m.pendingClearHistory = uid
	return m.setStatusMessage(
		"Clear all chat history with "+m.editFriend.Name+
			"?  Backup first if needed: "+path+
			"  — press y to confirm, any other key to cancel",
		0,
	)
}

// doClearChatHistory performs the actual deletion + recreation of an
// empty history file and notifies the parent model so it can wipe its
// in-memory cache and refresh the live message view.
func (m *FriendsConfigModel) doClearChatHistory(uid string) tea.Cmd {
	if m.friendHistory == nil {
		return m.setStatusMessage("History store unavailable", 0)
	}
	path, err := m.friendHistory.ClearHistory(uid)
	if err != nil {
		return m.setStatusMessage("Clear failed: "+err.Error(), 0)
	}
	statusCmd := m.setStatusMessage("Cleared chat history → "+path, 0)
	notifyCmd := func() tea.Msg {
		return FriendChatHistoryClearedMsg{FriendUserID: uid}
	}
	return tea.Batch(statusCmd, notifyCmd)
}

// runTestConnection validates that the friend's Multiaddr and Public
// Key are present and dispatches a FriendTestConnectionMsg to the
// model. Sets a status message immediately on local validation
// failures so the user gets instant feedback even if the model
// handler doesn't run.
func (m *FriendsConfigModel) runTestConnection() tea.Cmd {
	if m.editFriend == nil {
		return m.setStatusMessage("No friend selected", 0)
	}
	if m.editFriend.PublicKey == "" || m.editFriend.Multiaddr == "" {
		return m.setStatusMessage("Public Key and Multiaddr must be set to test the connection", 0)
	}
	uid := m.editFriend.UserID
	return tea.Batch(
		m.setStatusMessage("Testing connection…", 0),
		func() tea.Msg {
			return FriendTestConnectionMsg{FriendUserID: uid, AlsoHandshake: false}
		},
	)
}

// mergeCardIntoEditFriend fills any blank fields on the friend
// currently being edited with values from the supplied contact card.
// Existing non-empty values are never overwritten. The friend store
// is updated and the visible field list rebuilt so the new values
// show immediately. Returns the number of fields that were filled.
func (m *FriendsConfigModel) mergeCardIntoEditFriend(card friends.ContactCard) int {
	if m.editFriend == nil {
		return 0
	}
	filled := 0
	if m.editFriend.Name == "" && card.Name != "" {
		m.editFriend.Name = card.Name
		filled++
	}
	if m.editFriend.Email == "" && card.Email != "" {
		m.editFriend.Email = card.Email
		filled++
	}
	if m.editFriend.SlackerID == "" && card.SlackerID != "" {
		m.editFriend.SlackerID = card.SlackerID
		filled++
	}
	if m.editFriend.PublicKey == "" && card.PublicKey != "" {
		m.editFriend.PublicKey = card.PublicKey
		filled++
	}
	if m.editFriend.Multiaddr == "" && card.Multiaddr != "" {
		m.editFriend.Multiaddr = card.Multiaddr
		filled++
	}
	if filled > 0 && m.store != nil {
		_ = m.store.Update(*m.editFriend)
		_ = m.store.Save()
		m.buildEditFriendFields()
	}
	return filled
}

// cycleEditOption advances the highlighted edit-friend option field
// (e.g. Connection p2p ↔ e2e) and updates the underlying friend record
// + persists.
func (m *FriendsConfigModel) cycleEditOption() {
	f := &m.editFields[m.editSelected]
	if len(f.options) == 0 {
		return
	}
	next := f.options[0]
	for i, opt := range f.options {
		if opt == f.value {
			next = f.options[(i+1)%len(f.options)]
			break
		}
	}
	f.value = next
	if m.editFriend != nil && f.key == "conn_type" {
		m.editFriend.ConnectionType = next
		if m.store != nil {
			_ = m.store.Update(*m.editFriend)
			_ = m.store.Save()
		}
	}
}

func (m FriendsConfigModel) saveFriendEdit() (FriendsConfigModel, tea.Cmd) {
	if m.editFriend == nil {
		return m, nil
	}
	// Snapshot the original (pre-edit) values for the
	// connection-affecting fields so we can detect changes and
	// trigger a re-handshake when needed.
	var origPubKey, origMultiaddr string
	if existing := m.store.Get(m.editFriend.UserID); existing != nil {
		origPubKey = existing.PublicKey
		origMultiaddr = existing.Multiaddr
	}
	for _, f := range m.editFields {
		switch f.key {
		case "name":
			m.editFriend.Name = f.value
		case "email":
			m.editFriend.Email = f.value
		case "public_key":
			m.editFriend.PublicKey = f.value
		case "multiaddr":
			m.editFriend.Multiaddr = f.value
		case "conn_type":
			m.editFriend.ConnectionType = f.value
		}
	}
	_ = m.store.Update(*m.editFriend)
	_ = m.store.Save()
	connChanged := m.editFriend.PublicKey != origPubKey || m.editFriend.Multiaddr != origMultiaddr
	uid := m.editFriend.UserID
	cmd := m.setStatusMessage(m.editFriend.Name+" updated", 0)
	// Stay on the edit page if we're in the standalone (cog/shortcut)
	// flow so the user can see the test result; otherwise drop back
	// to the friends list as before.
	if !m.editFriendStandalone {
		m.page = fcPageList
		m.editFriend = nil
	}
	// Auto-test the connection whenever any save happens; on saves
	// where the public key or multiaddr changed, also re-run the
	// friend-request handshake so the peer rebinds to the new key.
	if uid != "" {
		dispatch := func() tea.Msg {
			return FriendTestConnectionMsg{FriendUserID: uid, AlsoHandshake: connChanged}
		}
		return m, tea.Batch(cmd, dispatch)
	}
	return m, cmd
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
		title := "Friend Config Edit"
		if m.editFriend != nil {
			title = "Friend Config Edit: " + m.editFriend.Name
		}
		// Both modes use the same renderer; the "Friends Config
		// Menu" virtual row only appears in standalone mode.
		return m.viewEditFriendStandalone(title)
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
		// The Share Format row (index 6) is rendered with a live
		// label that reflects the current config value.
		if i == 6 {
			item = shareFormatLabel(m.cfg)
		}
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		style := ChannelItemStyle
		if i == m.selected {
			style = ChannelSelectedStyle
		}
		b.WriteString(style.Render(cursor + item))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
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
		if maxVisible < 3 {
			maxVisible = 3
		}
		if maxVisible > len(all) {
			maxVisible = len(all)
		}
		start := 0
		if m.selected >= maxVisible {
			start = m.selected - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(all) {
			end = len(all)
		}

		for i := start; i < end; i++ {
			f := all[i]
			cursor := "  "
			if i == m.selected {
				cursor = "> "
			}

			status := offStyle.Render("  offline")
			if f.Online {
				status = onStyle.Render("  online")
			} else if f.LastOnline > 0 {
				status = offStyle.Render("  " + formatLastOnline(f.LastOnline))
			}

			name := f.Name
			if name == "" {
				name = f.UserID
			}
			style := ChannelItemStyle
			if i == m.selected {
				style = ChannelSelectedStyle
			}
			b.WriteString(style.Render(cursor + name))
			b.WriteString(status)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
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
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > len(fields) {
		maxVisible = len(fields)
	}
	scrollOff := 0
	if selected >= maxVisible {
		scrollOff = selected - maxVisible + 1
	}
	end := scrollOff + maxVisible
	if end > len(fields) {
		end = len(fields)
	}

	for i := scrollOff; i < end; i++ {
		f := fields[i]
		// Visual separator: render as a blank row, not a field.
		if f.key == "_separator" {
			b.WriteString("\n")
			continue
		}
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
			if val == "" {
				val = "(empty)"
			}
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
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  " + hint))

	return m.renderBox(b.String())
}

// viewEditFriendStandalone renders the Edit Friend page with an extra
// "Friends Config Menu" navigation row. Selection index len(m.editFields)
// represents the navigation row.
func (m FriendsConfigModel) viewEditFriendStandalone(title string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Width(16).Foreground(lipgloss.Color("252"))
	selLabelStyle := lipgloss.NewStyle().Width(16).Bold(true).Foreground(ColorPrimary)
	valueStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	descStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	selectedItemStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// fields + Test Connection + Clear Chat History (always) +
	// Friends Config Menu (standalone only)
	totalRows := len(m.editFields) + 2
	if m.editFriendStandalone {
		totalRows = len(m.editFields) + 3
	}
	maxVisible := m.height - 14
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > totalRows {
		maxVisible = totalRows
	}
	scrollOff := 0
	if m.editSelected >= maxVisible {
		scrollOff = m.editSelected - maxVisible + 1
	}
	end := scrollOff + maxVisible
	if end > totalRows {
		end = totalRows
	}

	for i := scrollOff; i < end; i++ {
		if i < len(m.editFields) {
			f := m.editFields[i]
			cursor := "  "
			ls := labelStyle
			if i == m.editSelected {
				cursor = "> "
				ls = selLabelStyle
			}
			b.WriteString(cursor)
			b.WriteString(ls.Render(f.label))

			if m.editing && i == m.editSelected {
				b.WriteString(m.input.View())
			} else {
				val := f.value
				if val == "" {
					val = "(empty)"
				}
				b.WriteString(valueStyle.Render(val))
			}
			b.WriteString("\n")

			if i == m.editSelected {
				b.WriteString("    ")
				b.WriteString(descStyle.Render(f.description))
				b.WriteString("\n")
			}
			continue
		}
		// Virtual rows in order:
		//   N    → [ Test Connection ]
		//   N+1  → [ Clear Chat History ]
		//   N+2  → [ Friends Config Menu ]   (standalone only)
		var label, desc string
		switch i {
		case len(m.editFields):
			label = "[ Test Connection ]"
			desc = "Dial the friend with the current multiaddr and report online status"
		case len(m.editFields) + 1:
			label = "[ Clear Chat History ]"
			desc = "Wipe the locally-saved chat history file for this friend (confirmation required — back it up first if you want to keep it)"
		default:
			label = "[ Friends Config Menu ]"
			desc = "Open the main Friends Config menu"
		}
		if i == m.editSelected {
			b.WriteString(selectedItemStyle.Render("> " + label))
		} else {
			b.WriteString(itemStyle.Render("  " + label))
		}
		b.WriteString("\n")
		if i == m.editSelected {
			b.WriteString("    ")
			b.WriteString(descStyle.Render(desc))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	if m.editFriendStandalone {
		b.WriteString(dimStyle.Render("  Enter: edit/select | Ctrl-J: paste-merge | Ctrl-S: save | Esc: close"))
	} else {
		b.WriteString(dimStyle.Render("  Enter: edit/select | Ctrl-J: paste-merge | Ctrl-S: save | Esc: back"))
	}

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewShareInfo() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	codeStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	descStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("My Contact Card"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Pick how you want to share your profile. The active 'Share Format'"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  setting (Friends Config menu) controls what [FRIEND:me] sends in chat."))
	b.WriteString("\n\n")

	opts := shareInfoOptions()
	for i, opt := range opts {
		// Each option block is separated from the next by a single
		// blank line. The hovered option additionally renders the
		// description and a preview of what will be copied.
		cursor := "  "
		row := opt.label
		if i == m.shareInfoSelected {
			cursor = selectedStyle.Render("> ")
			row = selectedStyle.Render(opt.label)
		} else {
			row = itemStyle.Render(row)
		}
		b.WriteString(cursor)
		b.WriteString(row)
		b.WriteString("\n")
		if i == m.shareInfoSelected {
			// Use lipgloss padding/width for indented blocks so
			// that long unbreakable tokens (like a multiaddr or
			// SLF2 hash) wrap cleanly inside the indent. Manual
			// "    " prefixes get rendered as a lone blank line
			// when the next token can't fit beside them.
			boxWidth := min(70, m.width-4) - 6
			indentBlock := lipgloss.NewStyle().PaddingLeft(4).Width(boxWidth)
			b.WriteString(indentBlock.Render(descStyle.Render(opt.desc)))
			b.WriteString("\n")
			val := strings.TrimSpace(opt.getValue(&m))
			if val == "" {
				b.WriteString(indentBlock.Render(descStyle.Render("(value unavailable — Secure Mode may be off)")))
				b.WriteString("\n")
			} else {
				b.WriteString(indentBlock.Render(codeStyle.Render(val)))
				b.WriteString("\n")
			}
		}
		// Blank separator line between options.
		b.WriteString("\n")
	}

	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  ↑/↓ navigate · Enter: copy or export · Esc: back"))

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
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  Enter: parse | Esc: back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) viewImport() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Import Friends"))
	b.WriteString("\n\n")

	row := func(idx int, label, value string) {
		marker := "  "
		if m.importSelected == idx {
			marker = "> "
		}
		labelStr := marker + label
		if m.importSelected == idx {
			labelStr = selStyle.Render(labelStr)
		} else {
			labelStr = itemStyle.Render(labelStr)
		}
		valueStr := lipgloss.NewStyle().Foreground(ColorAccent).Render(value)
		b.WriteString(labelStr + " " + valueStr + "\n")
	}

	pathDisplay := m.importPath
	if pathDisplay == "" {
		pathDisplay = "(none — Enter to browse)"
	}
	row(0, "Select File:    ", pathDisplay)
	b.WriteString("\n")

	overwriteLabel := "[ ] Overwrite conflicts"
	if m.importOverwrite {
		overwriteLabel = "[x] Overwrite conflicts"
	}
	row(1, "Overwrite:      ", overwriteLabel)
	b.WriteString("    " + dimStyle.Render("(Space/Enter to toggle — replaces matching friends)") + "\n")
	b.WriteString("\n")

	importLabel := "[ Import friends ]"
	if m.importSelected == 2 {
		importLabel = selStyle.Render("> " + importLabel)
	} else {
		importLabel = itemStyle.Render("  " + importLabel)
	}
	b.WriteString(importLabel + "\n")

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  ↑/↓ navigate · Enter select/toggle · Tab next · Esc back"))

	return m.renderBox(b.String())
}

func (m FriendsConfigModel) renderBox(content string) string {
	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

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
