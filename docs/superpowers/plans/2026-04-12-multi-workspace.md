# Multi-Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable slackers to connect to multiple Slack workspaces simultaneously with per-workspace isolation, instant switching, and background connectivity.

**Architecture:** A new `internal/workspace` package owns the `Workspace` type and filesystem operations. The Model gains a `workspaces map[string]*Workspace` and `activeWsID` field, with accessor methods that delegate to the active workspace. Each workspace runs its own socket connection and polling in a separate goroutine context. Two new overlays (`overlayWorkspaces`, `overlayWorkspaceEdit`) provide workspace management UI.

**Tech Stack:** Go 1.25.7, Bubbletea, slack-go v0.15.0, lipgloss

**Spec:** `docs/superpowers/specs/2026-04-12-multi-workspace-design.md`

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `internal/workspace/workspace.go` | `Workspace` struct, `WorkspaceConfig`, `ChannelMeta` types, sign-in/sign-out/switch lifecycle methods |
| `internal/workspace/store.go` | Filesystem ops: `LoadAll`, `Save`, `Create`, `Delete`, `Migrate` for `~/.config/slackers/workspaces/` |
| `internal/workspace/workspace_test.go` | Tests for store, config, compound IDs |
| `internal/tui/workspaces_ui.go` | `WorkspacesModel` (list overlay) + `WorkspaceEditModel` (edit overlay) |
| `internal/tui/handlers_workspace.go` | Model message handlers for workspace events (sign-in result, switch, sign-out) |

### Modified files

| File | Changes |
|------|---------|
| `internal/config/config.go` | Remove workspace-specific fields (tokens, aliases, hidden, last channel). Add `LastActiveWorkspace`, `WorkspacesShortcut`. |
| `internal/slack/client.go` | `AuthTest()` returns `(teamName, teamID, error)` instead of `(teamName, error)` |
| `internal/slack/client.go` | `SlackService` interface: `AuthTest()` signature change |
| `internal/types/types.go` | `Channel` gains `WorkspaceID string` field |
| `internal/notifications/notifications.go` | `Notification` gains `WorkspaceID string` field |
| `internal/tui/model.go` | Add `workspaces`, `activeWsID`, workspace accessors. Remove direct `slackSvc`/`socketSvc`/`users`/`teamName`/`myUserID` fields. Add overlay constants. |
| `internal/tui/model.go` | `NewModel` signature changes — no longer takes `slackSvc`/`socketSvc` directly |
| `internal/tui/model.go` | `Init()` dispatches per-workspace sign-in commands |
| `internal/shortcuts/defaults.json` | Add `"workspaces": ["alt+w"]` |
| `internal/tui/channels.go` | Sidebar header shows workspace name |
| `internal/tui/cmds.go` | Per-workspace poll/connect commands tagged with workspace ID |
| `cmd/slackers/main.go` | `setup` command creates/updates workspace folders; `rootCmd` loads workspaces |
| `internal/setup/setup.go` | `Config` gains `TeamID` field for round-trip encoding |

---

## Phase 1: Workspace Abstraction + Data Layer

### Task 1: Create workspace types

**Files:**
- Create: `internal/workspace/workspace.go`

- [ ] **Step 1: Create the workspace package with core types**

```go
// internal/workspace/workspace.go
package workspace

import (
	"context"
	"sync"

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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/workspace/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/workspace.go
git commit -m "feat(workspace): add core Workspace types and CompoundID helpers"
```

---

### Task 2: Create workspace filesystem store

**Files:**
- Create: `internal/workspace/store.go`

- [ ] **Step 1: Implement the store**

```go
// internal/workspace/store.go
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Dir returns the workspaces root directory under the config dir.
// e.g. ~/.config/slackers/workspaces/
func Dir(configDir string) string {
	return filepath.Join(configDir, "workspaces")
}

// WorkspaceDir returns the directory for a specific workspace.
// e.g. ~/.config/slackers/workspaces/T04ABCDEF/
func WorkspaceDir(configDir, teamID string) string {
	return filepath.Join(Dir(configDir), teamID)
}

// LoadAll reads all workspace configs from disk.
// Returns an empty slice (not nil) if no workspaces exist.
func LoadAll(configDir string) ([]*Workspace, error) {
	wsDir := Dir(configDir)
	entries, err := os.ReadDir(wsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Workspace{}, nil
		}
		return nil, fmt.Errorf("workspace: read dir: %w", err)
	}
	var result []*Workspace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ws, err := Load(configDir, e.Name())
		if err != nil {
			// Skip corrupt workspaces, don't block others.
			continue
		}
		result = append(result, ws)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].DisplayName() < result[j].DisplayName()
	})
	if result == nil {
		result = []*Workspace{}
	}
	return result, nil
}

// Load reads a single workspace config from disk.
func Load(configDir, teamID string) (*Workspace, error) {
	dir := WorkspaceDir(configDir, teamID)
	cfgPath := filepath.Join(dir, "workspace.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: read config: %w", err)
	}
	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: parse config: %w", err)
	}
	// Ensure TeamID matches the directory name.
	cfg.TeamID = teamID

	// Load channel metadata.
	meta := make(map[string]ChannelMeta)
	metaPath := filepath.Join(dir, "channels.json")
	if metaData, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(metaData, &meta)
	}

	return &Workspace{
		Config:      cfg,
		ChannelMeta: meta,
		Users:       make(map[string]types.User),
		LastSeen:    make(map[string]string),
	}, nil
}

// Save persists a workspace config and channel metadata to disk.
func Save(configDir string, ws *Workspace) error {
	dir := WorkspaceDir(configDir, ws.ID())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspace: mkdir: %w", err)
	}
	// Write workspace.json.
	cfgData, err := json.MarshalIndent(ws.Config, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal config: %w", err)
	}
	cfgPath := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(cfgPath, cfgData, 0o600); err != nil {
		return fmt.Errorf("workspace: write config: %w", err)
	}
	// Write channels.json.
	if len(ws.ChannelMeta) > 0 {
		metaData, err := json.MarshalIndent(ws.ChannelMeta, "", "  ")
		if err != nil {
			return fmt.Errorf("workspace: marshal meta: %w", err)
		}
		metaPath := filepath.Join(dir, "channels.json")
		if err := os.WriteFile(metaPath, metaData, 0o600); err != nil {
			return fmt.Errorf("workspace: write meta: %w", err)
		}
	}
	return nil
}

// Create makes a new workspace directory and saves its config.
func Create(configDir string, cfg WorkspaceConfig) (*Workspace, error) {
	if cfg.TeamID == "" {
		return nil, fmt.Errorf("workspace: team ID is required")
	}
	ws := &Workspace{
		Config:      cfg,
		ChannelMeta: make(map[string]ChannelMeta),
		Users:       make(map[string]types.User),
		LastSeen:    make(map[string]string),
	}
	if err := Save(configDir, ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// Delete removes a workspace directory from disk.
func Delete(configDir, teamID string) error {
	dir := WorkspaceDir(configDir, teamID)
	return os.RemoveAll(dir)
}

// Exists checks if a workspace directory exists.
func Exists(configDir, teamID string) bool {
	dir := WorkspaceDir(configDir, teamID)
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/workspace/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/store.go
git commit -m "feat(workspace): filesystem store for workspace configs"
```

