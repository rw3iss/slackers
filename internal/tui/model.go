package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/notifications"
	"github.com/rw3iss/slackers/internal/secure"
	"github.com/rw3iss/slackers/internal/shortcuts"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/theme"
	"github.com/rw3iss/slackers/internal/types"
)

// Overlay represents which overlay is currently shown.
type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlaySettings
	overlaySearch
	overlayHidden
	overlayRename
	overlayMsgSearch
	overlayFileBrowser
	overlayFilesList
	overlayShortcuts
	overlayWhitelist
	overlayFriendRequest
	overlayFriendsConfig
	overlayEmojiPicker
	overlayMsgOptions
	overlaySidebarOptions
	overlayAbout
	overlayThemePicker
	overlayThemeEditor
	overlayThemeColorPicker
	overlayNotifications
)

// fileBrowserPurpose tracks why the file browser is open.
type fileBrowserPurpose int

const (
	fbPurposeAttach        fileBrowserPurpose = iota // selecting a file to send
	fbPurposeSettings                                // selecting a download folder
	fbPurposeImportTheme                             // selecting a theme JSON to import
	fbPurposeFriendImport                            // selecting a friend contact card JSON
	fbPurposeFriendsImport                           // selecting a friends list JSON
)

// Custom message types for the TUI update loop.

// UpdateAvailableMsg signals that a new version is available.
type UpdateAvailableMsg struct {
	Version string
}

type ChannelsLoadedMsg struct{ Channels []types.Channel }
type HistoryLoadedMsg struct {
	Messages []types.Message
	Err      error
}
type UsersLoadedMsg struct{ Users map[string]types.User }
type MessageSentMsg struct{}
type SlackEventMsg struct{ Event slackpkg.SocketEvent }
type ConnStatusMsg struct {
	Status types.ConnectionStatus
	Err    error
}
type ErrMsg struct{ Err error }

// SilentHistoryMsg is like HistoryLoadedMsg but doesn't change focus or scroll position.
type SilentHistoryMsg struct{ Messages []types.Message }

// FileUploadedMsg is sent when file uploads complete.
type FileUploadedMsg struct{ Count int }

// FileDownloadCompleteMsg signals a file download finished.
type FileDownloadCompleteMsg struct {
	DestPath string
	Err      error
}

// FileDownloadCancelledMsg signals a download was cancelled.
type FileDownloadCancelledMsg struct{}

// FileDownloadProgressMsg reports download progress.
type FileDownloadProgressMsg struct {
	FileName   string
	Downloaded int64
	Total      int64
}

// FileUploadDoneMsg marks one file in an optimistic message as fully
// uploaded (Slack) or delivered (P2P). The renderer flips its uploading
// indicator off.
type FileUploadDoneMsg struct {
	MessageID string // local message ID containing the file
	FileID    string // FileInfo.ID inside that message
	Err       error  // non-nil if upload failed
}

// SeedLastSeenMsg carries baseline timestamps without triggering unread markers.
type SeedLastSeenMsg struct {
	Timestamps map[string]string
}

// ActivityCheckMsg triggers an away-status check.
type ActivityCheckMsg struct{}

// ClearWarningMsg clears the status bar warning if the user was recently active.
type ClearWarningMsg struct{}

// NotifyWatchdogMsg fires periodically (~1s) so the model can age out
// any non-empty status warning that has exceeded the configured
// NotificationTimeout. This makes every m.warning = "..." assignment
// auto-clear without each call site having to schedule its own timer.
type NotifyWatchdogMsg struct{}

// WhitelistOpenMsg signals that the whitelist overlay should open.
type WhitelistOpenMsg struct{}

// P2PReceivedMsg is sent when a message arrives over the P2P connection.
type P2PReceivedMsg struct {
	SenderID  string
	Text      string
	PubKey    string // for friend requests
	Multiaddr string // for friend requests

	// Regular text-message metadata. The sender embeds these in the
	// wire payload so cross-instance reply / reaction / edit / delete
	// actions can target the same logical message on both sides.
	MsgID     string // sender's locally-generated id for this message
	ReplyToID string // parent message id when this is a reply

	// SentAt is the unix timestamp the sender stamped the message
	// with at the time of sending (not the receive time). Used for
	// preserving ordering when a batch of pending messages is
	// resent after a reconnect — otherwise the receiver would
	// order them by arrival time and see them out of order.
	SentAt int64
}

// SecureSessionReadyMsg signals that a secure session was established with a peer.
type SecureSessionReadyMsg struct {
	PeerID string
	State  secure.SessionState
}

// FriendsLoadedMsg carries friend channels to display in the sidebar.
type FriendsLoadedMsg struct {
	Channels []types.Channel
	Online   map[string]bool
}

// friendPingTickMsg triggers a friend online check.
type friendPingTickMsg struct{}

// FriendPingMsg carries online status for friends.
type FriendPingMsg struct {
	Online map[string]bool
}

// FriendSendResultMsg reports the outcome of a P2P text send to a
// friend. PeerUID is the friend's UserID, MessageID is the local
// message identifier used in the friend chat history, and Success
// is true if the wire-level send (or its one-shot retry) completed
// without an error. On failure the history entry is marked Pending
// so it can be resent automatically when the peer reconnects. On
// success any pre-existing Pending flag is cleared.
type FriendSendResultMsg struct {
	PeerUID   string
	MessageID string
	Success   bool
}

// channelInfo stores the name and alias for a channel.
type channelInfo struct {
	name  string
	alias string
}

var filePattern = regexp.MustCompile(`\[FILE:([^\]]+)\]`)
var replyPattern = regexp.MustCompile(`^\s*\[REPLY:([^\]]+)\][^\n]*\n?`)
var editPattern = regexp.MustCompile(`^\s*\[EDIT:([^\]]+)\][^\n]*\n?`)

// generateMessageID creates a unique ID for a P2P message.
func generateMessageID() string {
	return fmt.Sprintf("p2p-%d", time.Now().UnixNano())
}

// ContextHistoryMsg carries messages around a search result for context viewing.
type ContextHistoryMsg struct {
	Messages    []types.Message
	TargetIdx   int
	ChannelName string
}

// PollTickMsg triggers a poll for the current channel and priority channels.
type PollTickMsg struct{}

// BgPollTickMsg triggers a background poll for rotation channels.
type BgPollTickMsg struct{}

// UnreadChannelsMsg carries channel IDs with new messages and all latest timestamps.
type UnreadChannelsMsg struct {
	ChannelIDs   []string
	LatestTS     map[string]string
	IsBackground bool // true if from background poll
}

// Model is the root TUI model composing all sub-components.
type Model struct {
	// Sub-models
	channels         ChannelListModel
	messages         MessageViewModel
	input            InputModel
	keymap           KeyMap
	settings         SettingsModel
	search           SearchModel
	hidden           HiddenChannelsModel
	rename           RenameModel
	msgSearch        MsgSearchModel
	fileBrowser      FileBrowserModel
	fbPurpose        fileBrowserPurpose
	filesList        FilesListModel
	shortcutsEditor  ShortcutsEditorModel
	whitelist        WhitelistModel
	help             HelpModel
	friendRequest    FriendRequestModel
	friendsConfig    FriendsConfigModel
	about            AboutModel
	themePicker      ThemePickerModel
	themeEditor      ThemeEditorModel
	themeColorPicker ThemeColorPickerModel
	emojiPicker      EmojiPickerModel
	msgOptions       MsgOptionsModel
	sidebarOptions   SidebarOptionsModel

	// Reactions
	reactMsgID string // message ID for pending reaction

	// Pending message deletion confirmation. When non-empty, the next y/Enter
	// confirms deleting that message; any other key cancels.
	pendingDeleteMsgID string

	// Friends
	friendStore    *friends.FriendStore
	friendHistory  *friends.ChatHistoryStore
	friendMessages map[string][]types.Message // in-memory cache (backed by friendHistory)

	// Notifications
	notifStore *notifications.Store
	notifs     NotificationsOverlayModel

	// friendActivity tracks the last time a friend chat was
	// touched (opened, focused, typed in). Connections that go
	// quiet for FriendIdleTimeout are dropped by the inactivity
	// watchdog so we don't keep open libp2p sessions to every
	// friend in the user's list.
	friendActivity map[string]time.Time

	// friendPrevOnline tracks the last known online state for each
	// friend so the FriendPingMsg handler can detect offline→online
	// transitions and trigger auto-resend of any Pending messages.
	friendPrevOnline map[string]bool

	// State
	focus      types.Focus
	overlay    overlay
	currentCh  *types.Channel
	users      map[string]types.User
	connStatus types.ConnectionStatus
	connErr    error
	teamName   string
	myUserID   string // local Slack user ID (cached from AuthTest)
	err        error
	warning    string

	// Secure messaging
	secureMgr *secure.SessionManager
	p2pNode   *secure.P2PNode
	p2pChan   chan P2PReceivedMsg

	// Shortcuts
	shortcutMap       shortcuts.ShortcutMap
	shortcutOverrides shortcuts.ShortcutMap

	// Channel index: ID -> {name, alias}
	channelIndex map[string]channelInfo

	// Polling
	lastSeen     map[string]string
	lastChecked  map[string]time.Time // when each channel was last polled
	pollChannels []string             // ordered list for round-robin polling

	// Config
	cfg *config.Config

	// Dependencies (interfaces for SOLID)
	slackSvc  slackpkg.SlackService
	socketSvc slackpkg.SocketService
	eventChan chan slackpkg.SocketEvent

	// Activity tracking
	lastActivity time.Time
	isAway       bool

	// Notification watchdog: tracks when the current m.warning was
	// first set so the watchdog ticker can clear it after the
	// user-configured timeout. prevWarning lets us detect transitions.
	warningSetAt time.Time
	prevWarning  string

	// Download state
	downloading    bool
	downloadCancel context.CancelFunc

	// pendingKeyRotation tracks key rotations the user has initiated.
	// Keyed by friend UserID, the value is the local ephemeral private
	// key we just generated for that friendship. When the friend's ack
	// arrives we use this to derive the new pair key.
	pendingKeyRotation map[string][32]byte

	// Upload tracking. uploadCancels keys are "<msgID>|<fileID>" and the
	// value is a cancel function (Slack uploads) or nil (P2P, where the
	// cancel is handled by removing the file from the P2P serving table
	// and notifying the peer).
	uploadCancels map[string]context.CancelFunc

	// pendingCancelUploadKey carries the upload key the user is being
	// asked to confirm cancelling at the status bar. Empty when no
	// prompt is active. Format: "<msgID>|<fileID>".
	pendingCancelUploadKey string

	// pendingFriendCard carries the contact card the user clicked on
	// in a chat message and is currently being asked to import or
	// merge. nil = no prompt active.
	pendingFriendCard *friends.ContactCard

	// Layout
	width        int
	height       int
	sidebarWidth int
	msgTop       int
	inputTop     int
	ready        bool
	splash       bool
	initialLoad  bool
	fullMode     bool
	dragging     bool // sidebar resize drag in progress
	version      string
}

// NewModel creates a new root TUI model.
func NewModel(slackSvc slackpkg.SlackService, socketSvc slackpkg.SocketService, cfg *config.Config, version string, friendStore *friends.FriendStore, friendHistory *friends.ChatHistoryStore) Model {
	// Apply the user's selected theme (if any) before any styles are read.
	if cfg.Theme != "" {
		if t, ok := theme.FindByName(cfg.Theme); ok {
			ApplyTheme(t)
		}
	}
	ch := NewChannelList()
	ch.SetFocused(true)
	ch.SetItemSpacing(cfg.SidebarItemSpacing)

	inp := NewInput()
	inp.SetHistory(cfg.InputHistory)
	histMax := cfg.InputHistoryMax
	if histMax <= 0 {
		histMax = 20
	}
	inp.SetMaxHistory(histMax)
	// Recognise pasted contact-card JSON / SLF1./SLF2. hashes and
	// rewrite them to compact [FRIEND:me] / [FRIEND:<id>] markers.
	// The actual expansion to a full hash happens at send time.
	inp.SetFriendResolver(func(blob string) string {
		card, err := friends.ParseAnyContactCard(blob)
		if err != nil {
			return ""
		}
		// Self-paste? Match by SlackerID against the local id.
		if card.SlackerID != "" && cfg.SlackerID != "" && card.SlackerID == cfg.SlackerID {
			return "[FRIEND:me]"
		}
		if friendStore != nil {
			for _, f := range friendStore.All() {
				if (card.SlackerID != "" && f.SlackerID == card.SlackerID) ||
					(card.Multiaddr != "" && f.Multiaddr == card.Multiaddr) {
					return "[FRIEND:" + f.UserID + "]"
				}
			}
		}
		return ""
	})

	// Initialize secure messaging if enabled.
	var secureMgr *secure.SessionManager
	if cfg.SecureMode {
		keyPath := cfg.SecureKeyPath
		if keyPath == "" {
			keyPath = secure.DefaultKeyPath()
		}
		var kp *secure.KeyPair
		if secure.KeyExists(keyPath) {
			kp, _ = secure.LoadKeyPair(keyPath)
		} else {
			kp, _ = secure.GenerateKeyPair()
			if kp != nil {
				_ = kp.SavePrivateKey(keyPath)
			}
		}
		if kp != nil {
			secureMgr = secure.NewSessionManager(kp)
		}
	}

	// Start P2P node if secure mode is enabled.
	var p2pNode *secure.P2PNode
	var p2pChan chan P2PReceivedMsg
	if cfg.SecureMode && secureMgr != nil {
		port := cfg.P2PPort
		if port <= 0 {
			port = 9900
		}
		p2pChan = make(chan P2PReceivedMsg, 64)
		onMsg := func(peerSlackID string, msg secure.P2PMessage) {
			switch msg.Type {
			case secure.MsgTypeFriendRequest, secure.MsgTypeFriendAccept:
				parts := strings.SplitN(msg.Text, "|", 2)
				pubKey, maddr := "", ""
				if len(parts) == 2 {
					pubKey, maddr = parts[0], parts[1]
				}
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__" + msg.Type + "__",
					PubKey:    pubKey,
					Multiaddr: maddr,
				}
			case secure.MsgTypeDisconnect:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__disconnect__",
				}
			case secure.MsgTypeReaction:
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__reaction__",
					PubKey:    msg.TargetMsgID,
					Multiaddr: msg.ReactionEmoji,
				}
			case secure.MsgTypeReactionRemove:
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__reaction_remove__",
					PubKey:    msg.TargetMsgID,
					Multiaddr: msg.ReactionEmoji,
				}
			case secure.MsgTypeDelete:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__delete__",
					PubKey:   msg.TargetMsgID,
				}
			case secure.MsgTypeDeleteAck:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__delete_ack__",
					PubKey:   msg.TargetMsgID,
				}
			case secure.MsgTypeEdit:
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__edit__",
					PubKey:    msg.TargetMsgID,
					Multiaddr: msg.Text,
				}
			case secure.MsgTypeEditAck:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__edit_ack__",
					PubKey:   msg.TargetMsgID,
				}
			case secure.MsgTypeFileOffer:
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__file_offer__",
					PubKey:    msg.FileID,                                       // reuse field for fileID
					Multiaddr: fmt.Sprintf("%s|%d", msg.FileName, msg.FileSize), // reuse for name|size
				}
			case secure.MsgTypeFileCancel:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__file_cancel__",
					PubKey:   msg.FileID,
				}
			case secure.MsgTypeKeyRotate:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__key_rotate__",
					PubKey:   msg.Text, // sender's new public key (base64)
				}
			case secure.MsgTypeKeyRotateAck:
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__key_rotate_ack__",
					PubKey:   msg.Text, // peer's new public key (base64)
				}
			case secure.MsgTypeProfileSync:
				// msg.Text carries the sender's full contact card
				// as JSON. The model-side handler merges any fresh
				// fields into the stored friend record.
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      "__profile_sync__",
					Multiaddr: msg.Text,
				}
			case secure.MsgTypeRequestPending:
				// Peer is asking us to scan our history for any
				// messages addressed to them that still have
				// Pending set and re-send them. Triggered when
				// the peer detects the connection is up.
				p2pChan <- P2PReceivedMsg{
					SenderID: peerSlackID,
					Text:     "__request_pending__",
				}
			case secure.MsgTypeMessage:
				p2pChan <- P2PReceivedMsg{
					SenderID:  peerSlackID,
					Text:      msg.Text,
					MsgID:     msg.MessageID,
					ReplyToID: msg.ReplyToMsgID,
					SentAt:    msg.Timestamp,
				}
			default:
				p2pChan <- P2PReceivedMsg{SenderID: peerSlackID, Text: msg.Text}
			}
		}
		p2pNode, _ = secure.NewP2PNode(port, cfg.P2PAddress, onMsg)
		// When a peer downloads one of our shared files, route the
		// notification through the P2P channel as a synthetic
		// "__file_served__" message so the model can flip the file
		// from "uploading…" to ready.
		if p2pNode != nil {
			p2pNode.SetFileServedCallback(func(fileID string) {
				p2pChan <- P2PReceivedMsg{
					Text:   "__file_served__",
					PubKey: fileID,
				}
			})
			// Wire the friend-store fallback lookup so inbound-only
			// connections (where the remote dialed us first) can
			// still be attributed to a known friend. Without this,
			// messages from never-dialed peers arrive with slackID
			// "unknown" and get dropped silently.
			store := friendStore
			p2pNode.SetPeerLookup(func(peerIDStr string) string {
				if store == nil || peerIDStr == "" {
					return ""
				}
				for _, f := range store.All() {
					if f.Multiaddr == "" {
						continue
					}
					// Multiaddr format: /ip4/.../tcp/.../p2p/<peerID>
					parts := strings.Split(strings.TrimPrefix(f.Multiaddr, "/"), "/")
					if len(parts) >= 6 && parts[4] == "p2p" && parts[5] == peerIDStr {
						return f.UserID
					}
				}
				return ""
			})
		}
	}

	// Load and merge shortcuts.
	defaults := shortcuts.DefaultShortcuts()
	overrides, _ := shortcuts.Load(shortcuts.UserConfigPath())
	merged := shortcuts.Merge(defaults, overrides)
	km := BuildKeyMap(merged)

	mv := NewMessageView()
	rf := cfg.ReplyFormat
	if rf == "" {
		rf = "inline"
	}
	mv.SetReplyFormat(rf)
	mv.SetItemSpacing(cfg.MessageItemSpacing)

	return Model{
		channels:           ch,
		messages:           mv,
		input:              inp,
		keymap:             km,
		secureMgr:          secureMgr,
		p2pNode:            p2pNode,
		p2pChan:            p2pChan,
		shortcutMap:        merged,
		shortcutOverrides:  overrides,
		settings:           NewSettingsModel(cfg, version),
		help:               NewHelpModel(version),
		focus:              types.FocusSidebar,
		users:              make(map[string]types.User),
		channelIndex:       make(map[string]channelInfo),
		lastSeen:           loadLastSeen(cfg),
		lastChecked:        make(map[string]time.Time),
		cfg:                cfg,
		lastActivity:       time.Now(),
		splash:             true,
		initialLoad:        true,
		version:            version,
		friendStore:        friendStore,
		friendHistory:      friendHistory,
		friendMessages:     make(map[string][]types.Message),
		uploadCancels:      make(map[string]context.CancelFunc),
		pendingKeyRotation: make(map[string][32]byte),
		friendActivity:     make(map[string]time.Time),
		friendPrevOnline:   make(map[string]bool),
		notifStore: func() *notifications.Store {
			ns := notifications.NewStore(notifications.DefaultPath())
			_ = ns.Load()
			return ns
		}(),
		slackSvc:  slackSvc,
		socketSvc: socketSvc,
		eventChan: make(chan slackpkg.SocketEvent, 100),
	}
}

// Init returns the initial commands to run at startup.
func (m Model) Init() tea.Cmd {
	// Apply reply format from config.
	rf := m.cfg.ReplyFormat
	if rf == "" {
		rf = "inline"
	}
	m.messages.SetReplyFormat(rf)
	// Seed the renderer's local-identity cache so ownership-based
	// UI works in friends-only mode (where UsersLoadedMsg never
	// runs and m.myUserID stays empty).
	m.messages.SetLocalIdentity(m.myUserID, m.cfg.SlackerID)

	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		splashTimerCmd(),
		checkUpdateCmd(m.version),
		loadFriendsCmd(m.friendStore, m.p2pNode),
	}
	// Workspace commands — only if Slack services are configured.
	if m.slackSvc != nil {
		cmds = append(cmds,
			loadUsersCmd(m.slackSvc),
			pollTickCmd(m.cfg.PollInterval),
			bgPollTickCmd(m.cfg.PollIntervalBg),
		)
	}
	if m.socketSvc != nil {
		cmds = append(cmds,
			connectSocketCmd(m.socketSvc, m.eventChan),
			waitForSocketEvent(m.eventChan),
		)
	}
	cmds = append(cmds, activityCheckCmd(m.cfg.AwayTimeout))
	cmds = append(cmds, notifyWatchdogCmd())
	if m.p2pChan != nil {
		cmds = append(cmds, waitForP2PMsg(m.p2pChan))
	}
	if m.friendStore != nil && m.p2pNode != nil {
		cmds = append(cmds, m.friendPingTickCmd())
		cmds = append(cmds, friendIdleCheckCmd())
	}
	return tea.Batch(cmds...)
}

