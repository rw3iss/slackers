```
 ██████╗ ██╗      ██████╗  ██████╗██╗  ██╗███████╗██████╗  ██████╗
██╔════╝ ██║     ██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗██╔════╝
╚█████╗  ██║     ████████║██║     █████╔╝ █████╗  ██████╔╝╚█████╗
 ╚═══██╗ ██║     ██╔═══██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗ ╚═══██╗
██████╔╝ ███████╗██║   ██║╚██████╗██║  ██╗███████╗██║  ██║██████╔╝
╚═════╝  ╚══════╝╚═╝   ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝
```

A lightweight, terminal-based Slack client.

<a href=".github/screenshot.png"><img src=".github/screenshot.png" alt="Slackers channels" width="220"></a> <a href=".github/screenshot-help.png"><img src=".github/screenshot-help.png" alt="Slackers help" width="220"></a> <a href=".github/screenshot-settings.png"><img src=".github/screenshot-settings.png" alt="Slackers settings" width="220"></a> <a href=".github/screenshot-search.png"><img src=".github/screenshot-search.png" alt="Slackers edit mode" width="220"></a> <a href=".github/screenshot-files.png"><img src=".github/screenshot-files.png" alt="Slackers edit mode" width="220"></a> <a href=".github/screenshot-edit.png"><img src=".github/screenshot-edit.png" alt="Slackers edit mode" width="180"></a>

## Features

- **Smart unread detection** -- batched polling with priority channels, rate-limit aware
- **Message search** -- search current or all channels, jump to results with context view
- **File drop and browser** -- drop files, upload, download, browse/search all files across channels
- **Mouse support** -- click channels, scroll panels, drag sidebar, click files to download
- **Multi-line editor** -- expandable textarea with normal/edit mode toggle
- **Customizable shortcuts** -- rebind any key in-app, changes take effect immediately
- **Channel management** -- hide, alias, collapse groups, sort by type/name/recent
- **Auto-update** -- new versions downloaded and installed on startup
- **One-command onboarding** -- `slackers join <url>` for team setup, OAuth browser login
- **Single binary** -- cross-platform (Linux, macOS, Windows), no dependencies

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

<details>
<summary>Keyboard shortcuts (customizable)</summary>

All shortcuts are fully customizable. Open **Settings** (`Ctrl-S`) > **Keyboard Shortcuts** to rebind any key in-app (changes take effect immediately). You can also edit `~/.config/slackers/shortcuts.json` directly — only overridden keys need to be listed; defaults fill in the rest. See [internal/shortcuts/defaults.json](internal/shortcuts/defaults.json) for the full default mapping.

