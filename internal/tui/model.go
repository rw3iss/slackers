package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/config"
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
)

// Custom message types for the TUI update loop.

type ChannelsLoadedMsg struct{ Channels []types.Channel }
type HistoryLoadedMsg struct{ Messages []types.Message }
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

// ContextHistoryMsg carries messages around a search result for context viewing.
type ContextHistoryMsg struct {
	Messages    []types.Message
	TargetIdx   int
	ChannelName string
}

// PollTickMsg triggers a new-message poll.
type PollTickMsg struct{}

// UnreadChannelsMsg carries channel IDs with new messages and all latest timestamps.
type UnreadChannelsMsg struct {
	ChannelIDs []string
	LatestTS   map[string]string
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
	rename    RenameModel
	msgSearch MsgSearchModel

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

	// Polling
	lastSeen map[string]string // channelID -> latest message timestamp

	// Config
	cfg *config.Config

	// Dependencies (interfaces for SOLID)
	slackSvc  slackpkg.SlackService
	socketSvc slackpkg.SocketService
	eventChan chan slackpkg.SocketEvent

	// Layout
	width   int
	height  int
	ready   bool
	splash  bool
}

// NewModel creates a new root TUI model.
func NewModel(slackSvc slackpkg.SlackService, socketSvc slackpkg.SocketService, cfg *config.Config) Model {
	ch := NewChannelList()
	ch.SetFocused(true)

	return Model{
		channels:  ch,
		messages:  NewMessageView(),
		input:     NewInput(),
		keymap:    DefaultKeyMap(),
		settings:  NewSettingsModel(cfg),
		focus:     types.FocusSidebar,
		users:     make(map[string]types.User),
		lastSeen:  make(map[string]string),
		cfg:       cfg,
		splash:    true,
		slackSvc:  slackSvc,
		socketSvc: socketSvc,
		eventChan: make(chan slackpkg.SocketEvent, 100),
	}
}

