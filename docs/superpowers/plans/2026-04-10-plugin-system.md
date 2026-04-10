# Plugin System & Internal API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a plugin/module system with an internal API/SDK, UI component framework, plugin lifecycle management, P2P message routing for plugins, config merging, and example plugins (games, weather).

**Architecture:** The system is split into 5 independent phases, each producing working, testable software. Phase 1 creates the internal API that wraps the existing Model into a stable, public interface. Phase 2 builds the plugin lifecycle (discovery, loading, config merging). Phase 3 adds the UI component SDK for plugins to create custom views. Phase 4 adds P2P message routing for inter-plugin communication. Phase 5 implements example plugins (games, weather) to validate the system.

**Tech Stack:** Go 1.25, Bubbletea (Elm architecture TUI), lipgloss (styling), libp2p (P2P), existing command/registry/overlay framework.

---

## Scope: 5 Independent Sub-Plans

This plan covers 5 subsystems that should be implemented in order but each produces independently testable, committable software:

| Phase | Subsystem | Description |
|-------|-----------|-------------|
| **1** | Internal API (`internal/api/`) | Stable interface wrapping Model state, services, and operations |
| **2** | Plugin Lifecycle (`internal/plugins/`) | Discovery, manifest, loading, config merge, commands |
| **3** | UI SDK (`internal/api/ui/`) | Component system for plugins to create custom views |
| **4** | P2P Plugin Routing | `[PLUGIN:name:*]` message protocol, listeners, forwarding |
| **5** | Example Plugins | Games (snake, tetris), weather viewer — validate the full stack |

---

## Phase 1: Internal API (`internal/api/`)

### Design Rationale

The existing codebase has all functionality scattered across `*Model` methods, service interfaces, and package-level functions. The API layer wraps these into a **stable, documented interface** that plugins (and internal code) can depend on without reaching into Model internals.

The API is a **facade** — it doesn't duplicate logic, it delegates to existing services. It's organized into sub-interfaces matching the domain:

```
api.App          → app lifecycle, config, status bar
api.Messages     → send/fetch/edit/delete messages
api.Channels     → list/create/rename/hide channels
api.Friends      → friend management, P2P operations
api.Files        → upload/download/browse files
api.View         → overlay/window management
api.Shortcuts    → register/query keyboard shortcuts
api.Commands     → register/run slash commands
api.Theme        → read current theme colors
api.Events       → subscribe to app events
```

### File Structure

```
internal/api/
├── api.go              # Top-level API interface + App sub-interface
├── messages.go         # Messages sub-interface
├── channels.go         # Channels sub-interface
├── friends.go          # Friends sub-interface
├── files.go            # Files sub-interface
├── view.go             # View management sub-interface
├── shortcuts.go        # Shortcuts sub-interface
├── commands.go         # Commands sub-interface (wraps registry)
├── theme.go            # Theme color access sub-interface
├── events.go           # Event subscription system
├── host.go             # Concrete implementation (wraps *Model)
└── types.go            # Shared API types (PluginInfo, ViewID, etc.)
```

### Key Interfaces

```go
// api.go — the root interface every plugin receives
type API interface {
    App() App
    Messages() Messages
    Channels() Channels
    Friends() Friends
    Files() Files
    View() View
    Shortcuts() Shortcuts
    Commands() Commands
    Theme() Theme
    Events() Events
}

// App — lifecycle, config, status
type App interface {
    Version() string
    Config() map[string]any          // read-only config snapshot
    SetConfig(key string, val any)   // debounced save
    SetStatusBar(text string)        // bottom status message
    SetWarning(text string)          // warning message
    ClearWarning()
    IsSlackConnected() bool
    IsP2PEnabled() bool
    SlackerID() string
    MyName() string
}

// Messages — chat operations
type Messages interface {
    Send(channelID, text string) error
    SendReply(channelID, parentTS, text string) error
    Edit(channelID, messageTS, newText string) error
    Delete(channelID, messageTS string) error
    React(channelID, messageTS, emoji string) error
    FetchHistory(channelID string, limit int) ([]MessageInfo, error)
    CurrentChannel() *ChannelInfo
    CurrentMessages() []MessageInfo
}

// View — overlay/window management
type View interface {
    ShowOverlay(id string, model OverlayModel)
    CloseOverlay()
    CurrentOverlay() string
    SetFocus(pane FocusPane)
    ScreenSize() (width, height int)
}

// Events — pub/sub for app events
type Events interface {
    Subscribe(eventType string, handler EventHandler) UnsubscribeFunc
    Emit(event Event)
}
```