---

### Task 3: Write workspace tests

**Files:**
- Create: `internal/workspace/workspace_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/workspace/workspace_test.go
package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompoundID(t *testing.T) {
	id := CompoundID("T123", "C456")
	if id != "T123:C456" {
		t.Fatalf("got %q, want T123:C456", id)
	}
	team, ch := SplitCompoundID(id)
	if team != "T123" || ch != "C456" {
		t.Fatalf("split got (%q, %q), want (T123, C456)", team, ch)
	}
	// Friend channel (no colon).
	team2, ch2 := SplitCompoundID("friend-peer-id")
	if team2 != "" || ch2 != "friend-peer-id" {
		t.Fatalf("friend split got (%q, %q), want (\"\", friend-peer-id)", team2, ch2)
	}
}

func TestDisplayName(t *testing.T) {
	ws := &Workspace{Config: WorkspaceConfig{TeamID: "T123"}}
	if ws.DisplayName() != "T123" {
		t.Fatalf("got %q, want T123", ws.DisplayName())
	}
	ws.TeamName = "Acme Corp"
	if ws.DisplayName() != "Acme Corp" {
		t.Fatalf("got %q, want Acme Corp", ws.DisplayName())
	}
	ws.Config.Name = "My Work"
	if ws.DisplayName() != "My Work" {
		t.Fatalf("got %q, want My Work", ws.DisplayName())
	}
}

func TestCreateLoadDelete(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		TeamID:     "T999",
		Name:       "Test WS",
		BotToken:   "xoxb-test",
		AppToken:   "xapp-test",
		AutoSignIn: true,
	}
	ws, err := Create(dir, cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ws.ID() != "T999" {
		t.Fatalf("ID: got %q", ws.ID())
	}
	// Verify files exist.
	if _, err := os.Stat(filepath.Join(dir, "workspaces", "T999", "workspace.json")); err != nil {
		t.Fatalf("workspace.json not created: %v", err)
	}
	// Load it back.
	ws2, err := Load(dir, "T999")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ws2.Config.Name != "Test WS" || ws2.Config.BotToken != "xoxb-test" {
		t.Fatalf("loaded config mismatch: %+v", ws2.Config)
	}
	// LoadAll.
	all, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("loadAll: got %d, want 1", len(all))
	}
	// Delete.
	if err := Delete(dir, "T999"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if Exists(dir, "T999") {
		t.Fatal("workspace still exists after delete")
	}
}

func TestChannelMetaPersistence(t *testing.T) {
	dir := t.TempDir()
	ws, _ := Create(dir, WorkspaceConfig{TeamID: "T100", Name: "Meta Test"})
	ws.ChannelMeta["C001"] = ChannelMeta{Alias: "dev", Group: "Engineering", Hidden: false}
	ws.ChannelMeta["C002"] = ChannelMeta{Alias: "ops", Hidden: true}
	if err := Save(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	ws2, err := Load(dir, "T100")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ws2.ChannelMeta["C001"].Alias != "dev" {
		t.Fatalf("alias: got %q", ws2.ChannelMeta["C001"].Alias)
	}
	if !ws2.ChannelMeta["C002"].Hidden {
		t.Fatal("C002 should be hidden")
	}
}

func TestLoadAllEmpty(t *testing.T) {
	dir := t.TempDir()
	all, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("loadAll empty: %v", err)
	}
	if all == nil || len(all) != 0 {
		t.Fatalf("expected empty slice, got %v", all)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/workspace/ -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/workspace_test.go
git commit -m "test(workspace): store, CompoundID, display name, channel meta"
```

---

### Task 4: Add migration from single-workspace config

**Files:**
- Modify: `internal/workspace/store.go`

- [ ] **Step 1: Add Migrate function**

Add this function to the end of `internal/workspace/store.go`:

```go
// MigrateFromConfig checks if the global config.json still has workspace-
// specific fields (tokens, aliases, hidden channels) from the pre-multi-
// workspace era. If so, it creates a workspace directory and moves the
// data there. Returns the team ID of the migrated workspace, or "" if no
// migration was needed.
//
// The caller must provide the teamID and teamName obtained from AuthTest
// (since config.json didn't store team ID).
func MigrateFromConfig(configDir string, teamID, teamName string, cfg MigrationSource) error {
	if teamID == "" {
		return fmt.Errorf("workspace: migration requires team ID")
	}
	if Exists(configDir, teamID) {
		// Already migrated.
		return nil
	}
	wsCfg := WorkspaceConfig{
		TeamID:     teamID,
		Name:       teamName,
		BotToken:   cfg.BotToken,
		AppToken:   cfg.AppToken,
		UserToken:  cfg.UserToken,
		ClientID:   cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AutoSignIn: true,
	}
	ws := &Workspace{
		Config:      wsCfg,
		TeamName:    teamName,
		ChannelMeta: make(map[string]ChannelMeta),
		Users:       make(map[string]types.User),
		LastSeen:    make(map[string]string),
	}
	// Migrate channel aliases.
	for chID, alias := range cfg.ChannelAliases {
		meta := ws.ChannelMeta[chID]
		meta.Alias = alias
		ws.ChannelMeta[chID] = meta
	}
	// Migrate hidden channels.
	for _, chID := range cfg.HiddenChannels {
		meta := ws.ChannelMeta[chID]
		meta.Hidden = true
		ws.ChannelMeta[chID] = meta
	}
	// Migrate collapsed groups → not stored per-workspace (global setting).
	// Migrate last channel.
	ws.Config.LastChannel = cfg.LastChannelID

	return Save(configDir, ws)
}

// MigrationSource carries the fields extracted from the old config.json
// that need to move into the workspace. The caller populates this from
// its config.Config struct.
type MigrationSource struct {
	BotToken       string
	AppToken       string
	UserToken      string
	ClientID       string
	ClientSecret   string
	ChannelAliases map[string]string
	HiddenChannels []string
	LastChannelID  string
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/workspace/...`
Expected: no errors

- [ ] **Step 3: Add migration test**

Add to `internal/workspace/workspace_test.go`:

```go
func TestMigrateFromConfig(t *testing.T) {
	dir := t.TempDir()
	src := MigrationSource{
		BotToken:       "xoxb-old",
		AppToken:       "xapp-old",
		UserToken:      "xoxp-old",
		ChannelAliases: map[string]string{"C001": "dev", "C002": "ops"},
		HiddenChannels: []string{"C003"},
		LastChannelID:  "C001",
	}
	err := MigrateFromConfig(dir, "T_OLD", "Old Corp", src)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Load and verify.
	ws, err := Load(dir, "T_OLD")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ws.Config.BotToken != "xoxb-old" {
		t.Fatalf("token: %q", ws.Config.BotToken)
	}
	if ws.Config.Name != "Old Corp" {
		t.Fatalf("name: %q", ws.Config.Name)
	}
	if ws.ChannelMeta["C001"].Alias != "dev" {
		t.Fatalf("alias: %q", ws.ChannelMeta["C001"].Alias)
	}
	if !ws.ChannelMeta["C003"].Hidden {
		t.Fatal("C003 should be hidden")
	}
	if ws.Config.LastChannel != "C001" {
		t.Fatalf("last channel: %q", ws.Config.LastChannel)
	}
	// Running again should be a no-op.
	err = MigrateFromConfig(dir, "T_OLD", "Old Corp", src)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/workspace/ -v`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/workspace/store.go internal/workspace/workspace_test.go
git commit -m "feat(workspace): migration from single-workspace config"
```

---

### Task 5: Update AuthTest to return team ID

**Files:**
- Modify: `internal/slack/client.go`

- [ ] **Step 1: Update SlackService interface**

In `internal/slack/client.go`, change the `AuthTest` method in the `SlackService` interface (line ~26) from:

```go
AuthTest() (string, error)
```

to:

```go
AuthTest() (teamName string, teamID string, err error)
```

- [ ] **Step 2: Update slackClient.AuthTest implementation**

Change the `AuthTest` method on `slackClient` (around line 235):

```go
func (c *slackClient) AuthTest() (string, string, error) {
	resp, err := c.primary.AuthTest()
	if err != nil && c.fallback != nil {
		resp, err = c.fallback.AuthTest()
	}
	if err != nil {
		return "", "", fmt.Errorf("slack auth test: %w", err)
	}
	c.userID = resp.UserID
	return resp.Team, resp.TeamID, nil
}
```

- [ ] **Step 3: Fix all call sites**

Search for `AuthTest()` calls across the codebase and update them to handle the new 3-return signature. Common patterns:

In `cmd/slackers/main.go`, wherever `AuthTest()` is called:
```go
// Old:
teamName, err := slackSvc.AuthTest()
// New:
teamName, teamID, err := slackSvc.AuthTest()
```

In `internal/tui/model.go`, `Init()` or wherever AuthTest result is handled:
```go
// Old:
case AuthTestResultMsg:
    m.teamName = msg.TeamName
// New:
case AuthTestResultMsg:
    m.teamName = msg.TeamName
    // Store teamID on the workspace
```

Update `AuthTestResultMsg` in `internal/tui/cmds.go` or wherever it's defined to include `TeamID string`.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go cmd/slackers/main.go internal/tui/model.go internal/tui/cmds.go
git commit -m "refactor(slack): AuthTest returns teamID alongside teamName"
```

---

### Task 6: Add WorkspaceID to Channel and Notification types

**Files:**
- Modify: `internal/types/types.go`
- Modify: `internal/notifications/notifications.go`

- [ ] **Step 1: Add WorkspaceID to Channel**

In `internal/types/types.go`, add `WorkspaceID` to the `Channel` struct:

```go
type Channel struct {
	ID          string
	Name        string
	WorkspaceID string // Slack team ID; empty for friend channels
	IsDM        bool
	IsPrivate   bool
	IsGroup     bool
	IsFriend    bool
	UserID      string
	UnreadCount int
}
```

- [ ] **Step 2: Add WorkspaceID to Notification**

In `internal/notifications/notifications.go`, add `WorkspaceID` to the `Notification` struct:

```go
type Notification struct {
	ID               string    `json:"id"`
	Type             Type      `json:"type"`
	WorkspaceID      string    `json:"workspace_id,omitempty"` // empty for P2P
	ChannelID        string    `json:"channel_id"`
	// ... rest unchanged
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors (new fields are zero-valued by default)

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go internal/notifications/notifications.go
git commit -m "feat: add WorkspaceID to Channel and Notification types"
```

---

### Task 7: Update global config for multi-workspace

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add multi-workspace fields to Config**

Add these fields to the `Config` struct in `internal/config/config.go`:

```go
LastActiveWorkspace string `json:"last_active_workspace,omitempty"`
```

**Do NOT remove** the old token fields yet — they're needed for migration detection. The migration code will check if `BotToken != ""` and move tokens to a workspace folder. After migration, these fields become empty in the saved config.

- [ ] **Step 2: Add migration detection helper**

```go
// NeedsMigration returns true if this config still has workspace-specific
// tokens that should be moved to a workspace folder.
func (c *Config) NeedsMigration() bool {
	return c.BotToken != "" || c.AppToken != ""
}

// ClearWorkspaceFields zeroes the workspace-specific fields after migration.
func (c *Config) ClearWorkspaceFields() {
	c.BotToken = ""
	c.AppToken = ""
	c.UserToken = ""
	c.ClientID = ""
	c.ClientSecret = ""
	c.ChannelAliases = nil
	c.HiddenChannels = nil
	c.LastChannelID = ""
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add LastActiveWorkspace, migration helpers"
```

---

### Task 8: Add workspaces shortcut to defaults

**Files:**
- Modify: `internal/shortcuts/defaults.json`

- [ ] **Step 1: Add the workspaces shortcut**

Add to `internal/shortcuts/defaults.json`:

```json
"workspaces": ["alt+w"]
```

- [ ] **Step 2: Verify the JSON is valid**

Run: `python3 -c "import json; json.load(open('internal/shortcuts/defaults.json'))"`
Expected: no error

- [ ] **Step 3: Commit**

```bash
git add internal/shortcuts/defaults.json
git commit -m "feat(shortcuts): add alt+w for workspaces overlay"
```

---

## Phase 2: Model Refactor

### Task 9: Add workspace map and accessors to Model

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add overlay constants**

After `overlayDownloads` (line 74), add:

```go
overlayWorkspaces
overlayWorkspaceEdit
```

- [ ] **Step 2: Add workspace fields to Model struct**

Add these fields to the `Model` struct (around line 435):

```go
// Multi-workspace state.
workspaces  map[string]*workspace.Workspace
activeWsID  string
```

Add the import for the workspace package at the top of the file.

- [ ] **Step 3: Add accessor methods**

