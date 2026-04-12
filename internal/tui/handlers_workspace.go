package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/debug"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/workspace"
)

// ---------------------------------------------------------------------------
// Workspace lifecycle message types
// ---------------------------------------------------------------------------

// WorkspaceSignInMsg is sent when a workspace sign-in attempt completes.
type WorkspaceSignInMsg struct {
	TeamID   string
	TeamName string
	MyUserID string
	Err      error
}

// WorkspaceSignOutMsg requests signing out of a workspace.
type WorkspaceSignOutMsg struct {
	TeamID string
}

// WorkspaceSwitchMsg requests switching the active workspace.
type WorkspaceSwitchMsg struct {
	TeamID string
}

// WorkspaceAddedMsg is sent after a new workspace has been persisted to disk.
type WorkspaceAddedMsg struct {
	TeamID string
}

// WorkspaceRemovedMsg is sent after a workspace has been deleted from disk.
type WorkspaceRemovedMsg struct {
	TeamID string
}

// ---------------------------------------------------------------------------
// signInWorkspaceCmd — background tea.Cmd that authenticates a workspace
// ---------------------------------------------------------------------------

func signInWorkspaceCmd(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		// Create Slack API client.
		svc := slackpkg.NewSlackClient(ws.Config.BotToken, ws.Config.UserToken)

		// Authenticate to get team info.
		teamName, teamID, err := svc.AuthTest()
		if err != nil {
			return WorkspaceSignInMsg{
				TeamID: ws.Config.TeamID,
				Err:    fmt.Errorf("workspace %s auth failed: %w", ws.Config.TeamID, err),
			}
		}

		// Populate workspace runtime state.
		ws.SlackSvc = svc
		ws.TeamName = teamName
		ws.MyUserID = svc.MyUserID()

		// Create socket service if both bot and app tokens are present.
		if ws.Config.BotToken != "" && ws.Config.AppToken != "" {
			ws.SocketSvc = slackpkg.NewSocketClient(ws.Config.BotToken, ws.Config.AppToken)
		}

		// Buffered event channel for socket events.
		ws.EventChan = make(chan slackpkg.SocketEvent, 64)

		// Lifecycle context for this workspace's goroutines.
		ws.Ctx, ws.Cancel = context.WithCancel(context.Background())

		ws.SignedIn = true
		ws.Config.SignedOut = false

		debug.Log("workspace %s (%s) signed in, userID=%s", teamID, teamName, ws.MyUserID)

		return WorkspaceSignInMsg{
			TeamID:   teamID,
			TeamName: teamName,
			MyUserID: ws.MyUserID,
		}
	}
}

// ---------------------------------------------------------------------------
// Handler methods on *Model
// ---------------------------------------------------------------------------

// handleWorkspaceSignIn processes a completed sign-in attempt. On success it
// dispatches commands to load users, channels, connect the socket, and start
// polling for the workspace.
func (m *Model) handleWorkspaceSignIn(msg WorkspaceSignInMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		debug.Log("workspace sign-in error: %v", msg.Err)
		m.err = msg.Err
		return *m, nil
	}

	ws, ok := m.workspaces[msg.TeamID]
	if !ok {
		debug.Log("workspace sign-in for unknown team %s — ignoring", msg.TeamID)
		return *m, nil
	}

	// If no workspace is active yet, make this one active.
	if m.activeWsID == "" {
		m.activeWsID = msg.TeamID
		m.cfg.LastActiveWorkspace = msg.TeamID
	}

	// Sync legacy fields if this is the active workspace.
	if m.activeWsID == msg.TeamID {
		m.syncActiveWorkspace()
	}

	// Persist workspace config (marks SignedOut=false on disk).
	if err := workspace.Save(m.cfg.ConfigDir(), ws); err != nil {
		debug.Log("workspace save error after sign-in: %v", err)
	}

	// Dispatch background commands to populate the workspace.
	cmds := []tea.Cmd{
		loadUsersForWorkspaceCmd(ws),
		loadChannelsForWorkspaceCmd(ws),
	}

	// Connect socket if available.
	if ws.SocketSvc != nil {
		cmds = append(cmds, connectWorkspaceSocketCmd(ws))
		cmds = append(cmds, waitForWorkspaceEvent(ws))
	}

	// Start polling ticks.
	cmds = append(cmds,
		workspacePollTickCmd(ws.ID(), m.cfg.PollInterval),
		workspaceBgPollTickCmd(ws.ID(), m.cfg.PollIntervalBg),
	)

	debug.Log("workspace %s signed in — dispatching %d startup cmds", msg.TeamID, len(cmds))
	return *m, tea.Batch(cmds...)
}

// handleWorkspaceSignOut signs out a workspace: cancels its context, clears
// services, marks it as signed out, and switches to another workspace if
// this was the active one.
func (m *Model) handleWorkspaceSignOut(msg WorkspaceSignOutMsg) (Model, tea.Cmd) {
	ws, ok := m.workspaces[msg.TeamID]
	if !ok {
		return *m, nil
	}

	// Cancel all goroutines for this workspace.
	if ws.Cancel != nil {
		ws.Cancel()
	}

	// Clear runtime services.
	ws.SlackSvc = nil
	ws.SocketSvc = nil
	ws.EventChan = nil
	ws.Ctx = nil
	ws.Cancel = nil
	ws.SignedIn = false
	ws.Config.SignedOut = true

	// Persist the signed-out state.
	if err := workspace.Save(m.cfg.ConfigDir(), ws); err != nil {
		debug.Log("workspace save error after sign-out: %v", err)
	}

	debug.Log("workspace %s signed out", msg.TeamID)

	// If this was the active workspace, switch to the next signed-in one.
	if m.activeWsID == msg.TeamID {
		m.switchToNextSignedIn()
	}

	return *m, nil
}