### Implementation Strategy

The `host.go` file implements `API` by holding a pointer to `*Model` and delegating calls. Since Model is a value type in bubbletea, mutations go through `tea.Cmd` messages. The host queues these via a channel that the Model's Update loop drains:

```go
type Host struct {
    model     *Model           // live pointer (set in NewModel)
    cmdQueue  chan tea.Cmd      // plugin → Model command channel
    eventBus  *EventBus        // pub/sub for app events
}

// The Model drains cmdQueue in its Update loop:
// case PluginCmdMsg:
//     return m, msg.Cmd
```

---

## Phase 2: Plugin Lifecycle (`internal/plugins/`)

### Design Rationale

Plugins are **Go packages compiled into the binary** (not dynamic `.so` loading — Go's `plugin` package is fragile and Linux-only). Each plugin lives in its own directory under `internal/plugins/` and registers itself via an `init()` function or a registry call in `main.go`.

For user-installable plugins (future), the system supports a **manifest-only mode** where plugin metadata is indexed from `~/.config/slackers/plugins/` but the actual code must be compiled into the binary. This keeps the architecture simple while allowing the plugin management UI to exist now.

### File Structure

```
internal/plugins/
├── manager.go          # PluginManager: lifecycle, enable/disable, config merge
├── manifest.go         # PluginManifest: metadata, version, author
├── registry.go         # Global plugin registry (register at init time)
├── loader.go           # Config/shortcut/emote/theme merging
├── types.go            # Plugin interface, PluginState enum
│
├── games/              # Example: mini games plugin
│   ├── plugin.go       # Plugin interface implementation
│   ├── snake.go        # Snake game
│   ├── tetris.go       # Tetris game
│   └── manifest.json   # Plugin metadata
│
└── weather/            # Example: weather viewer plugin
    ├── plugin.go       # Plugin interface implementation
    ├── weather.go       # Weather API + view
    └── manifest.json   # Plugin metadata
```

### Plugin Interface

```go
// Plugin is the interface every plugin must implement.
type Plugin interface {
    // Metadata
    Manifest() Manifest

    // Lifecycle
    Init(api api.API) error     // called when plugin is enabled
    Start() error               // called when plugin is activated (lazy)
    Stop() error                // called when plugin is deactivated
    Destroy() error             // called when plugin is uninstalled

    // Extension points (all optional — return nil/empty for unused)
    Commands() []*commands.Command
    Shortcuts() map[string][]string    // action → key bindings
    MessageFilter(msg P2PPluginMsg) bool  // return true to consume
}

// Manifest describes a plugin's metadata.
type Manifest struct {
    Name        string    `json:"name"`
    Version     string    `json:"version"`
    Author      string    `json:"author"`
    Description string    `json:"description"`
    Homepage    string    `json:"homepage,omitempty"`
    MinVersion  string    `json:"min_version,omitempty"` // min slackers version
}

// PluginState tracks a plugin's lifecycle state.
type PluginState int
const (
    StateDisabled PluginState = iota  // installed but not active
    StateEnabled                       // active, commands registered
    StateRunning                       // main process executing
)
```

### Plugin Manager

```go
type Manager struct {
    plugins   map[string]*pluginEntry   // name → entry
    api       api.API                    // shared API instance
    configDir string                     // ~/.config/slackers/plugins/
}

type pluginEntry struct {
    plugin  Plugin
    state   PluginState
    manifest Manifest
}

func (m *Manager) Register(p Plugin)
func (m *Manager) Enable(name string) error
func (m *Manager) Disable(name string) error
func (m *Manager) Uninstall(name string) error
func (m *Manager) List() []PluginInfo
func (m *Manager) Get(name string) *PluginInfo
func (m *Manager) InitAll(api api.API) error     // called at startup
func (m *Manager) MergeConfigs() error            // merge plugin configs
func (m *Manager) MergeShortcuts(base shortcuts.ShortcutMap) shortcuts.ShortcutMap
```

### Config Merge Order

At startup, configs load in this order (later overrides earlier):

1. **Embedded defaults** — `defaults.json`, `shortcuts/defaults.json`, `emotes/defaults.json`, built-in themes
2. **Plugin configs** — each enabled plugin's `.config/` folder:
   - `settings.json` → merged into main config
   - `shortcuts.json` → merged into shortcut map
   - `emotes.json` → merged into emote store
   - `themes/` → added to theme list
3. **User configs** — `~/.config/slackers/`:
   - `config.json` → final override
   - `shortcuts.json` → final override
   - `emotes.json` → final override
   - `themes/` → final additions

### Plugin Commands

The plugin system registers these slash commands:

```
/plugins              → Open plugins management overlay
/plugin install <file> → Install from local file (future: URL)
/plugin uninstall <name> → Remove plugin + confirm
/plugin enable <name>  → Activate plugin
/plugin disable <name> → Deactivate plugin
/plugin info <name>    → Show plugin details
```

Argument completion:
- `<name>` → fuzzy-match against installed plugin names
- `<file>` → fuzzy-match against files in `~/Downloads/`

### Plugins Overlay

A new overlay (`overlayPlugins`) shows a table of installed plugins:

```
┌─────────────────────── Plugins ───────────────────────┐
│                                                        │
│  Name          Version  Author      Status             │
│  ────────────  ───────  ──────────  ──────             │
│> Mini Games    1.0.0    slackers    enabled             │
│  Weather       1.0.0    slackers    disabled            │
│  Stock Viewer  0.1.0    community   enabled             │
│                                                        │
│  Enter: details · e: enable/disable · d: uninstall     │
│  Esc: close                                            │
└────────────────────────────────────────────────────────┘
```

---

## Phase 3: UI SDK (`internal/api/ui/`)

### Design Rationale

Plugins need to create custom views without importing `tui` internals. The UI SDK wraps Bubbletea components into a higher-level component system that plugins can use to build overlays, panes, and interactive views.

The SDK is **declarative** — plugins describe what they want, and the SDK translates it into Bubbletea model/view/update patterns.

### File Structure

```
internal/api/ui/
├── component.go        # Base Component interface
├── container.go        # VBox, HBox layout containers
├── text.go             # Label, Paragraph, StyledText
├── list.go             # SelectableList wrapper
├── input.go            # TextInput, TextArea wrappers
├── table.go            # Table with sortable columns
├── canvas.go           # Raw character grid (for games)
├── builder.go          # Fluent builder API for components
└── renderer.go         # Renders component tree to string
```

### Component Interface

```go
// Component is the base interface for all UI elements.
type Component interface {
    ID() string
    Render(width, height int) string
    HandleKey(key string) (Component, bool)  // returns updated component, consumed
    SetSize(width, height int)
}

// Container arranges child components.
type Container interface {
    Component
    Add(child Component)
    Remove(id string)
    Children() []Component
}
```

### Canvas Component (for games)

```go
// Canvas provides a character-addressable grid for games.
type Canvas struct {
    id     string
    width  int
    height int
    cells  [][]Cell
}

type Cell struct {
    Char  rune
    FG    lipgloss.Color
    BG    lipgloss.Color
}

func NewCanvas(id string, w, h int) *Canvas
func (c *Canvas) Set(x, y int, ch rune, fg, bg lipgloss.Color)
func (c *Canvas) Clear()
func (c *Canvas) DrawRect(x, y, w, h int, ch rune, fg, bg lipgloss.Color)
func (c *Canvas) DrawText(x, y int, text string, fg, bg lipgloss.Color)
func (c *Canvas) Render(width, height int) string
```

---

## Phase 4: P2P Plugin Routing

### Design Rationale

Plugins need to send custom messages to friends who also have the plugin installed. Messages use a new P2P message type `MsgTypePlugin` with a structured payload that includes the plugin name, so the receiving side can route it to the correct plugin's message handler.

### Wire Protocol

```go
// New P2P message type
MsgTypePlugin = "plugin"

// P2PMessage gains a PluginData field:
type P2PMessage struct {
    // ... existing fields ...
    PluginName string `json:"plugin_name,omitempty"` // for MsgTypePlugin
    PluginData string `json:"plugin_data,omitempty"` // JSON payload
}
```

### Routing

When a `MsgTypePlugin` message arrives:

1. Model.Update receives `P2PReceivedMsg` with `Text: "__plugin__"`
2. Extracts `PluginName` and `PluginData`
3. Calls `pluginManager.RouteMessage(pluginName, senderID, data)`
4. Manager finds the plugin, calls its `MessageFilter()` method
5. Plugin processes the message, optionally sends a response via `api.Friends().SendPluginMessage()`

If the receiver doesn't have the plugin installed, the message is silently dropped (no error sent back — the sender can't assume the receiver has any given plugin).