// Update handles all messages and delegates to sub-models.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.settings.SetSize(msg.Width, msg.Height)
		m.resizeComponents()
		return m, nil

	case tea.MouseMsg:
		m.lastActivity = time.Now()
		if m.splash {
			return m, nil
		}
		// Pass mouse events to active overlays.
		if m.overlay != overlayNone {
			return m.handleOverlayMouse(msg)
		}
		return m.handleMouse(msg)

	case tea.KeyMsg:
		// Track activity for away detection.
		m.lastActivity = time.Now()
		clearWindowUrgent()
		// Refresh per-friend inactivity timer if the user is in a
		// friend chat — any keystroke counts as activity.
		if m.currentCh != nil && m.currentCh.IsFriend {
			m.touchFriendActivity(m.currentCh.UserID)
		}

		// Caps-lock tolerance: build a normalized copy of the key
		// where any plain ASCII uppercase rune (no Alt modifier) is
		// lowercased. We use the normalized version for shortcut
		// matching but keep the original around so text inputs
		// (friend profile fields, etc.) still receive capital
		// letters as typed.
		origMsg := msg
		msg = normalizeShortcutKey(msg)
		// Cheap key trace for diagnosing "this shortcut hides my
		// channel" type issues. Only active when debug logging is on.
		debug.Log("[key] %q (type=%d alt=%v)", msg.String(), msg.Type, msg.Alt)

		// When the shortcuts editor is capturing a key, bypass ALL other handlers.
		// This prevents quit, help, settings, etc. from firing during rebind.
		if m.overlay == overlayShortcuts && m.shortcutsEditor.IsCapturing() {
			var cmd tea.Cmd
			m.shortcutsEditor, cmd = m.shortcutsEditor.Update(msg)
			return m, cmd
		}

		// Quit must work from anywhere — checked before any overlay
		// early-return below.
		if key.Matches(msg, m.keymap.Quit) {
			clearWindowUrgent()
			// Flush any pending debounced config save so
			// last-second edits (theme change, shortcut
			// rebind, etc.) actually hit disk.
			config.FlushDebounced()
			// Same for the notifications store — mutations
			// self-schedule a debounced save, so a quit
			// within the 750 ms window would otherwise lose
			// the last change.
			if m.notifStore != nil {
				m.notifStore.FlushPending()
			}
			if m.p2pNode != nil {
				_ = m.p2pNode.Close()
			}
			return m, tea.Quit
		}

		// When the theme color picker is open, route every key through it
		// before any global shortcut so e.g. Ctrl-B doesn't open the
		// befriend dialog.
		if m.overlay == overlayThemeColorPicker {
			var cmd tea.Cmd
			m.themeColorPicker, cmd = m.themeColorPicker.Update(msg)
			return m, cmd
		}
		// Same precedence for the friends config overlay so Ctrl-J,
		// Ctrl-B, etc. reach its key handler instead of triggering the
		// global "select message" / "befriend" actions. The friends
		// config has text-input fields (Name, Email, etc.) so it
		// gets the *original* unnormalized key — otherwise typed
		// uppercase letters would be silently lowercased.
		if m.overlay == overlayFriendsConfig {
			var cmd tea.Cmd
			m.friendsConfig, cmd = m.friendsConfig.Update(origMsg)
			return m, cmd
		}

		// Pending message-delete confirmation: y/Enter confirm, anything else cancel.
		if m.pendingDeleteMsgID != "" {
			s := msg.String()
			if s == "y" || s == "Y" || s == "enter" {
				return m, m.confirmMessageDelete()
			}
			m.pendingDeleteMsgID = ""
			m.warning = "Delete cancelled"
			return m, nil
		}

		// Pending friend-card import/merge prompt: y=import, m=merge,
		// r=replace, anything else cancels.
		if m.pendingFriendCard != nil {
			s := strings.ToLower(msg.String())
			card := *m.pendingFriendCard
			m.pendingFriendCard = nil
			switch s {
			case "y", "enter":
				return m, m.applyFriendCard(card, false, false)
			case "m":
				return m, m.applyFriendCard(card, true, false)
			case "r":
				return m, m.applyFriendCard(card, false, true)
			default:
				m.warning = "Friend card prompt cancelled"
				return m, nil
			}
		}

		// Pending file-upload-cancel confirmation: y/Enter confirm.
		if m.pendingCancelUploadKey != "" {
			s := msg.String()
			if s == "y" || s == "Y" || s == "enter" {
				key := m.pendingCancelUploadKey
				m.pendingCancelUploadKey = ""
				m.cancelUpload(key)
				return m, nil
			}
			m.pendingCancelUploadKey = ""
			m.warning = ""
			return m, nil
		}

		// If returning from away, refresh the current channel only.
		// Socket Mode and regular polling will catch up on other channels.
		if m.isAway {
			m.isAway = false
			m.warning = ""
			if m.currentCh != nil {
				return m, silentLoadHistoryCmd(m.slackSvc, m.currentCh.ID)
			}
		}

		// Global shortcuts that work even in overlays. Quit is handled
		// above so it works even in friend-config / color-picker.
		switch {
		case key.Matches(msg, m.keymap.Help):
			if m.overlay == overlayHelp {
				m.overlay = overlayNone
			} else {
				m.help = NewHelpModel(m.version)
				m.help.SetSize(m.width, m.height)
				m.help.BuildLines(m.shortcutMap)
				m.overlay = overlayHelp
			}
			return m, nil

		case key.Matches(msg, m.keymap.ShareMyInfo):
			// Quick-insert: drops [FRIEND:me] at the cursor position
			// in the input. The send handler expands it to a full
			// SLF2 contact card hash so the recipient can decode it.
			m.input.InsertAtCursor("[FRIEND:me]")
			m.focus = types.FocusInput
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.Notifications):
			if m.overlay == overlayNotifications {
				m.overlay = overlayNone
			} else {
				m.notifs = NewNotificationsOverlay(m.notifStore.All())
				m.notifs.SetSize(m.width, m.height)
				m.overlay = overlayNotifications
			}
			return m, nil

		case key.Matches(msg, m.keymap.ShortcutsEditor):
			// Open the keyboard shortcuts editor directly — same
			// path the Settings overlay uses for its "Keyboard
			// Shortcuts" row.
			return m, func() tea.Msg {
				return ShortcutsEditorOpenMsg{}
			}

		case key.Matches(msg, m.keymap.Settings):
			if m.overlay == overlaySettings {
				m.overlay = overlayNone
				m.applySettings()
			} else {
				m.settings = NewSettingsModel(m.cfg, m.version)
				m.settings.SetSize(m.width, m.height)
				m.overlay = overlaySettings
			}
			return m, nil

		case key.Matches(msg, m.keymap.ToggleTheme):
			// Swap the active theme with the configured alternate so the user
			// can flip between, e.g., a dark and a light theme on the fly.
			if m.cfg.AltTheme != "" {
				newPrimary := m.cfg.AltTheme
				m.cfg.AltTheme = m.cfg.Theme
				m.cfg.Theme = newPrimary
				if t, ok := theme.FindByName(newPrimary); ok {
					ApplyTheme(t)
					m.messages.Refresh()
				}
				config.SaveDebounced(m.cfg)
				m.warning = "Switched to theme: " + newPrimary
			} else {
				m.warning = "No alternate theme set (configure in Settings → Appearance)"
			}
			return m, nil

		case key.Matches(msg, m.keymap.SearchMessages):
			if m.overlay == overlayMsgSearch {
				m.overlay = overlayNone
			} else {
				chID := ""
				if m.currentCh != nil {
					chID = m.currentCh.ID
				}
				m.msgSearch = NewMsgSearchModel(m.slackSvc, chID, m.resolveChannelDisplay)
				m.msgSearch.SetSize(m.width, m.height)
				m.overlay = overlayMsgSearch
			}
			return m, nil

		case key.Matches(msg, m.keymap.AttachFile):
			// Ctrl+U is shared with half-page-up in the viewport.
			// Only open file browser when messages pane is NOT focused.
			if m.focus == types.FocusMessages {
				break // fall through to viewport handler
			}
			if m.currentCh != nil {
				startDir := m.cfg.DownloadPath
				if startDir == "" {
					startDir, _ = os.UserHomeDir()
				}
				m.fileBrowser = NewFileBrowser(FileBrowserConfig{
					StartDir:    startDir,
					Title:       "Select File to Send",
					ShowFiles:   true,
					ShowFolders: true,
					Favorites:   m.cfg.FavoriteFolders,
				})
				m.fileBrowser.SetSize(m.width, m.height)
				m.fbPurpose = fbPurposeAttach
				m.overlay = overlayFileBrowser
			}
			return m, nil

		case key.Matches(msg, m.keymap.Search):
			if m.overlay == overlaySearch {
				m.overlay = overlayNone
			} else {
				m.search = NewSearchModel(m.channels.AllChannels(), m.cfg.ChannelAliases)
				m.search.SetSize(m.width, m.height)
				m.overlay = overlaySearch
			}
			return m, nil

		case key.Matches(msg, m.keymap.ShowHidden):
			if m.overlay == overlayHidden {
				m.overlay = overlayNone
			} else {
				m.hidden = NewHiddenChannelsModel(m.channels.HiddenChannelsList(), m.cfg.ChannelAliases)
				m.hidden.SetSize(m.width, m.height)
				m.overlay = overlayHidden
			}
			return m, nil

		case key.Matches(msg, m.keymap.EmojiPicker):
			m.emojiPicker = NewEmojiPicker(m.cfg.EmojiFavorites, EmojiPurposeInsert)
			m.emojiPicker.SetMouseEnabled(m.cfg.MouseEnabled)
			m.emojiPicker.SetSize(m.width, m.height)
			m.overlay = overlayEmojiPicker
			return m, nil

		case key.Matches(msg, m.keymap.SelectMessage):
			if m.currentCh != nil {
				m.focus = types.FocusMessages
				m.updateFocus()
				m.messages.EnterReactMode()
			}
			return m, nil

		case key.Matches(msg, m.keymap.FriendDetails):
			if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID != "" {
				friendID := m.currentCh.UserID
				return m, func() tea.Msg { return FriendsConfigOpenMsg{FriendID: friendID} }
			}
			return m, nil

		case key.Matches(msg, m.keymap.Befriend):
			if m.currentCh != nil && m.currentCh.IsDM && m.currentCh.UserID != "" {
				if m.friendStore != nil && m.friendStore.Get(m.currentCh.UserID) != nil {
					m.warning = "Already friends with " + m.currentCh.Name
					return m, nil
				}
				m.friendRequest = NewOutgoingFriendRequest(m.currentCh.UserID, m.currentCh.Name)
				m.friendRequest.SetSize(m.width, m.height)
				m.overlay = overlayFriendRequest
			} else {
				m.warning = "Select a DM channel to befriend"
			}
			return m, nil
		}

		// If an overlay is open, handle its input
		if m.overlay == overlayNotifications {
			var cmd tea.Cmd
			m.notifs, cmd = m.notifs.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayHelp {
			if msg.String() == "esc" {
				// First Esc clears any active filter; second Esc
				// closes the overlay.
				if m.help.SearchValue() != "" {
					m.help.ClearSearch()
					return m, nil
				}
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			return m, cmd
		}
		if m.overlay == overlaySettings {
			if msg.String() == "esc" && !m.settings.editing {
				m.overlay = overlayNone
				m.applySettings()
				return m, nil
			}
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			return m, cmd
		}
		if m.overlay == overlaySearch {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayHidden {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			// The hidden-channels overlay contains a filter
			// textinput, so route the *original* unnormalized
			// key so case-sensitive characters reach it as
			// typed (same reasoning as the friendsConfig path).
			var cmd tea.Cmd
			m.hidden, cmd = m.hidden.Update(origMsg)
			return m, cmd
		}
		if m.overlay == overlayRename {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			// The rename overlay contains a textinput — pass
			// origMsg so capital letters / mixed case aren't
			// silently lowercased by the global caps-tolerance
			// pass.
			var cmd tea.Cmd
			m.rename, cmd = m.rename.Update(origMsg)
			return m, cmd
		}
		if m.overlay == overlayMsgSearch {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			// MsgSearch also has a textinput.
			var cmd tea.Cmd
			m.msgSearch, cmd = m.msgSearch.Update(origMsg)
			return m, cmd
		}
		if m.overlay == overlayFileBrowser {
			// Let the browser handle Esc itself — it has two-stage
			// navigation (outer/sub-list) and only the outer pane
			// should close the modal.
			var cmd tea.Cmd
			m.fileBrowser, cmd = m.fileBrowser.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayFilesList {
			if msg.String() == "esc" || msg.String() == "ctrl+l" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.filesList, cmd = m.filesList.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayShortcuts {
			if msg.String() == "esc" && !m.shortcutsEditor.editing {
				m.overlay = overlayNone
				// Rebuild keymap from updated shortcuts.
				m.keymap = BuildKeyMap(m.shortcutsEditor.Merged())
				m.shortcutMap = m.shortcutsEditor.Merged()
				m.shortcutOverrides = m.shortcutsEditor.Overrides()
				return m, nil
			}
			// The editor has a filter textinput — route the
			// un-normalized key so capital letters reach it
			// as typed (same fix as FriendsConfig / Hidden /
			// Rename / MsgSearch).
			var cmd tea.Cmd
			m.shortcutsEditor, cmd = m.shortcutsEditor.Update(origMsg)
			return m, cmd
		}
		if m.overlay == overlayWhitelist {
			if msg.String() == "esc" && !m.whitelist.adding {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.whitelist, cmd = m.whitelist.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayFriendRequest {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.friendRequest, cmd = m.friendRequest.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayFriendsConfig {
			var cmd tea.Cmd
			m.friendsConfig, cmd = m.friendsConfig.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayAbout {
			var cmd tea.Cmd
			m.about, cmd = m.about.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayThemePicker {
			var cmd tea.Cmd
			m.themePicker, cmd = m.themePicker.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayThemeEditor {
			var cmd tea.Cmd
			m.themeEditor, cmd = m.themeEditor.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayThemeColorPicker {
			var cmd tea.Cmd
			m.themeColorPicker, cmd = m.themeColorPicker.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayEmojiPicker {
			if msg.String() == "esc" {
				// Persist favorite changes before closing.
				if m.emojiPicker.FavDirty() {
					m.cfg.EmojiFavorites = m.emojiPicker.Favorites()
					config.SaveDebounced(m.cfg)
				}
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.emojiPicker, cmd = m.emojiPicker.Update(msg)
			// Persist favorites immediately after every modification so they
			// survive even if the app exits abruptly.
			if m.emojiPicker.FavDirty() {
				m.cfg.EmojiFavorites = m.emojiPicker.Favorites()
				config.SaveDebounced(m.cfg)
				m.emojiPicker.ClearFavDirty()
			}
			return m, cmd
		}
		if m.overlay == overlayMsgOptions {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.msgOptions, cmd = m.msgOptions.Update(msg)
			return m, cmd
		}
		if m.overlay == overlaySidebarOptions {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.sidebarOptions, cmd = m.sidebarOptions.Update(msg)
			return m, cmd
		}

		// Normal key handling (no overlay)
		switch {
		case key.Matches(msg, m.keymap.Tab):
			m.cycleFocusForward()
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.ShiftTab):
			m.cycleFocusBackward()
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.Escape):
			// 1. Exit any active selection mode first.
			if m.messages.InReactMode() {
				m.messages.ExitReactMode()
				return m, nil
			}
			if m.messages.InSelectMode() {
				m.messages.ExitSelectMode()
				return m, nil
			}
			if m.messages.InThreadMode() {
				m.messages.ExitThreadMode()
				return m, nil
			}
			// 2. In input pane: first esc → cursor to start, second esc → save+clear.
			if m.focus == types.FocusInput {
				if m.input.Value() == "" {
					m.input.ClearEscapeOnce()
					return m, nil
				}
				// Treat the second esc as "save+clear" if either the
				// flag was set OR the cursor is already at the very top
				// of the textarea (defensive).
				if m.input.AtStart() || m.input.CursorAtStart() {
					prev := m.input.Value()
					m.input.PushHistory(prev)
					m.cfg.InputHistory = m.input.History()
					config.SaveDebounced(m.cfg)
					m.input.Reset()
					m.input.ClearEscapeOnce()
					m.resizeComponents()
					m.warning = "Draft saved to history"
				} else {
					m.input.CursorToStart()
					m.input.MarkEscapeOnce()
				}
				return m, nil
			}
			// 3. In sidebar or messages: focus the input.
			m.focus = types.FocusInput
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.ToggleInputMode):
			// Toggle input mode and focus the input bar.
			m.input.ToggleMode()
			m.focus = types.FocusInput
			m.updateFocus()
			if m.input.Mode() == InputModeEdit {
				return m, setWarning(&m, "Edit mode: Enter = new line, Alt+Enter = send")
			}
			return m, setWarning(&m, "Normal mode: Enter = send, Alt+Enter = new line")

		case key.Matches(msg, m.keymap.FocusInput):
			if m.focus != types.FocusInput {
				m.focus = types.FocusInput
				m.updateFocus()
				return m, nil
			}

		case key.Matches(msg, m.keymap.FilesList):
			if m.overlay == overlayFilesList {
				m.overlay = overlayNone
				return m, nil
			}
			chID := ""
			if m.currentCh != nil {
				chID = m.currentCh.ID
			}
			m.filesList = NewFilesListModel(m.slackSvc, chID, m.resolveChannelDisplay)
			m.filesList.SetSize(m.width, m.height)
			m.overlay = overlayFilesList
			// Default to current channel if viewing one, otherwise all.
			loadChID := chID
			if chID == "" {
				m.filesList.scopeAll = true
			}
			return m, loadFilesCmd(m.slackSvc, loadChID)

		case key.Matches(msg, m.keymap.CancelDownload):
			// Ctrl+D is shared with half-page-down in the viewport.
			// Only cancel download when one is active; otherwise fall through.
			if m.downloading && m.downloadCancel != nil {
				m.downloadCancel()
				m.downloading = false
				m.downloadCancel = nil
				m.warning = "Download cancelled"
				return m, nil
			}
			if m.focus == types.FocusMessages {
				break // fall through to viewport handler
			}

		case key.Matches(msg, m.keymap.ToggleFullMode):
			m.fullMode = !m.fullMode
			m.resizeComponents()
			return m, nil

		case key.Matches(msg, m.keymap.Refresh):
			return m, loadChannelsCmd(m.slackSvc)

		case key.Matches(msg, m.keymap.NextUnread):
			ch := m.channels.NextUnreadChannel()
			if ch != nil {
				if m.messages.InThreadMode() {
					m.messages.ExitThreadMode()
				}
				m.currentCh = ch
				m.channels.ClearUnread(ch.ID)
				m.clearChannelNotifs(ch.ID)
				m.setChannelHeader()
				m.saveLastChannel(ch.ID)
				// Friend channels load from local P2P history;
				// Slack channels need a working slack service.
				if ch.IsFriend {
					m.loadFriendHistory(ch.UserID)
					return m, nil
				}
				if m.slackSvc == nil {
					return m, nil
				}
				return m, loadHistoryCmd(m.slackSvc, ch.ID)
			}
			return m, nil

		case key.Matches(msg, m.keymap.ToggleHidden):
			m.channels.ToggleShowHidden()
			return m, nil

		case key.Matches(msg, m.keymap.HideChannel):
			if m.focus == types.FocusSidebar {
				ch := m.channels.SelectedChannel()
				if ch != nil {
					m.channels.HideChannel(ch.ID)
					m.cfg.HiddenChannels = m.channels.HiddenChannelIDs()
					if err := config.Save(m.cfg); err != nil {
						m.warning = "Failed to persist hidden channels: " + err.Error()
					}
					m.rebuildPollChannels()
				}
				return m, nil
			}

		case key.Matches(msg, m.keymap.RenameGroup):
			if m.focus == types.FocusSidebar {
				ch := m.channels.SelectedChannel()
				if ch != nil {
					currentAlias := ""
					if m.cfg.ChannelAliases != nil {
						currentAlias = m.cfg.ChannelAliases[ch.ID]
					}
					m.rename = NewRenameModel(ch.ID, ch.Name, currentAlias)
					m.rename.SetSize(m.width, m.height)
					m.overlay = overlayRename
					return m, nil
				}
			}

		case key.Matches(msg, m.keymap.FocusInputGlobal):
			m.messages.ExitSelectMode()
			m.focus = types.FocusInput
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.EnterFileSelect):
			// From input or anywhere: jump to messages and enter file select mode.
			if m.messages.EnterFileSelectMode() {
				m.focus = types.FocusMessages
				m.updateFocus()
			}
			return m, nil

		case key.Matches(msg, m.keymap.Enter):
			if m.focus == types.FocusSidebar {
				ch := m.channels.SelectedChannel()
				if ch != nil {
					if m.messages.InThreadMode() {
						m.messages.ExitThreadMode()
					}
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.clearChannelNotifs(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
					// Move focus to the input so the user can
					// start typing immediately after picking a
					// channel.
					m.focus = types.FocusInput
					m.updateFocus()

					// Friend channel — load local P2P message history.
					if ch.IsFriend {
						m.loadFriendHistory(ch.UserID)
						return m, nil
					}

					cmds := []tea.Cmd{loadHistoryCmd(m.slackSvc, ch.ID)}
					// Trigger peer discovery for whitelisted DM peers.
					if ch.IsDM && ch.UserID != "" && m.secureMgr != nil {
						if isWhitelisted(m.cfg.SecureWhitelist, ch.UserID) {
							cmds = append(cmds, discoverPeerCmd(m.secureMgr, ch.UserID))
						}
					}
					return m, tea.Batch(cmds...)
				}
				// Header selected — fall through to channel list Update for collapse toggle.
			}
			if m.focus == types.FocusInput {
				// Enter is handled by the input component — it sends InputSendMsg.
				// Fall through to delegate to input.Update.
			}
		}

		// Delegate to focused sub-model
		switch m.focus {
		case types.FocusSidebar:
			var cmd tea.Cmd
			m.channels, cmd = m.channels.Update(msg)
			cmds = append(cmds, cmd)
		case types.FocusMessages:
			var cmd tea.Cmd
			m.messages, cmd = m.messages.Update(msg)
			cmds = append(cmds, cmd)
		case types.FocusInput:
			prevHeight := m.input.DisplayHeight()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
			// Resize layout if input height changed.
			if m.input.DisplayHeight() != prevHeight {
				m.resizeComponents()
			}
		}

		return m, tea.Batch(cmds...)

	case MsgSearchResultsMsg:
		m.msgSearch, _ = m.msgSearch.Update(msg)
		return m, nil

	case MsgSearchSelectMsg:
		m.overlay = overlayNone
		channelName := ""
		if m.currentCh == nil || m.currentCh.ID != msg.ChannelID {
			for _, ch := range m.channels.AllChannels() {
				if ch.ID == msg.ChannelID {
					m.currentCh = &ch
					m.channels.SelectByID(ch.ID)
					m.channels.ClearUnread(ch.ID)
					m.clearChannelNotifs(ch.ID)
					channelName = "#" + m.channels.displayName(ch)
					m.saveLastChannel(ch.ID)
					break
				}
			}
		} else {
			channelName = m.messages.channelName
		}
		// Load context around the search result.
		ts := fmt.Sprintf("%d.%06d", msg.Timestamp.Unix(), msg.Timestamp.Nanosecond()/1000)
		return m, fetchContextCmd(m.slackSvc, msg.ChannelID, ts, channelName)

	case LoadMoreContextMsg:
		return m, loadMoreContextCmd(m.slackSvc, msg.ChannelID, msg.OldestTS)

	case MoreContextLoadedMsg:
		m.messages.PrependContextMessages(msg.Messages)
		return m, nil

	case ContextHistoryMsg:
		m.messages.SetContextMessages(msg.Messages, msg.TargetIdx, msg.ChannelName)
		m.focus = types.FocusMessages
		m.updateFocus()
		return m, nil

	case ToggleCollapseMsg:
		m.cfg.CollapsedGroups = m.channels.CollapsedGroups()
		config.SaveDebounced(m.cfg)
		return m, nil

	case SettingsSavedMsg:
		m.applySettings()
		return m, nil

	case SearchSelectMsg:
		m.overlay = overlayNone
		for _, ch := range m.channels.AllChannels() {
			if ch.ID == msg.ChannelID {
				m.currentCh = &ch
				m.channels.SelectByID(ch.ID)
				m.channels.ClearUnread(ch.ID)
				m.clearChannelNotifs(ch.ID)
				m.channels.UnhideChannel(ch.ID)
				m.setChannelHeader()
				m.saveLastChannel(ch.ID)
				// Friend channels load from local P2P history —
				// they have no Slack channel ID and the Slack
				// fetch path would error with channel_not_found.
				if ch.IsFriend {
					m.loadFriendHistory(ch.UserID)
					return m, nil
				}
				if m.slackSvc == nil {
					return m, nil
				}
				return m, loadHistoryCmd(m.slackSvc, ch.ID)
			}
		}
		return m, nil

	case UnhideChannelMsg:
		m.channels.UnhideChannel(msg.ChannelID)
		m.cfg.HiddenChannels = m.channels.HiddenChannelIDs()
		if err := config.Save(m.cfg); err != nil {
			m.warning = "Failed to persist hidden channels: " + err.Error()
		}
		m.rebuildPollChannels()
		if len(m.channels.HiddenChannelsList()) == 0 {
			m.overlay = overlayNone
		}
		return m, nil

	case RenameChannelMsg:
		m.overlay = overlayNone
		if m.cfg.ChannelAliases == nil {
			m.cfg.ChannelAliases = make(map[string]string)
		}
		if msg.Alias == "" {
			delete(m.cfg.ChannelAliases, msg.ChannelID)
		} else {
			m.cfg.ChannelAliases[msg.ChannelID] = msg.Alias
		}
		m.channels.SetAliases(m.cfg.ChannelAliases)
		m.buildChannelIndex()
		if err := config.Save(m.cfg); err != nil {
			m.warning = "Failed to persist channel rename: " + err.Error()
		}
		return m, nil

	case ChannelsLoadedMsg:
		// Coalesce the six setter calls below into a single
		// buildRows pass — previously each setter rebuilt the
		// sidebar independently, resulting in up to 6 full
		// rebuilds per ChannelsLoadedMsg on large workspaces.
		m.channels.BeginBulkUpdate()
		m.channels.SetChannels(msg.Channels)
		// Re-apply friend channels — SetChannels above replaces the entire
		// channel slice and would otherwise wipe the friends loaded earlier.
		m.channels.SetFriendChannels(m.buildFriendChannels())
		m.channels.SetHiddenChannels(m.cfg.HiddenChannels)
		m.channels.SetAliases(m.cfg.ChannelAliases)
		m.channels.SetCollapsedGroups(m.cfg.CollapsedGroups)
		m.buildChannelIndex()
		sortAsc := true
		if m.cfg.ChannelSortAsc != nil {
			sortAsc = *m.cfg.ChannelSortAsc
		}
		sortBy := m.cfg.ChannelSortBy
		if sortBy == "" {
			sortBy = SortByType
		}
		m.channels.SetSort(sortBy, sortAsc)
		m.channels.EndBulkUpdate()
		drainWarnings(&m)

		// SetChannels + buildRows resets the sidebar selection. If we
		// already restored a friend channel from FriendsLoadedMsg
		// (which fires before ChannelsLoadedMsg), re-apply the
		// selection so the sidebar shows it as active.
		if m.currentCh != nil {
			m.channels.SelectByID(m.currentCh.ID)
		}

		// Seed lastSeen for all channels.
		for _, ch := range msg.Channels {
			if _, ok := m.lastSeen[ch.ID]; !ok {
				m.lastSeen[ch.ID] = "0"
			}
		}
		m.rebuildPollChannels()

		// Restore last viewed channel on first load.
		var cmds []tea.Cmd
		if m.currentCh == nil && m.cfg.LastChannelID != "" {
			for _, ch := range msg.Channels {
				if ch.ID == m.cfg.LastChannelID {
					m.currentCh = &ch
					m.channels.SelectByID(ch.ID)
					m.setChannelHeader()
					cmds = append(cmds, loadHistoryCmd(m.slackSvc, ch.ID))
					break
				}
			}
		}

		// Run an initial seed poll to establish baseline timestamps
		// without marking anything as unread.
		cmds = append(cmds, seedLastSeenCmd(m.slackSvc, m.lastSeen))
		return m, tea.Batch(cmds...)

	case SilentHistoryMsg:
		msg.Messages = m.decryptMessages(msg.Messages)
		m.messages.SetMessagesSilent(msg.Messages)
		if m.currentCh != nil && len(msg.Messages) > 0 {
			latest := msg.Messages[len(msg.Messages)-1]
			m.lastSeen[m.currentCh.ID] = fmt.Sprintf("%d.%06d", latest.Timestamp.Unix(), latest.Timestamp.Nanosecond()/1000)
			m.persistLastSeen()
		}
		return m, nil

	case HistoryLoadedMsg:
		if msg.Messages != nil {
			msg.Messages = m.decryptMessages(msg.Messages)
			m.messages.SetMessages(msg.Messages)
		} else {
			m.messages.SetMessages(nil)
		}
		if m.currentCh != nil && len(msg.Messages) > 0 {
			latest := msg.Messages[len(msg.Messages)-1]
			m.lastSeen[m.currentCh.ID] = fmt.Sprintf("%d.%06d", latest.Timestamp.Unix(), latest.Timestamp.Nanosecond()/1000)
			m.persistLastSeen()
		}
		if m.initialLoad {
			m.initialLoad = false
		} else {
			m.focus = types.FocusInput
			m.updateFocus()
		}
		drainWarnings(&m)
		// Show error if history fetch failed, but channel is still open.
		if msg.Err != nil {
			return m, setError(&m, msg.Err)
		}
		return m, nil

	case UsersLoadedMsg:
		m.users = msg.Users
		userMap := make(map[string]string, len(msg.Users))
		for id, u := range msg.Users {
			name := u.DisplayName
			if name == "" {
				name = u.RealName
			}
			userMap[id] = name
		}
		m.messages.SetUsers(userMap)
		// Cache the local Slack user ID for reaction matching.
		if m.slackSvc != nil {
			if uid := m.slackSvc.MyUserID(); uid != "" {
				m.myUserID = uid
			}
		}
		// Push the cached identity into the message view so the
		// renderer can decide ownership-dependent UI bits (e.g.
		// hiding the "d: delete" hint on messages we didn't author)
		// without recomputing per render.
		m.messages.SetLocalIdentity(m.myUserID, m.cfg.SlackerID)
		drainWarnings(&m)
		// Now that users are cached, load channels so DM names resolve properly.
		return m, loadChannelsCmd(m.slackSvc)

	case MessageSentMsg:
		drainWarnings(&m)
		// Only reload history if socket is disconnected. When connected,
		// the socket event handler already appends the sent message.
		if m.connStatus != types.StatusConnected && m.currentCh != nil {
			debug.Log("[tui] MessageSent: socket disconnected, reloading history for %s", m.currentCh.ID)
			return m, loadHistoryCmd(m.slackSvc, m.currentCh.ID)
		}
		debug.Log("[tui] MessageSent: socket connected, skipping history reload")
		return m, nil

	case SlackEventMsg:
		switch msg.Event.Type {
		case "message":
			evMsg := msg.Event.Message
			ts := msg.Event.SlackTS
			debug.Log("[tui] socket message: channel=%s user=%s ts=%s", evMsg.ChannelID, evMsg.UserID, ts)
			// Update lastSeen for the current channel so polling doesn't
			// re-detect this message. For other channels, don't update
			// lastSeen (that would hide the unread flag).
			if m.currentCh != nil && evMsg.ChannelID == m.currentCh.ID {
				if ts != "" {
					m.lastSeen[evMsg.ChannelID] = ts
				}
				// Dedupe: remove any optimistic "pending-" copy of this message.
				m.messages.RemovePendingMatching(evMsg.Text)
				m.messages.AppendMessage(evMsg)
			} else {
				m.channels.MarkUnread(evMsg.ChannelID)
				// Record an unread-message notification.
				userName := evMsg.UserName
				if userName == "" {
					if u, ok := m.users[evMsg.UserID]; ok {
						userName = u.DisplayName
					}
				}
				m.recordUnreadMessage(evMsg.ChannelID, ts, evMsg.UserID, userName, evMsg.Text)
			}
		case "reaction_added":
			if m.currentCh != nil && msg.Event.ChannelID == m.currentCh.ID {
				m.messages.AddReactionLocal(msg.Event.TargetTS, msg.Event.EmojiName, msg.Event.ReactionUser)
			} else {
				// Reaction landed on a channel we're not viewing —
				// surface it as a notification.
				reactorName := msg.Event.ReactionUser
				if u, ok := m.users[msg.Event.ReactionUser]; ok {
					reactorName = u.DisplayName
				}
				m.recordReaction(msg.Event.ChannelID, msg.Event.TargetTS, msg.Event.ReactionUser, reactorName, msg.Event.EmojiName, "")
			}
		case "reaction_removed":
			if m.currentCh != nil && msg.Event.ChannelID == m.currentCh.ID {
				m.messages.RemoveReactionLocal(msg.Event.TargetTS, msg.Event.EmojiName, msg.Event.ReactionUser)
			}
		case "status":
			m.connStatus = msg.Event.Status
		}
		return m, waitForSocketEvent(m.eventChan)

	case ConnStatusMsg:
		m.connStatus = msg.Status
		m.connErr = msg.Err
		return m, nil

	case PollTickMsg:
		batch := make(map[string]string)

		// 1. Current channel — always poll for content refresh.
		if m.currentCh != nil {
			if ts, ok := m.lastSeen[m.currentCh.ID]; ok {
				batch[m.currentCh.ID] = ts
			}
			debug.Log("[poll] primary tick: current=%s socket=%v", m.currentCh.ID, m.connStatus == types.StatusConnected)
		}

		// 2. Priority channels — only when socket is NOT connected,
		//    since Socket Mode already provides real-time unread events.
		if m.connStatus != types.StatusConnected {
			priority := m.cfg.PollPriority
			if priority <= 0 {
				priority = 3
			}
			if len(m.pollChannels) > 0 {
				type chTS struct{ id, ts string }
				sorted := make([]chTS, 0, len(m.pollChannels))
				for _, id := range m.pollChannels {
					if _, already := batch[id]; already {
						continue
					}
					if ts, ok := m.lastSeen[id]; ok {
						sorted = append(sorted, chTS{id, ts})
					}
				}
				for i := 0; i < len(sorted); i++ {
					for j := i + 1; j < len(sorted); j++ {
						if sorted[j].ts > sorted[i].ts {
							sorted[i], sorted[j] = sorted[j], sorted[i]
						}
					}
				}
				for i := 0; i < priority && i < len(sorted); i++ {
					batch[sorted[i].id] = sorted[i].ts
				}
			}
		}

		if len(batch) == 0 {
			return m, pollTickCmd(m.cfg.PollInterval)
		}

		// Record check times.
		now := time.Now()
		for id := range batch {
			m.lastChecked[id] = now
		}

		return m, checkNewMessagesCmd(m.slackSvc, batch, m.cfg.PollInterval)

	case BgPollTickMsg:
		// Background rotation poll — safety net for catching missed socket events.
		// Runs at PollIntervalBg (default 30s), checks least-recently-polled channels.
		debug.Log("[poll] background tick")
		rotationSize := 5
		batch := make(map[string]string)

		if len(m.pollChannels) > 0 {
			type chCheck struct {
				id      string
				checked time.Time
			}
			unchecked := make([]chCheck, 0)
			for _, id := range m.pollChannels {
				// Skip current channel (handled by primary poll).
				if m.currentCh != nil && id == m.currentCh.ID {
					continue
				}
				checked := m.lastChecked[id]
				unchecked = append(unchecked, chCheck{id, checked})
			}
			// Sort by oldest-checked first.
			for i := 0; i < len(unchecked); i++ {
				for j := i + 1; j < len(unchecked); j++ {
					if unchecked[j].checked.Before(unchecked[i].checked) {
						unchecked[i], unchecked[j] = unchecked[j], unchecked[i]
					}
				}
			}
			for i := 0; i < rotationSize && i < len(unchecked); i++ {
				id := unchecked[i].id
				if ts, ok := m.lastSeen[id]; ok {
					batch[id] = ts
				}
			}
		}

		if len(batch) == 0 {
			return m, bgPollTickCmd(m.cfg.PollIntervalBg)
		}

		now := time.Now()
		for id := range batch {
			m.lastChecked[id] = now
		}

		return m, checkNewMessagesBgCmd(m.slackSvc, batch, m.cfg.PollIntervalBg)

	case UnreadChannelsMsg:
		if msg.LatestTS != nil {
			m.channels.SetLatestTimestamps(msg.LatestTS)
		}
		newUnread := 0
		refreshCurrent := false
		for _, id := range msg.ChannelIDs {
			if m.currentCh != nil && id == m.currentCh.ID {
				refreshCurrent = true
				continue
			}
			m.channels.MarkUnread(id)
			newUnread++
		}
		if newUnread > 0 && m.cfg.Notifications {
			sendNotification("multiple channels", newUnread)
			setWindowUrgent()
		}

		// Reschedule the correct timer based on which poll triggered this.
		nextTick := pollTickCmd(m.cfg.PollInterval)
		if msg.IsBackground {
			nextTick = bgPollTickCmd(m.cfg.PollIntervalBg)
		}

		if refreshCurrent && m.currentCh != nil && !m.messages.InContextMode() {
			return m, tea.Batch(
				nextTick,
				silentLoadHistoryCmd(m.slackSvc, m.currentCh.ID),
			)
		}
		return m, nextTick

	case FilesListLoadedMsg:
		m.filesList, _ = m.filesList.Update(msg)
		return m, nil

	case FilesListDownloadMsg:
		m.overlay = overlayNone
		downloadPath := m.cfg.DownloadPath
		if downloadPath == "" {
			home, _ := os.UserHomeDir()
			downloadPath = filepath.Join(home, "Downloads")
		}
		destPath := filepath.Join(downloadPath, msg.File.Name)
		m.warning = fmt.Sprintf("Downloading %s...", msg.File.Name)
		return m, m.startDownload(msg.File, destPath)

	case ShortcutsEditorOpenMsg:
		m.shortcutsEditor = NewShortcutsEditorModel(m.shortcutMap, m.shortcutOverrides, m.version)
		m.shortcutsEditor.SetSize(m.width, m.height)
		m.overlay = overlayShortcuts
		return m, nil

	case WhitelistOpenMsg:
		m.whitelist = NewWhitelistModel(m.cfg.SecureWhitelist, m.users)
		m.whitelist.SetSize(m.width, m.height)
		m.overlay = overlayWhitelist
		return m, nil

	case FriendsConfigOpenMsg:
		m.friendsConfig = NewFriendsConfigModel(m.friendStore, m.cfg)
		m.friendsConfig.SetSize(m.width, m.height)
		// Wire in the local public key + multiaddr so the Share My
		// Info card actually contains a connectable identity.
		var pubKey, multiaddr string
		if m.secureMgr != nil {
			pubKey = m.secureMgr.OwnPublicKeyBase64()
		}
		if m.p2pNode != nil {
			multiaddr = m.p2pNode.Multiaddr()
		}
		m.friendsConfig.SetIdentity(pubKey, multiaddr)
		m.friendsConfig.SetFriendHistory(m.friendHistory)
		// Allow the friends config to query the live P2P node for a
		// connected peer's current multiaddr (used for auto-fill).
		if m.p2pNode != nil {
			node := m.p2pNode
			m.friendsConfig.SetPeerMultiaddrLookup(func(uid string) string {
				return node.PeerMultiaddr(uid)
			})
		}
		if msg.FriendID != "" {
			m.friendsConfig.OpenEditFriend(msg.FriendID)
		}
		m.overlay = overlayFriendsConfig
		return m, nil

	case NotificationsCloseMsg:
		m.overlay = overlayNone
		return m, nil

	case NotificationActivateMsg:
		m.overlay = overlayNone
		return m, m.activateNotification(msg.Notif)

	case NotificationDeleteMsg:
		if m.notifStore != nil {
			m.notifStore.Remove(msg.NotifID)
		}
		// Refresh the overlay list in place.
		m.notifs.SetItems(m.notifStore.All())
		return m, nil

	case fcMessageClearMsg:
		// The friends-config status timer fires globally; route it
		// into the overlay so it can clear the matching message.
		var cmd tea.Cmd
		m.friendsConfig, cmd = m.friendsConfig.Update(msg)
		return m, cmd

	case FriendTestConnectionMsg:
		// Verify (or reestablish) a P2P connection to an existing
		// friend. Sets m.warning + the friends-config message so the
		// user sees feedback in both the status bar and inside the
		// friend edit pane.
		setBoth := func(s string) {
			m.warning = s
			m.friendsConfig.message = s
		}
		if m.friendStore == nil || m.p2pNode == nil || m.secureMgr == nil {
			setBoth("P2P not available")
			return m, nil
		}
		f := m.friendStore.Get(msg.FriendUserID)
		if f == nil {
			setBoth("Friend not found")
			return m, nil
		}
		if f.PublicKey == "" || f.Multiaddr == "" {
			setBoth("Public Key and Multiaddr must be set to test the connection")
			return m, nil
		}
		// Best-effort connect (resets the peerstore mapping if the
		// multiaddr changed).
		if err := m.p2pNode.ConnectToPeer(f.UserID, f.Multiaddr); err != nil {
			// libp2p refuses to dial its own peer ID. For
			// self-testing, fall back to a raw TCP socket dial
			// against the host:port from the multiaddr — this
			// confirms port forwarding works even though we
			// can't actually establish a libp2p session with
			// ourselves. Run a second instance to fully verify.
			if strings.Contains(err.Error(), "dial to self") || strings.Contains(err.Error(), "self attempted") {
				host, port := hostPortFromMultiaddr(f.Multiaddr)
				if host == "" {
					setBoth("Cannot self-dial via libp2p (this is your own peer ID). Run a second slackers instance with XDG_CONFIG_HOME=/tmp/slackers-test on a different P2P port to fully verify.")
					return m, nil
				}
				conn, derr := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 4*time.Second)
				if derr != nil {
					setBoth(fmt.Sprintf("libp2p won't self-dial; raw TCP %s:%d also failed: %v — port forwarding likely missing", host, port, derr))
					return m, nil
				}
				_ = conn.Close()
				setBoth(fmt.Sprintf("✓ Raw TCP reach OK on %s:%d. libp2p won't dial yourself — run a second instance to verify the full handshake.", host, port))
				return m, nil
			}
			setBoth("Connect failed: " + err.Error())
			m.friendStore.SetOnline(f.UserID, false)
			return m, nil
		}
		online := m.p2pNode.IsConnected(f.UserID)
		m.friendStore.SetOnline(f.UserID, online)
		if online {
			m.friendStore.UpdateLastOnline(f.UserID)
			m.channels.SetFriendChannels(m.buildFriendChannels())
			m.setChannelHeader()
			setBoth("✓ Connected to " + f.Name + " (online)")
			// On a save where the public key or multiaddr changed,
			// also re-run the friend-request handshake so the peer
			// rebinds to the new identity in their friend store.
			if msg.AlsoHandshake {
				go func(uid string) {
					req := secure.P2PMessage{
						Type:     secure.MsgTypeFriendRequest,
						Text:     m.secureMgr.OwnPublicKeyBase64() + "|" + m.p2pNode.Multiaddr(),
						SenderID: uid,
					}
					_ = m.p2pNode.SendMessage(uid, req)
				}(f.UserID)
			}
		} else {
			setBoth("Could not reach " + f.Name + " — peer is offline or unreachable")
		}
		return m, nil

	case FriendCardClickedMsg:
		// Decide based on whether the friend already exists in the
		// local store. New: confirm import. Existing: ask whether
		// to merge missing fields, replace, or cancel. Matching
		// uses SlackerID, PublicKey, or Multiaddr — so a re-shared
		// card under a different SlackerID still resolves to the
		// existing record.
		card := msg.Card
		card.Multiaddr = strings.TrimSpace(card.Multiaddr)
		// Self-check: clicking your own card shouldn't offer to
		// import it as a new friend. Detect by SlackerID,
		// PublicKey, or Multiaddr against the local identity.
		ownPub := ""
		if m.secureMgr != nil {
			ownPub = m.secureMgr.OwnPublicKeyBase64()
		}
		ownMaddr := ""
		if m.p2pNode != nil {
			ownMaddr = m.p2pNode.Multiaddr()
		}
		if (m.cfg != nil && card.SlackerID != "" && card.SlackerID == m.cfg.SlackerID) ||
			(ownPub != "" && card.PublicKey == ownPub) ||
			(ownMaddr != "" && card.Multiaddr == ownMaddr) {
			m.warning = "That's your own contact card — nothing to import."
			return m, nil
		}
		var existing *friends.Friend
		if m.friendStore != nil {
			existing = m.friendStore.FindByCard(card)
		}
		m.pendingFriendCard = &card
		name := friendCardLabel(card)
		if existing != nil {
			m.warning = fmt.Sprintf("Friend %s already exists. m=merge, r=replace, any other key=cancel", name)
		} else {
			m.warning = fmt.Sprintf("Add %s as a new friend? y=yes, any other key=cancel", name)
		}
		return m, nil

	case FriendChatHistoryClearedMsg:
		// The on-disk file was just wiped by the friends config
		// pane. Drop the in-memory cache and, if we're currently
		// viewing this friend's chat (or visit it later), reload
		// the now-empty history.
		uid := msg.FriendUserID
		delete(m.friendMessages, uid)
		if m.friendHistory != nil {
			pairKey := ""
			if m.friendStore != nil {
				if f := m.friendStore.Get(uid); f != nil {
					pairKey = f.PairKey
				}
			}
			fresh := m.friendHistory.GetDecrypted(uid, pairKey)
			m.friendMessages[uid] = fresh
			if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID == uid {
				m.messages.SetMessages(fresh)
			}
		}
		return m, nil

	case FriendAddedHandshakeMsg:
		// User just added a friend via Add a Friend. Try to connect
		// to their multiaddr and send a friend request handshake.
		// On a successful send, drop any pending friend-request
		// notification we had for that peer.
		if m.p2pNode == nil || m.secureMgr == nil {
			m.warning = "P2P not available — friend saved locally only"
			return m, nil
		}
		uid := msg.UserID
		multiaddr := msg.Multiaddr
		name := msg.Name
		go func() {
			// Best-effort connect (may already be connected).
			_ = m.p2pNode.ConnectToPeer(uid, multiaddr)
			req := secure.P2PMessage{
				Type:     secure.MsgTypeFriendRequest,
				Text:     m.secureMgr.OwnPublicKeyBase64() + "|" + m.p2pNode.Multiaddr(),
				SenderID: uid,
			}
			if err := m.p2pNode.SendMessage(uid, req); err == nil {
				if m.notifStore != nil {
					if m.notifStore.ClearFriendRequest(uid) > 0 {
					}
				}
			}
		}()
		m.warning = "Sent friend request to " + name
		return m, nil

	case FriendKeyRotateRequestMsg:
		// Initiate a key rotation with the selected friend. The friend
		// must be online — we send our new public key, store the
		// matching private key under pendingKeyRotation, and wait for
		// their ack to derive the new shared pair key.
		if m.friendStore == nil || m.p2pNode == nil {
			m.warning = "P2P not available"
			return m, nil
		}
		f := m.friendStore.Get(msg.FriendUserID)
		if f == nil {
			m.warning = "Friend not found"
			return m, nil
		}
		if !m.p2pNode.IsConnected(msg.FriendUserID) {
			m.warning = f.Name + " is offline — cannot rotate key until they come online"
			return m, nil
		}
		kp, err := secure.GenerateKeyPair()
		if err != nil {
			m.warning = "Key generation failed: " + err.Error()
			return m, nil
		}
		m.pendingKeyRotation[msg.FriendUserID] = kp.PrivateKey
		go m.p2pNode.SendMessage(msg.FriendUserID, secure.P2PMessage{
			Type:      secure.MsgTypeKeyRotate,
			Text:      kp.PublicKeyBase64(),
			SenderID:  msg.FriendUserID,
			Timestamp: time.Now().Unix(),
		})
		m.warning = "Rotating secure key with " + f.Name + "…"
		return m, nil

	case FriendsConfigCloseMsg:
		m.overlay = overlayNone
		// Refresh friend channels in sidebar after config changes.
		m.channels.SetFriendChannels(m.buildFriendChannels())
		return m, nil

	case FriendImportBrowseMsg:
		m.fileBrowser = NewFileBrowser(FileBrowserConfig{
			Title:       "Select friend contact card JSON",
			ShowFiles:   true,
			ShowFolders: true,
			FileTypes:   []string{".json"},
			Favorites:   m.cfg.FavoriteFolders,
		})
		m.fileBrowser.SetSize(m.width, m.height)
		m.fbPurpose = fbPurposeFriendImport
		m.overlay = overlayFileBrowser
		return m, nil

	case FriendImportFileMsg:
		cmd := m.friendsConfig.LoadContactCardFile(msg.Path)
		m.overlay = overlayFriendsConfig
		return m, cmd

	case FriendsImportBrowseMsg:
		m.fileBrowser = NewFileBrowser(FileBrowserConfig{
			Title:       "Select friends list JSON",
			ShowFiles:   true,
			ShowFolders: true,
			FileTypes:   []string{".json"},
			Favorites:   m.cfg.FavoriteFolders,
		})
		m.fileBrowser.SetSize(m.width, m.height)
		m.fbPurpose = fbPurposeFriendsImport
		m.overlay = overlayFileBrowser
		return m, nil

	case FriendsImportFileMsg:
		cmd := m.friendsConfig.LoadFriendsImportFile(msg.Path)
		m.overlay = overlayFriendsConfig
		return m, cmd

	case AboutOpenMsg:
		m.about = NewAboutModel(m.version)
		m.about.SetSize(m.width, m.height)
		m.overlay = overlayAbout
		return m, nil

	case AboutCloseMsg:
		m.overlay = overlayNone
		return m, nil

	case ThemePickerOpenMsg:
		m.themePicker = NewThemePicker(msg.ForAlt)
		m.themePicker.SetSize(m.width, m.height)
		m.overlay = overlayThemePicker
		return m, nil

	case ThemePickerCloseMsg:
		m.overlay = overlayNone
		return m, nil

	case ThemeImportBrowseMsg:
		m.fileBrowser = NewFileBrowser(FileBrowserConfig{
			Title:       "Select theme JSON to import",
			ShowFiles:   true,
			ShowFolders: true,
			FileTypes:   []string{".json"},
			Favorites:   m.cfg.FavoriteFolders,
		})
		m.fileBrowser.SetSize(m.width, m.height)
		m.fbPurpose = fbPurposeImportTheme
		m.overlay = overlayFileBrowser
		return m, nil

	case ThemeImportFileMsg:
		m.themePicker.BeginImport(msg.Path)
		m.overlay = overlayThemePicker
		return m, nil

	case ThemeAppliedMsg:
		// Persist the user's chosen theme — to either slot depending on
		// what the picker said it was selecting.
		if msg.ForAlt {
			m.cfg.AltTheme = msg.Name
			// Don't change the renderer — restore the primary theme so the
			// user keeps seeing what they were using.
			if t, ok := theme.FindByName(m.cfg.Theme); ok {
				ApplyTheme(t)
			}
		} else {
			m.cfg.Theme = msg.Name
		}
		config.SaveDebounced(m.cfg)
		m.messages.Refresh()
		return m, nil

	case ThemeEditorOpenMsg:
		m.themeEditor = NewThemeEditor(msg.Theme)
		m.themeEditor.SetSize(m.width, m.height)
		m.overlay = overlayThemeEditor
		return m, nil

	case ThemeEditorCloseMsg:
		// Return to the picker so the user can re-select / continue editing.
		m.themePicker.Refresh()
		m.messages.Refresh()
		m.overlay = overlayThemePicker
		return m, nil

	case ThemeEditorSavedMsg:
		// Editor saved a theme to disk; refresh any cached message lines so
		// the new colors apply immediately to the chat history without a
		// restart.
		m.messages.Refresh()
		return m, nil

	case ThemeColorPickerOpenMsg:
		m.themeEditor.BeginPreview(msg.Key)
		m.themeColorPicker = NewThemeColorPicker(msg.Key, msg.Initial)
		m.themeColorPicker.SetSize(m.width, m.height)
		m.overlay = overlayThemeColorPicker
		return m, nil

	case ThemeColorPreviewMsg:
		m.themeEditor.PreviewColor(msg.Color)
		m.messages.Refresh()
		return m, nil

	case ThemeColorPickerCloseMsg:
		// Cancelled — revert the editor's working theme to the original.
		m.themeEditor.EndPreview(false)
		m.messages.Refresh()
		m.overlay = overlayThemeEditor
		return m, nil

	case ThemeColorPickedMsg:
		// Committed — finalize and write the value into the working theme.
		m.themeEditor.EndPreview(true)
		m.themeEditor.SetColor(msg.Key, msg.Color)
		m.messages.Refresh()
		m.overlay = overlayThemeEditor
		return m, nil

	case MsgOptionsSelectMsg:
		m.overlay = overlayNone
		switch msg.Action {
		case MsgActionReact:
			m.reactMsgID = msg.MessageID
			m.emojiPicker = NewEmojiPicker(m.cfg.EmojiFavorites, EmojiPurposeReaction)
			m.emojiPicker.SetMouseEnabled(m.cfg.MouseEnabled)
			m.emojiPicker.SetSize(m.width, m.height)
			m.overlay = overlayEmojiPicker
		case MsgActionReply:
			return m, func() tea.Msg {
				return ReplyToMessageMsg{MessageID: msg.MessageID, Preview: msg.Preview}
			}
		case MsgActionEdit:
			return m, func() tea.Msg {
				return EditMessageRequestMsg{MessageID: msg.MessageID}
			}
		case MsgActionDelete:
			m.requestMessageDelete(msg.MessageID)
		}
		return m, nil

	case SidebarOptionsSelectMsg:
		m.overlay = overlayNone
		switch msg.Action {
		case SidebarActionHide:
			m.channels.HideChannel(msg.ChannelID)
			m.cfg.HiddenChannels = m.channels.HiddenChannelIDs()
			config.SaveDebounced(m.cfg)
			m.rebuildPollChannels()
			return m, nil
		case SidebarActionRename:
			// Open the rename overlay pre-loaded for this channel.
			chName := msg.ChannelID
			currentAlias := ""
			if m.cfg.ChannelAliases != nil {
				currentAlias = m.cfg.ChannelAliases[msg.ChannelID]
			}
			for _, ch := range m.channels.AllChannels() {
				if ch.ID == msg.ChannelID {
					chName = ch.Name
					break
				}
			}
			m.rename = NewRenameModel(msg.ChannelID, chName, currentAlias)
			m.rename.SetSize(m.width, m.height)
			m.overlay = overlayRename
			return m, nil
		case SidebarActionInvite:
			// Open the DM / group chat for this user and pre-fill
			// the input with a Slack-formatted invite message:
			//   - "Slackers" is a Slack-style hyperlink to the repo
			//     (mrkdwn syntax: <url|label>).
			//   - The contact card JSON is placed inside a code
			//     span on its own line so the recipient can
			//     cleanly copy it into their Slackers client's
			//     Add Friend → Paste screen without Slack
			//     auto-linkifying parts of the payload.
			//
			// The full JSON marker is baked in directly (not via
			// [FRIEND:me]) so expandFriendMarkers leaves it alone
			// and the text arrives on Slack with real ']'
			// characters instead of the \u005d escape the wire
			// format would normally use.
			for _, ch := range m.channels.AllChannels() {
				if ch.ID == msg.ChannelID {
					chCopy := ch
					m.currentCh = &chCopy
					m.channels.SelectByID(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
					break
				}
			}
			inviteText := buildSlackInviteMessage(m)
			m.input.Reset()
			m.input.InsertAtCursor(inviteText)
			m.focus = types.FocusInput
			m.updateFocus()
			// Load the Slack channel history so the chat view
			// reflects the switch.
			if m.currentCh != nil && !m.currentCh.IsFriend && m.slackSvc != nil {
				return m, loadHistoryCmd(m.slackSvc, m.currentCh.ID)
			}
			return m, nil
		case SidebarActionViewContact:
			friendID := msg.UserID
			if friendID == "" {
				friendID = strings.TrimPrefix(msg.ChannelID, "friend:")
			}
			if friendID == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return FriendsConfigOpenMsg{FriendID: friendID}
			}
		}
		return m, nil

	case ReplyToMessageMsg:
		if msg.MessageID != "" {
			replyText := fmt.Sprintf("[REPLY:%s] %q\n", msg.MessageID, msg.Preview)
			m.input.InsertAtCursor(replyText)
			m.focus = types.FocusInput
			m.updateFocus()
		}
		return m, nil

	case ThreadOpenedMsg:
		// Auto-focus the input so the user can immediately reply.
		m.focus = types.FocusInput
		m.updateFocus()
		// Pre-fill input with the reply syntax.
		if msg.MessageID != "" {
			m.input.InsertAtCursor(fmt.Sprintf("[REPLY:%s] ", msg.MessageID))
		}
		return m, nil

	case ToggleReactionMsg:
		m.toggleReaction(msg.MessageID, msg.Emoji)
		return m, nil

	case DeleteMessageRequestMsg:
		m.requestMessageDelete(msg.MessageID)
		return m, nil

	case EditMessageRequestMsg:
		mm := m.messages.MessageByID(msg.MessageID)
		if mm == nil {
			m.warning = "Message not found"
			return m, nil
		}
		if !m.isMyMessage(*mm) {
			m.warning = "You can only edit your own messages"
			return m, nil
		}
		// Pre-fill the input with the [EDIT:id] header followed by the
		// original message text verbatim. The send-handler strips the
		// header on submit.
		var pre strings.Builder
		pre.WriteString("[EDIT:" + mm.MessageID + "]\n")
		pre.WriteString(mm.Text)
		m.input.SetValue(pre.String())
		// Don't force edit mode — leave the input in whatever mode the user
		// already selected. They can still navigate the multi-line buffer
		// and use Enter (normal) or Alt-Enter (edit) to submit.
		m.focus = types.FocusInput
		m.updateFocus()
		return m, nil

	case ReactModeSelectMsg:
		if msg.MessageID != "" {
			m.reactMsgID = msg.MessageID
			m.emojiPicker = NewEmojiPicker(m.cfg.EmojiFavorites, EmojiPurposeReaction)
			m.emojiPicker.SetMouseEnabled(m.cfg.MouseEnabled)
			m.emojiPicker.SetSize(m.width, m.height)
			m.overlay = overlayEmojiPicker
		}
		return m, nil

	case EmojiSelectedMsg:
		m.overlay = overlayNone
		// Save favorites if changed.
		if m.emojiPicker.FavDirty() {
			m.cfg.EmojiFavorites = m.emojiPicker.Favorites()
			config.SaveDebounced(m.cfg)
		}
		switch msg.Purpose {
		case EmojiPurposeInsert:
			// Insert emoji into the text input at cursor.
			m.input.InsertAtCursor(msg.Emoji)
			m.focus = types.FocusInput
			m.updateFocus()
		case EmojiPurposeReaction:
			// Send reaction to the selected message.
			if m.currentCh != nil && m.reactMsgID != "" {
				if m.currentCh.IsFriend && m.p2pNode != nil {
					// P2P reaction.
					go m.p2pNode.SendMessage(m.currentCh.UserID, secure.P2PMessage{
						Type:          secure.MsgTypeReaction,
						TargetMsgID:   m.reactMsgID,
						ReactionEmoji: msg.Code,
						SenderID:      m.currentCh.UserID,
						Timestamp:     time.Now().Unix(),
					})
					// Update local history.
					if m.friendHistory != nil {
						peerUID := m.currentCh.UserID
						m.friendHistory.UpdateReaction(peerUID, m.reactMsgID, msg.Code, "me")
						go m.friendHistory.Save(peerUID)
					}
					// Update in-memory.
					m.addLocalReaction(m.currentCh.UserID, m.reactMsgID, msg.Code)
				} else if m.slackSvc != nil {
					// Slack reaction — optimistic local add, then API call.
					m.messages.AddReactionLocal(m.reactMsgID, msg.Code, "me")
					channelID := m.currentCh.ID
					targetTS := m.reactMsgID
					emoji := msg.Code
					go m.slackSvc.AddReaction(channelID, targetTS, emoji)
				}
				m.reactMsgID = ""
			}
		}
		return m, nil

	case ShortcutsSavedMsg:
		// Immediately rebuild keymap from the editor's current state.
		m.shortcutMap = m.shortcutsEditor.Merged()
		m.shortcutOverrides = m.shortcutsEditor.Overrides()
		m.keymap = BuildKeyMap(m.shortcutMap)
		return m, nil

	case SettingsOpenFileBrowserMsg:
		m.fileBrowser = NewFileBrowser(FileBrowserConfig{
			StartDir:    msg.CurrentPath,
			Title:       "Select Download Folder",
			ShowFiles:   false,
			ShowFolders: true,
			Favorites:   m.cfg.FavoriteFolders,
		})
		m.fileBrowser.SetSize(m.width, m.height)
		m.fbPurpose = fbPurposeSettings
		m.overlay = overlayFileBrowser
		return m, nil

	case FileBrowserFavoritesChangedMsg:
		// Persist the new favorites list to user config; keep the
		// browser open so the user can keep working with it.
		m.cfg.FavoriteFolders = msg.Favorites
		config.SaveDebounced(m.cfg)
		return m, nil

	case FileBrowserCancelMsg:
		m.overlay = overlayNone
		return m, nil

	case FileBrowserSelectMsg:
		m.overlay = overlayNone
		switch m.fbPurpose {
		case fbPurposeAttach:
			if !msg.IsDir {
				// Insert [FILE:<path>] at the cursor position.
				m.input.InsertAtCursor("[FILE:" + msg.Path + "]")
				m.focus = types.FocusInput
				m.updateFocus()
			}
		case fbPurposeSettings:
			if msg.IsDir {
				m.cfg.DownloadPath = msg.Path
				if err := config.Save(m.cfg); err != nil {
					m.warning = "Failed to persist download path: " + err.Error()
				}
				// Update the settings field value and reopen settings.
				m.settings = NewSettingsModel(m.cfg, m.version)
				m.settings.SetSize(m.width, m.height)
				m.overlay = overlaySettings
				return m, nil
			}
		case fbPurposeImportTheme:
			if !msg.IsDir {
				path := msg.Path
				return m, func() tea.Msg { return ThemeImportFileMsg{Path: path} }
			}
		case fbPurposeFriendImport:
			if !msg.IsDir {
				path := msg.Path
				return m, func() tea.Msg { return FriendImportFileMsg{Path: path} }
			}
		case fbPurposeFriendsImport:
			if !msg.IsDir {
				path := msg.Path
				return m, func() tea.Msg { return FriendsImportFileMsg{Path: path} }
			}
		}
		return m, nil

	case CancelUploadRequestMsg:
		// Queue a status-bar confirmation; same flow as click-on-uploading.
		msgID := m.messages.MessageIDForFile(msg.File.ID)
		if msgID != "" {
			m.pendingCancelUploadKey = msgID + "|" + msg.File.ID
			m.warning = fmt.Sprintf("Cancel upload of %s? [y/N]", msg.File.Name)
		}
		return m, nil

	case FileDownloadMsg:
		downloadPath := m.cfg.DownloadPath
		if downloadPath == "" {
			home, _ := os.UserHomeDir()
			downloadPath = filepath.Join(home, "Downloads")
		}
		destPath := filepath.Join(downloadPath, msg.File.Name)
		m.warning = fmt.Sprintf("Downloading %s...", msg.File.Name)

		// P2P file download — URL starts with p2p://
		if strings.HasPrefix(msg.File.URL, "p2p://") && m.p2pNode != nil {
			parts := strings.SplitN(strings.TrimPrefix(msg.File.URL, "p2p://"), "/", 2)
			if len(parts) == 2 {
				peerUID, fileID := parts[0], parts[1]
				return m, m.startP2PDownload(peerUID, fileID, destPath)
			}
		}

		return m, m.startDownload(msg.File, destPath)

	case FileDownloadCancelledMsg:
		m.downloading = false
		m.downloadCancel = nil
		m.warning = "Download cancelled"
		return m, clearWarningCmd()

	case FileDownloadCompleteMsg:
		m.downloading = false
		m.downloadCancel = nil
		if msg.Err != nil {
			m.err = msg.Err
			m.warning = ""
		} else {
			m.warning = fmt.Sprintf("Downloaded: %s", msg.DestPath)
		}
		return m, clearWarningCmd()

	case FileDownloadProgressMsg:
		pct := 0
		if msg.Total > 0 {
			pct = int(msg.Downloaded * 100 / msg.Total)
		}
		m.warning = fmt.Sprintf("Downloading %s... %d%%", msg.FileName, pct)
		return m, nil

	case FileUploadedMsg:
		// File uploads need a history reload since the file message
		// won't arrive via socket for the sender.
		if m.currentCh != nil {
			m.warning = fmt.Sprintf("Uploaded %d file(s)", msg.Count)
			return m, loadHistoryCmd(m.slackSvc, m.currentCh.ID)
		}
		return m, nil

	case FileUploadDoneMsg:
		key := msg.MessageID + "|" + msg.FileID
		delete(m.uploadCancels, key)
		if msg.Err != nil {
			// On error, remove the file from the message and warn.
			m.messages.RemoveFile(msg.MessageID, msg.FileID)
			m.warning = "Upload failed: " + msg.Err.Error()
			return m, nil
		}
		m.messages.SetFileUploaded(msg.MessageID, msg.FileID)
		return m, nil

	case InputSendMsg:
		text := msg.Text
		if text != "" && m.currentCh != nil {
			m.input.PushHistory(text)
			m.cfg.InputHistory = m.input.History()
			config.SaveDebounced(m.cfg)
			m.input.Reset()
			// The input bar may have been multi-line — after reset it
			// shrinks back to one row, so re-flow the panes so the messages
			// view grows to fill the freed space and the input stays pinned
			// to the bottom of the terminal.
			m.resizeComponents()

			// Expand any [FRIEND:me] / [FRIEND:<id>] markers to a
			// full SLF2 hash so the recipient can decode them.
			text = m.expandFriendMarkers(text)

			// Detect [EDIT:id] syntax and route to the edit handler. The
			// rest of the text (with each line possibly prefixed by "> ")
			// becomes the new message body.
			if em := editPattern.FindStringSubmatch(text); em != nil {
				editID := em[1]
				body := editPattern.ReplaceAllString(text, "")
				body = stripQuotePrefix(body)
				m.editMessage(editID, body)
				return m, nil
			}

			// Detect [REPLY:id] syntax and strip it.
			replyToID := ""
			if rm := replyPattern.FindStringSubmatch(text); rm != nil {
				replyToID = rm[1]
				text = strings.TrimSpace(replyPattern.ReplaceAllString(text, ""))
			}
			// If no explicit reply marker but the user is in thread view,
			// implicitly direct the message at the thread parent.
			if replyToID == "" && m.messages.InThreadMode() {
				replyToID = m.messages.ThreadParentID()
			}

			// Friend channel — send via P2P, not Slack.
			if m.currentCh.IsFriend && m.p2pNode != nil {
				peerUID := m.currentCh.UserID

				// Extract and share any [FILE:path] attachments.
				matches := filePattern.FindAllStringSubmatch(text, -1)
				cleanText := strings.TrimSpace(filePattern.ReplaceAllString(text, ""))
				var fileInfos []types.FileInfo
				for _, match := range matches {
					if len(match) < 2 {
						continue
					}
					path := match[1]
					fileID, err := m.p2pNode.ShareFile(path)
					if err != nil {
						debug.Log("[p2p] share file error: %v", err)
						continue
					}
					info, _ := os.Stat(path)
					fileName := filepath.Base(path)
					var size int64
					if info != nil {
						size = info.Size()
					}
					go m.p2pNode.SendFileOffer(peerUID, fileID, fileName, size)
					fileInfos = append(fileInfos, types.FileInfo{
						ID:        fileID,
						Name:      fileName,
						Size:      size,
						URL:       "p2p://" + peerUID + "/" + fileID,
						LocalPath: path,
						// Pending pickup by the peer. Stays uploading
						// until the peer requests the file (or the
						// user cancels).
						Uploading: true,
					})
				}

				// Generate the local id BEFORE sending so it can
				// travel in the wire payload — that way the
				// receiver stores the message under the same id and
				// can resolve replies/reactions/edits later.
				p2pLocalMsgID := generateMessageID()
				// Make sure we have a live libp2p connection to
				// the friend before firing off the send. This
				// covers the case where the connection dropped
				// since the last interaction (NAT eviction,
				// idle timeout, etc) — we re-dial just-in-time
				// instead of failing silently.
				m.connectFriend(peerUID)
				// Send text portion if any. The actual network
				// call happens inside a tea.Cmd so we can observe
				// its outcome via FriendSendResultMsg and flip
				// the history entry's Pending flag accordingly.
				var sendCmd tea.Cmd
				if cleanText != "" {
					friendMsg := secure.P2PMessage{
						Type:         secure.MsgTypeMessage,
						Text:         cleanText,
						SenderID:     peerUID,
						Timestamp:    time.Now().Unix(),
						MessageID:    p2pLocalMsgID,
						ReplyToMsgID: replyToID,
					}
					sendCmd = sendFriendMessageCmd(m.p2pNode, m.friendStore, peerUID, friendMsg)
				}

				// Show locally.
				displayText := text
				if cleanText == "" && len(fileInfos) > 0 {
					displayText = ""
				} else {
					displayText = cleanText
				}
				localMsg := types.Message{
					MessageID: p2pLocalMsgID,
					UserID:    "me",
					UserName:  "You",
					Text:      displayText,
					Timestamp: time.Now(),
					Files:     fileInfos,
					ReplyTo:   replyToID,
				}
				// Register each file in uploadCancels with a nil cancel
				// func — cancellation for P2P uploads is dispatched
				// through p2pNode.CancelFileOffer rather than a context.
				for _, fi := range fileInfos {
					m.uploadCancels[p2pLocalMsgID+"|"+fi.ID] = nil
				}

				// If reply, attach to parent in friend history.
				if replyToID != "" && m.friendHistory != nil {
					m.friendHistory.AppendReply(peerUID, replyToID, localMsg)
					go m.friendHistory.Save(peerUID)
					// Show the reply tree expanded immediately.
					m.messages.ExpandReplies(replyToID)
				} else {
					m.appendFriendMessage(peerUID, localMsg)
				}
				m.messages.AppendMessage(localMsg)
				// Refresh viewport to show the new reply tree.
				if replyToID != "" {
					pairKey := ""
					if f := m.friendStore.Get(peerUID); f != nil {
						pairKey = f.PairKey
					}
					msgs := m.friendHistory.GetDecrypted(peerUID, pairKey)
					m.friendMessages[peerUID] = msgs
					m.messages.SetMessages(msgs)
				}
				return m, sendCmd
			}

			// If secure mode is active and this is a DM with a whitelisted peer,
			// encrypt the message before sending.
			sendText := text
			if m.secureMgr != nil && m.currentCh.IsDM && m.currentCh.UserID != "" {
				if isWhitelisted(m.cfg.SecureWhitelist, m.currentCh.UserID) {
					encrypted, err := m.secureMgr.EncryptMessage(m.currentCh.UserID, text)
					if err == nil {
						sendText = encrypted
					}
				}
			}

			// Optimistic local append — show the message immediately.
			myName := "You"
			myID := "me"
			if u, ok := m.users[m.cfg.SlackerID]; ok && u.DisplayName != "" {
				myName = u.DisplayName
			}

			// Pull [FILE:path] markers out of the text so the local
			// message can render an "uploading…" placeholder per file
			// while the actual upload runs in the background.
			var slackFileInfos []types.FileInfo
			cleanSlackText := sendText
			if matches := filePattern.FindAllStringSubmatch(sendText, -1); len(matches) > 0 {
				cleanSlackText = strings.TrimSpace(filePattern.ReplaceAllString(sendText, ""))
				for _, match := range matches {
					if len(match) < 2 {
						continue
					}
					path := match[1]
					info, _ := os.Stat(path)
					var size int64
					if info != nil {
						size = info.Size()
					}
					slackFileInfos = append(slackFileInfos, types.FileInfo{
						ID:        generateMessageID(),
						Name:      filepath.Base(path),
						Size:      size,
						LocalPath: path,
						Uploading: true,
					})
				}
			}

			localMsgID := "pending-" + generateMessageID()
			localMsg := types.Message{
				MessageID: localMsgID,
				UserID:    myID,
				UserName:  myName,
				Text:      cleanSlackText,
				Timestamp: time.Now(),
				ChannelID: m.currentCh.ID,
				Files:     slackFileInfos,
			}
			m.messages.AppendMessage(localMsg)

			// Kick off background uploads for any files. Each upload
			// is tracked by "<msgID>|<fileID>" so the user can cancel
			// it from the status-bar prompt or the file-select shortcut.
			var uploadCmds []tea.Cmd
			for _, fi := range slackFileInfos {
				ctx, cancel := context.WithCancel(context.Background())
				m.uploadCancels[localMsgID+"|"+fi.ID] = cancel
				path := fi.LocalPath
				fileID := fi.ID
				channelID := m.currentCh.ID
				svc := m.slackSvc
				uploadCmds = append(uploadCmds, func() tea.Msg {
					done := make(chan error, 1)
					go func() {
						done <- svc.UploadFile(channelID, path)
					}()
					select {
					case err := <-done:
						return FileUploadDoneMsg{MessageID: localMsgID, FileID: fileID, Err: err}
					case <-ctx.Done():
						return FileUploadDoneMsg{MessageID: localMsgID, FileID: fileID, Err: ctx.Err()}
					}
				})
			}

			// Slack thread reply if reply ID set.
			if replyToID != "" && m.slackSvc != nil {
				channelID := m.currentCh.ID
				cmds := []tea.Cmd{
					func() tea.Msg {
						_ = m.slackSvc.SendThreadReply(channelID, replyToID, cleanSlackText)
						return nil
					},
					silentLoadHistoryCmd(m.slackSvc, channelID),
				}
				cmds = append(cmds, uploadCmds...)
				return m, tea.Batch(cmds...)
			}
			// No files: original synchronous-text path is fine.
			if len(slackFileInfos) == 0 {
				return m, sendMessageWithFilesCmd(m.slackSvc, m.currentCh.ID, sendText)
			}
			// Has files: send the text only (no [FILE:] markers any
			// more), and run uploads in parallel.
			cmds := make([]tea.Cmd, 0, 1+len(uploadCmds))
			if cleanSlackText != "" {
				channelID := m.currentCh.ID
				cmds = append(cmds, func() tea.Msg {
					if err := m.slackSvc.SendMessage(channelID, cleanSlackText); err != nil {
						return ErrMsg{Err: err}
					}
					return nil
				})
			}
			cmds = append(cmds, uploadCmds...)
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case SeedLastSeenMsg:
		// Establish baseline timestamps without marking anything as unread.
		for id, ts := range msg.Timestamps {
			if ts != "" {
				m.lastSeen[id] = ts
			}
		}
		m.channels.SetLatestTimestamps(msg.Timestamps)
		m.persistLastSeen()
		return m, nil

	case NotifyWatchdogMsg:
		// Age out any status warning that's exceeded the configured
		// notification timeout. The first time a new warning is seen
		// (different from prevWarning) we record warningSetAt; the
		// next tick (or one after) clears it once stale.
		if m.warning != "" {
			if m.warning != m.prevWarning {
				m.warningSetAt = time.Now()
				m.prevWarning = m.warning
			} else if !m.warningSetAt.IsZero() && time.Since(m.warningSetAt) >= m.cfg.NotificationTTL() {
				m.warning = ""
				m.warningSetAt = time.Time{}
				m.prevWarning = ""
			}
		} else {
			m.prevWarning = ""
			m.warningSetAt = time.Time{}
		}
		return m, notifyWatchdogCmd()

	case ClearWarningMsg:
		if m.warning == "" && m.err == nil {
			return m, nil
		}
		if time.Since(m.lastActivity) < 5*time.Second {
			m.warning = ""
			m.err = nil
			return m, nil
		}
		return m, clearWarningCmd()

	case ActivityCheckMsg:
		awayTimeout := m.cfg.AwayTimeout
		if awayTimeout > 0 {
			elapsed := time.Since(m.lastActivity)
			if !m.isAway && elapsed >= time.Duration(awayTimeout)*time.Second {
				m.isAway = true
				m.warning = "Away (idle)"
			}
		}
		return m, activityCheckCmd(m.cfg.AwayTimeout)

	case UpdateAvailableMsg:
		if msg.Version != "" {
			m.warning = fmt.Sprintf("Update available: %s (run 'slackers update')", msg.Version)
			return m, tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
				return ClearWarningMsg{}
			})
		}
		return m, nil

	case SplashDoneMsg:
		m.splash = false
		return m, nil

	case P2PReceivedMsg:
		// Handle disconnect notifications.
		if msg.Text == "__disconnect__" {
			if m.friendStore != nil {
				m.friendStore.SetOnline(msg.SenderID, false)
				m.friendStore.UpdateLastOnline(msg.SenderID)
				m.channels.ClearUnread("friend:" + msg.SenderID)
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle incoming delete request: peer wants to delete one of THEIR messages.
		// libp2p has already authenticated the sender at the transport layer, so
		// we just verify the target message exists and belongs to a known friend
		// before applying the delete + acking back.
		if msg.Text == "__delete__" {
			targetMsgID := msg.PubKey
			senderID := msg.SenderID
			if m.friendStore == nil || m.friendStore.Get(senderID) == nil {
				if m.p2pChan != nil {
					return m, waitForP2PMsg(m.p2pChan)
				}
				return m, nil
			}
			// Use the persistent history as the source of truth so
			// the lookup works even if we haven't opened the chat
			// in this session yet.
			pairKey := ""
			if f := m.friendStore.Get(senderID); f != nil {
				pairKey = f.PairKey
			}
			var msgs []types.Message
			if m.friendHistory != nil {
				msgs = m.friendHistory.GetDecrypted(senderID, pairKey)
			}
			if msgs == nil {
				msgs = m.friendMessages[senderID]
			}
			found := findFriendMsgPtr(msgs, targetMsgID) != nil
			if !found {
				debug.Log("[p2p] delete request from %s for %s — not found", senderID, targetMsgID)
				if m.p2pChan != nil {
					return m, waitForP2PMsg(m.p2pChan)
				}
				return m, nil
			}
			if m.friendHistory != nil {
				m.friendHistory.DeleteMessage(senderID, targetMsgID)
				go m.friendHistory.Save(senderID)
			}
			if cached, ok := m.friendMessages[senderID]; ok {
				m.friendMessages[senderID] = deleteFriendMessage(cached, targetMsgID)
			}
			// Refresh visible chat if we're looking at this friend.
			friendChID := "friend:" + senderID
			if m.currentCh != nil && m.currentCh.ID == friendChID {
				m.messages.DeleteMessageLocal(targetMsgID)
			}
			// Ack back to the initiator.
			if m.p2pNode != nil {
				go m.p2pNode.SendMessage(senderID, secure.P2PMessage{
					Type:        secure.MsgTypeDeleteAck,
					TargetMsgID: targetMsgID,
					SenderID:    senderID,
					Timestamp:   time.Now().Unix(),
				})
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		if msg.Text == "__delete_ack__" {
			// Peer confirmed they deleted the message; we already deleted optimistically
			// when the user requested it, so this is just an acknowledgment we can log.
			debug.Log("[p2p] received delete ack for %s from %s", msg.PubKey, msg.SenderID)
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle incoming edit request: peer wants to edit one of their own
		// messages. Look up via the persistent history so it works
		// even if we haven't opened the chat in this session yet.
		if msg.Text == "__edit__" {
			targetMsgID := msg.PubKey
			newText := msg.Multiaddr
			senderID := msg.SenderID
			if m.friendStore == nil || m.friendStore.Get(senderID) == nil {
				if m.p2pChan != nil {
					return m, waitForP2PMsg(m.p2pChan)
				}
				return m, nil
			}
			pairKey := ""
			if f := m.friendStore.Get(senderID); f != nil {
				pairKey = f.PairKey
			}
			var msgs []types.Message
			if m.friendHistory != nil {
				msgs = m.friendHistory.GetDecrypted(senderID, pairKey)
			}
			if msgs == nil {
				msgs = m.friendMessages[senderID]
			}
			if findFriendMsgPtr(msgs, targetMsgID) == nil {
				debug.Log("[p2p] edit request from %s for %s — not found", senderID, targetMsgID)
				if m.p2pChan != nil {
					return m, waitForP2PMsg(m.p2pChan)
				}
				return m, nil
			}
			if m.friendHistory != nil {
				m.friendHistory.EditMessage(senderID, targetMsgID, newText)
				go m.friendHistory.Save(senderID)
			}
			if cached, ok := m.friendMessages[senderID]; ok {
				if target := findFriendMsgPtr(cached, targetMsgID); target != nil {
					target.Text = newText
				}
			}
			friendChID := "friend:" + senderID
			if m.currentCh != nil && m.currentCh.ID == friendChID {
				m.messages.EditMessageLocal(targetMsgID, newText)
			}
			if m.p2pNode != nil {
				go m.p2pNode.SendMessage(senderID, secure.P2PMessage{
					Type:        secure.MsgTypeEditAck,
					TargetMsgID: targetMsgID,
					SenderID:    senderID,
					Timestamp:   time.Now().Unix(),
				})
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		if msg.Text == "__edit_ack__" {
			debug.Log("[p2p] received edit ack for %s from %s", msg.PubKey, msg.SenderID)
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle incoming reactions.
		if msg.Text == "__reaction_remove__" {
			targetMsgID := msg.PubKey
			emoji := msg.Multiaddr
			if m.friendHistory != nil {
				m.friendHistory.RemoveReaction(msg.SenderID, targetMsgID, emoji, msg.SenderID)
				go m.friendHistory.Save(msg.SenderID)
			}
			friendChID := "friend:" + msg.SenderID
			if m.currentCh != nil && m.currentCh.ID == friendChID {
				m.messages.RemoveReactionLocal(targetMsgID, emoji, msg.SenderID)
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		if msg.Text == "__reaction__" {
			targetMsgID := msg.PubKey
			emoji := msg.Multiaddr
			if m.friendHistory != nil {
				m.friendHistory.UpdateReaction(msg.SenderID, targetMsgID, emoji, msg.SenderID)
				go m.friendHistory.Save(msg.SenderID)
			}
			// Update in-memory cache too (walks nested replies).
			if msgs, ok := m.friendMessages[msg.SenderID]; ok {
				if target := findFriendMsgPtr(msgs, targetMsgID); target != nil {
					found := false
					for j, r := range target.Reactions {
						if r.Emoji == emoji {
							target.Reactions[j].Count++
							target.Reactions[j].UserIDs = append(target.Reactions[j].UserIDs, msg.SenderID)
							found = true
							break
						}
					}
					if !found {
						target.Reactions = append(target.Reactions, types.Reaction{
							Emoji: emoji, UserIDs: []string{msg.SenderID}, Count: 1,
						})
					}
				}
				// Refresh view if we're looking at this channel.
				friendChID := "friend:" + msg.SenderID
				if m.currentCh != nil && m.currentCh.ID == friendChID {
					m.messages.SetMessages(msgs)
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Local notification: a peer just finished downloading one of
		// our shared files. Flip the matching file's uploading flag.
		if msg.Text == "__file_served__" {
			fileID := msg.PubKey
			// Walk all friend message caches looking for the file.
			for uid, msgs := range m.friendMessages {
				updated := false
				for i := range msgs {
					for fi := range msgs[i].Files {
						if msgs[i].Files[fi].ID == fileID && msgs[i].Files[fi].Uploading {
							msgs[i].Files[fi].Uploading = false
							updated = true
						}
					}
				}
				if updated {
					m.friendMessages[uid] = msgs
					friendChID := "friend:" + uid
					if m.currentCh != nil && m.currentCh.ID == friendChID {
						m.messages.SetMessages(msgs)
					}
					if m.friendHistory != nil {
						go m.friendHistory.Save(uid)
					}
				}
			}
			// Drop any cancel registration.
			for k := range m.uploadCancels {
				if strings.HasSuffix(k, "|"+fileID) {
					delete(m.uploadCancels, k)
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Incoming key rotation request from a friend. Auto-accept,
		// derive a new pair key, store the friend's new public key,
		// and reply with our new public key for symmetry.
		if msg.Text == "__key_rotate__" {
			if m.friendStore != nil && m.secureMgr != nil && m.p2pNode != nil {
				newPub := msg.PubKey
				if newPub != "" {
					if f := m.friendStore.Get(msg.SenderID); f != nil {
						// Generate our own fresh keypair just for
						// this friendship's pair key derivation.
						kp, err := secure.GenerateKeyPair()
						if err == nil {
							peerKey, perr := secure.PublicKeyFromBase64(newPub)
							if perr == nil {
								shared, serr := secure.ComputeSharedSecret(kp.PrivateKey, peerKey)
								if serr == nil {
									f.PublicKey = newPub
									f.PairKey = base64.StdEncoding.EncodeToString(shared[:])
									_ = m.friendStore.Update(*f)
									_ = m.friendStore.Save()
									// Reply with our new public key.
									go m.p2pNode.SendMessage(msg.SenderID, secure.P2PMessage{
										Type:      secure.MsgTypeKeyRotateAck,
										Text:      kp.PublicKeyBase64(),
										SenderID:  msg.SenderID,
										Timestamp: time.Now().Unix(),
									})
									m.warning = f.Name + " rotated their secure key"
								}
							}
						}
					}
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Ack from the friend completing a rotation we initiated.
		if msg.Text == "__key_rotate_ack__" {
			if m.friendStore != nil {
				if f := m.friendStore.Get(msg.SenderID); f != nil {
					// We stored our pending private key under f.UserID
					// in pendingKeyRotation when we initiated. Apply
					// it against the peer's new public key now.
					if priv, ok := m.pendingKeyRotation[msg.SenderID]; ok {
						peerKey, perr := secure.PublicKeyFromBase64(msg.PubKey)
						if perr == nil {
							shared, serr := secure.ComputeSharedSecret(priv, peerKey)
							if serr == nil {
								f.PublicKey = msg.PubKey
								f.PairKey = base64.StdEncoding.EncodeToString(shared[:])
								_ = m.friendStore.Update(*f)
								_ = m.friendStore.Save()
								m.warning = "Key rotation complete with " + f.Name
							}
						}
						delete(m.pendingKeyRotation, msg.SenderID)
					}
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle incoming file cancel: the sender revoked an offer.
		// Walk our cached friend messages and remove the matching file.
		if msg.Text == "__file_cancel__" {
			fileID := msg.PubKey
			if msgs, ok := m.friendMessages[msg.SenderID]; ok {
				for i := range msgs {
					for fi := range msgs[i].Files {
						if msgs[i].Files[fi].ID == fileID {
							msgs[i].Files = append(msgs[i].Files[:fi], msgs[i].Files[fi+1:]...)
							break
						}
					}
				}
				m.friendMessages[msg.SenderID] = msgs
				friendChID := "friend:" + msg.SenderID
				if m.currentCh != nil && m.currentCh.ID == friendChID {
					m.messages.SetMessages(msgs)
				}
				if m.friendHistory != nil {
					go m.friendHistory.Save(msg.SenderID)
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle incoming file offers.
		if msg.Text == "__file_offer__" {
			fileID := msg.PubKey
			parts := strings.SplitN(msg.Multiaddr, "|", 2)
			fileName := fileID
			var fileSize int64
			if len(parts) == 2 {
				fileName = parts[0]
				fileSize, _ = strconv.ParseInt(parts[1], 10, 64)
			}

			userName := msg.SenderID
			if u, ok := m.users[msg.SenderID]; ok {
				userName = u.DisplayName
			}

			fileMsg := types.Message{
				MessageID: generateMessageID(),
				UserID:    msg.SenderID,
				UserName:  userName,
				Text:      "",
				Timestamp: time.Now(),
				Files: []types.FileInfo{{
					ID:   fileID,
					Name: fileName,
					Size: fileSize,
					URL:  "p2p://" + msg.SenderID + "/" + fileID,
				}},
			}

			friendChID := "friend:" + msg.SenderID
			if m.currentCh != nil && m.currentCh.ID == friendChID {
				m.messages.AppendMessage(fileMsg)
			} else {
				m.channels.MarkUnread(friendChID)
			}
			m.appendFriendMessage(msg.SenderID, fileMsg)

			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Handle friend request messages.
		if msg.Text == "__friend_request__" {
			senderName := msg.SenderID
			if u, ok := m.users[msg.SenderID]; ok {
				senderName = u.DisplayName
			}
			// Auto-accept path: silently add the friend, send the
			// accept response, and surface a status bar message.
			// No notification is recorded (the connection is done).
			if m.cfg != nil && m.cfg.AutoAcceptFriendRequests && m.friendStore != nil {
				f := friends.Friend{
					UserID:    msg.SenderID,
					Name:      senderName,
					PublicKey: msg.PubKey,
					Multiaddr: msg.Multiaddr,
					Online:    true,
				}
				_ = m.friendStore.Add(f)
				_ = m.friendStore.Save()
				m.channels.SetFriendChannels(m.buildFriendChannels())
				if m.p2pNode != nil && m.secureMgr != nil {
					go func(uid string) {
						resp := secure.P2PMessage{
							Type:     secure.MsgTypeFriendAccept,
							Text:     m.secureMgr.OwnPublicKeyBase64() + "|" + m.p2pNode.Multiaddr(),
							SenderID: uid,
						}
						_ = m.p2pNode.SendMessage(uid, resp)
					}(msg.SenderID)
				}
				m.warning = "Auto-accepted friend request from " + senderName
				if m.p2pChan != nil {
					return m, waitForP2PMsg(m.p2pChan)
				}
				return m, nil
			}
			// Manual path: cache as a notification first so the user
			// can find it later from the notifications view even if
			// they dismiss the immediate modal.
			m.recordFriendRequest(msg.SenderID, senderName, msg.PubKey, msg.Multiaddr)
			m.friendRequest = NewIncomingFriendRequest(msg.SenderID, senderName, msg.PubKey, msg.Multiaddr)
			m.friendRequest.SetSize(m.width, m.height)
			m.overlay = overlayFriendRequest
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}
		if msg.Text == "__friend_accept__" {
			// Peer accepted our friend request — add them.
			if m.friendStore != nil {
				senderName := msg.SenderID
				if u, ok := m.users[msg.SenderID]; ok {
					senderName = u.DisplayName
				}
				f := friends.Friend{
					UserID:    msg.SenderID,
					Name:      senderName,
					PublicKey: msg.PubKey,
					Multiaddr: msg.Multiaddr,
					Online:    true,
				}
				_ = m.friendStore.Add(f)
				_ = m.friendStore.Save()
				m.channels.SetFriendChannels(m.buildFriendChannels())
				m.warning = senderName + " accepted your friend request!"
				// Drop any pending friend-request notification.
				if m.notifStore != nil {
					if m.notifStore.ClearFriendRequest(msg.SenderID) > 0 {
					}
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Profile sync announcement from a peer. The Multiaddr
		// field carries the full contact card JSON; merge any
		// fresh fields into the matching stored friend record
		// without overwriting the locally-chosen display name.
		if msg.Text == "__profile_sync__" {
			if m.friendStore != nil && msg.Multiaddr != "" {
				var card friends.ContactCard
				if err := json.Unmarshal([]byte(msg.Multiaddr), &card); err == nil {
					m.mergeFriendProfile(msg.SenderID, card)
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		// Pending-message request from a peer. They just came up
		// and are asking us to scan our history for anything
		// addressed to them that's still flagged Pending so we
		// can redeliver it in order. This is the primary
		// recovery path when the local ping-cycle misses the
		// transition on our side.
		if msg.Text == "__request_pending__" {
			resend := m.resendPendingFriendMessagesCmd(msg.SenderID)
			if m.p2pChan != nil {
				if resend != nil {
					return m, tea.Batch(resend, waitForP2PMsg(m.p2pChan))
				}
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, resend
		}

		// Regular P2P message — display in current channel or friend channel.
		// Prefer the locally-saved friend name (the one shown in the
		// sidebar), then the Slack workspace user display name, then
		// the email, and finally fall back to the raw sender ID.
		userName := msg.SenderID
		if m.friendStore != nil {
			if f := m.friendStore.Get(msg.SenderID); f != nil {
				switch {
				case f.Name != "":
					userName = f.Name
				case f.Email != "":
					userName = f.Email
				}
			}
		}
		if userName == msg.SenderID {
			if u, ok := m.users[msg.SenderID]; ok && u.DisplayName != "" {
				userName = u.DisplayName
			}
		}
		// Every message — incoming or outgoing — needs a MessageID
		// so reply / react / delete can target it. Prefer the
		// sender's id (passed in MsgID via the wire payload) so
		// both sides agree on which message a reply / reaction
		// targets; fall back to a fresh local id only when an
		// older sender didn't include one.
		incomingID := msg.MsgID
		if incomingID == "" {
			incomingID = generateMessageID()
		}
		// Prefer the sender's original send timestamp when it's
		// available — this keeps re-sent pending messages in
		// correct chronological order instead of bunching them
		// up at the reconnect time.
		msgTime := time.Now()
		if msg.SentAt > 0 {
			msgTime = time.Unix(msg.SentAt, 0)
		}
		p2pMsg := types.Message{
			MessageID: incomingID,
			UserID:    msg.SenderID,
			UserName:  userName,
			Text:      msg.Text,
			Timestamp: msgTime,
			ReplyTo:   msg.ReplyToID,
		}

		// If the sender flagged this as a reply, attach it to the
		// parent message in our local store instead of appending it
		// at the top level. The sender embeds their local parent id
		// in ReplyToMsgID, and we now use the sender's id when
		// storing each message — so the parent lookup matches
		// regardless of which side originated the parent.
		if msg.ReplyToID != "" && m.friendStore != nil && m.friendStore.Get(msg.SenderID) != nil {
			if m.friendHistory != nil {
				m.friendHistory.AppendReply(msg.SenderID, msg.ReplyToID, p2pMsg)
				go m.friendHistory.Save(msg.SenderID)
			}
			// Refresh the visible cache so the reply tree shows.
			pairKey := ""
			if f := m.friendStore.Get(msg.SenderID); f != nil {
				pairKey = f.PairKey
			}
			if m.friendHistory != nil {
				updated := m.friendHistory.GetDecrypted(msg.SenderID, pairKey)
				m.friendMessages[msg.SenderID] = updated
				friendChID := "friend:" + msg.SenderID
				if m.currentCh != nil && m.currentCh.ID == friendChID {
					m.messages.SetMessages(updated)
					// Auto-expand the parent so the user sees the
					// new reply immediately instead of having to
					// click "X replies".
					m.messages.ExpandReplies(msg.ReplyToID)
				} else {
					m.channels.MarkUnread(friendChID)
					m.recordUnreadMessage(friendChID, p2pMsg.MessageID, msg.SenderID, userName, p2pMsg.Text)
				}
			}
			if m.p2pChan != nil {
				return m, waitForP2PMsg(m.p2pChan)
			}
			return m, nil
		}

		friendChID := "friend:" + msg.SenderID
		if m.currentCh != nil && m.currentCh.ID == friendChID {
			// Viewing this friend's channel — append directly.
			m.messages.AppendMessage(p2pMsg)
			m.appendFriendMessage(msg.SenderID, p2pMsg)
		} else if m.currentCh != nil && m.currentCh.IsDM && m.currentCh.UserID == msg.SenderID {
			// Viewing this user's Slack DM — show as encrypted.
			p2pMsg.Text = "🔒 " + p2pMsg.Text
			m.messages.AppendMessage(p2pMsg)
		} else if m.friendStore != nil && m.friendStore.Get(msg.SenderID) != nil {
			// Message from a friend, not viewing their channel — mark unread.
			m.appendFriendMessage(msg.SenderID, p2pMsg)
			m.channels.MarkUnread(friendChID)
			// And surface as a notification.
			m.recordUnreadMessage(friendChID, p2pMsg.MessageID, msg.SenderID, userName, p2pMsg.Text)
		} else {
			// Regular P2P message from non-friend.
			m.channels.MarkUnread(msg.SenderID)
		}

		if m.p2pChan != nil {
			return m, waitForP2PMsg(m.p2pChan)
		}
		return m, nil

	case SecureSessionReadyMsg:
		if m.secureMgr != nil {
			m.secureMgr.SetState(msg.PeerID, msg.State)
			// Refresh the header if we're viewing this peer's DM.
			if m.currentCh != nil && m.currentCh.UserID == msg.PeerID {
				m.setChannelHeader()
			}
		}
		return m, nil

	case WhitelistUpdateMsg:
		m.cfg.SecureWhitelist = msg.Whitelist
		config.SaveDebounced(m.cfg)
		return m, nil

	case FriendsLoadedMsg:
		// Coalesce the four setter calls below into a single
		// sidebar rebuild.
		m.channels.BeginBulkUpdate()
		m.channels.SetFriendChannels(msg.Channels)
		// Wire the alias / hidden / collapsed maps onto the
		// channel list. ChannelsLoadedMsg normally does this for
		// Slack channels, but in friends-only mode that path
		// never fires — without this call, friend channel
		// renames never apply across restarts.
		m.channels.SetAliases(m.cfg.ChannelAliases)
		m.channels.SetHiddenChannels(m.cfg.HiddenChannels)
		m.channels.SetCollapsedGroups(m.cfg.CollapsedGroups)
		m.channels.EndBulkUpdate()
		for uid, on := range msg.Online {
			if on {
				m.channels.MarkUnread("friend:" + uid)
			}
		}
		// Restore the last viewed friend chat (and its history) if the
		// user quit while in a friend conversation. Slack channels get
		// restored in ChannelsLoadedMsg, but friend channels live in a
		// separate list and need their own pass.
		if m.currentCh == nil && strings.HasPrefix(m.cfg.LastChannelID, "friend:") {
			for i := range msg.Channels {
				if msg.Channels[i].ID == m.cfg.LastChannelID {
					ch := msg.Channels[i]
					m.currentCh = &ch
					m.channels.SelectByID(ch.ID)
					m.setChannelHeader()
					m.loadFriendHistory(ch.UserID)
					break
				}
			}
		}
		return m, nil

	case FriendRequestSentMsg:
		m.overlay = overlayNone
		if m.p2pNode != nil && m.secureMgr != nil {
			pubKey := m.secureMgr.OwnPublicKeyBase64()
			multiaddr := m.p2pNode.Multiaddr()
			go func() {
				req := secure.P2PMessage{
					Type:     secure.MsgTypeFriendRequest,
					Text:     pubKey + "|" + multiaddr,
					SenderID: msg.UserID,
				}
				if err := m.p2pNode.SendMessage(msg.UserID, req); err != nil {
					// Fallback: send invite via Slack DM.
					if m.slackSvc != nil && m.currentCh != nil {
						inviteText := fmt.Sprintf("Hey! I'd like to chat privately using Slackers TUI. " +
							"Check it out: https://github.com/rw3iss/slackers")
						_ = m.slackSvc.SendMessage(m.currentCh.ID, inviteText)
					}
				}
			}()
			m.warning = "Friend request sent to " + msg.Name
		} else {
			m.warning = "P2P not available — enable Secure Mode in settings"
		}
		return m, nil

	case FriendRequestRespondMsg:
		m.overlay = overlayNone
		// Either accepted or rejected — clear any pending
		// friend-request notification for this peer.
		if m.notifStore != nil {
			if m.notifStore.ClearFriendRequest(msg.UserID) > 0 {
			}
		}
		if msg.Accepted && m.friendStore != nil {
			f := friends.Friend{
				UserID:    msg.UserID,
				Name:      msg.Name,
				PublicKey: msg.PublicKey,
				Multiaddr: msg.Multiaddr,
			}
			_ = m.friendStore.Add(f)
			_ = m.friendStore.Save()
			m.channels.SetFriendChannels(m.buildFriendChannels())
			m.warning = msg.Name + " added as friend!"
			// Send accept response over P2P.
			if m.p2pNode != nil && m.secureMgr != nil {
				go func() {
					resp := secure.P2PMessage{
						Type:     secure.MsgTypeFriendAccept,
						Text:     m.secureMgr.OwnPublicKeyBase64() + "|" + m.p2pNode.Multiaddr(),
						SenderID: msg.UserID,
					}
					_ = m.p2pNode.SendMessage(msg.UserID, resp)
				}()
			}
		} else if !msg.Accepted && msg.UserID != "" {
			m.warning = "Friend request declined"
		}
		return m, nil

	case friendPingTickMsg:
		currentFriend := ""
		if m.currentCh != nil && m.currentCh.IsFriend {
			currentFriend = m.currentCh.UserID
		}
		return m, friendPingCmdWithCurrent(m.friendStore, m.p2pNode, currentFriend)

	case FriendSendResultMsg:
		// Flip the history entry's Pending flag based on the
		// outcome of the attempted wire send. Refresh the active
		// chat view so the pending indicator appears/disappears.
		if m.friendHistory == nil || msg.MessageID == "" {
			return m, nil
		}
		pendingFlag := !msg.Success
		changed := m.friendHistory.SetPending(msg.PeerUID, msg.MessageID, pendingFlag)
		if changed {
			go m.friendHistory.Save(msg.PeerUID)
			pairKey := ""
			if f := m.friendStore.Get(msg.PeerUID); f != nil {
				pairKey = f.PairKey
			}
			updated := m.friendHistory.GetDecrypted(msg.PeerUID, pairKey)
			m.friendMessages[msg.PeerUID] = updated
			if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID == msg.PeerUID {
				m.messages.SetMessages(updated)
			}
		}
		return m, nil

	case FriendPingMsg:
		// friendPingCmd already updated friendStore online flags.
		// Only rebuild the sidebar when a friend's online state
		// actually *changed* this tick — previously this ran on
		// every ping (default: every 5 s) regardless, thrashing
		// the channel list and re-sorting. Also refresh the
		// message-pane header if the user is currently looking
		// at a friend whose state flipped.
		var resendCmds []tea.Cmd
		anyStateChanged := false
		currentFriendFlipped := false
		if len(msg.Online) > 0 {
			for uid, online := range msg.Online {
				prev := m.friendPrevOnline[uid]
				m.friendPrevOnline[uid] = online
				if prev != online {
					anyStateChanged = true
					if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID == uid {
						currentFriendFlipped = true
					}
					if online && !prev {
						// offline → online: pull any pending resends.
						if cmd := m.resendPendingFriendMessagesCmd(uid); cmd != nil {
							resendCmds = append(resendCmds, cmd)
						}
					}
				}
			}
		}
		if anyStateChanged {
			m.channels.SetFriendChannels(m.buildFriendChannels())
		}
		if currentFriendFlipped {
			m.setChannelHeader()
		}
		cmds := append(resendCmds, m.friendPingTickCmd())
		return m, tea.Batch(cmds...)

	case friendIdleCheckMsg:
		// Drop friend connections that have sat untouched longer
		// than FriendIdleTimeout. Keep the chat the user is
		// currently viewing alive regardless.
		if m.p2pNode != nil && m.friendStore != nil {
			now := time.Now()
			currentFriend := ""
			if m.currentCh != nil && m.currentCh.IsFriend {
				currentFriend = m.currentCh.UserID
			}
			for uid, last := range m.friendActivity {
				if uid == currentFriend {
					continue
				}
				if now.Sub(last) >= FriendIdleTimeout {
					if m.p2pNode.IsConnected(uid) {
						m.p2pNode.DisconnectPeer(uid)
					}
					m.friendStore.SetOnline(uid, false)
					delete(m.friendActivity, uid)
				}
			}
		}
		return m, friendIdleCheckCmd()

	case ErrMsg:
		return m, setError(&m, msg.Err)
	}

	return m, nil
}

// View renders the full TUI layout.
func (m Model) View() string {
	return ApplyBackgroundReset(m.viewInner())
}

func (m Model) viewInner() string {
	if !m.ready {
		return ""
	}

	if m.splash {
		return renderSplash(m.width, m.height, m.version)
	}

	// Render overlays on top
	switch m.overlay {
	case overlayHelp:
		return m.help.View()
	case overlaySettings:
		return m.settings.View()
	case overlaySearch:
		return m.search.View()
	case overlayHidden:
		return m.hidden.View()
	case overlayRename:
		return m.rename.View()
	case overlayMsgSearch:
		return m.msgSearch.View()
	case overlayFileBrowser:
		return m.fileBrowser.View()
	case overlayFilesList:
		return m.filesList.View()
	case overlayShortcuts:
		return m.shortcutsEditor.View()
	case overlayWhitelist:
		return m.whitelist.View()
	case overlayFriendRequest:
		return m.friendRequest.View()
	case overlayFriendsConfig:
		return m.friendsConfig.View()
	case overlayAbout:
		return m.about.View()
	case overlayThemePicker:
		return m.themePicker.View()
	case overlayThemeEditor:
		return m.themeEditor.View()
	case overlayThemeColorPicker:
		return m.themeColorPicker.View()
	case overlayEmojiPicker:
		return m.emojiPicker.View()
	case overlayNotifications:
		return m.notifs.View()
	case overlayMsgOptions:
		// Render base view, then overlay options on top.
		base := m.renderBaseView()
		return m.msgOptions.View(base)
	case overlaySidebarOptions:
		base := m.renderBaseView()
		return m.sidebarOptions.View(base)
	}

	msgView := m.messages.View()

	var topRow string
	showSidebar := !m.fullMode || m.focus == types.FocusSidebar
	if showSidebar {
		sidebar := m.channels.View()
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, msgView)
	} else {
		topRow = msgView
	}
	inputBar := m.input.View()
	statusLine := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		topRow,
		inputBar,
		statusLine,
	)
}

// renderBaseView builds the base TUI view (channels + messages + input + status).
func (m Model) renderBaseView() string {
	msgView := m.messages.View()
	var topRow string
	showSidebar := !m.fullMode || m.focus == types.FocusSidebar
	if showSidebar {
		sidebar := m.channels.View()
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, msgView)
	} else {
		topRow = msgView
	}
	inputBar := m.input.View()
	statusLine := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, topRow, inputBar, statusLine)
}

// applySettings reads the current config and resizes components.
func (m *Model) applySettings() {
	m.cfg = m.settings.Config()
	if m.cfg.ReplyFormat != "" {
		m.messages.SetReplyFormat(m.cfg.ReplyFormat)
	}
	sortAsc := true
	if m.cfg.ChannelSortAsc != nil {
		sortAsc = *m.cfg.ChannelSortAsc
	}
	sortBy := m.cfg.ChannelSortBy
	if sortBy == "" {
		sortBy = SortByType
	}
	m.channels.SetSort(sortBy, sortAsc)
	m.channels.SetItemSpacing(m.cfg.SidebarItemSpacing)
	m.messages.SetItemSpacing(m.cfg.MessageItemSpacing)
	m.resizeComponents()
}

// resizeComponents calculates and sets sizes for all sub-models.
func (m *Model) resizeComponents() {
	sidebarWidth := m.cfg.SidebarWidth
	if sidebarWidth < 10 {
		sidebarWidth = 10
	}
	if sidebarWidth > m.width/2 {
		sidebarWidth = m.width / 2
	}

	// In full mode, hide sidebar unless it's focused.
	showSidebar := true
	if m.fullMode && m.focus != types.FocusSidebar {
		showSidebar = false
	}

	inputHeight := m.input.DisplayHeight()
	statusHeight := 1
	topHeight := m.height - inputHeight - statusHeight - 2

	var msgWidth int
	if showSidebar {
		msgWidth = m.width - sidebarWidth - 2
	} else {
		sidebarWidth = 0
		msgWidth = m.width - 2
	}

	if topHeight < 1 {
		topHeight = 1
	}
	if msgWidth < 1 {
		msgWidth = 1
	}

	m.sidebarWidth = sidebarWidth
	m.msgTop = 0
	m.inputTop = topHeight + 2 // after sidebar/messages + borders

	m.channels.SetSize(sidebarWidth, topHeight)
	m.messages.SetSize(msgWidth, topHeight)
	m.input.SetSize(m.width - 2)
}

// handleOverlayMouse delegates mouse events to the active overlay.
func (m Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHelp:
		var cmd tea.Cmd
		m.help, cmd = m.help.Update(msg)
		return m, cmd
	case overlaySettings:
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		return m, cmd
	case overlaySearch:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	case overlayHidden:
		var cmd tea.Cmd
		m.hidden, cmd = m.hidden.Update(msg)
		return m, cmd
	case overlayShortcuts:
		var cmd tea.Cmd
		m.shortcutsEditor, cmd = m.shortcutsEditor.Update(msg)
		return m, cmd
	case overlayWhitelist:
		var cmd tea.Cmd
		m.whitelist, cmd = m.whitelist.Update(msg)
		return m, cmd
	case overlayFriendsConfig:
		var cmd tea.Cmd
		m.friendsConfig, cmd = m.friendsConfig.Update(msg)
		return m, cmd
	case overlayEmojiPicker:
		var cmd tea.Cmd
		m.emojiPicker, cmd = m.emojiPicker.Update(msg)
		return m, cmd
	case overlayFileBrowser:
		var cmd tea.Cmd
		m.fileBrowser, cmd = m.fileBrowser.UpdateMouse(msg)
		return m, cmd
	case overlayMsgOptions:
		// Click outside the popup box → close the overlay.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.msgOptions.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.msgOptions, cmd = m.msgOptions.Update(msg)
		return m, cmd
	case overlaySidebarOptions:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.sidebarOptions.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.sidebarOptions, cmd = m.sidebarOptions.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleMouse processes mouse click and scroll events.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y

	// Handle drag for sidebar resize.
	if m.dragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			newWidth := x
			if newWidth < 10 {
				newWidth = 10
			}
			maxWidth := m.width / 2
			if newWidth > maxWidth {
				newWidth = maxWidth
			}
			m.cfg.SidebarWidth = newWidth
			m.resizeComponents()
			return m, nil
		case tea.MouseActionRelease:
			m.dragging = false
			config.SaveDebounced(m.cfg)
			return m, nil
		}
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		// Right-click on messages area opens the message options
		// menu; right-click on the sidebar opens a channel context
		// menu with Hide / Rename / Invite / View Contact Info.
		if msg.Button == tea.MouseButtonRight {
			if y < m.inputTop && x >= m.sidebarWidth+1 {
				msgID, preview := m.messages.MessageAtClick(y)
				if msgID != "" {
					// Popup minimum X is the chat history left edge.
					minX := m.sidebarWidth + 2
					allowDelete := false
					if mm := m.messages.MessageByID(msgID); mm != nil && m.isMyMessage(*mm) {
						allowDelete = true
					}
					m.msgOptions = NewMsgOptions(msgID, preview, x+1, y, minX, allowDelete)
					m.msgOptions.SetSize(m.width, m.height)
					m.overlay = overlayMsgOptions
					return m, nil
				}
			}
			// Sidebar right-click: identify the row and open the
			// sidebar context menu.
			if !m.fullMode && y < m.inputTop && x < m.sidebarWidth+1 {
				viewportY := y - 1
				if viewportY < 0 {
					return m, nil
				}
				ch, isChannel, _ := m.channels.SelectByRow(viewportY)
				if isChannel && ch != nil {
					items := m.buildSidebarOptionsItems(*ch)
					if len(items) == 0 {
						return m, nil
					}
					m.sidebarOptions = NewSidebarOptions(ch.ID, ch.UserID, items, x+1, y)
					m.sidebarOptions.SetSize(m.width, m.height)
					m.overlay = overlaySidebarOptions
					return m, nil
				}
			}
			return m, nil
		}
		if msg.Button == tea.MouseButtonLeft {
			// Check if clicking on the sidebar divider (within 1 char of the border).
			if !m.fullMode && y < m.inputTop {
				dividerX := m.sidebarWidth + 1
				if x >= dividerX-1 && x <= dividerX+1 {
					m.dragging = true
					return m, nil
				}
			}
			// Determine which panel was clicked based on layout.
			// Check input bar first since it spans full width.
			// Click on status bar (last line) cancels download.
			if y >= m.height-1 && m.downloading && m.downloadCancel != nil {
				m.downloadCancel()
				m.downloading = false
				m.downloadCancel = nil
				m.warning = "Download cancelled"
				return m, nil
			}
			// Click on the settings cog in the bottom-right of the status bar.
			if y >= m.height-1 {
				if cogStart, cogEnd := m.settingsCogClickArea(); cogEnd > cogStart && x >= cogStart && x < cogEnd {
					m.settings = NewSettingsModel(m.cfg, m.version)
					m.settings.SetSize(m.width, m.height)
					m.overlay = overlaySettings
					return m, nil
				}
			}

			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
			} else if x < m.sidebarWidth+1 {
				// Sidebar clicked.
				m.focus = types.FocusSidebar
				m.updateFocus()

				// SelectByRow expects a viewport-relative row. The sidebar
				// has a top border (1 row) before its content, so subtract.
				viewportY := y - 1
				if viewportY < 0 {
					return m, nil
				}
				ch, isChannel, headerKey := m.channels.SelectByRow(viewportY)
				if headerKey != "" {
					// Header clicked — toggle collapse.
					m.channels.ToggleCollapse(headerKey)
					m.channels.buildRows()
					m.cfg.CollapsedGroups = m.channels.CollapsedGroups()
					config.SaveDebounced(m.cfg)
				} else if isChannel && ch != nil {
					// Switching channels exits thread view if it
					// was open — the thread belongs to whatever
					// chat we were just viewing.
					if m.messages.InThreadMode() {
						m.messages.ExitThreadMode()
					}
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.clearChannelNotifs(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
					// Move focus to the input so the user can
					// start typing immediately after picking a
					// channel via mouse.
					m.focus = types.FocusInput
					m.updateFocus()
					if ch.IsFriend {
						m.loadFriendHistory(ch.UserID)
						return m, nil
					}
					return m, loadHistoryCmd(m.slackSvc, ch.ID)
				}
			} else {
				// Messages area clicked.
				m.focus = types.FocusMessages
				m.updateFocus()

				msgPaneX := x - m.sidebarWidth - 2

				// Friend chat: header line is at y == 1 (top border at 0).
				// If the user clicked the cog icon in the upper-right of
				// the header, open Friend Details for the current friend.
				if y == 1 && m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID != "" {
					if cs, ce := m.messages.FriendCogPaneClickArea(); ce > cs && msgPaneX >= cs && msgPaneX < ce {
						friendID := m.currentCh.UserID
						return m, func() tea.Msg { return FriendsConfigOpenMsg{FriendID: friendID} }
					}
				}

				// Check if a [FRIEND:...] pill was clicked.
				if card := m.messages.FriendCardAtClick(msgPaneX, y); card != nil {
					return m, func() tea.Msg { return FriendCardClickedMsg{Card: *card} }
				}

				// Check if a reaction badge was clicked — toggle the reaction.
				if reactMsgID, emoji := m.messages.ReactionAtClick(msgPaneX, y); reactMsgID != "" {
					m.toggleReaction(reactMsgID, emoji)
					return m, nil
				}

				// Check if a "X replies" line was clicked.
				if replyParentID := m.messages.ReplyLineMessageID(y); replyParentID != "" {
					if m.cfg.ReplyFormat == "inside" {
						// Find parent index and enter thread mode.
						for i, mm := range m.messages.messages {
							if mm.MessageID == replyParentID {
								m.messages.EnterThreadMode(i)
								break
							}
						}
					} else {
						// Inline mode: toggle collapse.
						m.messages.ToggleReplyCollapse(replyParentID)
					}
					return m, nil
				}

				// Check if a file was clicked.
				file := m.messages.FileAtClick(y)
				if file != nil {
					if file.Uploading {
						// Click on an uploading file: queue a
						// confirmation in the status bar.
						msgID := m.messages.MessageIDForFile(file.ID)
						if msgID != "" {
							m.pendingCancelUploadKey = msgID + "|" + file.ID
							m.warning = fmt.Sprintf("Cancel upload of %s? [y/N]", file.Name)
						}
						return m, nil
					}
					downloadPath := m.cfg.DownloadPath
					if downloadPath == "" {
						home, _ := os.UserHomeDir()
						downloadPath = filepath.Join(home, "Downloads")
					}
					destPath := filepath.Join(downloadPath, file.Name)
					m.warning = fmt.Sprintf("Downloading %s...", file.Name)
					return m, m.startDownload(*file, destPath)
				}
			}

		} else if msg.Button == tea.MouseButtonWheelUp {
			lines := 3
			if msg.Ctrl || msg.Shift {
				lines = 15
			}
			// Scroll based on mouse position, not focus.
			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
			} else if x < m.sidebarWidth+1 && !m.fullMode {
				m.focus = types.FocusSidebar
				m.updateFocus()
				m.channels.selected -= lines
				if m.channels.selected < 0 {
					m.channels.selected = 0
				}
				m.channels.ensureVisible()
			} else {
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.messages, _ = m.messages.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
			}
			return m, nil

		} else if msg.Button == tea.MouseButtonWheelDown {
			lines := 3
			if msg.Ctrl || msg.Shift {
				lines = 15
			}
			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
			} else if x < m.sidebarWidth+1 && !m.fullMode {
				m.focus = types.FocusSidebar
				m.updateFocus()
				m.channels.selected += lines
				if m.channels.selected >= len(m.channels.rows) {
					m.channels.selected = len(m.channels.rows) - 1
				}
				m.channels.ensureVisible()
			} else {
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.messages, _ = m.messages.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
			}
			return m, nil
		}
	}

	return m, nil
}

// Tab cycle: Sidebar → Input → Messages → Sidebar.
func (m *Model) cycleFocusForward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusInput
	case types.FocusInput:
		m.focus = types.FocusMessages
	case types.FocusMessages:
		m.focus = types.FocusSidebar
	}
}

func (m *Model) cycleFocusBackward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusMessages
	case types.FocusInput:
		m.focus = types.FocusSidebar
	case types.FocusMessages:
		m.focus = types.FocusInput
	}
}

// saveLastChannel persists the currently viewed channel ID to config.
func (m *Model) saveLastChannel(channelID string) {
	m.cfg.LastChannelID = channelID
	config.SaveDebounced(m.cfg) // fire-and-forget, don't block the UI
}

// buildChannelIndex populates the channel ID -> name/alias lookup.
func (m *Model) buildChannelIndex() {
	m.channelIndex = make(map[string]channelInfo, len(m.channels.channels))
	for _, ch := range m.channels.channels {
		ci := channelInfo{name: ch.Name}
		if alias, ok := m.cfg.ChannelAliases[ch.ID]; ok && alias != "" {
			ci.alias = alias
		}
		m.channelIndex[ch.ID] = ci
	}
}

// resolveChannelDisplay returns "alias (#name)" or "#name" or "channelID" for display.
func (m *Model) resolveChannelDisplay(channelID string) string {
	ci, ok := m.channelIndex[channelID]
	if !ok {
		return channelID
	}
	if ci.alias != "" {
		if ci.name != "" && ci.name != ci.alias {
			return ci.alias + " (#" + ci.name + ")"
		}
		return ci.alias
	}
	if ci.name != "" {
		return "#" + ci.name
	}
	return channelID
}

// setWarning sets a status bar warning and schedules auto-clear after 5s of activity.
func setWarning(m *Model, msg string) tea.Cmd {
	m.warning = msg
	return clearWarningCmd()
}

// setError sets a status bar error and schedules auto-clear after 5s of activity.
func setError(m *Model, err error) tea.Cmd {
	m.err = err
	return clearWarningCmd()
}

// loadLastSeen initializes lastSeen from persisted config.
func loadLastSeen(cfg *config.Config) map[string]string {
	if cfg.LastSeenTS != nil && len(cfg.LastSeenTS) > 0 {
		// Clone it so we don't mutate the config map directly.
		m := make(map[string]string, len(cfg.LastSeenTS))
		for k, v := range cfg.LastSeenTS {
			m[k] = v
		}
		return m
	}
	return make(map[string]string)
}

// persistLastSeen saves lastSeen timestamps to config (fire-and-forget).
func (m *Model) persistLastSeen() {
	m.cfg.LastSeenTS = make(map[string]string, len(m.lastSeen))
	for k, v := range m.lastSeen {
		if v != "0" && v != "" {
			m.cfg.LastSeenTS[k] = v
		}
	}
	config.SaveDebounced(m.cfg)
}

// stripQuotePrefix removes a leading "> " (or ">") from each non-empty line.
// The edit pre-fill wraps each original line in this prefix so the user
// can clearly distinguish editable text from the [EDIT:id] header.
func stripQuotePrefix(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "> "):
			out = append(out, line[2:])
		case strings.HasPrefix(line, ">"):
			out = append(out, line[1:])
		default:
			out = append(out, line)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// editMessage performs an edit on a message authored by the local user.
// Routes to chat.update for Slack channels and the new MsgTypeEdit P2P
// protocol for friend chats. Updates local state, refreshes the view, and
// shows a temporary success message.
func (m *Model) editMessage(messageID, newText string) {
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Edit failed: message not found"
		return
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only edit your own messages"
		return
	}
	if strings.TrimSpace(newText) == "" {
		m.warning = "Edit cancelled (empty body)"
		return
	}
	if m.currentCh == nil {
		return
	}

	if m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		// Optimistically update local state.
		m.messages.EditMessageLocal(messageID, newText)
		if m.friendHistory != nil {
			m.friendHistory.EditMessage(peerUID, messageID, newText)
			go m.friendHistory.Save(peerUID)
		}
		if msgs, ok := m.friendMessages[peerUID]; ok {
			if target := findFriendMsgPtr(msgs, messageID); target != nil {
				target.Text = newText
			}
		}
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:        secure.MsgTypeEdit,
			TargetMsgID: messageID,
			Text:        newText,
			SenderID:    peerUID,
			Timestamp:   time.Now().Unix(),
		})
		m.warning = "Message edited"
		return
	}

	if m.slackSvc != nil {
		channelID := m.currentCh.ID
		go m.slackSvc.UpdateMessage(channelID, messageID, newText)
		m.messages.EditMessageLocal(messageID, newText)
		m.warning = "Message edited"
	}
}

// isMyMessage returns true if the given message was authored by the local user.
// Friend messages stored locally use UserID == "me"; Slack messages use the
// real user ID, cached as m.myUserID.
func (m *Model) isMyMessage(msg types.Message) bool {
	if msg.UserID == "me" {
		return true
	}
	if m.myUserID != "" && msg.UserID == m.myUserID {
		return true
	}
	return false
}

// requestMessageDelete handles a user-initiated delete: validates authorship,
// then prompts in the status bar for confirmation.
func (m *Model) requestMessageDelete(messageID string) {
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Message not found"
		return
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only delete your own messages"
		return
	}
	m.pendingDeleteMsgID = messageID
	m.warning = "Delete this message? (y to confirm, Esc to cancel)"
}

// cancelUpload performs the actual cancellation for an in-flight file
// upload identified by "<msgID>|<fileID>". For Slack uploads it cancels
// the context (the HTTP request may complete in the background but the
// result is discarded). For P2P uploads it removes the file from the
// node's serving table and notifies the peer so they can clean up.
// In both cases the file is removed from the local message and the view
// is refreshed.
func (m *Model) cancelUpload(key string) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 {
		return
	}
	msgID, fileID := parts[0], parts[1]

	// Slack: cancel the context if any.
	if cancel, ok := m.uploadCancels[key]; ok {
		if cancel != nil {
			cancel()
		}
		delete(m.uploadCancels, key)
	}

	// P2P friend channel: tell the peer to drop the offer.
	if m.currentCh != nil && m.currentCh.IsFriend && m.p2pNode != nil {
		_ = m.p2pNode.CancelFileOffer(m.currentCh.UserID, fileID)
		// Also update the friend message cache so a re-render shows
		// the file removed.
		if msgs, ok := m.friendMessages[m.currentCh.UserID]; ok {
			for i := range msgs {
				if msgs[i].MessageID != msgID {
					continue
				}
				for fi := range msgs[i].Files {
					if msgs[i].Files[fi].ID == fileID {
						msgs[i].Files = append(msgs[i].Files[:fi], msgs[i].Files[fi+1:]...)
						break
					}
				}
				break
			}
			m.friendMessages[m.currentCh.UserID] = msgs
			if m.friendHistory != nil {
				go m.friendHistory.Save(m.currentCh.UserID)
			}
		}
	}

	m.messages.RemoveFile(msgID, fileID)
	m.warning = "Upload cancelled"
}

// confirmMessageDelete performs the actual deletion that was requested via
// requestMessageDelete. Routes to Slack API or P2P delete request as appropriate.
func (m *Model) confirmMessageDelete() tea.Cmd {
	messageID := m.pendingDeleteMsgID
	m.pendingDeleteMsgID = ""
	m.warning = ""
	if messageID == "" {
		return nil
	}
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Message not found"
		return nil
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only delete your own messages"
		return nil
	}

	// Friend / P2P channel: send delete request, wait for ack to delete locally.
	if m.currentCh != nil && m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:        secure.MsgTypeDelete,
			TargetMsgID: messageID,
			SenderID:    peerUID,
			Timestamp:   time.Now().Unix(),
		})
		// Optimistically delete locally — the peer's ack confirms persistence
		// on their side, but we don't want to leave the message hanging on
		// our screen if the peer is briefly unreachable.
		m.messages.DeleteMessageLocal(messageID)
		if m.friendHistory != nil {
			m.friendHistory.DeleteMessage(peerUID, messageID)
			go m.friendHistory.Save(peerUID)
		}
		// Also drop from in-memory cache.
		if msgs, ok := m.friendMessages[peerUID]; ok {
			m.friendMessages[peerUID] = deleteFriendMessage(msgs, messageID)
		}
		m.warning = "Message deleted"
		return nil
	}

	// Slack channel: call API, then drop locally.
	if m.slackSvc != nil && m.currentCh != nil {
		channelID := m.currentCh.ID
		go m.slackSvc.DeleteMessage(channelID, messageID)
		m.messages.DeleteMessageLocal(messageID)
		m.warning = "Message deleted"
		return nil
	}
	return nil
}

// deleteFriendMessage removes a message (top-level or nested reply) from a slice.
func deleteFriendMessage(msgs []types.Message, messageID string) []types.Message {
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			return append(msgs[:i], msgs[i+1:]...)
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				msgs[i].Replies = append(msgs[i].Replies[:j], msgs[i].Replies[j+1:]...)
				return msgs
			}
		}
	}
	return msgs
}

// setChannelHeader updates the message view header with channel name and secure indicator.
// toggleReaction adds or removes the user's reaction on a message.
func (m *Model) toggleReaction(messageID, emoji string) {
	// Build the set of identifiers that count as "me".
	myIDs := []string{"me"}
	if m.myUserID != "" {
		myIDs = append(myIDs, m.myUserID)
	}

	// Check if any of my IDs is already in the reaction's user list.
	hasReacted := false
	var matchedID string
	for _, r := range m.messages.MessageReactions(messageID) {
		if r.Emoji != emoji {
			continue
		}
		for _, uid := range r.UserIDs {
			for _, myID := range myIDs {
				if uid == myID {
					hasReacted = true
					matchedID = uid
					break
				}
			}
			if hasReacted {
				break
			}
		}
		break
	}

	if m.currentCh == nil {
		return
	}

	// Use the real user ID for storage if known, else "me".
	storeID := "me"
	if m.myUserID != "" {
		storeID = m.myUserID
	}

	if m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		msgType := secure.MsgTypeReaction
		if hasReacted {
			msgType = secure.MsgTypeReactionRemove
			m.messages.RemoveReactionLocal(messageID, emoji, matchedID)
			if m.friendHistory != nil {
				m.friendHistory.RemoveReaction(peerUID, messageID, emoji, matchedID)
				go m.friendHistory.Save(peerUID)
			}
		} else {
			m.messages.AddReactionLocal(messageID, emoji, storeID)
			if m.friendHistory != nil {
				m.friendHistory.UpdateReaction(peerUID, messageID, emoji, storeID)
				go m.friendHistory.Save(peerUID)
			}
		}
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:          msgType,
			TargetMsgID:   messageID,
			ReactionEmoji: emoji,
			SenderID:      peerUID,
			Timestamp:     time.Now().Unix(),
		})
		return
	}

	if m.slackSvc != nil {
		channelID := m.currentCh.ID
		if hasReacted {
			m.messages.RemoveReactionLocal(messageID, emoji, matchedID)
			go m.slackSvc.RemoveReaction(channelID, messageID, emoji)
		} else {
			m.messages.AddReactionLocal(messageID, emoji, storeID)
			go m.slackSvc.AddReaction(channelID, messageID, emoji)
		}
	}
}

// findFriendMsgPtr returns a pointer to a friend message by ID, walking nested replies.
func findFriendMsgPtr(msgs []types.Message, messageID string) *types.Message {
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			return &msgs[i]
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				return &msgs[i].Replies[j]
			}
		}
	}
	return nil
}

// addLocalReaction updates in-memory friend message reactions and refreshes the view.
func (m *Model) addLocalReaction(friendUID, messageID, emoji string) {
	msgs := m.friendMessages[friendUID]
	if target := findFriendMsgPtr(msgs, messageID); target != nil {
		found := false
		for j, r := range target.Reactions {
			if r.Emoji == emoji {
				target.Reactions[j].Count++
				target.Reactions[j].UserIDs = append(target.Reactions[j].UserIDs, "me")
				found = true
				break
			}
		}
		if !found {
			target.Reactions = append(target.Reactions, types.Reaction{
				Emoji: emoji, UserIDs: []string{"me"}, Count: 1,
			})
		}
	}
	friendChID := "friend:" + friendUID
	if m.currentCh != nil && m.currentCh.ID == friendChID {
		m.messages.SetMessages(msgs)
	}
}

// appendFriendMessage adds a message to a friend's history and persists it.
func (m *Model) appendFriendMessage(userID string, msg types.Message) {
	m.friendMessages[userID] = append(m.friendMessages[userID], msg)
	if m.friendHistory != nil {
		pairKey := ""
		if f := m.friendStore.Get(userID); f != nil {
			pairKey = f.PairKey
		}
		m.friendHistory.Append(userID, msg, pairKey)
		go m.friendHistory.Save(userID)
	}
}

// loadFriendHistory loads a friend's persisted chat history into the
// message view. Used by both the keyboard Enter handler and the mouse
// click handler.
func (m *Model) loadFriendHistory(friendUserID string) {
	// Opening the chat is the user's signal that they want to talk
	// to this friend — try to (re)connect now and refresh the
	// inactivity clock so the watchdog won't immediately drop us.
	m.connectFriend(friendUserID)
	if m.friendHistory != nil {
		pairKey := ""
		if m.friendStore != nil {
			if f := m.friendStore.Get(friendUserID); f != nil {
				pairKey = f.PairKey
			}
		}
		msgs := m.friendHistory.GetDecrypted(friendUserID, pairKey)
		m.friendMessages[friendUserID] = msgs
		m.messages.SetMessages(msgs)
		return
	}
	if history, ok := m.friendMessages[friendUserID]; ok {
		m.messages.SetMessages(history)
	} else {
		m.messages.SetMessages(nil)
	}
}

// buildFriendChannels creates Channel entries from the friend store.
func (m *Model) buildFriendChannels() []types.Channel {
	if m.friendStore == nil {
		return nil
	}
	var channels []types.Channel
	for _, f := range m.friendStore.All() {
		channels = append(channels, types.Channel{
			ID:       "friend:" + f.UserID,
			Name:     f.Name,
			IsFriend: true,
			IsDM:     true,
			UserID:   f.UserID,
		})
	}
	return channels
}

func (m *Model) setChannelHeader() {
	if m.currentCh == nil {
		return
	}
	prefix := "#"
	if m.currentCh.IsFriend {
		prefix = ""
	}
	m.messages.SetChannelName(prefix + m.channels.displayName(*m.currentCh))
	m.messages.SetSecureLabel(m.secureIndicator())
	m.messages.SetIsFriendChannel(m.currentCh.IsFriend)
	if m.currentCh.IsFriend {
		// Show the configured shortcut for "friend_details" so the
		// user can see how to open the friend config from the chat.
		hint := ""
		if keys := shortcuts.KeysForAction(m.shortcutMap, "friend_details"); len(keys) > 0 {
			hint = keys[0]
		}
		m.messages.SetFriendDetailsHint(hint)
	} else {
		m.messages.SetFriendDetailsHint("")
	}
}

// secureIndicator returns a status label for the current channel's secure state.
func (m *Model) secureIndicator() string {
	if m.currentCh == nil {
		return ""
	}
	// Friend channels: always end-to-end encrypted over libp2p (P2P).
	// Show online/offline state of the secure tunnel.
	if m.currentCh.IsFriend && m.friendStore != nil {
		f := m.friendStore.Get(m.currentCh.UserID)
		if f != nil {
			if f.Online {
				return " 🔒 secure p2p"
			}
			return " 🔓 p2p offline"
		}
		return ""
	}
	if m.secureMgr == nil || !m.currentCh.IsDM {
		return ""
	}
	if !isWhitelisted(m.cfg.SecureWhitelist, m.currentCh.UserID) {
		return ""
	}
	sess := m.secureMgr.GetSession(m.currentCh.UserID)
	if sess == nil {
		return ""
	}
	return " [" + sess.State.String() + "]"
}

// decryptMessages decrypts any E2E encrypted messages in the list using the secure manager.
func (m *Model) decryptMessages(msgs []types.Message) []types.Message {
	if m.secureMgr == nil {
		return msgs
	}
	for i, msg := range msgs {
		if secure.IsEncryptedMessage(msg.Text) {
			plaintext, err := m.secureMgr.DecryptMessage(msg.UserID, msg.Text)
			if err == nil {
				msgs[i].Text = "🔒 " + plaintext
			} else {
				msgs[i].Text = "🔒 [encrypted message]"
			}
		}
	}
	return msgs
}

// waitForP2PMsg returns a command that waits for the next P2P message.
func waitForP2PMsg(ch chan P2PReceivedMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// discoverPeerCmd attempts to discover a peer and set session state.
func discoverPeerCmd(mgr *secure.SessionManager, userID string) tea.Cmd {
	return func() tea.Msg {
		// Check if we already have a session with this peer.
		sess := mgr.GetSession(userID)
		if sess != nil {
			return SecureSessionReadyMsg{PeerID: userID, State: sess.State}
		}
		// No session yet — mark as discovering. Full key exchange happens
		// when the peer also has Slackers with secure mode enabled.
		return SecureSessionReadyMsg{PeerID: userID, State: secure.SessionDiscovering}
	}
}

// isWhitelisted checks if a user ID is in the secure whitelist.
func isWhitelisted(whitelist []string, userID string) bool {
	for _, id := range whitelist {
		if id == userID {
			return true
		}
	}
	return false
}

// rebuildPollChannels builds the list of channels to poll, excluding hidden ones.
func (m *Model) rebuildPollChannels() {
	hidden := make(map[string]bool)
	for _, id := range m.cfg.HiddenChannels {
		hidden[id] = true
	}
	m.pollChannels = make([]string, 0)
	for _, ch := range m.channels.AllChannels() {
		if !hidden[ch.ID] {
			m.pollChannels = append(m.pollChannels, ch.ID)
		}
	}
}

func drainWarnings(m *Model) {
	if warns := m.slackSvc.Warnings(); len(warns) > 0 {
		m.warning = warns[len(warns)-1]
	} else {
		m.warning = ""
	}
}

func (m *Model) updateFocus() {
	m.channels.SetFocused(m.focus == types.FocusSidebar)
	m.messages.SetFocused(m.focus == types.FocusMessages)
	m.input.SetFocused(m.focus == types.FocusInput)
	// When leaving the sidebar, reset selection to the current channel.
	if m.focus != types.FocusSidebar && m.currentCh != nil {
		m.channels.SelectByID(m.currentCh.ID)
	}
	if m.fullMode {
		m.resizeComponents()
	}
}

func (m Model) renderStatusBar() string {
	team := m.teamName
	if team == "" {
		team = "slackers"
	}

	// The status indicator depends on what the user is currently
	// looking at:
	//   - Friend chat → reflect THAT friend's P2P connection
	//   - Slack channel → reflect the Slack socket status, but
	//     only when Slack mode is configured (BotToken set).
	//   - No tokens AND no friend selected → hide entirely.
	var connStr string
	showConn := true
	switch {
	case m.currentCh != nil && m.currentCh.IsFriend:
		online := false
		if m.friendStore != nil {
			if f := m.friendStore.Get(m.currentCh.UserID); f != nil {
				online = f.Online
			}
		}
		if online {
			connStr = StatusConnected.Render("● P2P connected")
		} else {
			connStr = StatusDisconnected.Render("○ P2P disconnected")
		}
	case m.cfg != nil && m.cfg.BotToken != "":
		switch m.connStatus {
		case types.StatusConnected:
			connStr = StatusConnected.Render("● Connected")
		case types.StatusConnecting:
			connStr = StatusBarStyle.Render("○ Connecting...")
		case types.StatusError:
			errStr := "error"
			if m.connErr != nil {
				errStr = m.connErr.Error()
			}
			connStr = StatusDisconnected.Render("✕ " + errStr)
		default:
			connStr = StatusDisconnected.Render("○ Disconnected")
		}
	default:
		// Friends-only mode and no friend chat selected — no
		// meaningful global connection state to show.
		showConn = false
	}

	extra := ""
	if m.warning != "" {
		extra = " | " + lipgloss.NewStyle().Foreground(ColorHighlight).Render("! "+m.warning)
	}
	if m.err != nil {
		extra += " | " + StatusDisconnected.Render(m.err.Error())
	}

	hintsText := "Ctrl-H: help | Ctrl-S: settings | Ctrl-\\: edit mode | Ctrl-C: quit"
	if m.input.Mode() == InputModeEdit {
		hintsText = "EDIT | Alt-Enter: send | Ctrl-\\: normal mode | Ctrl-C: quit"
	}
	hints := HelpStyle.Render(hintsText)

	var left string
	if showConn {
		left = fmt.Sprintf(" %s | %s%s", connStr, hints, extra)
	} else {
		left = fmt.Sprintf(" %s%s", hints, extra)
	}
	versionStr := fmt.Sprintf(" slackers v%s ", m.version)
	cogPart := ""
	if m.cfg != nil && m.cfg.MouseEnabled {
		// 1 column of padding on each side of the cog emoji.
		cogPart = " " + settingsCogGlyph + " "
	}
	right := StatusBarStyle.Render(cogPart + versionStr)

	// Pad the middle to push right label to the edge.
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	status := left + strings.Repeat(" ", gap) + right

	return StatusBarStyle.Width(m.width).Render(status)
}

// settingsCogGlyph is the cog emoji shown in the status bar (when mouse mode
// is enabled). Includes the VS16 variation selector so terminals render it as
// a graphical emoji rather than a tiny monochrome glyph.
const settingsCogGlyph = "⚙\ufe0f"

// settingsCogClickArea returns the [startX, endX) column range for the
// settings cog in the status bar. Returns (0, 0) when the cog is not shown.
func (m Model) settingsCogClickArea() (int, int) {
	if m.cfg == nil || !m.cfg.MouseEnabled {
		return 0, 0
	}
	versionStr := fmt.Sprintf(" slackers v%s ", m.version)
	cogPart := " " + settingsCogGlyph + "  "
	rightWidth := lipgloss.Width(cogPart + versionStr)
	rightStart := m.width - rightWidth
	// Click area covers the cog glyph plus its surrounding pad spaces for forgiveness.
	startX := rightStart
	endX := rightStart + lipgloss.Width(cogPart)
	return startX, endX
}

// Command functions

func (m *Model) startDownload(file types.FileInfo, destPath string) tea.Cmd {
	if m.downloadCancel != nil {
		m.downloadCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.downloadCancel = cancel
	m.downloading = true
	m.warning = fmt.Sprintf("Downloading %s... (Ctrl-D to cancel)", file.Name)

	svc := m.slackSvc
	return func() tea.Msg {
		err := svc.DownloadFile(ctx, file.URL, destPath)
		if ctx.Err() != nil {
			os.Remove(destPath)
			return FileDownloadCancelledMsg{}
		}
		if err != nil {
			return FileDownloadCompleteMsg{Err: err}
		}
		return FileDownloadCompleteMsg{DestPath: destPath}
	}
}

// startP2PDownload downloads a file from a connected friend via P2P.
func (m *Model) startP2PDownload(peerUID, fileID, destPath string) tea.Cmd {
	if m.downloadCancel != nil {
		m.downloadCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.downloadCancel = cancel
	m.downloading = true
	m.warning = fmt.Sprintf("Downloading from friend... (Ctrl-D to cancel)")

	p2p := m.p2pNode
	return func() tea.Msg {
		err := p2p.DownloadFileFromPeer(ctx, peerUID, fileID, destPath)
		if ctx.Err() != nil {
			os.Remove(destPath)
			return FileDownloadCancelledMsg{}
		}
		if err != nil {
			return FileDownloadCompleteMsg{Err: err}
		}
		return FileDownloadCompleteMsg{DestPath: destPath}
	}
}

// normalizeShortcutKey lowercases an unmodified ASCII uppercase rune in
// the key message so shortcut bindings match regardless of caps-lock
// state. Ctrl/Alt/special-key combinations and multi-rune sequences
// are returned unchanged. This means a binding for "i" matches whether
// the user typed 'i', 'I', or 'i' with caps-lock on.
func normalizeShortcutKey(msg tea.KeyMsg) tea.KeyMsg {
	if msg.Alt || msg.Type != tea.KeyRunes {
		return msg
	}
	if len(msg.Runes) != 1 {
		return msg
	}
	r := msg.Runes[0]
	if r >= 'A' && r <= 'Z' {
		out := msg
		out.Runes = []rune{r + ('a' - 'A')}
		return out
	}
	return msg
}

// clearChannelNotifs clears any notification entries for the given
// channel ID and persists. Used when the user opens a channel so the
// notifications view stays in sync with what's actually unread.
func (m *Model) clearChannelNotifs(channelID string) {
	if m.notifStore == nil || channelID == "" {
		return
	}
	if m.notifStore.ClearChannel(channelID) > 0 {
	}
}

// friendMarkerPattern matches [FRIEND:<id>] tokens in outgoing text.
// The id may be the literal "me" (own contact card) or a saved
// friend's UserID. Anything else is left untouched.
var friendMarkerPattern = regexp.MustCompile(`\[FRIEND:([^\]]+)\]`)

// expandFriendMarkers replaces any [FRIEND:me] / [FRIEND:<id>] tokens
// in an outgoing message with a full SLF2 contact-card hash so the
// recipient can decode and add the contact. Unresolvable tokens are
// left in place and an inline `(error: ...)` note is added so the
// sender can see why.
func (m *Model) expandFriendMarkers(text string) string {
	if !strings.Contains(text, "[FRIEND:") {
		return text
	}
	return friendMarkerPattern.ReplaceAllStringFunc(text, func(token string) string {
		match := friendMarkerPattern.FindStringSubmatch(token)
		if len(match) < 2 {
			return token
		}
		id := strings.TrimSpace(match[1])

		// Already-encoded markers — pass through unchanged. These
		// come from places that bake in the full card directly
		// (e.g. the Slack invite builder), and we don't want to
		// round-trip them back through MyContactCard / Marshal.
		if strings.HasPrefix(id, "{") ||
			strings.HasPrefix(id, "SLF1.") ||
			strings.HasPrefix(id, "SLF2.") ||
			strings.HasPrefix(id, "#") {
			return token
		}

		var card friends.ContactCard
		if id == "me" {
			// Build the local user's card from the live identity.
			pubKey := ""
			if m.secureMgr != nil {
				pubKey = m.secureMgr.OwnPublicKeyBase64()
			}
			multiaddr := ""
			if m.p2pNode != nil {
				multiaddr = m.p2pNode.Multiaddr()
			}
			if pubKey == "" || multiaddr == "" {
				m.warning = "Could not share my info — Secure Mode must be on (Friends Config → Edit My Info)"
				return token + " (error: my contact card unavailable)"
			}
			card = friends.MyContactCard(
				m.cfg.SlackerID,
				m.cfg.MyName,
				m.cfg.MyEmail,
				pubKey,
				multiaddr,
			)
		} else if m.friendStore != nil {
			f := m.friendStore.Get(id)
			if f == nil {
				m.warning = "Friend " + id + " not found in your local store"
				return token + " (error: friend not found)"
			}
			if f.PublicKey == "" || f.Multiaddr == "" {
				m.warning = "Friend " + f.Name + " is missing public key or multiaddr"
				return token + " (error: friend has no public key or multiaddr)"
			}
			card = f.ToContactCard()
		} else {
			return token
		}

		// Choose the encoding format. The user controls this via
		// Friends Config → Share Format. JSON includes the full
		// profile in plain text on the wire; hash is compact and
		// obfuscated. Empty/missing config value defaults to JSON
		// (new-user default). Sharing other friends always uses
		// hash — re-broadcasting someone else's full JSON would
		// leak more of their identity than they may have intended.
		useJSON := id == "me" && (m.cfg == nil || m.cfg.ShareMyInfoFormat != "hash")
		if useJSON {
			raw, jerr := json.Marshal(card)
			if jerr != nil {
				m.warning = "Could not marshal contact card: " + jerr.Error()
				return token + " (error: marshal failed)"
			}
			// The marker regex uses ']' as a terminator, so any
			// literal ']' inside the JSON would truncate the
			// payload. Escape via the JSON unicode escape — when
			// the receiver runs json.Unmarshal it decodes back
			// to the original character.
			safe := strings.ReplaceAll(string(raw), "]", `\u005d`)
			return "[FRIEND:" + safe + "]"
		}
		hash, err := friends.EncodeContactCard(card)
		if err != nil {
			m.warning = "Could not encode contact card: " + err.Error()
			return token + " (error: encode failed)"
		}
		return "[FRIEND:" + hash + "]"
	})
}

// activateNotification navigates the user to the chat / message that
// owns the notification, then clears the notification (and any
// matching unread state on the channel).
func (m *Model) activateNotification(n notifications.Notification) tea.Cmd {
	if m.notifStore == nil {
		return nil
	}

	switch n.Type {
	case notifications.TypeFriendRequest:
		// Re-open the friend request modal with the cached identity.
		if m.friendRequest = NewIncomingFriendRequest(n.UserID, n.UserName, n.FriendPublicKey, n.FriendMultiaddr); true {
			m.friendRequest.SetSize(m.width, m.height)
			m.overlay = overlayFriendRequest
		}
		// The notification will be cleared when the user accepts /
		// rejects from the modal (handled in the FriendRequestSentMsg
		// + friend_accept paths) — we leave it intact for now so it
		// stays in the list if the user just peeks.
		return nil

	case notifications.TypeUnreadMessage, notifications.TypeReaction:
		// Drop the notification and any siblings from the same channel,
		// then switch to that channel.
		m.notifStore.ClearChannel(n.ChannelID)
		ch := m.lookupChannelByID(n.ChannelID)
		if ch == nil {
			m.warning = "Channel not found for notification"
			return nil
		}
		if m.messages.InThreadMode() {
			m.messages.ExitThreadMode()
		}
		m.currentCh = ch
		m.channels.SelectByID(ch.ID)
		m.channels.ClearUnread(ch.ID)
		m.clearChannelNotifs(ch.ID)
		m.setChannelHeader()
		m.saveLastChannel(ch.ID)
		if ch.IsFriend {
			m.loadFriendHistory(ch.UserID)
			return nil
		}
		return loadHistoryCmd(m.slackSvc, ch.ID)
	}
	return nil
}

// lookupChannelByID returns the *types.Channel for an ID by walking
// the sidebar's known channel list (Slack + friend channels).
func (m *Model) lookupChannelByID(id string) *types.Channel {
	for _, ch := range m.channels.AllChannels() {
		if ch.ID == id {
			cp := ch
			return &cp
		}
	}
	return nil
}

// recordUnreadMessage drops a TypeUnreadMessage notification into the
// store. Called by message-arrival code paths when the user is not
// currently viewing the originating channel.
func (m *Model) recordUnreadMessage(channelID, messageID, userID, userName, text string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:      notifications.TypeUnreadMessage,
		ChannelID: channelID,
		MessageID: messageID,
		UserID:    userID,
		UserName:  userName,
		Text:      text,
	})
}

// recordReaction drops a TypeReaction notification.
func (m *Model) recordReaction(channelID, messageID, reactorID, reactorName, emoji, targetText string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:             notifications.TypeReaction,
		ChannelID:        channelID,
		MessageID:        messageID,
		UserID:           reactorID,
		UserName:         reactorName,
		Emoji:            emoji,
		TargetMessageTxt: targetText,
	})
}

// recordFriendRequest drops a TypeFriendRequest notification.
func (m *Model) recordFriendRequest(senderID, senderName, pubKey, multiaddr string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:            notifications.TypeFriendRequest,
		ChannelID:       "friend:" + senderID,
		UserID:          senderID,
		UserName:        senderName,
		FriendPublicKey: pubKey,
		FriendMultiaddr: multiaddr,
	})
}

// hostPortFromMultiaddr parses a /ip4/<ip>/tcp/<port>/p2p/<id> string
// and returns just the host + port, used by the test-connection
// fallback to do a raw TCP socket dial when libp2p refuses to dial
// our own peer ID.
func hostPortFromMultiaddr(maddr string) (string, int) {
	parts := strings.Split(strings.TrimPrefix(maddr, "/"), "/")
	if len(parts) < 4 || parts[0] != "ip4" || parts[2] != "tcp" {
		return "", 0
	}
	port, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0
	}
	return parts[1], port
}

// FriendIdleTimeout is how long a friend chat must sit untouched
// before the inactivity watchdog drops its libp2p connection.
const FriendIdleTimeout = 60 * time.Second

// friendIdleCheckMsg fires periodically (~10s) so the model can
// disconnect any friends whose activity timestamp has aged past
// FriendIdleTimeout.
type friendIdleCheckMsg struct{}

func friendIdleCheckCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return friendIdleCheckMsg{}
	})
}

// touchFriendActivity records that the user just interacted with the
// given friend chat. The inactivity watchdog uses this to decide
// whether to keep the libp2p session open.
func (m *Model) touchFriendActivity(friendUID string) {
	if friendUID == "" {
		return
	}
	if m.friendActivity == nil {
		m.friendActivity = make(map[string]time.Time)
	}
	m.friendActivity[friendUID] = time.Now()
}

// connectFriend opens (or refreshes) a libp2p connection to the
// given friend, marks them online if reachable, and stamps the
// activity clock so the inactivity watchdog won't immediately drop
// it. No-op if P2P or the friend is missing. On successful connect
// the local profile is sent over so the peer can refresh any stale
// cached fields for us on their side.
// connectFriend kicks off a (re)dial to the given friend and, on
// success, runs the profile-sync / request-pending follow-up.
//
// IMPORTANT: the dial is dispatched in a goroutine so this method
// ALWAYS returns immediately. Running ConnectToPeer inline on the
// bubbletea Update loop would block the entire UI (including the
// input textarea) for the full dial timeout whenever the friend is
// offline. If the peer is already connected we skip the dial
// entirely and just refresh the activity clock. The send path has
// its own reconnect-and-retry logic in sendFriendMessageCmd, so
// the async dial here is purely opportunistic.
func (m *Model) connectFriend(friendUID string) {
	if m.p2pNode == nil || m.friendStore == nil || friendUID == "" {
		return
	}
	f := m.friendStore.Get(friendUID)
	if f == nil || f.Multiaddr == "" {
		return
	}
	m.touchFriendActivity(friendUID)
	if m.p2pNode.IsConnected(friendUID) {
		m.friendStore.SetOnline(friendUID, true)
		m.friendStore.UpdateLastOnline(friendUID)
		return
	}
	// Fire the dial in the background — never block Update.
	// Capture everything we need by value so the goroutine is
	// fully detached from the main model state.
	node := m.p2pNode
	store := m.friendStore
	multiaddr := f.Multiaddr
	uid := friendUID
	sendProfile := m.sendProfileSync
	sendRequest := m.sendRequestPending
	go func() {
		_ = node.ConnectToPeer(uid, multiaddr)
		if node.IsConnected(uid) {
			store.SetOnline(uid, true)
			store.UpdateLastOnline(uid)
			// These already ship as independent goroutines,
			// but calling them here ensures they only fire
			// on the offline→online edge observed from this
			// call site.
			sendProfile(uid)
			sendRequest(uid)
		} else {
			store.SetOnline(uid, false)
		}
	}()
}

// sendRequestPending asks a peer to scan its chat history for any
// messages addressed to us that are still flagged Pending and
// re-send them. Safe to call from any goroutine; no-op when the
// P2P layer isn't up.
func (m *Model) sendRequestPending(peerUID string) {
	if m.p2pNode == nil || peerUID == "" {
		return
	}
	_ = m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
		Type:      secure.MsgTypeRequestPending,
		SenderID:  peerUID,
		Timestamp: time.Now().Unix(),
	})
}

