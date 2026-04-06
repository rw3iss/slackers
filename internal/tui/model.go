package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/secure"
	"github.com/rw3iss/slackers/internal/shortcuts"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
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
)

// fileBrowserPurpose tracks why the file browser is open.
type fileBrowserPurpose int

const (
	fbPurposeAttach   fileBrowserPurpose = iota // selecting a file to send
	fbPurposeSettings                           // selecting a download folder
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

// SeedLastSeenMsg carries baseline timestamps without triggering unread markers.
type SeedLastSeenMsg struct {
	Timestamps map[string]string
}

// ActivityCheckMsg triggers an away-status check.
type ActivityCheckMsg struct{}

// ClearWarningMsg clears the status bar warning if the user was recently active.
type ClearWarningMsg struct{}

// WhitelistOpenMsg signals that the whitelist overlay should open.
type WhitelistOpenMsg struct{}

// P2PReceivedMsg is sent when a message arrives over the P2P connection.
type P2PReceivedMsg struct {
	SenderID string
	Text     string
}

// SecureSessionReadyMsg signals that a secure session was established with a peer.
type SecureSessionReadyMsg struct {
	PeerID string
	State  secure.SessionState
}

// channelInfo stores the name and alias for a channel.
type channelInfo struct {
	name  string
	alias string
}

var filePattern = regexp.MustCompile(`\[FILE:([^\]]+)\]`)

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
	channels ChannelListModel
	messages MessageViewModel
	input    InputModel
	keymap   KeyMap
	settings SettingsModel
	search   SearchModel
	hidden   HiddenChannelsModel
	rename      RenameModel
	msgSearch   MsgSearchModel
	fileBrowser FileBrowserModel
	fbPurpose   fileBrowserPurpose
	filesList       FilesListModel
	shortcutsEditor ShortcutsEditorModel
	whitelist       WhitelistModel

	// State
	focus      types.Focus
	overlay    overlay
	currentCh  *types.Channel
	users      map[string]types.User
	connStatus types.ConnectionStatus
	connErr    error
	teamName   string
	err        error
	warning    string

	// Secure messaging
	secureMgr  *secure.SessionManager
	p2pNode    *secure.P2PNode
	p2pChan    chan P2PReceivedMsg

	// Shortcuts
	shortcutMap shortcuts.ShortcutMap
	shortcutOverrides shortcuts.ShortcutMap

	// Channel index: ID -> {name, alias}
	channelIndex map[string]channelInfo

	// Polling
	lastSeen      map[string]string
	lastChecked   map[string]time.Time // when each channel was last polled
	pollChannels  []string             // ordered list for round-robin polling
	pollOffset    int

	// Config
	cfg *config.Config

	// Dependencies (interfaces for SOLID)
	slackSvc  slackpkg.SlackService
	socketSvc slackpkg.SocketService
	eventChan chan slackpkg.SocketEvent

	// Activity tracking
	lastActivity   time.Time
	isAway         bool

	// Download state
	downloading    bool
	downloadCancel context.CancelFunc

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
func NewModel(slackSvc slackpkg.SlackService, socketSvc slackpkg.SocketService, cfg *config.Config, version string) Model {
	ch := NewChannelList()
	ch.SetFocused(true)

	inp := NewInput()
	inp.SetHistory(cfg.InputHistory)
	histMax := cfg.InputHistoryMax
	if histMax <= 0 {
		histMax = 20
	}
	inp.SetMaxHistory(histMax)

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
			p2pChan <- P2PReceivedMsg{SenderID: peerSlackID, Text: msg.Text}
		}
		p2pNode, _ = secure.NewP2PNode(port, cfg.P2PAddress, onMsg)
	}

	// Load and merge shortcuts.
	defaults := shortcuts.DefaultShortcuts()
	overrides, _ := shortcuts.Load(shortcuts.UserConfigPath())
	merged := shortcuts.Merge(defaults, overrides)
	km := BuildKeyMap(merged)

	return Model{
		channels:          ch,
		messages:          NewMessageView(),
		input:             inp,
		keymap:            km,
		secureMgr:         secureMgr,
		p2pNode:           p2pNode,
		p2pChan:           p2pChan,
		shortcutMap:       merged,
		shortcutOverrides: overrides,
		settings:          NewSettingsModel(cfg, version),
		focus:     types.FocusSidebar,
		users:     make(map[string]types.User),
		channelIndex: make(map[string]channelInfo),
		lastSeen:     loadLastSeen(cfg),
		lastChecked:  make(map[string]time.Time),
		cfg:       cfg,
		lastActivity: time.Now(),
		splash:       true,
		initialLoad:  true,
		version:      version,
		slackSvc:     slackSvc,
		socketSvc: socketSvc,
		eventChan: make(chan slackpkg.SocketEvent, 100),
	}
}

