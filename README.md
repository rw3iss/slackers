# Slackers

A lightweight, terminal-based Slack client. Read and send messages, switch channels, and get real-time notifications -- all from your terminal.

## Features

- **Three-panel TUI** -- channel sidebar, message viewport, and input bar with focus cycling
- **Real-time messages** via Slack Socket Mode -- incoming messages appear instantly
- **New message polling** -- configurable background polling detects new messages across all channels
- **Send as yourself** -- uses your user token so messages come from you, not a bot
- **Channel search** (`Ctrl-K`) -- filter and jump to any channel instantly
- **Message search** (`Ctrl-F`) -- search messages in the current channel or across all channels, with context view
- **Search context view** -- select a search result to see surrounding messages with the match highlighted; load more history with PgUp
- **Hide channels** (`Ctrl-X`) -- declutter the sidebar; view and unhide with `Ctrl-G`; toggle inline with `Ctrl-O`
- **Channel aliases** (`Ctrl-A`) -- rename any channel or group chat with a custom display name
- **Next unread** (`Ctrl-N`) -- jump directly to the next channel with new messages
- **Unread indicators** -- channels with new messages are marked with `*` in the sidebar
- **Notifications** -- terminal bell, OSC 9 desktop notifications, and window urgency hints (configurable)
- **Channel sorting** -- sort by type, name, or most recent message; ascending or descending
- **Date headers** -- sticky date bar in the header and date separators between messages from different days
- **Sidebar scrolling** -- selection always stays in view; PgUp/PgDn/Home/End support
- **Configurable sidebar width** -- adjust via the settings menu (`Ctrl-S`)
- **Slack mrkdwn rendering** -- bold, italic, user mentions, links, and code blocks converted to plain text
- **Interactive settings** (`Ctrl-S`) -- cycle through options with Enter/Tab, changes persist immediately
- **Built-in help** (`Ctrl-H`) -- full shortcut reference overlay
- **OAuth browser login** -- `slackers login` opens your browser to authorize; teammates can onboard in seconds
- **Cross-platform** -- single static binary for Linux, macOS, and Windows
- **Persistent config** -- tokens, hidden channels, aliases, sort preferences, and all settings saved to `~/.config/slackers/`

```
+---------------------+------------------------------------------+
|                     |  #general                    Today        |
|  Channels           |------------------------------------------|
|  ---------------    |  -- Mon, Apr 3, 2026 --                   |
|  # general          |                                          |
|  # engineering      |  alice  Apr 3 10:32                       |
|  # random           |    Hey everyone, standup in 5 min         |
|                     |                                          |
|  Direct Messages    |  bob  Apr 3 10:33                         |
|  ---------------    |    Thanks for the heads up!               |
|  @ alice            |                                          |
|  @ bob              |                                          |
|                     |------------------------------------------|
|                     |  > Type a message...                     |
+---------------------+------------------------------------------+
  slackers | Connected | Ctrl-H: help | Ctrl-S: settings
```

## Installation

### Download a pre-built binary