---

## Phase 5: Example Plugins

### Games Plugin (`internal/plugins/games/`)

```
internal/plugins/games/
├── plugin.go       # Plugin implementation, /games command
├── menu.go         # Game selection menu overlay
├── snake.go        # Snake game using Canvas
├── tetris.go       # Tetris game using Canvas
├── common.go       # Shared game types (Direction, GameState)
└── manifest.json
```

**Snake game:**
- Canvas-based rendering on a 40x20 grid
- Arrow keys for direction
- Score display in header
- Speed increases with score
- P2P multiplayer (future): send position updates via plugin messages

**Tetris game:**
- Canvas-based rendering on a 10x20 grid + 4x4 preview
- Arrow keys for movement, up for rotate
- Score + level display
- Line clear animation

### Weather Plugin (`internal/plugins/weather/`)

```
internal/plugins/weather/
├── plugin.go       # Plugin implementation, /weather command
├── weather.go      # API client (wttr.in — no API key needed)
├── view.go         # Weather display overlay
├── .config/
│   └── shortcuts.json  # Custom shortcut: Ctrl-W → show weather
└── manifest.json
```

**Weather display:**
- Uses `wttr.in` public API (no key required)
- Shows 3-day forecast with temperature, conditions, wind
- Custom shortcut registered via plugin config merge
- Zipcode/city saved in plugin config