// handleWorkspaceSwitch switches the active workspace to the one identified
// by msg.TeamID. Saves the current workspace's last channel, updates the
// global config, and rebuilds the sidebar.
func (m *Model) handleWorkspaceSwitch(msg WorkspaceSwitchMsg) (Model, tea.Cmd) {
	if msg.TeamID == m.activeWsID {
		return *m, nil
	}

	ws, ok := m.workspaces[msg.TeamID]
	if !ok || !ws.SignedIn {
		debug.Log("workspace switch to %s failed — not found or not signed in", msg.TeamID)
		return *m, nil
	}

	// Save the current workspace's last-viewed channel.
	if prev, ok := m.workspaces[m.activeWsID]; ok && m.currentCh != nil {
		prev.Config.LastChannel = m.currentCh.ID
		if err := workspace.Save(m.cfg.ConfigDir(), prev); err != nil {
			debug.Log("workspace save error (prev last channel): %v", err)
		}
	}

	// Switch.
	m.activeWsID = msg.TeamID
	m.cfg.LastActiveWorkspace = msg.TeamID
	config.SaveDebounced(m.cfg)

	// Sync legacy fields and rebuild sidebar.
	m.syncActiveWorkspace()
	m.rebuildSidebarForWorkspace(ws)

	// Restore last channel.
	if ws.Config.LastChannel != "" {
		for i := range ws.Channels {
			if ws.Channels[i].ID == ws.Config.LastChannel {
				m.currentCh = &ws.Channels[i]
				m.setChannelHeader()
				break
			}
		}
	}

	debug.Log("switched to workspace %s (%s)", msg.TeamID, ws.DisplayName())
	return *m, nil
}

// ---------------------------------------------------------------------------
// Helper methods
// ---------------------------------------------------------------------------

// syncActiveWorkspace copies the active workspace's services and state
// into the Model's legacy fields (slackSvc, socketSvc, users, etc.) so
// all existing code that references m.slackSvc continues to work.
func (m *Model) syncActiveWorkspace() {
	ws := m.activeWs()
	if ws != nil && ws.SignedIn {
		m.slackSvc = ws.SlackSvc
		m.socketSvc = ws.SocketSvc
		m.users = ws.Users
		m.myUserID = ws.MyUserID
		m.teamName = ws.TeamName
		m.eventChan = ws.EventChan
		m.lastSeen = ws.LastSeen
	} else {
		m.slackSvc = nil
		m.socketSvc = nil
		m.users = nil
		m.myUserID = ""
		m.teamName = ""
		m.eventChan = nil
	}
}

// switchToNextSignedIn finds another signed-in workspace and makes it active.
// If none are signed in, clears activeWsID.
func (m *Model) switchToNextSignedIn() {
	for id, ws := range m.workspaces {
		if id != m.activeWsID && ws.SignedIn {
			m.activeWsID = id
			m.cfg.LastActiveWorkspace = id
			config.SaveDebounced(m.cfg)
			m.syncActiveWorkspace()
			m.rebuildSidebarForWorkspace(ws)
			debug.Log("auto-switched to workspace %s", id)
			return
		}
	}
	// No signed-in workspace remains.
	m.activeWsID = ""
	m.cfg.LastActiveWorkspace = ""
	config.SaveDebounced(m.cfg)
	m.syncActiveWorkspace()
	debug.Log("no signed-in workspaces remain")
}

// rebuildSidebarForWorkspace replaces the sidebar channel list with the
// given workspace's channels merged with friend channels.
func (m *Model) rebuildSidebarForWorkspace(ws *workspace.Workspace) {
	m.channels.BeginBulkUpdate()
	defer m.channels.EndBulkUpdate()

	// Set the workspace's Slack channels.
	m.channels.SetChannels(ws.Channels)

	// Merge in friend channels (friends are workspace-independent).
	m.channels.SetFriendChannels(m.buildFriendChannels())

	// Apply workspace-scoped aliases from ChannelMeta.
	aliases := make(map[string]string, len(ws.ChannelMeta))
	for chID, meta := range ws.ChannelMeta {
		if meta.Alias != "" {
			aliases[chID] = meta.Alias
		}
	}
	m.channels.SetAliases(aliases)

	// Apply workspace-scoped hidden channels.
	var hidden []string
	for chID, meta := range ws.ChannelMeta {
		if meta.Hidden {
			hidden = append(hidden, chID)
		}
	}
	m.channels.SetHiddenChannels(hidden)

	// Apply latest timestamps for sort-by-recent.
	m.channels.SetLatestTimestamps(ws.LastSeen)

	// Update the sidebar workspace name header.
	m.channels.SetWorkspaceName(ws.DisplayName())
	m.channels.SetMultipleWorkspaces(m.signedInWorkspaceCount() > 1)
}

// signedInWorkspaceCount returns how many workspaces are currently signed in.
func (m *Model) signedInWorkspaceCount() int {
	count := 0
	for _, ws := range m.workspaces {
		if ws.SignedIn {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions (these ensure the msg types satisfy
// tea.Msg which is interface{}, so this is really just documentation).
// ---------------------------------------------------------------------------

var (
	_ tea.Msg = WorkspaceSignInMsg{}
	_ tea.Msg = WorkspaceSignOutMsg{}
	_ tea.Msg = WorkspaceSwitchMsg{}
	_ tea.Msg = WorkspaceAddedMsg{}
	_ tea.Msg = WorkspaceRemovedMsg{}
)