// buildSlackInviteMessage returns a Slack-mrkdwn-formatted invite
// string that embeds the local user's contact card JSON inside a
// code span on its own line, with a linkified "Slackers" word
// pointing at the project repo. The returned text is safe to send
// over Slack — the JSON payload is baked in directly so
// expandFriendMarkers won't try to re-encode it (which would
// replace ']' with '\u005d' and make the code span unreadable).
//
// Falls back to a simpler plain-text message if the local P2P
// identity isn't available (Secure Mode off).
func buildSlackInviteMessage(m Model) string {
	const slackersURL = "https://github.com/rw3iss/slackers"
	// Two \n produce a blank line before the marker so the JSON
	// always lands on its own paragraph in the recipient's Slack
	// client (single \n is treated as a soft wrap by some
	// renderers).
	const preface = "Hey! I'm using <" + slackersURL + "|Slackers> — paste this contact info into its Add Friend screen so we can chat privately over P2P:\n\n"

	if m.secureMgr == nil || m.p2pNode == nil {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	pub := m.secureMgr.OwnPublicKeyBase64()
	maddr := m.p2pNode.Multiaddr()
	if pub == "" || maddr == "" {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		pub,
		maddr,
	)
	raw, err := json.Marshal(card)
	if err != nil {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	return preface + "[FRIEND:" + string(raw) + "]"
}

// buildSidebarOptionsItems assembles the context-menu items for a
// right-clicked sidebar channel. Every entry gets Hide and Rename;
// DM / Private-group entries pointing at a user who is NOT already
// a slackers friend also get "Invite to Slackers"; friend entries
// get "View Contact Info" instead.
func (m *Model) buildSidebarOptionsItems(ch types.Channel) []sidebarOptionsItem {
	items := []sidebarOptionsItem{
		{"Hide Channel", SidebarActionHide},
		{"Rename Channel", SidebarActionRename},
	}
	// Friend channels: contact card viewer.
	if ch.IsFriend {
		items = append(items, sidebarOptionsItem{
			label: "View Contact Info", action: SidebarActionViewContact,
		})
		return items
	}
	// DM / private-group entries that represent a specific user and
	// are not already a friend → offer "Invite to Slackers". The
	// friend-store check uses the Slack UserID as the lookup key
	// since that's what sidebar DMs carry.
	if ch.UserID != "" && (ch.IsDM || ch.IsPrivate || ch.IsGroup) {
		alreadyFriend := false
		if m.friendStore != nil {
			if f := m.friendStore.Get(ch.UserID); f != nil {
				alreadyFriend = true
			}
			if !alreadyFriend {
				for _, f := range m.friendStore.All() {
					if f.SlackerID == ch.UserID || f.UserID == "slacker:"+ch.UserID {
						alreadyFriend = true
						break
					}
				}
			}
		}
		if !alreadyFriend {
			items = append(items, sidebarOptionsItem{
				label: "Invite to Slackers", action: SidebarActionInvite,
			})
		}
	}
	return items
}

// sendFriendMessageCmd returns a tea.Cmd that attempts to deliver
// a P2P text message to a friend. The peer is always (re)dialed
// before the send — libp2p.Connect is idempotent and this
// guarantees the P2PNode's internal peerMap is populated even when
// the friend came online by dialing us (inbound-only, we never
// dialed back). Without this up-front dial the first SendMessage
// fails with "peer X not connected" on the sender side even though
// an active libp2p connection exists, which left pending messages
// stuck in place until the next ping-cycle retry.
//
// On a send failure the command does one more reconnect-and-retry
// pass with a short backoff before giving up. The returned
// FriendSendResultMsg.Success is true only if the final wire send
// returned no error.
func sendFriendMessageCmd(node *secure.P2PNode, store *friends.FriendStore, peerUID string, fm secure.P2PMessage) tea.Cmd {
	return func() tea.Msg {
		if f := store.Get(peerUID); f != nil && f.Multiaddr != "" {
			_ = node.ConnectToPeer(peerUID, f.Multiaddr)
		}
		err := node.SendMessage(peerUID, fm)
		if err != nil {
			// Brief pause before retry — lets any in-flight
			// dial / handshake finish if the first send raced
			// the new connection.
			time.Sleep(150 * time.Millisecond)
			if f := store.Get(peerUID); f != nil && f.Multiaddr != "" {
				_ = node.ConnectToPeer(peerUID, f.Multiaddr)
			}
			err = node.SendMessage(peerUID, fm)
		}
		return FriendSendResultMsg{
			PeerUID:   peerUID,
			MessageID: fm.MessageID,
			Success:   err == nil,
		}
	}
}

// resendPendingFriendMessagesCmd returns a tea.Cmd that queries the
// friend chat history for any messages still marked Pending and
// fires a sendFriendMessageCmd for each in chronological order.
//
// IMPORTANT: uses tea.Sequence rather than tea.Batch so each send
// completes (and its FriendSendResultMsg is processed) before the
// next one starts. A parallel batch lets the wire sends race each
// other, which causes the receiver to observe messages out of
// order — making the chat history appear scrambled on the other
// side. The original send Timestamp also travels on the wire so
// the receiver can stamp each message with its true send time
// instead of the arrival time.
func (m *Model) resendPendingFriendMessagesCmd(peerUID string) tea.Cmd {
	if m.p2pNode == nil || m.friendHistory == nil || m.friendStore == nil || peerUID == "" {
		return nil
	}
	pairKey := ""
	if f := m.friendStore.Get(peerUID); f != nil {
		pairKey = f.PairKey
	}
	pending := m.friendHistory.PendingForResend(peerUID, pairKey)
	if len(pending) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, pm := range pending {
		fm := secure.P2PMessage{
			Type:         secure.MsgTypeMessage,
			Text:         pm.Text,
			SenderID:     peerUID,
			Timestamp:    pm.Timestamp.Unix(),
			MessageID:    pm.MessageID,
			ReplyToMsgID: pm.ReplyTo,
		}
		cmds = append(cmds, sendFriendMessageCmd(m.p2pNode, m.friendStore, peerUID, fm))
	}
	return tea.Sequence(cmds...)
}

// sendProfileSync announces the local user's current contact card
// (as single-line JSON) to the given peer. The receiver merges any
// fresh fields into their stored friend record for us without
// overwriting their locally-chosen display name. Safe to call from
// any goroutine; a nil/offline node is a silent no-op.
func (m *Model) sendProfileSync(peerUID string) {
	if m.p2pNode == nil || m.secureMgr == nil || peerUID == "" {
		return
	}
	pub := m.secureMgr.OwnPublicKeyBase64()
	maddr := m.p2pNode.Multiaddr()
	if pub == "" || maddr == "" {
		return
	}
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		pub,
		maddr,
	)
	raw, err := json.Marshal(card)
	if err != nil {
		return
	}
	_ = m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
		Type:      secure.MsgTypeProfileSync,
		Text:      string(raw),
		SenderID:  peerUID,
		Timestamp: time.Now().Unix(),
	})
}

