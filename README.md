# Slackers

A lightweight, terminal-based Slack client. Read and send messages, switch channels, and get real-time notifications -- all from your terminal.

```
+---------------------+------------------------------------------+
|                     |  #general                                |
|  Channels           |------------------------------------------|
|  ---------------    |  alice [10:32]                            |
|  # general          |    Hey everyone, standup in 5 min         |
|  # engineering      |                                          |
|  # random           |  bob [10:33]                              |
|                     |    Thanks for the heads up!               |
|  Direct Messages    |                                          |
|  ---------------    |                                          |
|  @ alice            |                                          |
|  @ bob              |                                          |
|                     |------------------------------------------|
|                     |  > Type a message...                     |
+---------------------+------------------------------------------+
```

## Installation

### Download a pre-built binary

Grab the latest release for your platform from the [Releases](https://github.com/rw3iss/slackers/releases) page:

- **Linux**: `slackers-linux-amd64`
- **macOS**: `slackers-darwin-arm64`
- **Windows**: `slackers-windows-amd64.exe`

Move it somewhere on your PATH:

```bash
chmod +x slackers-linux-amd64
mv slackers-linux-amd64 ~/.local/bin/slackers
```

### Build from source

Requires Go 1.22 or later.

```bash
git clone https://github.com/rw3iss/slackers.git
cd slackers
make install
```

This builds the binary and copies it to `~/.local/bin/slackers`.

## Setup

Slackers connects to Slack through a Slack App that you create in your workspace. This takes about 2 minutes.

### 1. Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App** > **From an app manifest**
2. Select your workspace
3. Paste the contents of the [app manifest](configs/slack-app-manifest.json) included in this repo (or copy it from below)
4. Click **Create**

<details>
<summary>App manifest (click to expand)</summary>

```json
{
  "display_information": {
    "name": "Slackers TUI",
    "description": "Terminal-based Slack client",
    "background_color": "#1a1a2e"
  },
  "features": {
    "bot_user": {
      "display_name": "Slackers",
      "always_online": true
    }
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "channels:read", "channels:history", "channels:join", "channels:manage",
        "groups:read", "groups:history", "groups:write",
        "im:read", "im:history", "im:write",
        "mpim:read", "mpim:history", "mpim:write",
        "chat:write", "chat:write.customize", "chat:write.public",
        "reactions:read", "reactions:write", "pins:read", "pins:write",
        "files:read", "files:write",
        "users:read", "users:read.email", "users.profile:read", "users:write",
        "bookmarks:read", "bookmarks:write",
        "usergroups:read", "usergroups:write",
        "team:read", "emoji:read", "commands"
      ],
      "user": [
        "channels:read", "channels:history", "channels:write",
        "groups:read", "groups:history",
        "im:read", "im:history", "mpim:read", "mpim:history",
        "chat:write",
        "reactions:read", "reactions:write",
        "files:read", "files:write",
        "search:read", "stars:read", "stars:write",
        "users:read", "users.profile:read", "users.profile:write",
        "dnd:read", "dnd:write",
        "reminders:read", "reminders:write",
        "identify", "emoji:read", "team:read", "pins:read"
      ]
    }
  },
  "settings": {
    "event_subscriptions": {
      "bot_events": [
        "message.channels", "message.groups", "message.im", "message.mpim",
        "reaction_added", "reaction_removed",
        "channel_created", "channel_archive", "channel_unarchive",
        "member_joined_channel", "member_left_channel",
        "user_status_changed", "team_join",
        "pin_added", "pin_removed", "file_shared", "emoji_changed"
      ]
    },
    "socket_mode_enabled": true,
    "token_rotation_enabled": false
  }
}
```

</details>

### 2. Collect your tokens

After creating the app, you need three tokens:

| Token | Where to find it | Looks like |
|-------|-----------------|------------|
| **Bot Token** | OAuth & Permissions > Bot User OAuth Token | `xoxb-...` |
| **App Token** | Basic Information > App-Level Tokens (generate one with `connections:write` scope) | `xapp-...` |
| **User Token** | OAuth & Permissions > User OAuth Token | `xoxp-...` |

The **User Token** is what lets you send messages as yourself instead of as the bot. If you skip it, messages will appear as coming from "Slackers" rather than your name.

### 3. Configure Slackers

Run the setup wizard:

```bash
slackers setup
```

Paste each token when prompted. They are saved to `~/.config/slackers/config.json` and reused on every launch.

You can also edit the config file directly:

```json
{
  "bot_token": "xoxb-...",
  "app_token": "xapp-...",
  "user_token": "xoxp-...",
  "sidebar_width": 25,
  "timestamp_format": "15:04"
}
```

### 4. Launch

```bash
slackers
```

## Usage

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Tab` / `Shift-Tab` | Cycle focus between panels |
| `Up` / `Down` | Navigate channels or scroll messages |
| `Enter` | Select channel (sidebar) or send message (input) |
| `i` or `/` | Focus the message input |
| `Esc` | Cancel input, return to sidebar |
| `Ctrl-r` | Refresh channel list |
| `PgUp` / `PgDn` | Scroll messages by page |
| `Ctrl-c` or `Ctrl-q` | Quit |

### CLI commands

```
slackers              Launch the TUI
slackers setup        Interactive token setup
slackers config       Show current configuration
slackers version      Print version
slackers --help       Show all options
```

### CLI flags

Any config value can be overridden per-session with a flag:

```bash
slackers --bot-token xoxb-... --sidebar-width 30
slackers --config /path/to/alternate-config.json
```

## Uninstalling

```bash
# Remove the binary and desktop entry
slackers scripts uninstall

# Or manually
rm ~/.local/bin/slackers
rm -rf ~/.config/slackers
```

---

## Development

Everything below is for contributors or anyone who wants to customize the client.

### Prerequisites

- Go 1.22+
- A Slack App configured as described above

### Project structure

```
slackers/
  cmd/slackers/main.go    CLI entry point (Cobra commands, flag handling)
  internal/
    config/                Configuration loading, saving, validation
    format/                Slack mrkdwn to plain text conversion
    slack/
      client.go            SlackService interface + Web API implementation
      socket.go            SocketService interface + Socket Mode real-time events
    tui/
      model.go             Root Bubbletea model (Init/Update/View)
      channels.go          Channel list sidebar component
      messages.go          Message viewport component
      input.go             Text input bar component
      styles.go            Lipgloss style definitions
      keymap.go            Key binding definitions
    types/                 Shared domain types (Channel, Message, User)
  scripts/                 Install/uninstall/cleanup shell scripts
  configs/                 Default config template, Slack app manifest
```

### Architecture

The codebase follows SOLID principles:

- **Single Responsibility**: Each package owns one concern. `config` handles persistence, `slack` wraps the API, `tui` handles rendering.
- **Dependency Inversion**: The TUI depends on `SlackService` and `SocketService` interfaces, not concrete implementations. Swap in mocks for testing.
- **Open/Closed**: New Slack event types can be handled by extending the socket client without modifying the TUI.
- **Interface Segregation**: Small, focused interfaces (`SlackService` for API calls, `SocketService` for real-time events).

The TUI uses Bubbletea's Elm architecture (Model/Update/View). All state mutations happen in `Update()`. The `View()` function is pure. Async work (API calls, socket events) runs via `tea.Cmd` functions.

### Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Build to `build/slackers` |
| `make run` | Build and launch |
| `make install` | Build and install to `~/.local/bin` |
| `make uninstall` | Remove installation |
| `make cleanup` | Remove local data and logs |
| `make setup` | Build and run setup wizard |
| `make test` | Run all tests |
| `make lint` | Run `go vet` |
| `make clean` | Remove build artifacts |
| `make build-all` | Cross-compile for Linux, macOS, Windows |

### Cross-compiling

```bash
make build-all
# Produces:
#   build/slackers-linux-amd64
#   build/slackers-darwin-arm64
#   build/slackers-windows-amd64.exe
```

### Running tests

```bash
make test
```

### Adding new Slack event types

1. Add the event handler in `internal/slack/socket.go` inside `handleEventsAPI()`
2. Define a new `tea.Msg` type in `internal/tui/model.go`
3. Handle the message in `Model.Update()`

### Adding new UI components

1. Create a new file in `internal/tui/` with its own model struct
2. Add `Update()` and `View()` methods
3. Compose it into the root `Model` in `model.go`

## License

MIT