| Key | Action |
|-----|--------|
| `Tab` / `Shift-Tab` | Cycle focus between panels |
| `Esc` | Toggle between sidebar and input |
| `Enter` | Select channel or send message |
| `i` or `/` | Focus input |
| `Ctrl-K` | Search channels |
| `Ctrl-F` | Search messages (Tab toggles scope) |
| `Ctrl-N` | Next unread channel |
| `Ctrl-L` | Browse all files |
| `Ctrl-X` | Hide channel |
| `Ctrl-G` | Unhide channels |
| `Ctrl-O` | Toggle hidden visible |
| `Ctrl-A` | Rename/alias channel |
| `Ctrl-U` | Attach file to send |
| `Ctrl-L` | Browse all files |
| `Ctrl-D` | Cancel file download |
| `Ctrl-W` | Toggle full screen chat |
| `f` (messages) | Toggle file select mode |
| `Ctrl-Up` | Enter file select mode from anywhere |
| `Ctrl-Down` | Exit file select, focus input |
| `Enter` / `Space` (header) | Collapse/expand channel group |
| `Up` / `Down` (input) | Browse sent message history |
| `Ctrl-R` | Refresh channels |
| `Ctrl-H` | Help |
| `Ctrl-\` | Toggle input mode (normal/edit) |
| `Alt-Enter` | New line (normal) or send (edit) |
| `Shift-Enter` | New line (both modes) |
| `Ctrl-D` | Cancel file download |
| `Ctrl-S` | Settings (includes Keyboard Shortcuts) |
| `Ctrl-Q` | Quit |

</details>

<details>
<summary>Settings (Ctrl-S)</summary>

| Setting | Options | Description |
|---------|---------|-------------|
| Auto Update | on / off | Auto-update on startup when new version available |
| Sidebar Width | 10-80 | Sidebar width in characters |
| Timestamp Format | Go format | e.g. `15:04`, `3:04 PM` |
| Away Timeout | 0+ seconds | Auto-away after idle (0 = disabled) |
| Mouse | on / off | Mouse click/scroll support (restart required) |
| Notifications | on / off | Terminal bell + desktop notifications |
| Poll Interval | 1-300 | Seconds between message checks |
| Priority Channels | 0-10 | Most-recent channels checked every poll cycle |
| Sort By | type / name / recent | Channel sorting |
| Sort Direction | asc / desc | Sort order |
| Input History | 1-200 | Max sent messages to remember |
| Download Path | folder | File download/upload location |

Fields with fixed options cycle with Enter/Tab.

</details>

### CLI

```
slackers              Launch the TUI
slackers setup        Interactive setup
slackers login        OAuth browser login
slackers join <url>   One-command team onboarding
slackers update       Check for and install latest version
slackers config       Show current config
slackers version      Print version
```

### Uninstall

```bash
rm ~/.local/bin/slackers
rm -rf ~/.config/slackers
```

---

## How it works

**User token first.** Slackers uses your user token (`xoxp-`) for API calls so messages appear as you and you see all your channels. The bot token (`xoxb-`) is a fallback.

**Polling for new messages.** Slack's Socket Mode only delivers events to channels the bot has joined (and joining posts a visible system message). Instead, Slackers polls `conversations.history` with `limit=1` to check for new messages — one lightweight API call per channel that returns just the latest timestamp.

**Batched rotation to respect rate limits.** Slack allows ~50 API calls per minute. Slackers doesn't check all channels every cycle. Each poll cycle checks a small batch:

1. **Current channel** — always included, so your active chat stays live
2. **Priority channels** — the N most-recently-active channels (configurable, default 3). These are the channels with the newest messages, so your busiest conversations update fastest
3. **Rotating batch** — 5 additional channels from the full list, advancing each cycle. This ensures every channel gets checked within `(total_channels / 5) × poll_interval` seconds

With 50 channels, a 10s poll interval, and 3 priority channels: each cycle makes ~9 API calls (1 current + 3 priority + 5 rotation) = ~54 calls/min. If that's too high, reduce priority channels or increase the poll interval.

**Hidden channels are skipped.** Channels you've hidden (`Ctrl-X`) are excluded from polling entirely, reducing API usage. Unhiding a channel adds it back to the poll rotation.

**Unread detection.** On startup, Slackers seeds baseline timestamps for all visible channels (batched with delays to avoid rate limits). After seeding, the poller compares each channel's latest message timestamp against the stored baseline. Changed channels get marked with `*` in the sidebar. Viewing a channel updates its timestamp, clearing the marker.

**Settings.** All values are configurable in settings (`Ctrl-S`):
- **Poll Interval** (1-300s, default 10s) — how often to check
- **Priority Channels** (0-10, default 3) — most-recent channels checked every cycle
- Lower interval + higher priority = more responsive but more API usage

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
  auth/             OAuth2 browser flow with team ID verification
  config/           Config loading, saving, validation (0600 perms)
  format/           Slack mrkdwn to plain text, emoji rendering
  shortcuts/        Customizable keyboard shortcuts with embedded defaults
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

## Support

If you find Slackers useful, consider buying me a coffee:

[buymeacoffee.com/ttv1xp6yAj](https://buymeacoffee.com/ttv1xp6yAj)

## License

MIT