// mergeFriendProfile refreshes an existing friend record with the
// values from a peer-announced contact card. Matching is tried
// first by the sender UID we received the packet over, then via
// FindByCard (SlackerID / PublicKey / Multiaddr). The locally-set
// display name is preserved unless it was empty — it is the user's
// own alias for that friend and shouldn't get clobbered by what the
// peer calls themselves.
//
// Saves the friend store on any change and refreshes the sidebar
// labels so a renamed peer shows up right away.
func (m *Model) mergeFriendProfile(senderUID string, card friends.ContactCard) {
	if m.friendStore == nil {
		return
	}
	var existing *friends.Friend
	if senderUID != "" {
		existing = m.friendStore.Get(senderUID)
	}
	if existing == nil {
		existing = m.friendStore.FindByCard(card)
	}
	if existing == nil {
		return
	}
	updated := *existing
	changed := false
	if card.Name != "" && updated.Name == "" {
		updated.Name = card.Name
		changed = true
	}
	if card.Email != "" && updated.Email != card.Email {
		updated.Email = card.Email
		changed = true
	}
	if card.PublicKey != "" && updated.PublicKey != card.PublicKey {
		updated.PublicKey = card.PublicKey
		// New public key means the ECDH-derived pair key is
		// stale; clear it so the next handshake re-derives.
		updated.PairKey = ""
		changed = true
	}
	if card.Multiaddr != "" && updated.Multiaddr != card.Multiaddr {
		updated.Multiaddr = card.Multiaddr
		changed = true
	}
	if card.SlackerID != "" && updated.SlackerID != card.SlackerID {
		updated.SlackerID = card.SlackerID
		changed = true
	}
	if !changed {
		return
	}
	if err := m.friendStore.Update(updated); err == nil {
		go m.friendStore.Save()
		m.channels.SetFriendChannels(m.buildFriendChannels())
	}
}

