# Slackers Plugin API Reference

This document describes the internal API that plugins use to interact with the slackers application. The API is defined in `internal/api/` and provides a stable interface over the app's internal model.

## Table of Contents

- [Overview](#overview)
- [Plugin Interface](#plugin-interface)
- [API Root](#api-root)
- [App](#app)
- [Messages](#messages)
- [Channels](#channels)
- [Friends](#friends)
- [Files](#files)
- [View](#view)
- [Shortcuts](#shortcuts)
- [Commands](#commands)
- [Theme](#theme)
- [Events](#events)
- [UI SDK](#ui-sdk)
- [P2P Plugin Messaging](#p2p-plugin-messaging)
- [Data Types](#data-types)
- [Plugin Lifecycle](#plugin-lifecycle)
- [Configuration](#configuration)
- [Examples](#examples)

---

## Overview

The slackers plugin API is a **facade** that wraps the application's internal model into stable, documented interfaces. Plugins receive an `api.API` instance during initialization and use it to:

- Read and modify app state (config, status bar, warnings)
- Send and receive chat messages (Slack and P2P)
- Manage channels and friends
- Upload/download files
- Create custom overlays and UI components
- Register slash commands and keyboard shortcuts
- Subscribe to app events
- Send P2P messages to other plugins on friends' devices

The API is designed around SOLID principles — each sub-interface has a single responsibility, and plugins depend on interfaces, not concrete types.

## Plugin Interface

Every plugin must implement the `plugins.Plugin` interface:

```go
type Plugin interface {
    Manifest() Manifest                           // metadata
    Init(appAPI api.API) error                    // called on enable
    Start() error                                  // lazy activation
    Stop() error                                   // deactivation
    Destroy() error                                // uninstall cleanup
    Commands() []*commands.Command                 // slash commands
    Shortcuts() map[string][]string                // key bindings
    MessageFilter(senderID, data string) bool      // P2P messages
    ConfigFields() []ConfigField                   // user settings
    SetConfig(key, value string)                   // save a setting
}
```

### Manifest

```go
type Manifest struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Author      string `json:"author"`
    Description string `json:"description"`
    Homepage    string `json:"homepage,omitempty"`
    MinVersion  string `json:"min_version,omitempty"`
}
```

### ConfigField

```go
type ConfigField struct {
    Key         string // internal key (e.g. "city")
    Label       string // display label (e.g. "City / Zipcode")
    Value       string // current value
    Description string // help text shown when selected
}
```

### Plugin States

| State | Value | Description |
|-------|-------|-------------|
| `StateDisabled` | 0 | Installed but not active |
| `StateEnabled` | 1 | Active, commands registered |
| `StateRunning` | 2 | Main process executing |

---

## API Root

```go
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
```

Access sub-interfaces via the root:

```go
func (p *MyPlugin) Init(appAPI api.API) error {
    version := appAPI.App().Version()
    isDark := appAPI.Theme().IsDark()
    return nil
}
```

---

## App

App lifecycle, configuration, and status bar.

```go
type App interface {
    Version() string                  // app version string
    Config() map[string]any           // read-only config snapshot
    SetConfig(key string, val any)    // debounced save to config.json
    SetStatusBar(text string)         // set bottom status message
    SetWarning(text string)           // set warning message
    ClearWarning()                    // clear warning
    IsSlackConnected() bool           // true if Slack tokens are configured
    IsP2PEnabled() bool               // true if P2P node is running
    SlackerID() string                // local user's slacker ID
    MyName() string                   // local user's display name
}
```

### Example: Show a status message

```go
appAPI.App().SetStatusBar("Weather loaded for New York")
```

---

## Messages

Send, edit, delete, and fetch chat messages.

```go
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
```

### Example: Send a message

```go
ch := appAPI.Messages().CurrentChannel()
if ch != nil {
    appAPI.Messages().Send(ch.ID, "Hello from my plugin!")
}
```

---

## Channels

List, manage, and navigate channels.

```go
type Channels interface {
    List() []ChannelInfo
    Get(channelID string) *ChannelInfo
    Hide(channelID string)
    Unhide(channelID string)
    Rename(channelID, newName string)
    MarkUnread(channelID string)
    ClearUnread(channelID string)
    SelectByID(channelID string)
}
```

---

## Friends

Friend management and P2P operations.

```go
type Friends interface {
    List() []FriendInfo
    Get(userID string) *FriendInfo
    IsOnline(userID string) bool
    SendMessage(userID, text string) error
    SendPluginMessage(userID, pluginName, data string) error
}
```

### Example: Send a plugin message to a friend

```go
// Send game state to a friend who has the same plugin
data := `{"type":"move","x":5,"y":3}`
appAPI.Friends().SendPluginMessage(friendUID, "games", data)
```

---

## Files

File upload and download operations.

```go
type Files interface {
    Upload(channelID, filePath string) error
    Download(url, destPath string) error
    DownloadPath() string     // configured download directory
    SharedFolder() string     // configured shared folder path
}
```

---

## View

Overlay and window management.

```go
type View interface {
    ShowOverlay(id string, model OverlayModel)
    CloseOverlay()
    CurrentOverlay() string
    SetFocus(pane FocusPane)
    ScreenSize() (width, height int)
}
```

### FocusPane

| Constant | Description |
|----------|-------------|
| `FocusSidebar` | Channel sidebar |
| `FocusMessages` | Message/chat pane |
| `FocusInput` | Text input bar |

### OverlayModel

Custom overlays must implement:

```go
type OverlayModel interface {
    Update(msg any) (OverlayModel, any)
    View() string
    SetSize(w, h int)
}
```

---

## Shortcuts

Register and query keyboard shortcuts.

```go
type Shortcuts interface {
    Register(action string, keys []string)
    KeysForAction(action string) []string
    AllActions() map[string][]string
}
```

### Example: Register a custom shortcut

```go
appAPI.Shortcuts().Register("show_weather", []string{"ctrl+w"})
```

---

## Commands

Register slash commands.

```go
type Commands interface {
    Register(name, description string, run func(args []string) error)
    Run(input string) error
    List() []CommandInfo
}
```

### CommandInfo

```go
type CommandInfo struct {
    Name        string
    Description string
    Usage       string
}
```

### Advanced command registration

For full control (aliases, arg specs, result types), use the `commands.Command` struct directly via the `Plugin.Commands()` method:

```go
func (p *MyPlugin) Commands() []*commands.Command {
    return []*commands.Command{
        {
            Name:        "mycommand",
            Aliases:     []string{"mc"},
            Kind:        commands.KindCommand,
            Description: "Does something cool",
            Usage:       "/mycommand [arg]",
            Args: []commands.ArgSpec{
                {Name: "arg", Kind: commands.ArgString, Optional: true},
            },
            Run: func(ctx *commands.Context) commands.Result {
                return commands.Result{
                    Status:    commands.StatusOK,
                    StatusBar: "Done!",
                }
            },
        },
    }
}
```

---

## Theme

Read current theme colors.

```go
type Theme interface {
    IsDark() bool
    Color(name string) string
    AllColors() map[string]string
}
```

### Available color names

`primary`, `secondary`, `accent`, `error`, `muted`, `highlight`, `message_text`, `background`, `timestamp`, `selection`, `border_default`, `border_active`, `emote`, and more. Call `AllColors()` for the full map.

---

## Events

Pub/sub system for app lifecycle events.

```go
type Events interface {
    Subscribe(eventType string, handler EventHandler) UnsubscribeFunc
    Emit(event Event)
}
```

### Event

```go
type Event struct {
    Type string
    Data any
}

type EventHandler func(Event)
type UnsubscribeFunc func()
```

### Example: Subscribe to events

```go
unsub := appAPI.Events().Subscribe("channel_changed", func(e api.Event) {
    channelID := e.Data.(string)
    // React to channel switch
})
// Later: unsub() to remove the handler
```

---

## UI SDK

The UI SDK (`internal/api/ui/`) provides components for building custom views.

### Component Interface

```go
type Component interface {
    ID() string
    Render(width, height int) string
    HandleKey(key string) bool
    SetSize(width, height int)
}
```

### Available Components

| Component | Package | Description |
|-----------|---------|-------------|
| `VBox` | `ui.NewVBox(id)` | Vertical layout container |
| `HBox` | `ui.NewHBox(id)` | Horizontal layout container |
| `Label` | `ui.NewLabel(id, text)` | Single-line text |
| `Paragraph` | `ui.NewParagraph(id, text)` | Word-wrapping text block |
| `List` | `ui.NewList(id)` | Scrollable selectable list |
| `Canvas` | `ui.NewCanvas(id, w, h)` | Character-addressable grid |

### Canvas

The Canvas is designed for games and graphical plugins. Each cell has its own foreground and background color.

```go
canvas := ui.NewCanvas("game", 40, 20)
canvas.Set(x, y, '█', lipgloss.Color("#ff0000"), lipgloss.Color("#000000"))
canvas.DrawText(0, 0, "Score: 100", fg, bg)
canvas.DrawRect(0, 0, 40, 20, '─', fg, bg)
canvas.Clear()
output := canvas.Render(40, 20)
```

### List

```go
list := ui.NewList("menu")
list.SetItems([]ui.ListItem{
    {Label: "Option 1", Value: "opt1"},
    {Label: "Option 2", Value: "opt2"},
})
list.OnSelect(func(item ui.ListItem) {
    // Handle selection
})
```

---

## P2P Plugin Messaging

Plugins can send custom messages to friends who have the same plugin installed.

### Sending

```go
appAPI.Friends().SendPluginMessage(friendUID, "myplugin", jsonPayload)
```

### Receiving

Implement `MessageFilter` on your plugin:

```go
func (p *MyPlugin) MessageFilter(senderID, data string) bool {
    var msg MyMessage
    if err := json.Unmarshal([]byte(data), &msg); err != nil {
        return false
    }
    // Process the message
    return true // consumed
}
```

### Wire Protocol

Plugin messages use `MsgTypePlugin` on the P2P wire with `PluginName` and `PluginData` fields. Messages to friends without the plugin installed are silently dropped.

---

## Data Types

### ChannelInfo

```go
type ChannelInfo struct {
    ID        string
    Name      string
    IsDM      bool
    IsPrivate bool
    IsGroup   bool
    IsFriend  bool
    UserID    string // for DMs: the other user's ID
}
```

### MessageInfo

```go
type MessageInfo struct {
    ID        string
    ChannelID string
    UserID    string
    UserName  string
    Text      string
    Timestamp time.Time
    IsEmote   bool
    ReplyTo   string // parent message ID if reply
}
```

### FriendInfo

```go
type FriendInfo struct {
    UserID          string
    SlackerID       string
    Name            string
    Email           string
    Online          bool
    AwayStatus      string // "online", "away", "back", "offline"
    AwayMessage     string
    HasSharedFolder bool
    LastOnline      int64
    Multiaddr       string
    ConnectionType  string // "p2p" or "e2e"
}
```

---

## Plugin Lifecycle

```
Register → Init → [Start → Stop] → Destroy
              ↑                 |
              └─── re-enable ───┘
```

1. **Register**: Plugin added to the manager (happens at app build time)
2. **Init**: Called at startup for enabled plugins. Receives `api.API`
3. **Start**: Lazy activation — heavy work (network, UI) goes here
4. **Stop**: Deactivation — release resources
5. **Destroy**: Uninstall — clean up persistent state

### Plugin Manager

```go
manager.Register(plugin)           // add to registry
manager.Enable("name")            // activate
manager.Disable("name")           // deactivate
manager.Uninstall("name")         // remove entirely
manager.List() []PluginInfo        // all plugins
manager.Get("name") *PluginInfo    // specific plugin
manager.GetPlugin("name") Plugin   // live instance
```

---

## Configuration

### Config Merge Order

Settings load in this order (later overrides earlier):

1. **Embedded defaults** — built-in defaults, themes, shortcuts
2. **Plugin configs** — each plugin's `.config/` directory
3. **User configs** — `~/.config/slackers/` (always wins)

### Plugin Config Directory

```
~/.config/slackers/plugins/
├── plugins.json              # enable/disable state
├── games/
│   └── manifest.json
└── weather/
    ├── manifest.json
    └── .config/
        ├── settings.json     # plugin-specific settings
        └── shortcuts.json    # plugin keyboard shortcuts
```

### Plugin Settings

Plugins expose settings via `ConfigFields()` and handle saves via `SetConfig()`. The Plugin Manager UI (accessible via `/plugins`) shows these in a config screen.

---

## Examples

### Minimal Plugin

```go
package myplugin

import (
    "github.com/rw3iss/slackers/internal/api"
    "github.com/rw3iss/slackers/internal/commands"
    "github.com/rw3iss/slackers/internal/plugins"
)

type MyPlugin struct {
    appAPI api.API
}

func New() *MyPlugin { return &MyPlugin{} }

func (p *MyPlugin) Manifest() plugins.Manifest {
    return plugins.Manifest{
        Name:        "myplugin",
        Version:     "1.0.0",
        Author:      "you",
        Description: "Does something useful",
    }
}

func (p *MyPlugin) Init(appAPI api.API) error {
    p.appAPI = appAPI
    return nil
}

func (p *MyPlugin) Start() error   { return nil }
func (p *MyPlugin) Stop() error    { return nil }
func (p *MyPlugin) Destroy() error { return nil }

func (p *MyPlugin) Commands() []*commands.Command {
    return []*commands.Command{
        {
            Name:        "hello",
            Description: "Say hello",
            Run: func(ctx *commands.Context) commands.Result {
                return commands.Result{
                    Status:    commands.StatusOK,
                    StatusBar: "Hello from my plugin!",
                }
            },
        },
    }
}

func (p *MyPlugin) Shortcuts() map[string][]string        { return nil }
func (p *MyPlugin) MessageFilter(senderID, data string) bool { return false }
func (p *MyPlugin) ConfigFields() []plugins.ConfigField    { return nil }
func (p *MyPlugin) SetConfig(key, value string)            {}
```

### Registering the Plugin

In `internal/tui/model.go`, add to `NewModel`:

```go
import myPlugin "github.com/rw3iss/slackers/internal/plugins/myplugin"

// In NewModel, after pluginManager is created:
m.pluginManager.Register(myPlugin.New())
```