Grab the latest release for your platform from the [Releases](https://github.com/rw3iss/slackers/releases) page:

- **Linux (x86_64)**: `slackers-linux-amd64`
- **Linux (ARM64)**: `slackers-linux-arm64`
- **macOS (Intel)**: `slackers-darwin-amd64`
- **macOS (Apple Silicon)**: `slackers-darwin-arm64`
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
    "redirect_urls": [
      "http://127.0.0.1:9876/callback",
      "http://127.0.0.1:9877/callback",
      "http://127.0.0.1:9878/callback"
    ],
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

### 2. Generate an App-Level Token

This is needed for real-time messaging (Socket Mode):

1. In your Slack app settings, go to **Basic Information** > **App-Level Tokens**
2. Click **Generate Token and Scopes**, name it anything, and add the `connections:write` scope
3. Copy the token (`xapp-...`)

### 3. Configure Slackers

There are two ways to set up. Both are offered when you run `slackers setup`.

#### Option A: OAuth browser flow (recommended)

This opens your browser to authorize with Slack and automatically obtains your bot and user tokens:

```bash
slackers setup    # choose option 2 when prompted
# -- or directly --
slackers login
```

You'll need the **Client ID** and **Client Secret** from your Slack app's Basic Information page (only on first run -- they're saved to config for future use). The app admin can share these with teammates so they can run `slackers login` to get their own tokens.

#### Option B: Manual token entry

```bash
slackers setup    # choose option 1 when prompted
```

You'll need to paste three tokens:

| Token | Where to find it | Looks like |
|-------|-----------------|------------|
| **Bot Token** | OAuth & Permissions > Bot User OAuth Token | `xoxb-...` |
| **App Token** | Basic Information > App-Level Tokens | `xapp-...` |
| **User Token** | OAuth & Permissions > User OAuth Token | `xoxp-...` |

The **User Token** is what lets you send messages as yourself instead of as the bot.

### For teammates

If someone on your team already set up the Slack app, they can share:
- The **Client ID** and **Client Secret** (from the app's Basic Information page)
- The **App-Level Token** (`xapp-...`)

Then each teammate just runs:

```bash
slackers login --client-id YOUR_ID --client-secret YOUR_SECRET
```

This opens their browser, they authorize with their own Slack account, and their personal tokens are saved automatically. They only need to paste the App-Level Token once.

### Config file

All tokens and settings are saved to `~/.config/slackers/config.json`:

```json
{
  "bot_token": "xoxb-...",
  "app_token": "xapp-...",
  "user_token": "xoxp-...",
  "sidebar_width": 30,
  "timestamp_format": "15:04",
  "notifications": false,
  "poll_interval": 10,
  "channel_sort_by": "type",
  "channel_sort_asc": true
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
| `Esc` | Toggle focus between sidebar and input |
| `Up` / `Down` | Navigate channels or scroll messages |
| `Enter` | Select channel (sidebar) or send message (input) |
| `i` or `/` | Focus the message input |
| `Ctrl-K` | Search and jump to a channel |
| `Ctrl-F` | Search messages (Tab toggles current/all scope) |
| `Ctrl-N` | Jump to next unread channel |
| `Ctrl-X` | Hide selected channel from sidebar |
| `Ctrl-G` | View and unhide hidden channels |
| `Ctrl-O` | Toggle hidden channels visible in sidebar |
| `Ctrl-A` | Rename/alias selected channel |
| `Ctrl-R` | Refresh channel list |
| `Ctrl-H` | Toggle help page |
| `Ctrl-S` | Open settings |
| `PgUp` / `PgDn` | Scroll messages by page |
| `Ctrl-C` or `Ctrl-Q` | Quit |

### Settings (Ctrl-S)

| Setting | Options | Description |
|---------|---------|-------------|
| Sidebar Width | 10-80 | Channel sidebar width in characters |
| Timestamp Format | Go time format | e.g. `15:04`, `3:04 PM` |
| Notifications | on / off | Terminal bell + desktop notifications |
| Poll Interval | 1-300 | Seconds between new-message checks |
| Sort By | type / name / recent | Channel list sorting mode |
| Sort Direction | asc / desc | Channel list sorting direction |

Settings with fixed options cycle with Enter/Tab. Changes save immediately and persist across restarts.

### CLI commands

```
slackers              Launch the TUI
slackers setup        Interactive setup (manual or OAuth)
slackers login        Authorize via browser (OAuth flow)
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
    auth/                  OAuth2 browser-based authentication flow
    config/                Configuration loading, saving, validation
    format/                Slack mrkdwn to plain text conversion
    slack/
      client.go            SlackService interface + Web API implementation
      socket.go            SocketService interface + Socket Mode real-time events
    tui/
      model.go             Root Bubbletea model (Init/Update/View)
      channels.go          Channel list sidebar component
      messages.go          Message viewport with context mode
      input.go             Text input bar component
      search.go            Channel search overlay
      msgsearch.go         Message search overlay
      hidden.go            Hidden channels management overlay
      rename.go            Channel rename/alias overlay
      settings.go          Interactive settings editor overlay
      help.go              Help page overlay
      notify.go            Terminal notification helpers
      styles.go            Lipgloss style definitions
      keymap.go            Key binding definitions
    types/                 Shared domain types (Channel, Message, User, SearchResult)
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
#   build/slackers-linux-arm64
#   build/slackers-darwin-amd64
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
