// internal/workspace/workspace.go
package workspace

import (
	"context"
	"sync"

	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// WorkspaceConfig is persisted to workspaces/<team-id>/workspace.json.
type WorkspaceConfig struct {
	TeamID       string `json:"team_id"`
	Name         string `json:"name"`
	BotToken     string `json:"bot_token"`
	AppToken     string `json:"app_token"`
	UserToken    string `json:"user_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AutoSignIn   bool   `json:"auto_sign_in"`
	SignedOut    bool   `json:"signed_out"`
	LastChannel  string `json:"last_channel,omitempty"`
}

// ChannelMeta stores per-channel workspace-scoped metadata.
type ChannelMeta struct {
	Alias   string `json:"alias,omitempty"`
	Group   string `json:"group,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
	SortKey int    `json:"sort_key,omitempty"`
}

// Workspace holds all runtime state for a single Slack workspace.
// Services (SlackSvc, SocketSvc) are nil when the workspace is signed out.
type Workspace struct {
	mu          sync.RWMutex
	Config      WorkspaceConfig
	TeamName    string // from AuthTest (display name from Slack)
	MyUserID    string // local user's Slack ID in this workspace
	SignedIn    bool
	Users       map[string]types.User
	Channels    []types.Channel
	ChannelMeta map[string]ChannelMeta // channelID → metadata
	LastSeen    map[string]string      // channelID → last-seen timestamp
	UnreadCount int

	// Services — nil when the workspace is signed out.
	SlackSvc  slackpkg.SlackService
	SocketSvc slackpkg.SocketService
	EventChan chan slackpkg.SocketEvent

	// Lifecycle context — cancelled on sign-out to stop socket + polling.
	Ctx    context.Context
	Cancel context.CancelFunc
}

// ID returns the workspace's team ID (folder name).
func (w *Workspace) ID() string { return w.Config.TeamID }

// DisplayName returns the user-set name, falling back to the Slack team name,
// then the team ID.
func (w *Workspace) DisplayName() string {
	if w.Config.Name != "" {
		return w.Config.Name
	}
	if w.TeamName != "" {
		return w.TeamName
	}
	return w.Config.TeamID
}

// CompoundID returns a globally unique channel key: "teamID:channelID".
func CompoundID(teamID, channelID string) string {
	return teamID + ":" + channelID
}

// SplitCompoundID extracts the team ID and channel ID from a compound key.
// For friend channels (no colon), returns ("", id).
func SplitCompoundID(id string) (teamID, channelID string) {
	for i := range id {
		if id[i] == ':' {
			return id[:i], id[i+1:]
		}
	}
	return "", id
}