// Init returns the initial commands to run at startup.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		splashTimerCmd(),
		checkUpdateCmd(m.version),
		loadUsersCmd(m.slackSvc),
		connectSocketCmd(m.socketSvc, m.eventChan),
		waitForSocketEvent(m.eventChan),
		pollTickCmd(m.cfg.PollInterval),
		bgPollTickCmd(m.cfg.PollIntervalBg),
		activityCheckCmd(m.cfg.AwayTimeout),
	}
	if m.p2pChan != nil {
		cmds = append(cmds, waitForP2PMsg(m.p2pChan))
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
		if m.overlay != overlayNone || m.splash {
			return m, nil
		}
		return m.handleMouse(msg)

	case tea.KeyMsg:
		// Track activity for away detection.
		m.lastActivity = time.Now()
		clearWindowUrgent()

		// When the shortcuts editor is capturing a key, bypass ALL other handlers.
		// This prevents quit, help, settings, etc. from firing during rebind.
		if m.overlay == overlayShortcuts && m.shortcutsEditor.IsCapturing() {
			var cmd tea.Cmd
			m.shortcutsEditor, cmd = m.shortcutsEditor.Update(msg)
			return m, cmd
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

		// Global shortcuts that work even in overlays
		switch {
		case key.Matches(msg, m.keymap.Quit):
			clearWindowUrgent()
			if m.p2pNode != nil {
				_ = m.p2pNode.Close()
			}
			return m, tea.Quit

		case key.Matches(msg, m.keymap.Help):
			if m.overlay == overlayHelp {
				m.overlay = overlayNone
			} else {
				m.overlay = overlayHelp
			}
			return m, nil

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
		}

		// If an overlay is open, handle its input
		if m.overlay == overlayHelp {
			if msg.String() == "esc" {
				m.overlay = overlayNone
			}
			return m, nil
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
			var cmd tea.Cmd
			m.hidden, cmd = m.hidden.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayRename {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.rename, cmd = m.rename.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayMsgSearch {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
			var cmd tea.Cmd
			m.msgSearch, cmd = m.msgSearch.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayFileBrowser {
			if msg.String() == "esc" {
				m.overlay = overlayNone
				return m, nil
			}
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
			var cmd tea.Cmd
			m.shortcutsEditor, cmd = m.shortcutsEditor.Update(msg)
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
			if m.focus == types.FocusSidebar {
				// Toggle back to input if already on sidebar
				m.focus = types.FocusInput
			} else {
				m.focus = types.FocusSidebar
			}
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
			if m.downloading && m.downloadCancel != nil {
				m.downloadCancel()
				m.downloading = false
				m.downloadCancel = nil
				m.warning = "Download cancelled"
				return m, nil
			}

		case key.Matches(msg, m.keymap.Escape):
			if m.focus == types.FocusSidebar {
				m.focus = types.FocusInput
			} else {
				m.focus = types.FocusSidebar
			}
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.ToggleFullMode):
			m.fullMode = !m.fullMode
			m.resizeComponents()
			return m, nil

		case key.Matches(msg, m.keymap.Refresh):
			return m, loadChannelsCmd(m.slackSvc)

		case key.Matches(msg, m.keymap.NextUnread):
			ch := m.channels.NextUnreadChannel()
			if ch != nil {
				m.currentCh = ch
				m.channels.ClearUnread(ch.ID)
				m.setChannelHeader()
				m.saveLastChannel(ch.ID)
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
					_ = config.Save(m.cfg)
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
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
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
		go config.Save(m.cfg)
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
				m.channels.UnhideChannel(ch.ID)
				m.setChannelHeader()
				m.saveLastChannel(ch.ID)
				return m, loadHistoryCmd(m.slackSvc, ch.ID)
			}
		}
		return m, nil

	case UnhideChannelMsg:
		m.channels.UnhideChannel(msg.ChannelID)
		m.cfg.HiddenChannels = m.channels.HiddenChannelIDs()
		_ = config.Save(m.cfg)
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
		_ = config.Save(m.cfg)
		return m, nil

	case ChannelsLoadedMsg:
		m.channels.SetChannels(msg.Channels)
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
		drainWarnings(&m)

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
		drainWarnings(&m)
		// Now that users are cached, load channels so DM names resolve properly.
		return m, loadChannelsCmd(m.slackSvc)

	case MessageSentMsg:
		drainWarnings(&m)
		// Refresh history to show the sent message (socket events may not
		// arrive if the bot isn't in the channel).
		if m.currentCh != nil {
			return m, loadHistoryCmd(m.slackSvc, m.currentCh.ID)
		}
		return m, nil

	case SlackEventMsg:
		switch msg.Event.Type {
		case "message":
			evMsg := msg.Event.Message
			ts := msg.Event.SlackTS
			// Update lastSeen for the current channel so polling doesn't
			// re-detect this message. For other channels, don't update
			// lastSeen (that would hide the unread flag).
			if m.currentCh != nil && evMsg.ChannelID == m.currentCh.ID {
				if ts != "" {
					m.lastSeen[evMsg.ChannelID] = ts
				}
				m.messages.AppendMessage(evMsg)
			} else {
				m.channels.MarkUnread(evMsg.ChannelID)
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
		rotationSize := 5
		batch := make(map[string]string)

		if len(m.pollChannels) > 0 {
			type chCheck struct{ id string; checked time.Time }
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
		})
		m.fileBrowser.SetSize(m.width, m.height)
		m.fbPurpose = fbPurposeSettings
		m.overlay = overlayFileBrowser
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
				_ = config.Save(m.cfg)
				// Update the settings field value and reopen settings.
				m.settings = NewSettingsModel(m.cfg, m.version)
				m.settings.SetSize(m.width, m.height)
				m.overlay = overlaySettings
				return m, nil
			}
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
		if m.currentCh != nil {
			return m, loadHistoryCmd(m.slackSvc, m.currentCh.ID)
		}
		return m, nil

	case InputSendMsg:
		text := msg.Text
		if text != "" && m.currentCh != nil {
			m.input.PushHistory(text)
			m.cfg.InputHistory = m.input.History()
			go config.Save(m.cfg)
			m.input.Reset()

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

			return m, sendMessageWithFilesCmd(m.slackSvc, m.currentCh.ID, sendText)
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
		// A message arrived over P2P — display it in the current DM if it matches.
		if m.currentCh != nil && m.currentCh.IsDM && m.currentCh.UserID == msg.SenderID {
			userName := msg.SenderID
			if u, ok := m.users[msg.SenderID]; ok {
				userName = u.DisplayName
			}
			p2pMsg := types.Message{
				UserID:    msg.SenderID,
				UserName:  userName,
				Text:      "🔒 " + msg.Text,
				Timestamp: time.Now(),
			}
			m.messages.AppendMessage(p2pMsg)
		}
		// Continue listening for more P2P messages.
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
		go config.Save(m.cfg)
		return m, nil

	case ErrMsg:
		return m, setError(&m, msg.Err)
	}

	return m, nil
}

// View renders the full TUI layout.
func (m Model) View() string {
	if !m.ready {
		return ""
	}

	if m.splash {
		return renderSplash(m.width, m.height, m.version)
	}

	// Render overlays on top
	switch m.overlay {
	case overlayHelp:
		return renderHelp(m.width, m.height, m.version)
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

// applySettings reads the current config and resizes components.
func (m *Model) applySettings() {
	m.cfg = m.settings.Config()
	sortAsc := true
	if m.cfg.ChannelSortAsc != nil {
		sortAsc = *m.cfg.ChannelSortAsc
	}
	sortBy := m.cfg.ChannelSortBy
	if sortBy == "" {
		sortBy = SortByType
	}
	m.channels.SetSort(sortBy, sortAsc)
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
			go config.Save(m.cfg)
			return m, nil
		}
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
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

			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
			} else if x < m.sidebarWidth+1 {
				// Sidebar clicked.
				m.focus = types.FocusSidebar
				m.updateFocus()

				ch, isChannel, headerKey := m.channels.SelectByRow(y)
				if headerKey != "" {
					// Header clicked — toggle collapse.
					m.channels.ToggleCollapse(headerKey)
					m.channels.buildRows()
					m.cfg.CollapsedGroups = m.channels.CollapsedGroups()
					go config.Save(m.cfg)
				} else if isChannel && ch != nil {
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
					return m, loadHistoryCmd(m.slackSvc, ch.ID)
				}
			} else {
				// Messages area clicked.
				m.focus = types.FocusMessages
				m.updateFocus()

				// Check if a file was clicked.
				file := m.messages.FileAtClick(y)
				if file != nil {
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

func (m *Model) cycleFocusForward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusMessages
	case types.FocusMessages:
		m.focus = types.FocusInput
	case types.FocusInput:
		m.focus = types.FocusSidebar
	}
}

func (m *Model) cycleFocusBackward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusInput
	case types.FocusMessages:
		m.focus = types.FocusSidebar
	case types.FocusInput:
		m.focus = types.FocusMessages
	}
}

// saveLastChannel persists the currently viewed channel ID to config.
func (m *Model) saveLastChannel(channelID string) {
	m.cfg.LastChannelID = channelID
	go config.Save(m.cfg) // fire-and-forget, don't block the UI
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
	go config.Save(m.cfg)
}

// setChannelHeader updates the message view header with channel name and secure indicator.
func (m *Model) setChannelHeader() {
	if m.currentCh == nil {
		return
	}
	m.messages.SetChannelName("#" + m.channels.displayName(*m.currentCh))
	m.messages.SetSecureLabel(m.secureIndicator())
}

// secureIndicator returns a status label for the current channel's secure state.
func (m *Model) secureIndicator() string {
	if m.secureMgr == nil || m.currentCh == nil || !m.currentCh.IsDM {
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

	var connStr string
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

	left := fmt.Sprintf(" %s | %s%s", connStr, hints, extra)
	right := StatusBarStyle.Render(fmt.Sprintf("slackers v%s ", m.version))

	// Pad the middle to push right label to the edge.
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	status := left + strings.Repeat(" ", gap) + right

	return StatusBarStyle.Width(m.width).Render(status)
}

// Command functions

func loadUsersCmd(svc slackpkg.SlackService) tea.Cmd {
	return func() tea.Msg {
		users, err := svc.ListUsers()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return UsersLoadedMsg{Users: users}
	}
}

func loadChannelsCmd(svc slackpkg.SlackService) tea.Cmd {
	return func() tea.Msg {
		channels, err := svc.ListChannels()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return ChannelsLoadedMsg{Channels: channels}
	}
}

func loadHistoryCmd(svc slackpkg.SlackService, channelID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := svc.FetchHistory(channelID, 50)
		if err != nil {
			// Return empty history so the channel still opens,
			// plus the error to display in the status bar.
			return HistoryLoadedMsg{Messages: nil, Err: err}
		}
		return HistoryLoadedMsg{Messages: msgs}
	}
}

func sendMessageCmd(svc slackpkg.SlackService, channelID, text string) tea.Cmd {
	return func() tea.Msg {
		err := svc.SendMessage(channelID, text)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return MessageSentMsg{}
	}
}

func connectSocketCmd(socketSvc slackpkg.SocketService, eventCh chan slackpkg.SocketEvent) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := socketSvc.Connect(ctx, eventCh)
		if err != nil {
			return ConnStatusMsg{Status: types.StatusError, Err: err}
		}
		return ConnStatusMsg{Status: types.StatusConnected}
	}
}

