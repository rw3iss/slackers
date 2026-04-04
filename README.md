# Slackers

A lightweight, terminal-based Slack client.

```
+---------------------+------------------------------------------+
|  # Channels         |  #general                    Today        |
|  > #general         |  -- Apr 3, 2026 --                        |
|    #engineering     |                                           |
|    #random          |  alice  Apr 3 10:32                       |
|                     |    Hey everyone, standup in 5 min          |
|  @ Direct Messages  |                                           |
|    alice            |  bob  Apr 3 10:33                          |
|    bob              |    Thanks for the heads up!                |
|                     |                                           |
+---------------------+------------------------------------------+
| > Type a message...                                             |
+---------------------+------------------------------------------+
  slackers | Connected | Ctrl-H: help | Ctrl-S: settings
```

## Features

- Read and send messages as yourself across channels, DMs, and group chats
- Real-time message polling with configurable interval
- Channel search (`Ctrl-K`) and message search (`Ctrl-F`) with context view
- Hide, rename, and sort channels
- Jump to next unread (`Ctrl-N`)
- Terminal notifications (bell, desktop, urgency hints)
- File uploads (`Ctrl-U`) with built-in file browser and `[FILE:<path>]` syntax
- Input history — Up/Down in the input bar recalls sent messages
- Interactive settings (`Ctrl-S`) and built-in help (`Ctrl-H`)
- OAuth browser login for quick onboarding
- Single binary, cross-platform (Linux, macOS, Windows)
- All settings persisted locally at `~/.config/slackers/`

## Install

Download a binary from the [Releases](https://github.com/rw3iss/slackers/releases) page, or build from source:

```bash
# Download (pick your platform)
curl -L https://github.com/rw3iss/slackers/releases/latest/download/slackers-linux-amd64 -o slackers
chmod +x slackers && mv slackers ~/.local/bin/

# Or build from source (requires Go 1.22+)
git clone https://github.com/rw3iss/slackers.git && cd slackers && make install
```

## Setup

Get your **Client ID**, **Client Secret**, and **App Token** from your team admin, then run:

```bash
slackers login --client-id CLIENT_ID --client-secret CLIENT_SECRET --app-token xapp-...
```

Your browser opens, you authorize with Slack, and you're ready. Run `slackers` to start.

Credentials are saved locally so you only do this once.

<details>
<summary>Don't have credentials yet? Setting up for a new team</summary>

A workspace admin needs to create the Slack app once:

1. Go to [api.slack.com/apps](https://api.slack.com/apps) > **Create New App** > **From an app manifest**
2. Paste the [app manifest](configs/slack-app-manifest.json) ([view below](#app-manifest)) and click **Create**
3. Under **OAuth & Permissions** > **Redirect URLs**, add `http://localhost:9876/callback`
4. Under **Basic Information** > **App-Level Tokens**, create a token with `connections:write` scope

Then share these with your team:
- **Client ID** and **Client Secret** (from Basic Information)
- **App-Level Token** (`xapp-...`)

The admin can also run `slackers login` themselves to set up their own client.

To make onboarding even easier, host a JSON file with the credentials:

```json
{"client_id": "...", "client_secret": "...", "app_token": "xapp-..."}
```

Teammates then run: `slackers join https://your-url.com/team.json`

</details>

## Usage

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Tab` / `Shift-Tab` | Cycle focus between panels |
| `Esc` | Toggle between sidebar and input |
| `Enter` | Select channel or send message |
| `i` or `/` | Focus input |
| `Ctrl-K` | Search channels |
| `Ctrl-F` | Search messages (Tab toggles scope) |
| `Ctrl-N` | Next unread channel |
| `Ctrl-X` | Hide channel |
| `Ctrl-G` | Unhide channels |
| `Ctrl-O` | Toggle hidden visible |
| `Ctrl-A` | Rename/alias channel |
| `Ctrl-U` | Attach file to send |
| `Up` / `Down` (input) | Browse sent message history |
| `Ctrl-R` | Refresh channels |
| `Ctrl-H` | Help |
| `Ctrl-S` | Settings |
| `Ctrl-Q` | Quit |

### Settings (Ctrl-S)

| Setting | Options | Description |
|---------|---------|-------------|
| Sidebar Width | 10-80 | Sidebar width in characters |
| Timestamp Format | Go format | e.g. `15:04`, `3:04 PM` |
| Notifications | on / off | Terminal bell + desktop notifications |
| Poll Interval | 1-300 | Seconds between message checks |
| Sort By | type / name / recent | Channel sorting |
| Sort Direction | asc / desc | Sort order |
| Input History | 1-200 | Max sent messages to remember |
| Download Path | folder | File download/upload location |

Fields with fixed options cycle with Enter/Tab.

### CLI

```
slackers              Launch the TUI
slackers setup        Interactive setup
slackers login        OAuth browser login
slackers join <url>   One-command team onboarding
slackers config       Show current config
slackers version      Print version
```

### Uninstall

```bash
rm ~/.local/bin/slackers
rm -rf ~/.config/slackers
```

---

## Development

### Build

```bash
make build        # Build to build/slackers
make run          # Build and run
make install      # Install to ~/.local/bin
make build-all    # Cross-compile all platforms
make test         # Run tests
make lint         # Run go vet
make clean        # Remove build artifacts
```

### Project structure

```
cmd/slackers/       CLI entry point, Cobra commands
internal/
  auth/             OAuth2 browser flow
  config/           Config loading, saving, validation
  format/           Slack mrkdwn to plain text
  slack/            SlackService + SocketService interfaces and implementations
  tui/              Bubbletea model, UI components, overlays
  types/            Shared domain types
scripts/            Install/uninstall/cleanup scripts
configs/            Default config, Slack app manifest
```

### Architecture

Elm architecture via Bubbletea (Model/Update/View). SOLID principles: interfaces for Slack API (`SlackService`, `SocketService`), single-responsibility packages, dependency inversion for testability.

### App manifest

<details>
<summary>configs/slack-app-manifest.json</summary>

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
      "http://localhost:9876/callback",
      "http://localhost:9877/callback",
      "http://localhost:9878/callback"
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
    "interactivity": {
      "is_enabled": false
    },
    "org_deploy_enabled": false,
    "socket_mode_enabled": true,
    "token_rotation_enabled": false
  }
}
```

</details>

## License

MIT
