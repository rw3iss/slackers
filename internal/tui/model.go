package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// Custom message types for the TUI update loop.

// ChannelsLoadedMsg is sent when the channel list has been fetched.
type ChannelsLoadedMsg struct{ Channels []types.Channel }

// HistoryLoadedMsg is sent when message history has been fetched.
type HistoryLoadedMsg struct{ Messages []types.Message }

// UsersLoadedMsg is sent when the user list has been fetched.
type UsersLoadedMsg struct{ Users map[string]types.User }

// MessageSentMsg is sent when a message has been successfully sent.
type MessageSentMsg struct{}

// SlackEventMsg wraps a real-time event from the socket connection.
type SlackEventMsg struct{ Event slackpkg.SocketEvent }

// ConnStatusMsg reports the connection status of the socket.
type ConnStatusMsg struct {
	Status types.ConnectionStatus
	Err    error
}

// ErrMsg wraps an error from any async command.
type ErrMsg struct{ Err error }

// Model is the root TUI model composing all sub-components.
type Model struct {
	// Sub-models
	channels ChannelListModel
	messages MessageViewModel
	input    InputModel
	keymap   KeyMap

	// State
	focus      types.Focus
	currentCh  *types.Channel
	users      map[string]types.User
	connStatus types.ConnectionStatus
	connErr    error
	teamName   string
	err        error
	warning    string

	// Dependencies (interfaces for SOLID)
	slackSvc  slackpkg.SlackService
	socketSvc slackpkg.SocketService
	eventChan chan slackpkg.SocketEvent

	// Layout
	width  int
	height int
	ready  bool
}

// NewModel creates a new root TUI model.
func NewModel(slackSvc slackpkg.SlackService, socketSvc slackpkg.SocketService) Model {
	return Model{
		channels:  NewChannelList(),
		messages:  NewMessageView(),
		input:     NewInput(),
		keymap:    DefaultKeyMap(),
		focus:     types.FocusSidebar,
		users:     make(map[string]types.User),
		slackSvc:  slackSvc,
		socketSvc: socketSvc,
		eventChan: make(chan slackpkg.SocketEvent, 100),
	}
}

// Init returns the initial commands to run at startup.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		loadUsersCmd(m.slackSvc),
		loadChannelsCmd(m.slackSvc),
		connectSocketCmd(m.socketSvc, m.eventChan),
		waitForSocketEvent(m.eventChan),
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
		m.resizeComponents()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keymap.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keymap.Tab):
			m.cycleFocusForward()
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.ShiftTab):
			m.cycleFocusBackward()
			m.updateFocus()
			return m, nil

		case key.Matches(msg, m.keymap.Escape):
			m.focus = types.FocusSidebar
			m.input.Reset()
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

		case key.Matches(msg, m.keymap.Enter):
			if m.focus == types.FocusSidebar {
				ch := m.channels.SelectedChannel()
				if ch != nil {
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.messages.SetChannelName("#" + ch.Name)
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

	case ChannelsLoadedMsg:
		m.channels.SetChannels(msg.Channels)
		drainWarnings(&m)
		return m, nil

	case HistoryLoadedMsg:
		m.messages.SetMessages(msg.Messages)
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
		return m, nil

	case MessageSentMsg:
		drainWarnings(&m)
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
		// Re-subscribe for the next event
		return m, waitForSocketEvent(m.eventChan)

	case ConnStatusMsg:
		m.connStatus = msg.Status
		m.connErr = msg.Err
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
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			"Loading...")
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

// resizeComponents calculates and sets sizes for all sub-models.
func (m *Model) resizeComponents() {
	sidebarWidth := 25
	if sidebarWidth > m.width/3 {
		sidebarWidth = m.width / 3
	}

	inputHeight := 3
	statusHeight := 1
	topHeight := m.height - inputHeight - statusHeight - 2 // account for borders

	msgWidth := m.width - sidebarWidth - 2 // account for borders

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

// cycleFocusForward advances focus: Sidebar -> Messages -> Input -> Sidebar.
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

// cycleFocusBackward reverses focus: Sidebar -> Input -> Messages -> Sidebar.
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

// drainWarnings pulls any fallback warnings from the slack service into the model.
func drainWarnings(m *Model) {
	if warns := m.slackSvc.Warnings(); len(warns) > 0 {
		m.warning = warns[len(warns)-1] // show the most recent
	} else {
		m.warning = ""
	}
}

// updateFocus propagates the focus state to all sub-models.
func (m *Model) updateFocus() {
	m.channels.SetFocused(m.focus == types.FocusSidebar)
	m.messages.SetFocused(m.focus == types.FocusMessages)
	m.input.SetFocused(m.focus == types.FocusInput)
}

// renderStatusBar renders the bottom status line.
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

	status := fmt.Sprintf(" %s | %s | Tab: switch focus | Ctrl-q: quit%s",
		StatusBarStyle.Render(team), connStr, extra)

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