// Init returns the initial commands to run at startup.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		splashTimerCmd(),
		loadUsersCmd(m.slackSvc),
		connectSocketCmd(m.socketSvc, m.eventChan),
		waitForSocketEvent(m.eventChan),
		pollTickCmd(m.cfg.PollInterval),
	)
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

	case tea.KeyMsg:
		// Clear urgency on any user interaction.
		clearWindowUrgent()

		// Global shortcuts that work even in overlays
		switch {
		case key.Matches(msg, m.keymap.Quit):
			clearWindowUrgent()
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
				m.settings = NewSettingsModel(m.cfg)
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
				m.msgSearch = NewMsgSearchModel(m.slackSvc, chID)
				m.msgSearch.SetSize(m.width, m.height)
				m.overlay = overlayMsgSearch
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

		case key.Matches(msg, m.keymap.FocusInput):
			if m.focus != types.FocusInput {
				m.focus = types.FocusInput
				m.updateFocus()
				return m, nil
			}

		case key.Matches(msg, m.keymap.Refresh):
			return m, loadChannelsCmd(m.slackSvc)

		case key.Matches(msg, m.keymap.NextUnread):
			ch := m.channels.NextUnreadChannel()
			if ch != nil {
				m.currentCh = ch
				m.channels.ClearUnread(ch.ID)
				m.messages.SetChannelName("#" + m.channels.displayName(*ch))
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

		case key.Matches(msg, m.keymap.Enter):
			if m.focus == types.FocusSidebar {
				ch := m.channels.SelectedChannel()
				if ch != nil {
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.messages.SetChannelName("#" + ch.Name)
					m.saveLastChannel(ch.ID)
					return m, loadHistoryCmd(m.slackSvc, ch.ID)
				}
				return m, nil
			}
			if m.focus == types.FocusInput {
				text := m.input.Value()
				if text != "" && m.currentCh != nil {
					m.input.Reset()
					return m, sendMessageCmd(m.slackSvc, m.currentCh.ID, text)
				}
				return m, nil
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
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
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
				m.messages.SetChannelName("#" + m.channels.displayName(ch))
				m.saveLastChannel(ch.ID)
				return m, loadHistoryCmd(m.slackSvc, ch.ID)
			}
		}
		return m, nil

	case UnhideChannelMsg:
		m.channels.UnhideChannel(msg.ChannelID)
		m.cfg.HiddenChannels = m.channels.HiddenChannelIDs()
		_ = config.Save(m.cfg)
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
		_ = config.Save(m.cfg)
		return m, nil

	case ChannelsLoadedMsg:
		m.channels.SetChannels(msg.Channels)
		m.channels.SetHiddenChannels(m.cfg.HiddenChannels)
		m.channels.SetAliases(m.cfg.ChannelAliases)
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

		// Restore last viewed channel on first load.
		if m.currentCh == nil && m.cfg.LastChannelID != "" {
			for _, ch := range msg.Channels {
				if ch.ID == m.cfg.LastChannelID {
					m.currentCh = &ch
					m.channels.SelectByID(ch.ID)
					m.messages.SetChannelName("#" + m.channels.displayName(ch))
					return m, loadHistoryCmd(m.slackSvc, ch.ID)
				}
			}
		}
		return m, nil

	case SilentHistoryMsg:
		// Always update the view — the poll already confirmed new messages exist.
		m.messages.SetMessagesSilent(msg.Messages)
		if m.currentCh != nil && len(msg.Messages) > 0 {
			latest := msg.Messages[len(msg.Messages)-1]
			m.lastSeen[m.currentCh.ID] = fmt.Sprintf("%d.%06d", latest.Timestamp.Unix(), latest.Timestamp.Nanosecond()/1000)
		}
		return m, nil

	case HistoryLoadedMsg:
		m.messages.SetMessages(msg.Messages)
		// Record the latest message timestamp so the poller won't re-flag this channel.
		if m.currentCh != nil && len(msg.Messages) > 0 {
			latest := msg.Messages[len(msg.Messages)-1]
			m.lastSeen[m.currentCh.ID] = fmt.Sprintf("%d.%06d", latest.Timestamp.Unix(), latest.Timestamp.Nanosecond()/1000)
		}
		m.focus = types.FocusInput
		m.updateFocus()
		drainWarnings(&m)
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
			if m.currentCh != nil && evMsg.ChannelID == m.currentCh.ID {
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
		// Ensure the current channel is always polled even if not yet in lastSeen.
		if m.currentCh != nil {
			if _, ok := m.lastSeen[m.currentCh.ID]; !ok {
				m.lastSeen[m.currentCh.ID] = "0"
			}
		}
		return m, checkNewMessagesCmd(m.slackSvc, m.lastSeen, m.cfg.PollInterval)

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
		if refreshCurrent && m.currentCh != nil && !m.messages.InContextMode() {
			return m, tea.Batch(
				pollTickCmd(m.cfg.PollInterval),
				silentLoadHistoryCmd(m.slackSvc, m.currentCh.ID),
			)
		}
		return m, pollTickCmd(m.cfg.PollInterval)

	case SplashDoneMsg:
		m.splash = false
		return m, nil

	case ErrMsg:
		m.err = msg.Err
		return m, nil
	}

	return m, nil
}

// View renders the full TUI layout.
func (m Model) View() string {
	if !m.ready {
		return ""
	}

	if m.splash {
		return renderSplash(m.width, m.height)
	}

	// Render overlays on top
	switch m.overlay {
	case overlayHelp:
		return renderHelp(m.width, m.height)
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
	}

	sidebar := m.channels.View()
	msgView := m.messages.View()

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, msgView)
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

	inputHeight := 3
	statusHeight := 1
	topHeight := m.height - inputHeight - statusHeight - 2

	msgWidth := m.width - sidebarWidth - 2

	if topHeight < 1 {
		topHeight = 1
	}
	if msgWidth < 1 {
		msgWidth = 1
	}

	m.channels.SetSize(sidebarWidth, topHeight)
	m.messages.SetSize(msgWidth, topHeight)
	m.input.SetSize(m.width - 2)
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

	hints := HelpStyle.Render("Ctrl-H: help | Ctrl-S: settings")

	status := fmt.Sprintf(" %s | %s | %s%s",
		StatusBarStyle.Render(team), connStr, hints, extra)

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
			return ErrMsg{Err: err}
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

func pollTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 1 {
		intervalSec = 10
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return PollTickMsg{}
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