Add these methods after the Model struct definition. These are the bridge that lets existing code work unchanged — they delegate to the active workspace:

```go
// activeWs returns the currently displayed workspace, or nil.
func (m *Model) activeWs() *workspace.Workspace {
	if m.activeWsID == "" {
		return nil
	}
	return m.workspaces[m.activeWsID]
}

// activeSlackSvc returns the active workspace's SlackService.
func (m *Model) activeSlackSvc() slackpkg.SlackService {
	if ws := m.activeWs(); ws != nil {
		return ws.SlackSvc
	}
	return m.slackSvc // fallback during migration
}

// activeSocketSvc returns the active workspace's SocketService.
func (m *Model) activeSocketSvc() slackpkg.SocketService {
	if ws := m.activeWs(); ws != nil {
		return ws.SocketSvc
	}
	return m.socketSvc // fallback during migration
}
```

**Note:** Keep the existing `slackSvc`/`socketSvc` fields temporarily as fallbacks. They'll be removed once all code paths go through workspaces. This allows incremental migration.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(model): add workspace map, overlay constants, accessor methods"
```

---

### Task 10: Create workspace event handlers

**Files:**
- Create: `internal/tui/handlers_workspace.go`

- [ ] **Step 1: Create the handler file with message types and handlers**

```go
// internal/tui/handlers_workspace.go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/debug"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/workspace"
)

// ── Messages ──────────────────────────────────────────────────

// WorkspaceSignInMsg is returned after a workspace's sign-in completes.
type WorkspaceSignInMsg struct {
	TeamID   string
	TeamName string
	MyUserID string
	Err      error
}

// WorkspaceSignOutMsg signals a workspace should be signed out.
type WorkspaceSignOutMsg struct{ TeamID string }

// WorkspaceSwitchMsg signals a switch to a different workspace.
type WorkspaceSwitchMsg struct{ TeamID string }

// WorkspaceAddedMsg signals a new workspace was created on disk.
type WorkspaceAddedMsg struct{ TeamID string }

// WorkspaceRemovedMsg signals a workspace was deleted.
type WorkspaceRemovedMsg struct{ TeamID string }

// WorkspaceSlackEventMsg wraps a slack event with the originating workspace.
type WorkspaceSlackEventMsg struct {
	TeamID string
	Event  slackpkg.SocketEvent
}

// WorkspacePollTickMsg is a per-workspace poll tick.
type WorkspacePollTickMsg struct{ TeamID string }

// WorkspaceBgPollTickMsg is a per-workspace background poll tick.
type WorkspaceBgPollTickMsg struct{ TeamID string }

// ── Commands ──────────────────────────────────────────────────

// signInWorkspaceCmd starts the sign-in process for a workspace.
func signInWorkspaceCmd(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws.Config.BotToken == "" {
			return WorkspaceSignInMsg{
				TeamID: ws.ID(),
				Err:    fmt.Errorf("no bot token configured"),
			}
		}
		svc := slackpkg.NewSlackClient(ws.Config.BotToken, ws.Config.UserToken)
		teamName, teamID, err := svc.AuthTest()
		if err != nil {
			return WorkspaceSignInMsg{TeamID: ws.ID(), Err: err}
		}
		ws.SlackSvc = svc
		ws.TeamName = teamName
		ws.MyUserID = svc.MyUserID()
		// Ensure the stored team ID matches.
		if ws.Config.TeamID == "" {
			ws.Config.TeamID = teamID
		}
		if ws.Config.BotToken != "" && ws.Config.AppToken != "" {
			ws.SocketSvc = slackpkg.NewSocketClient(ws.Config.BotToken, ws.Config.AppToken)
		}
		ws.EventChan = make(chan slackpkg.SocketEvent, 64)
		ws.Ctx, ws.Cancel = context.WithCancel(context.Background())
		ws.SignedIn = true
		ws.Config.SignedOut = false
		return WorkspaceSignInMsg{
			TeamID:   ws.ID(),
			TeamName: teamName,
			MyUserID: svc.MyUserID(),
		}
	}
}

// ── Handlers ──────────────────────────────────────────────────

func (m *Model) handleWorkspaceSignIn(msg WorkspaceSignInMsg) (Model, tea.Cmd) {
	ws := m.workspaces[msg.TeamID]
	if ws == nil || msg.Err != nil {
		if msg.Err != nil {
			debug.Log("[workspace] sign-in failed for %s: %v", msg.TeamID, msg.Err)
		}
		return *m, nil
	}
	debug.Log("[workspace] signed in: %s (%s) as %s", msg.TeamName, msg.TeamID, msg.MyUserID)

	var cmds []tea.Cmd
	// Load users and channels.
	if ws.SlackSvc != nil {
		cmds = append(cmds,
			loadUsersForWorkspaceCmd(ws),
			loadChannelsForWorkspaceCmd(ws),
		)
	}
	// Connect socket.
	if ws.SocketSvc != nil {
		cmds = append(cmds,
			connectWorkspaceSocketCmd(ws),
			waitForWorkspaceEvent(ws),
		)
	}
	// Start polling.
	cmds = append(cmds, workspacePollTickCmd(ws.ID(), m.cfg.PollInterval))
	cmds = append(cmds, workspaceBgPollTickCmd(ws.ID(), m.cfg.PollIntervalBg))

	// If this is the only workspace or no active one, make it active.
	if m.activeWsID == "" {
		m.activeWsID = msg.TeamID
	}
	// Save config to clear signed_out flag.
	_ = workspace.Save(m.cfg.ConfigDir(), ws)

	return *m, tea.Batch(cmds...)
}

func (m *Model) handleWorkspaceSignOut(msg WorkspaceSignOutMsg) (Model, tea.Cmd) {
	ws := m.workspaces[msg.TeamID]
	if ws == nil {
		return *m, nil
	}
	debug.Log("[workspace] signing out: %s", ws.DisplayName())
	// Cancel the workspace context (stops socket + polling).
	if ws.Cancel != nil {
		ws.Cancel()
	}
	if ws.EventChan != nil {
		close(ws.EventChan)
		ws.EventChan = nil
	}
	ws.SlackSvc = nil
	ws.SocketSvc = nil
	ws.SignedIn = false
	ws.Users = make(map[string]types.User)
	ws.Channels = nil
	ws.Config.SignedOut = true
	_ = workspace.Save(m.cfg.ConfigDir(), ws)

	// If this was the active workspace, switch to another.
	if m.activeWsID == msg.TeamID {
		m.switchToNextSignedIn()
	}
	return *m, nil
}