// friendPingTickCmd schedules the next friend-status ping. The
// interval is controlled by cfg.FriendPingSeconds (minimum 2s) so
// users on slow networks can back the poll off without rebuilding.
// A zero / missing value falls back to the 5s default.
func (m *Model) friendPingTickCmd() tea.Cmd {
	secs := 5
	if m.cfg != nil && m.cfg.FriendPingSeconds > 0 {
		secs = m.cfg.FriendPingSeconds
	}
	if secs < 2 {
		secs = 2
	}
	return tea.Tick(time.Duration(secs)*time.Second, func(t time.Time) tea.Msg {
		return friendPingTickMsg{}
	})
}

// friendCardLabel returns a short human-friendly name for a contact
// card — used in confirmation prompts when a card is clicked/imported.
// Mirrors the same Name → Email → ShortPeerID priority used by the
// in-chat pill renderer so confirmations and pills match.
func friendCardLabel(card friends.ContactCard) string {
	if s := strings.TrimSpace(card.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(card.Email); s != "" {
		return s
	}
	if s := friends.ShortPeerID(card); s != "" {
		return s
	}
	return "unknown"
}

// applyFriendCard saves an incoming contact card into the friend
// store. Behaviour depends on the flags:
//   - merge=false replace=false: add as a new friend (errors if one
//     with the same SlackerID already exists).
//   - merge=true: fill only missing fields on the existing record,
//     never overwrite present ones.
//   - replace=true: overwrite every persistent field on the existing
//     record (runtime-only fields like Online are preserved by Update).
//
// On success the friend sidebar is refreshed. For brand-new friends
// a FriendAddedHandshakeMsg is dispatched so the existing P2P
// handshake flow runs exactly as if the user had added them from the
// Add a Friend page.
func (m *Model) applyFriendCard(card friends.ContactCard, merge, replace bool) tea.Cmd {
	if m.friendStore == nil {
		m.warning = "Friend store not available"
		return nil
	}
	card.Multiaddr = strings.TrimSpace(card.Multiaddr)
	label := friendCardLabel(card)

	// Locate any existing record by SlackerID, PublicKey, or
	// Multiaddr (in that priority order). FindByCard centralises the
	// matching rules used elsewhere in the import flow.
	existing := m.friendStore.FindByCard(card)

	switch {
	case existing == nil:
		// Brand new friend. SLF2 hash imports arrive with no Name
		// or Email — fill in a synthetic placeholder so the friend
		// list always has something to display until the real name
		// arrives over the wire.
		f := friends.FriendFromCard(card)
		if strings.TrimSpace(f.Name) == "" {
			f.Name = friends.FallbackName(card)
		}
		if err := m.friendStore.Add(f); err != nil {
			m.warning = "Import failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Saved in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Imported friend " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		newUID := f.UserID
		if newUID == "" && f.SlackerID != "" {
			newUID = "slacker:" + f.SlackerID
		}
		if newUID != "" {
			return func() tea.Msg {
				return FriendAddedHandshakeMsg{
					UserID:    newUID,
					Name:      f.Name,
					Multiaddr: f.Multiaddr,
				}
			}
		}
		return nil

	case merge:
		updated := *existing
		if updated.Name == "" && card.Name != "" {
			updated.Name = card.Name
		}
		if updated.Email == "" && card.Email != "" {
			updated.Email = card.Email
		}
		if updated.PublicKey == "" && card.PublicKey != "" {
			updated.PublicKey = card.PublicKey
		}
		if updated.Multiaddr == "" && card.Multiaddr != "" {
			updated.Multiaddr = card.Multiaddr
		}
		if updated.SlackerID == "" && card.SlackerID != "" {
			updated.SlackerID = card.SlackerID
		}
		if err := m.friendStore.Update(updated); err != nil {
			m.warning = "Merge failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Merged in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Merged contact card into " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		return nil

	case replace:
		updated := *existing
		updated.Name = card.Name
		updated.Email = card.Email
		updated.PublicKey = card.PublicKey
		updated.Multiaddr = card.Multiaddr
		if card.SlackerID != "" {
			updated.SlackerID = card.SlackerID
		}
		// Dropping the stale per-pair key forces a fresh ECDH
		// derivation on the next handshake with the new public key.
		updated.PairKey = ""
		if err := m.friendStore.Update(updated); err != nil {
			m.warning = "Replace failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Replaced in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Replaced contact card for " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		return nil
	}

	return nil
}