func waitForSocketEvent(ch chan slackpkg.SocketEvent) tea.Cmd {
	return func() tea.Msg {
		event := <-ch
		return SlackEventMsg{Event: event}
	}
}

func loadMoreContextCmd(svc slackpkg.SlackService, channelID, oldestTS string) tea.Cmd {
	return func() tea.Msg {
		params := 25
		msgs, err := svc.FetchHistory(channelID, params)
		if err != nil {
			return ErrMsg{Err: err}
		}
		// FetchHistory returns chronological. We need messages BEFORE oldestTS.
		// Use FetchHistoryAround with the oldest timestamp to get earlier messages.
		olderMsgs, _, err := svc.FetchHistoryAround(channelID, oldestTS, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		// Filter to only messages older than oldestTS.
		_ = msgs // unused, we use FetchHistoryAround directly
		var filtered []types.Message
		for _, m := range olderMsgs {
			ts := fmt.Sprintf("%d.%06d", m.Timestamp.Unix(), m.Timestamp.Nanosecond()/1000)
			if ts < oldestTS {
				filtered = append(filtered, m)
			}
		}
		return MoreContextLoadedMsg{Messages: filtered}
	}
}

func silentLoadHistoryCmd(svc slackpkg.SlackService, channelID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := svc.FetchHistory(channelID, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return SilentHistoryMsg{Messages: msgs}
	}
}

func fetchContextCmd(svc slackpkg.SlackService, channelID, timestamp, channelName string) tea.Cmd {
	return func() tea.Msg {
		msgs, targetIdx, err := svc.FetchHistoryAround(channelID, timestamp, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return ContextHistoryMsg{
			Messages:    msgs,
			TargetIdx:   targetIdx,
			ChannelName: channelName,
		}
	}
}

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

// sendMessageWithFilesCmd parses [FILE:<path>] patterns from the message,
// uploads any files, and sends the remaining text as a message.
func sendMessageWithFilesCmd(svc slackpkg.SlackService, channelID, text string) tea.Cmd {
	return func() tea.Msg {
		matches := filePattern.FindAllStringSubmatch(text, -1)
		cleanText := strings.TrimSpace(filePattern.ReplaceAllString(text, ""))

		// Upload files
		uploadCount := 0
		for _, match := range matches {
			if len(match) >= 2 {
				path := match[1]
				if err := svc.UploadFile(channelID, path); err == nil {
					uploadCount++
				}
			}
		}

		// Send remaining text if any
		if cleanText != "" {
			if err := svc.SendMessage(channelID, cleanText); err != nil {
				return ErrMsg{Err: err}
			}
		}

		if uploadCount > 0 {
			return FileUploadedMsg{Count: uploadCount}
		}
		return MessageSentMsg{}
	}
}

// seedLastSeenCmd fetches baseline timestamps for unseeded channels.
// Batches requests to avoid rate limits (5 channels per batch with delays).
func seedLastSeenCmd(svc slackpkg.SlackService, lastSeen map[string]string) tea.Cmd {
	channelIDs := make([]string, 0)
	for id, ts := range lastSeen {
		if ts == "0" {
			channelIDs = append(channelIDs, id)
		}
	}

	return func() tea.Msg {
		timestamps := make(map[string]string)
		// Process in small batches to respect rate limits.
		for i := 0; i < len(channelIDs); i += 5 {
			end := i + 5
			if end > len(channelIDs) {
				end = len(channelIDs)
			}
			batch := make(map[string]string)
			for _, id := range channelIDs[i:end] {
				batch[id] = "0"
			}
			_, resultTS, _ := svc.CheckNewMessages(batch)
			for id, ts := range resultTS {
				timestamps[id] = ts
			}
			// Small delay between batches to stay under rate limits.
			if end < len(channelIDs) {
				time.Sleep(2 * time.Second)
			}
		}
		return SeedLastSeenMsg{Timestamps: timestamps}
	}
}

func checkUpdateCmd(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get("https://api.github.com/repos/rw3iss/slackers/releases/latest")
		if err != nil {
			return UpdateAvailableMsg{} // silently fail
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return UpdateAvailableMsg{}
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return UpdateAvailableMsg{}
		}
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return UpdateAvailableMsg{}
		}
		latest := strings.TrimPrefix(release.TagName, "v")
		if latest != currentVersion && latest > currentVersion {
			return UpdateAvailableMsg{Version: release.TagName}
		}
		return UpdateAvailableMsg{}
	}
}

func clearWarningCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return ClearWarningMsg{}
	})
}

func activityCheckCmd(awayTimeoutSec int) tea.Cmd {
	if awayTimeoutSec <= 0 {
		// Away detection disabled — check again in 30s in case setting changes.
		return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
			return ActivityCheckMsg{}
		})
	}
	// Check at half the timeout interval for responsiveness.
	interval := time.Duration(awayTimeoutSec) * time.Second / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return ActivityCheckMsg{}
	})
}

func pollTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 1 {
		intervalSec = 10
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return PollTickMsg{}
	})
}

func bgPollTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 5 {
		intervalSec = 30
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return BgPollTickMsg{}
	})
}

func checkNewMessagesCmd(svc slackpkg.SlackService, lastSeen map[string]string, intervalSec int) tea.Cmd {
	seen := make(map[string]string, len(lastSeen))
	for k, v := range lastSeen {
		seen[k] = v
	}

	return func() tea.Msg {
		ids, latestTS, err := svc.CheckNewMessages(seen)
		if err != nil {
			return UnreadChannelsMsg{LatestTS: latestTS}
		}
		return UnreadChannelsMsg{ChannelIDs: ids, LatestTS: latestTS}
	}
}

func checkNewMessagesBgCmd(svc slackpkg.SlackService, lastSeen map[string]string, intervalSec int) tea.Cmd {
	seen := make(map[string]string, len(lastSeen))
	for k, v := range lastSeen {
		seen[k] = v
	}

	return func() tea.Msg {
		ids, latestTS, err := svc.CheckNewMessages(seen)
		if err != nil {
			return UnreadChannelsMsg{LatestTS: latestTS, IsBackground: true}
		}
		return UnreadChannelsMsg{ChannelIDs: ids, LatestTS: latestTS, IsBackground: true}
	}
}