func (m *Model) handleWorkspaceSwitch(msg WorkspaceSwitchMsg) (Model, tea.Cmd) {
	ws := m.workspaces[msg.TeamID]
	if ws == nil {
		return *m, nil
	}
	// Save current workspace's last channel.
	if cur := m.activeWs(); cur != nil && m.currentCh != nil {
		cur.Config.LastChannel = m.currentCh.ID
		_ = workspace.Save(m.cfg.ConfigDir(), cur)
	}
	m.activeWsID = msg.TeamID
	m.cfg.LastActiveWorkspace = msg.TeamID
	debug.Log("[workspace] switched to: %s", ws.DisplayName())

	// Rebuild sidebar with the new workspace's channels.
	m.rebuildSidebarForWorkspace(ws)
	// Restore last channel.
	if ws.Config.LastChannel != "" {
		m.openChannelByID(ws.Config.LastChannel)
	}
	return *m, nil
}

// switchToNextSignedIn sets activeWsID to the next signed-in workspace.
func (m *Model) switchToNextSignedIn() {
	for id, ws := range m.workspaces {
		if ws.SignedIn && id != m.activeWsID {
			m.activeWsID = id
			m.rebuildSidebarForWorkspace(ws)
			return
		}
	}
	m.activeWsID = ""
}

// rebuildSidebarForWorkspace refreshes the channel list model with the
// given workspace's channels + global friends.
func (m *Model) rebuildSidebarForWorkspace(ws *workspace.Workspace) {
	// Merge workspace channels with friend channels.
	var channels []types.Channel
	if ws != nil {
		channels = append(channels, ws.Channels...)
	}
	// Add friend channels.
	if m.friendStore != nil {
		for _, f := range m.friendStore.Friends() {
			channels = append(channels, types.Channel{
				ID:       f.UserID,
				Name:     f.DisplayName(),
				IsFriend: true,
				UserID:   f.UserID,
			})
		}
	}
	m.channels.SetChannels(channels)
}
```

**Note:** Some helper commands (`loadUsersForWorkspaceCmd`, `loadChannelsForWorkspaceCmd`, `connectWorkspaceSocketCmd`, `waitForWorkspaceEvent`, `workspacePollTickCmd`, `workspaceBgPollTickCmd`) are referenced here but will be implemented in Task 11. The `fmt` import will be needed — add it to the imports. Also add the `types` import.

- [ ] **Step 2: Add missing import for fmt**

Make sure the imports include `"fmt"` and `"github.com/rw3iss/slackers/internal/types"`.

- [ ] **Step 3: Verify build** (expect errors for undefined commands — that's OK, Task 11 defines them)

Run: `go build ./internal/tui/... 2>&1 | grep -c "undefined"`
Expected: some undefined errors for the workspace command functions

- [ ] **Step 4: Commit**

```bash
git add internal/tui/handlers_workspace.go
git commit -m "feat(tui): workspace event handlers and lifecycle messages"
```

---

### Task 11: Add per-workspace commands to cmds.go

**Files:**
- Modify: `internal/tui/cmds.go`

- [ ] **Step 1: Add workspace-scoped command constructors**

Add these functions to `internal/tui/cmds.go`:

```go
// ── Per-workspace commands ──────────────────────────────────

func loadUsersForWorkspaceCmd(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws.SlackSvc == nil {
			return nil
		}
		users, err := ws.SlackSvc.ListUsers()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return WorkspaceUsersLoadedMsg{TeamID: ws.ID(), Users: users}
	}
}

func loadChannelsForWorkspaceCmd(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws.SlackSvc == nil {
			return nil
		}
		channels, err := ws.SlackSvc.ListChannels()
		if err != nil {
			return ErrMsg{Err: err}
		}
		// Tag channels with workspace ID.
		for i := range channels {
			channels[i].WorkspaceID = ws.ID()
		}
		return WorkspaceChannelsLoadedMsg{TeamID: ws.ID(), Channels: channels}
	}
}

func connectWorkspaceSocketCmd(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws.SocketSvc == nil || ws.EventChan == nil {
			return nil
		}
		_ = ws.SocketSvc.Connect(ws.Ctx, ws.EventChan)
		return nil
	}
}

func waitForWorkspaceEvent(ws *workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws.EventChan == nil {
			return nil
		}
		event, ok := <-ws.EventChan
		if !ok {
			return nil // channel closed (sign-out)
		}
		return WorkspaceSlackEventMsg{TeamID: ws.ID(), Event: event}
	}
}

func workspacePollTickCmd(teamID string, intervalSec int) tea.Cmd {
	if intervalSec <= 0 {
		intervalSec = 10
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return WorkspacePollTickMsg{TeamID: teamID}
	})
}

func workspaceBgPollTickCmd(teamID string, intervalSec int) tea.Cmd {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return WorkspaceBgPollTickMsg{TeamID: teamID}
	})
}

// ── Workspace data loaded messages ──────────────────────────

type WorkspaceUsersLoadedMsg struct {
	TeamID string
	Users  map[string]types.User
}

type WorkspaceChannelsLoadedMsg struct {
	TeamID   string
	Channels []types.Channel
}
```

Add the `workspace` package import to `cmds.go`.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/tui/cmds.go
git commit -m "feat(cmds): per-workspace socket, polling, and data loading commands"
```

---

## Phase 3: Multi-Workspace Lifecycle

### Task 12: Wire workspace loading into startup

**Files:**
- Modify: `cmd/slackers/main.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Update rootCmd to load workspaces**

In `cmd/slackers/main.go`, in the `rootCmd.RunE` function, after loading config but before creating the Model:

```go
// Load workspaces from disk.
wsList, err := workspace.LoadAll(cfg.ConfigDir())
if err != nil {
    debug.Log("workspace load error: %v", err)
    wsList = []*workspace.Workspace{}
}