---

## Implementation Order

Each phase is independently committable and testable:

1. **Phase 1** (API) — ~15 tasks, establishes the foundation
2. **Phase 2** (Plugin lifecycle) — ~12 tasks, depends on Phase 1
3. **Phase 3** (UI SDK) — ~10 tasks, depends on Phase 1
4. **Phase 4** (P2P routing) — ~5 tasks, depends on Phase 2
5. **Phase 5** (Example plugins) — ~10 tasks, depends on all phases

**Total: ~52 tasks across 5 phases.**

Each phase should be planned in detail as a separate plan document when ready for implementation. This master plan establishes the architecture and interfaces; detailed task-level plans with test code and exact file changes will be written per-phase.

---

## Architectural Decisions

### Why compiled-in plugins (not dynamic `.so`)?

Go's `plugin` package is:
- Linux/macOS only (no Windows)
- Requires exact Go version match between host and plugin
- Fragile with dependency version mismatches
- No unloading support

Compiled-in plugins are:
- Cross-platform
- Type-safe at compile time
- Easy to test
- Can be distributed as a single binary

Future: if user-installable plugins are needed, an **external process** model (plugin as a separate binary communicating via stdin/stdout JSON-RPC) is more robust than dynamic loading.

### Why a facade API instead of exposing Model directly?

- **Stability**: Model internals change frequently; the API provides a stable contract
- **Safety**: API can enforce invariants (e.g., debounced saves, nil checks)
- **Testing**: Plugins can mock the API interface
- **Documentation**: API methods have clear semantics independent of implementation

### Why not subprocess-per-plugin?

For v1, plugins run in-process because:
- Game rendering needs direct terminal access (Canvas)
- IPC overhead for 60fps game updates would be prohibitive
- Go's goroutine model provides good isolation without process boundaries
- Memory isolation can be added later via subprocess model for non-UI plugins

---

## Config & Settings Integration

### Plugin settings directory

```
~/.config/slackers/plugins/
├── plugins.json            # Plugin enable/disable state
├── games/
│   ├── manifest.json       # Plugin metadata
│   └── .config/
│       └── settings.json   # Plugin-specific settings
└── weather/
    ├── manifest.json
    └── .config/
        ├── settings.json   # { "city": "New York", "units": "F" }
        └── shortcuts.json  # { "show_weather": ["ctrl+w"] }
```

### plugins.json

```json
{
  "games": { "enabled": true, "installed_at": "2026-04-10" },
  "weather": { "enabled": true, "installed_at": "2026-04-10" }
}
```

---

## Next Steps

To begin implementation, start with **Phase 1: Internal API**. This should be planned in detail as a separate document with exact task-level steps, test code, and file changes. The API interfaces defined above are the contract that all subsequent phases depend on.

Run: `superpowers:writing-plans` with scope "Phase 1: Internal API for slackers plugin system" to generate the detailed task-by-task plan.
