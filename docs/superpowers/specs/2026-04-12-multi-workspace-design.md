# Multi-Workspace Support — Design Spec

## Goal

Enable slackers to connect to multiple Slack workspaces simultaneously, with independent channel lists, message histories, and socket connections. Users can switch between workspaces instantly, sign in/out of individual workspaces, and receive notifications from all active workspaces in a single unified view.

## Architecture

### Workspace Abstraction

A new `Workspace` struct encapsulates everything that is currently workspace-implicit in the Model:

```go
// internal/workspace/workspace.go
type Workspace struct {
    ID          string                   // Slack team ID (directory name, globally unique)
    Name        string                   // user-editable display name
    Config      WorkspaceConfig          // tokens, auto-sign-in flag, last-open-channel
    SlackSvc    slack.SlackService       // nil when signed out
    SocketSvc   slack.SocketService      // nil when signed out
    EventChan   chan slack.SocketEvent    // per-workspace event stream
    Ctx         context.Context          // workspace lifecycle context
    Cancel      context.CancelFunc       // cancels socket + polling
    Users       map[string]types.User    // workspace members
    Channels    []types.Channel          // workspace channels/DMs/groups
    ChannelMeta map[string]ChannelMeta   // aliases, groups, hidden, sort order
    TeamName    string                   // from Slack AuthTest response
    MyUserID    string                   // local user's Slack ID in this workspace
    SignedIn    bool                     // runtime state
    LastSeen    map[string]string        // channelID → last-seen timestamp
    UnreadCount int                      // total unread across all channels (for overlay badge)
}

type WorkspaceConfig struct {
    BotToken      string `json:"bot_token"`
    AppToken      string `json:"app_token"`
    UserToken     string `json:"user_token,omitempty"`
    Name          string `json:"name"`
    AutoSignIn    bool   `json:"auto_sign_in"`
    SignedOut     bool   `json:"signed_out"`       // persisted sign-out state
    LastChannel   string `json:"last_channel"`      // channel ID to restore on switch
    ClientID      string `json:"client_id,omitempty"`
    ClientSecret  string `json:"client_secret,omitempty"`
}

type ChannelMeta struct {
    Alias    string `json:"alias,omitempty"`
    Group    string `json:"group,omitempty"`
    Hidden   bool   `json:"hidden,omitempty"`
    SortKey  int    `json:"sort_key,omitempty"`
}
```

### Model Integration

The `Model` struct gains:

```go
workspaces   map[string]*workspace.Workspace  // teamID → workspace
activeWsID   string                            // currently displayed workspace ID
```

Existing fields (`slackSvc`, `socketSvc`, `users`, `myUserID`, `teamName`, `channels`) become **accessor methods** that delegate to the active workspace:

```go
func (m *Model) activeWs() *workspace.Workspace {
    return m.workspaces[m.activeWsID]
}
func (m *Model) slackSvc() slack.SlackService {
    if ws := m.activeWs(); ws != nil { return ws.SlackSvc }
    return nil
}
```

This minimizes the blast radius — most existing code calls `m.slackSvc` which now resolves through the active workspace. The refactor is largely mechanical: change field access to method calls.

### Channel ID Uniqueness

Slack channel IDs are unique within a workspace but can collide across workspaces. Internal compound keys use `teamID:channelID`:

- `channelIndex` keys: `"T04ABC:C01XYZ"`
- `unread` map keys: `"T04ABC:C01XYZ"`
- `lastSeen` map keys: `"T04ABC:C01XYZ"`
- Friend channel IDs remain unprefixed (they're globally unique via peer ID)
- Display code strips the prefix — the user never sees compound keys

Helper:
```go
func CompoundID(teamID, channelID string) string { return teamID + ":" + channelID }
func SplitCompoundID(id string) (teamID, channelID string) { ... }
```

## Filesystem Layout

```
~/.config/slackers/
  config.json                # global settings (theme, shortcuts prefs, P2P config,
                             #   last-active-workspace-id, notification timeout, etc.)
  secure.key                 # shared X25519 keypair (P2P identity)
  shortcuts.json             # shared keybinding overrides
  notifications.json         # global notification store (workspace-tagged entries)
  debug.log                  # debug output (--debug mode)
  themes/                    # user custom theme JSON files
  friends/
    friends.json             # shared P2P friend list
    history/                 # per-friend encrypted chat history files
  workspaces/
    <team-id-1>/
      workspace.json         # WorkspaceConfig: tokens, name, auto-sign-in, last channel
      channels.json          # ChannelMeta map: aliases, groups, hidden, sort order
    <team-id-2>/
      workspace.json
      channels.json
```

### Key decisions

- **Team ID** is the folder name — globally unique, stable across workspace renames.
- **Friends are global** — P2P identity is independent of any Slack workspace.
- **Notifications are global** — entries tagged with workspace ID for routing.
- **Tokens move out of config.json** — each workspace owns its own credentials.
- **Channel metadata is workspace-scoped** — aliases, hidden state, sort order live in the workspace folder.

## Lifecycle

### Sign-In Flow

Called on startup for `auto_sign_in: true` workspaces, or manually from the Workspaces overlay:

1. Load `workspace.json` from disk.
2. Create `SlackService(botToken, userToken)`.
3. Create `SocketService(botToken, appToken)`.
4. Create a derived `context.Context` with `CancelFunc` for this workspace.
5. Run `AuthTest()` → cache `myUserID`, `teamName`.
6. Load channels and users in parallel (`loadChannelsCmd`, `loadUsersCmd`).
7. Load `channels.json` → apply aliases, groups, hidden, sort order.
8. Start socket connection: `connectSocketCmd(ws.SocketSvc, ws.EventChan)`.
9. Start polling ticks: `pollTickCmd` and `bgPollTickCmd` scoped to this workspace.
10. Set `SignedIn = true`, clear `SignedOut` flag, persist to disk.

### Sign-Out Flow

User action from the Workspaces overlay (`s` key):

1. Call `ws.Cancel()` → cancels the workspace's context, stopping socket + polling goroutines.
2. Close `ws.EventChan`.
3. Clear in-memory state: `ws.Users = nil`, `ws.Channels = nil`, `ws.SlackSvc = nil`, `ws.SocketSvc = nil`.
4. Set `ws.SignedIn = false`.
5. Persist `signed_out: true` in `workspace.json`.
6. If this was the active workspace, switch to the next signed-in workspace. If none are signed in, show an empty state with a hint to open the Workspaces menu.

### Switch Flow

Instant — no network operations:

1. Save the current workspace's open channel as `last_channel` in its `WorkspaceConfig`.
2. Set `m.activeWsID` to the target workspace.
3. Rebuild the sidebar `ChannelListModel` from the target workspace's channels + global friends.
4. Restore the target workspace's `last_channel` in the message pane.
5. Re-render. No service reconnection — the background workspace continues running.

### Startup Sequence

1. Load `config.json` (global settings).
2. Scan `workspaces/` directory for subdirectories, load each `workspace.json`.
3. **Migration check**: if `config.json` still has tokens (pre-multi-workspace), run migration (see Migration section).
4. For each workspace where `auto_sign_in: true && signed_out: false`: run Sign-In Flow in parallel (each workspace gets its own goroutine returning `tea.Cmd`s).
5. Set `activeWsID` to `config.json`'s `last_active_workspace`. If that workspace isn't signed in, pick the first signed-in one.
6. Display the active workspace's channels and last open chat.

On a fresh install with one workspace, the experience is identical to today.

## Event Routing

### Per-Workspace Event Channels

Each workspace gets its own `eventChan chan slack.SocketEvent`. Socket events are workspace-scoped by the token that created the connection.

### Multiplexing in Update

The Model's `Update` loop needs to handle events from any workspace. Events carry a `WorkspaceID` field (set by the socket connection goroutine):

```go
type SlackEventMsg struct {
    WorkspaceID string
    Event       slack.SocketEvent
}
```

In `Update`:
```go
case SlackEventMsg:
    ws := m.workspaces[msg.WorkspaceID]
    if ws == nil || !ws.SignedIn { break }
    // Dispatch to workspace-specific handlers.
    // If ws == activeWs: update UI immediately.
    // If ws != activeWs: update state silently, increment unread badge.
```

### Polling

Each workspace runs its own poll timer. Poll results are tagged with `WorkspaceID`:

```go
type PollTickMsg struct{ WorkspaceID string }
type BgPollTickMsg struct{ WorkspaceID string }
```

The poll handler uses the workspace's own `SlackSvc` and `LastSeen` map. Background workspaces can optionally use a slower interval (future `bg_poll_multiplier` setting).

## UI Components

### Workspaces Overlay (`overlayWorkspaces`)

Opened by `Alt+W`. Uses `SelectableList`.

```
╭─ Workspaces ─────────────────────────╮
│                                       │
│  > ● My Company         3 unread      │
│    ● Side Project        signed in    │
│    ○ Old Job             signed out   │
│                                       │
│  ↑/↓: select · Enter: switch          │
│  a: add · e: edit · s: sign in/out    │
│  d: remove · Esc: close               │
╰───────────────────────────────────────╯
```

- `●` green = signed in, `○` muted = signed out
- Unread count badge for signed-in workspaces with new messages
- Enter on a signed-out workspace with `auto_sign_in: false` triggers sign-in first, then switches

### Edit Workspace Overlay (`overlayWorkspaceEdit`)

Opened from Workspaces overlay (`a` to add, `e` to edit).

```
╭─ Edit Workspace ─────────────────────╮
│                                       │
│  Name:          My Company            │
│  Team ID:       T04ABCDEF   (read-only)│
│  Bot Token:     xoxb-...****          │
│  App Token:     xapp-...****          │
│  User Token:    xoxp-...****          │
│  Auto Sign In:  on                    │
│                                       │
│  ── Actions ──                        │
│  Copy Setup Hash                      │
│  Copy Setup JSON                      │
│                                       │
│  ↑/↓: navigate · Enter: edit          │
│  s: save · Esc: back                  │
╰───────────────────────────────────────╯
```

For **adding** a workspace:
- User can paste a setup hash or JSON into a text field
- Or enter tokens manually
- On save: decode tokens, run `AuthTest` to get team ID, create `workspaces/<team-id>/`, persist, sign in

### Sidebar Header

The active workspace name renders centered at the top of the sidebar channel list, in `ColorPageHeader` style:

```
╭─────────────────────╮
│    My Company    ⇅   │   ← workspace name + switch indicator
│ ▼ Channels           │
│   #general           │
│   #random            │
│ ▼ Friends            │
│   Ryan Weiss         │
╰─────────────────────╯
```

The `⇅` indicator appears only when 2+ workspaces are signed in.

## Notifications

### Workspace-Tagged Entries

Each notification entry gains a `WorkspaceID` field:

```go
type Notification struct {
    ID          string
    WorkspaceID string    // "" for friend/P2P notifications
    ChannelID   string
    // ... existing fields
}
```

### Notification Click

When the user activates a notification:
1. If `WorkspaceID` differs from `activeWsID`: switch workspace first.
2. Open the target channel.
3. Scroll to the relevant message if applicable.

### Global View

The notifications overlay shows all notifications across all workspaces, optionally grouped by workspace name. The workspace name appears as a tag/prefix on each entry.

## Commands

### `/share setup`

- Default: generates hash for the active workspace.
- With argument: `/share setup "Side Project"` generates hash for the named workspace.
- Valid workspace names should be cached and show as auto-suggested arguments when entering a space after "/share setup ". If there is no workspace name it should default to its id.
- Output includes workspace name for clarity.

### `/invite`

- Default: sends invite with the active workspace's setup hash.
- With argument: `/invite workspace "Side Project"` uses the named workspace.
- Similarly the workspace argument options should populate as auto-suggested entries when it detects the workspace argument then a space.

### `slackers setup <hash-or-json>` (CLI)

1. Decode the input to extract tokens.
2. Run `AuthTest` with the tokens to get team ID.
3. Check if `workspaces/<team-id>/` exists:
   - **Exists**: update `workspace.json` with new tokens, print "Updated workspace: <name>".
   - **New**: create directory + `workspace.json`, set `auto_sign_in: true`, print "Added workspace: <name>".
4. If the app is running, it picks up the change on next config reload.

## Migration (v0.22 → v0.23)

On startup, if `config.json` contains `bot_token` or `app_token`:

1. Create `SlackService` with the old tokens, run `AuthTest` to get team ID.
2. Create `workspaces/<team-id>/` directory.
3. Write `workspace.json` with tokens extracted from config, `auto_sign_in: true`, `name: <teamName>`.
4. Move channel aliases/groups/hidden from config to `workspaces/<team-id>/channels.json`.
5. Strip token fields from `config.json`, add `last_active_workspace: <team-id>`.
6. Save both files.
7. Log migration to debug log. Print no user-visible message — the app just works.

If `AuthTest` fails (no internet, expired tokens): skip migration, keep old format, try again next launch. The app continues working in single-workspace mode.

## Global Config Changes

`config.json` loses workspace-specific fields and gains:

```json
{
  "last_active_workspace": "T04ABCDEF",
  "workspaces_shortcut": "alt+w",
  "theme": "Default",
  "p2p_port": 9900,
  "poll_interval": 10,
  "notification_timeout": 3,
  ...
}
```

Fields removed from config.json (moved to workspace.json):
- `bot_token`, `app_token`, `user_token`
- `client_id`, `client_secret`
- `team_name` (redundant, available via AuthTest)

## Shortcuts

New default shortcut:
```json
{
  "workspaces": ["alt+w"]
}
```

Added to `internal/shortcuts/defaults.json` and the action dispatch in Model's key handler.

## Error Handling

- **Workspace fails to sign in**: mark it as signed-out, show error in Workspaces overlay status, don't block other workspaces.
- **Socket disconnects for one workspace**: existing reconnect loop handles it per-workspace (each has its own goroutine). Other workspaces unaffected.
- **All workspaces signed out**: show empty state in sidebar + message pane with hint "Press Alt+W to manage workspaces".
- **Corrupt workspace.json**: skip that workspace, log warning, show "(error)" status in overlay.

## Implementation Phases

### Phase 1: Workspace Abstraction + Data Layer
- Create `internal/workspace/` package with `Workspace`, `WorkspaceConfig`, `ChannelMeta` types
- Implement `LoadAll()`, `Save()`, `Create()`, `Delete()` for workspace filesystem ops
- Implement compound channel ID helpers
- Write migration logic (old config → workspace folder)

### Phase 2: Model Refactor
- Add `workspaces map` and `activeWsID` to Model
- Convert `slackSvc`, `socketSvc`, `users`, `myUserID`, `teamName` from fields to accessor methods
- Update all call sites (mechanical refactor)
- Single workspace still works identically

### Phase 3: Multi-Workspace Lifecycle
- Per-workspace context, event channel, polling
- Sign-in / sign-out / switch routines
- Event routing with WorkspaceID tagging
- Startup sequence with parallel workspace init

### Phase 4: UI
- Workspaces overlay (`overlayWorkspaces`)
- Edit Workspace overlay (`overlayWorkspaceEdit`)
- Sidebar header with workspace name
- Workspace-aware notifications

### Phase 5: Commands & CLI
- Update `slackers setup` for multi-workspace create/update
- Update `/share setup` and `/invite` with workspace argument
- Add `Alt+W` shortcut to defaults