// Migration: if config still has tokens, migrate to workspace folder.
if cfg.NeedsMigration() {
    // Create a temporary service to get team ID.
    tmpSvc := slack.NewSlackClient(cfg.BotToken, cfg.UserToken)
    teamName, teamID, authErr := tmpSvc.AuthTest()
    if authErr == nil && teamID != "" {
        migSrc := workspace.MigrationSource{
            BotToken:       cfg.BotToken,
            AppToken:       cfg.AppToken,
            UserToken:      cfg.UserToken,
            ClientID:       cfg.ClientID,
            ClientSecret:   cfg.ClientSecret,
            ChannelAliases: cfg.ChannelAliases,
            HiddenChannels: cfg.HiddenChannels,
            LastChannelID:  cfg.LastChannelID,
        }
        if err := workspace.MigrateFromConfig(cfg.ConfigDir(), teamID, teamName, migSrc); err != nil {
            debug.Log("migration error: %v", err)
        } else {
            cfg.LastActiveWorkspace = teamID
            cfg.ClearWorkspaceFields()
            _ = config.Save(cfg)
            // Reload workspaces after migration.
            wsList, _ = workspace.LoadAll(cfg.ConfigDir())
        }
    }
}
```

- [ ] **Step 2: Update NewModel to accept workspaces**

Change `NewModel` signature to accept `[]*workspace.Workspace` instead of `slackSvc`/`socketSvc`:

```go
func NewModel(wsList []*workspace.Workspace, cfg *config.Config, version string, friendStore *friends.FriendStore, friendHistory *friends.ChatHistoryStore) Model
```

Inside `NewModel`, build the workspace map:

```go
wsMap := make(map[string]*workspace.Workspace)
for _, ws := range wsList {
    wsMap[ws.ID()] = ws
}
m.workspaces = wsMap
m.activeWsID = cfg.LastActiveWorkspace
```

Keep the old `slackSvc`/`socketSvc` fields nil — all access goes through workspaces now.

- [ ] **Step 3: Update Init() for workspace sign-ins**

In `Model.Init()`, instead of the single-workspace service commands, dispatch sign-in commands for each auto-sign-in workspace:

```go
for _, ws := range m.workspaces {
    if ws.Config.AutoSignIn && !ws.Config.SignedOut {
        cmds = append(cmds, signInWorkspaceCmd(ws))
    }
}
```

Remove the old `if m.slackSvc != nil` / `if m.socketSvc != nil` blocks.

- [ ] **Step 4: Update rootCmd call site**

In `cmd/slackers/main.go`, change the `NewModel` call:

```go
// Old:
model := tui.NewModel(slackSvc, socketSvc, cfg, version, friendStore, friendHistory)
// New:
model := tui.NewModel(wsList, cfg, version, friendStore, friendHistory)
```

Remove the `slackSvc`/`socketSvc` creation code from `rootCmd` since workspaces handle their own services.

- [ ] **Step 5: Handle WorkspaceUsersLoadedMsg and WorkspaceChannelsLoadedMsg in Update**

In `model.go`'s `Update`, add cases:

```go
case WorkspaceUsersLoadedMsg:
    if ws := m.workspaces[msg.TeamID]; ws != nil {
        ws.Users = msg.Users
        if msg.TeamID == m.activeWsID {
            m.users = msg.Users  // keep backward compat
        }
    }
    return m, nil

case WorkspaceChannelsLoadedMsg:
    if ws := m.workspaces[msg.TeamID]; ws != nil {
        ws.Channels = msg.Channels
        if msg.TeamID == m.activeWsID {
            m.rebuildSidebarForWorkspace(ws)
        }
    }
    return m, nil

case WorkspaceSlackEventMsg:
    // Route to the workspace's event handler.
    ws := m.workspaces[msg.TeamID]
    if ws == nil || !ws.SignedIn {
        return m, waitForWorkspaceEvent(ws)
    }
    // Process the event (reuse existing socket event handling).
    // If active workspace: update UI. If background: update state silently.
    cmd := m.handleSlackEventForWorkspace(ws, msg.Event)
    return m, tea.Batch(cmd, waitForWorkspaceEvent(ws))

case WorkspacePollTickMsg:
    ws := m.workspaces[msg.TeamID]
    if ws == nil || !ws.SignedIn {
        return m, nil
    }
    cmd := m.handlePollForWorkspace(ws)
    return m, tea.Batch(cmd, workspacePollTickCmd(msg.TeamID, m.cfg.PollInterval))

case WorkspaceBgPollTickMsg:
    ws := m.workspaces[msg.TeamID]
    if ws == nil || !ws.SignedIn {
        return m, nil
    }
    cmd := m.handleBgPollForWorkspace(ws)
    return m, tea.Batch(cmd, workspaceBgPollTickCmd(msg.TeamID, m.cfg.PollIntervalBg))

case WorkspaceSignInMsg:
    return m.handleWorkspaceSignIn(msg)

case WorkspaceSignOutMsg:
    return m.handleWorkspaceSignOut(msg)

case WorkspaceSwitchMsg:
    return m.handleWorkspaceSwitch(msg)
```

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: no errors (some handler stubs may need adding)

- [ ] **Step 7: Commit**

```bash
git add cmd/slackers/main.go internal/tui/model.go
git commit -m "feat: wire workspace loading, migration, and lifecycle into startup"
```

---

### Task 13: Add workspace key handler for Alt+W

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add workspaces shortcut to key dispatch**

In the Model's key handling section (where other global shortcuts like `ctrl+s` for settings are dispatched), add:

```go
case normalizeShortcutKey("workspaces"):
    m.workspacesList = NewWorkspacesModel(m.workspaces, m.activeWsID)
    m.workspacesList.SetSize(m.width, m.height)
    m.overlay = overlayWorkspaces
    return m, nil
```

Add the `workspacesList WorkspacesModel` field to the Model struct.

- [ ] **Step 2: Add overlay dispatch in Update**

In the `case tea.KeyMsg` overlay dispatch section (where `overlaySettings`, `overlayHelp`, etc. are handled), add:

```go
if m.overlay == overlayWorkspaces {
    var cmd tea.Cmd
    m.workspacesList, cmd = m.workspacesList.Update(msg)
    return m, cmd
}
if m.overlay == overlayWorkspaceEdit {
    var cmd tea.Cmd
    m.workspaceEdit, cmd = m.workspaceEdit.Update(msg)
    return m, cmd
}
```

Add `workspaceEdit WorkspaceEditModel` field to Model.

- [ ] **Step 3: Add overlay View dispatch**

In the `View` function's overlay rendering section, add:

```go
case overlayWorkspaces:
    return m.workspacesList.View()
case overlayWorkspaceEdit:
    return m.workspaceEdit.View()
```

- [ ] **Step 4: Verify build** (will fail — WorkspacesModel not yet defined)

This is expected — Task 14 creates the overlay.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(model): Alt+W shortcut and overlay dispatch for workspaces"
```

---

## Phase 4: UI

### Task 14: Create Workspaces overlay

**Files:**
- Create: `internal/tui/workspaces_ui.go`

- [ ] **Step 1: Create the workspaces overlay model**

```go
// internal/tui/workspaces_ui.go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/workspace"
)

// ── Messages ──────────────────────────────────────────────────

type WorkspacesOpenMsg struct{}
type WorkspacesCloseMsg struct{}

// ── Workspaces List Model ─────────────────────────────────────

type WorkspacesModel struct {
	workspaces []*workspace.Workspace
	activeID   string
	selected   int
	width      int
	height     int
	message    string
	confirmDel bool
}

func NewWorkspacesModel(wsMap map[string]*workspace.Workspace, activeID string) WorkspacesModel {
	var wsList []*workspace.Workspace
	for _, ws := range wsMap {
		wsList = append(wsList, ws)
	}
	// Sort alphabetically.
	sort.Slice(wsList, func(i, j int) bool {
		return wsList[i].DisplayName() < wsList[j].DisplayName()
	})
	// Find the active one.
	sel := 0
	for i, ws := range wsList {
		if ws.ID() == activeID {
			sel = i
			break
		}
	}
	return WorkspacesModel{
		workspaces: wsList,
		activeID:   activeID,
		selected:   sel,
	}
}

func (m *WorkspacesModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m WorkspacesModel) Update(msg tea.Msg) (WorkspacesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmDel {
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				if m.selected >= 0 && m.selected < len(m.workspaces) {
					ws := m.workspaces[m.selected]
					m.confirmDel = false
					m.message = "Removed " + ws.DisplayName()
					return m, func() tea.Msg { return WorkspaceRemovedMsg{TeamID: ws.ID()} }
				}
			default:
				m.confirmDel = false
				m.message = ""
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return WorkspacesCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			} else {
				m.selected = len(m.workspaces) - 1
			}
		case "down", "j":
			if m.selected < len(m.workspaces)-1 {
				m.selected++
			} else {
				m.selected = 0
			}
		case "enter":
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				if ws.SignedIn {
					return m, func() tea.Msg {
						return tea.Sequence(
							func() tea.Msg { return WorkspaceSwitchMsg{TeamID: ws.ID()} },
							func() tea.Msg { return WorkspacesCloseMsg{} },
						)()
					}
				}
				// Not signed in — sign in first, then switch.
				return m, func() tea.Msg {
					return tea.Sequence(
						func() tea.Msg { return WorkspaceSignInMsg{TeamID: ws.ID()} },
						func() tea.Msg { return WorkspaceSwitchMsg{TeamID: ws.ID()} },
						func() tea.Msg { return WorkspacesCloseMsg{} },
					)()
				}
			}
		case "a":
			return m, func() tea.Msg { return WorkspaceEditOpenMsg{} }
		case "e":
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				return m, func() tea.Msg { return WorkspaceEditOpenMsg{TeamID: ws.ID()} }
			}
		case "s":
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				if ws.SignedIn {
					return m, func() tea.Msg { return WorkspaceSignOutMsg{TeamID: ws.ID()} }
				}
				return m, signInWorkspaceCmd(ws)
			}
		case "d", "delete":
			if m.selected >= 0 && m.selected < len(m.workspaces) {
				ws := m.workspaces[m.selected]
				m.confirmDel = true
				m.message = fmt.Sprintf("Remove %q? (y/N)", ws.DisplayName())
			}
		}
	}
	return m, nil
}

func (m WorkspacesModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorSelection).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(ColorMenuItem)
	muteStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	onlineStyle := lipgloss.NewStyle().Foreground(ColorFriendOnline)
	offlineStyle := lipgloss.NewStyle().Foreground(ColorFriendOffline)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Workspaces"))
	b.WriteString("\n\n")

	if len(m.workspaces) == 0 {
		b.WriteString(dimStyle.Render("  No workspaces configured."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Press 'a' to add one."))
		b.WriteString("\n")
	}

	for i, ws := range m.workspaces {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		// Status indicator.
		var status string
		if ws.SignedIn {
			status = onlineStyle.Render("●")
		} else {
			status = offlineStyle.Render("○")
		}
		name := ws.DisplayName()
		// Badge.
		badge := ""
		if ws.SignedIn && ws.UnreadCount > 0 {
			badge = muteStyle.Render(fmt.Sprintf(" (%d unread)", ws.UnreadCount))
		} else if ws.SignedIn {
			badge = muteStyle.Render(" signed in")
		} else {
			badge = muteStyle.Render(" signed out")
		}
		// Active indicator.
		activeTag := ""
		if ws.ID() == m.activeID {
			activeTag = selStyle.Render(" ★")
		}

		var line string
		if i == m.selected {
			line = selStyle.Render(marker+status+" "+name) + badge + activeTag
		} else {
			line = textStyle.Render(marker+status+" "+name) + badge + activeTag
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n\n")
	}
	b.WriteString(dimStyle.Render("  ↑/↓: select" + HintSep + "Enter: switch" + HintSep + "a: add" + HintSep + "e: edit" + HintSep + "s: sign in/out" + HintSep + "d: remove" + HintSep + FooterHintClose))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// ── Edit Workspace Model ──────────────────────────────────────

type WorkspaceEditOpenMsg struct {
	TeamID string // empty = new workspace
}

type WorkspaceEditCloseMsg struct{}

type WorkspaceEditSavedMsg struct {
	TeamID string
}

// WorkspaceEditModel will be implemented as a settings-like overlay
// with editable fields for name, tokens, auto-sign-in, etc.
// For now, a minimal stub that compiles.
type WorkspaceEditModel struct {
	workspace *workspace.Workspace
	isNew     bool
	width     int
	height    int
	// Fields will be added when implementing the full edit overlay.
}

func NewWorkspaceEditModel(ws *workspace.Workspace) WorkspaceEditModel {
	isNew := ws == nil
	if isNew {
		ws = &workspace.Workspace{
			Config: workspace.WorkspaceConfig{AutoSignIn: true},
		}
	}
	return WorkspaceEditModel{workspace: ws, isNew: isNew}
}

func (m *WorkspaceEditModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m WorkspaceEditModel) Update(msg tea.Msg) (WorkspaceEditModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" {
			return m, func() tea.Msg { return WorkspaceEditCloseMsg{} }
		}
	}
	return m, nil
}

func (m WorkspaceEditModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	title := "Edit Workspace"
	if m.isNew {
		title = "Add Workspace"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	if m.workspace != nil && m.workspace.Config.Name != "" {
		b.WriteString("  Name: " + m.workspace.Config.Name)
	} else {
		b.WriteString(dimStyle.Render("  Paste a setup hash or enter tokens manually."))
	}
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  " + FooterHintClose))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderActive).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
```

- [ ] **Step 2: Add sort import**

Add `"sort"` to the imports.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/tui/workspaces_ui.go
git commit -m "feat(tui): Workspaces list overlay and Edit Workspace stub"
```

---

### Task 15: Add workspace name to sidebar header

**Files:**
- Modify: `internal/tui/channels.go`

- [ ] **Step 1: Add workspace name rendering**

In the `View()` method of `ChannelListModel` (or wherever the sidebar content is built), add a workspace header at the top of the channel list, before the first group header:

Find where channel lines are built (likely in a `renderSidebar` or similar method) and prepend:

```go
// Workspace header — centered name at top of sidebar.
if m.workspaceName != "" {
    wsHeader := lipgloss.NewStyle().
        Foreground(ColorPageHeader).
        Bold(true).
        Width(m.width - 4). // account for padding
        Align(lipgloss.Center).
        Render(m.workspaceName)
    if m.multipleWorkspaces {
        wsHeader += " ⇅"
    }
    lines = append([]displayLine{{text: wsHeader}}, lines...)
}
```

Add `workspaceName string` and `multipleWorkspaces bool` fields to `ChannelListModel`, with setter methods:

```go
func (m *ChannelListModel) SetWorkspaceName(name string) {
    m.workspaceName = name
}

func (m *ChannelListModel) SetMultipleWorkspaces(v bool) {
    m.multipleWorkspaces = v
}
```

- [ ] **Step 2: Call the setters when switching workspaces**

In `handleWorkspaceSwitch` and after sign-in, call:

```go
m.channels.SetWorkspaceName(ws.DisplayName())
m.channels.SetMultipleWorkspaces(len(m.signedInWorkspaces()) > 1)
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/tui/channels.go internal/tui/handlers_workspace.go
git commit -m "feat(sidebar): show workspace name in sidebar header"
```

---

## Phase 5: Commands & CLI

### Task 16: Update setup command for multi-workspace

**Files:**
- Modify: `cmd/slackers/main.go`
- Modify: `internal/setup/setup.go`

- [ ] **Step 1: Add TeamID to setup.Config**

In `internal/setup/setup.go`, add `TeamID` to the `Config` struct:

```go
type Config struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AppToken     string `json:"app_token,omitempty"`
	UserToken    string `json:"user_token,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
}
```

Update `IsEmpty` to include TeamID check.

- [ ] **Step 2: Update setup command handler**

In `cmd/slackers/main.go`, update the `setupCmd` handler to create or update workspace folders:

After decoding the setup config, instead of writing directly to `config.json`:

```go
// If we have tokens, determine team ID via AuthTest.
if setupCfg.AppToken != "" {
    botToken := "" // derive from OAuth if needed
    if setupCfg.ClientID != "" && setupCfg.ClientSecret != "" {
        // Run OAuth flow to get bot token.
        // ... existing OAuth logic ...
    }
    // Create or update workspace.
    svc := slack.NewSlackClient(botToken, setupCfg.UserToken)
    teamName, teamID, err := svc.AuthTest()
    if err != nil {
        return fmt.Errorf("auth test failed: %w", err)
    }
    wsCfg := workspace.WorkspaceConfig{
        TeamID:       teamID,
        Name:         teamName,
        BotToken:     botToken,
        AppToken:     setupCfg.AppToken,
        UserToken:    setupCfg.UserToken,
        ClientID:     setupCfg.ClientID,
        ClientSecret: setupCfg.ClientSecret,
        AutoSignIn:   true,
    }
    if workspace.Exists(cfg.ConfigDir(), teamID) {
        // Update existing.
        ws, _ := workspace.Load(cfg.ConfigDir(), teamID)
        ws.Config = wsCfg
        _ = workspace.Save(cfg.ConfigDir(), ws)
        fmt.Printf("Updated workspace: %s\n", teamName)
    } else {
        // Create new.
        _, err := workspace.Create(cfg.ConfigDir(), wsCfg)
        if err != nil {
            return err
        }
        fmt.Printf("Added workspace: %s\n", teamName)
    }
    cfg.LastActiveWorkspace = teamID
    _ = config.Save(cfg)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add cmd/slackers/main.go internal/setup/setup.go
git commit -m "feat(cli): setup command creates/updates workspace folders"
```

---

### Task 17: Update /share and /invite commands

**Files:**
- Modify: `internal/tui/commands_basic.go`

- [ ] **Step 1: Update /share setup to use active workspace**

Find the `/share setup` handler in `commands_basic.go`. Update it to:

1. Default to the active workspace's tokens
2. Accept an optional workspace name argument
3. Auto-suggest workspace names

```go
// In the share command handler:
ws := m.activeWs()
if ws == nil {
    return "No active workspace"
}
// Use ws.Config tokens instead of m.cfg tokens.
setupCfg := setup.Config{
    ClientID:     ws.Config.ClientID,
    ClientSecret: ws.Config.ClientSecret,
    AppToken:     ws.Config.AppToken,
}
hash, err := setup.Encode(setupCfg)
```

- [ ] **Step 2: Update /invite to use active workspace**

Similarly update the `/invite` handler to use `m.activeWs()` for workspace context.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands_basic.go
git commit -m "feat(commands): /share and /invite use active workspace"
```

---

### Task 18: Final build and smoke test

- [ ] **Step 1: Full build**

Run: `make build && make install`
Expected: clean build

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 3: Verify single-workspace backward compatibility**

Launch `slackers` with existing single-workspace config. Verify:
- Migration runs automatically (tokens move to `workspaces/<team-id>/`)
- App functions identically to before
- Settings overlay still works
- Messages still flow

- [ ] **Step 4: Verify multi-workspace**

Run `slackers setup <second-workspace-hash>` to add a second workspace. Then:
- Press `Alt+W` → Workspaces overlay shows both
- Enter to switch → sidebar shows new workspace's channels
- Enter to switch back → previous workspace restored
- Sign out one → it shows as signed out, other keeps working

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: multi-workspace support — complete implementation"
```

---

## Self-Review Checklist

### Spec Coverage
- [x] Workspace abstraction type → Task 1
- [x] Filesystem layout → Task 2
- [x] Migration → Task 4
- [x] AuthTest returns teamID → Task 5
- [x] Channel/Notification WorkspaceID → Task 6
- [x] Config changes → Task 7
- [x] Alt+W shortcut → Task 8, 13
- [x] Model refactor with accessors → Task 9
- [x] Event handlers → Task 10
- [x] Per-workspace commands → Task 11
- [x] Startup lifecycle → Task 12
- [x] Workspaces overlay → Task 14
- [x] Edit Workspace overlay → Task 14 (stub)
- [x] Sidebar header → Task 15
- [x] CLI setup multi-workspace → Task 16
- [x] /share and /invite → Task 17
- [x] Workspace-aware notifications → Task 6 (struct), needs overlay update (future)
- [x] Sign-in/out lifecycle → Task 10
- [x] Switch lifecycle → Task 10
- [x] Compound channel IDs → Task 1
- [x] Auto-suggest workspace names for commands → Task 17 (noted, detail for future)

### Type Consistency
- `WorkspaceConfig` fields consistent across Tasks 1, 2, 4, 16
- `CompoundID`/`SplitCompoundID` defined in Task 1, used in later tasks
- `WorkspaceSignInMsg`/`WorkspaceSignOutMsg`/`WorkspaceSwitchMsg` defined in Task 10, handled in Task 12
- `AuthTest` returns `(string, string, error)` in Task 5, consumed in Tasks 10, 12, 16
