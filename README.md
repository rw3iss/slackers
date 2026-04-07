```
 ██████╗ ██╗      ██████╗  ██████╗██╗  ██╗███████╗██████╗  ██████╗
██╔════╝ ██║     ██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗██╔════╝
╚█████╗  ██║     ████████║██║     █████╔╝ █████╗  ██████╔╝╚█████╗
 ╚═══██╗ ██║     ██╔═══██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗ ╚═══██╗
██████╔╝ ███████╗██║   ██║╚██████╗██║  ██╗███████╗██║  ██║██████╔╝
╚═════╝  ╚══════╝╚═╝   ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═════╝
```

A lightweight, terminal-based Slack client.

<a href=".github/screenshot.png"><img src=".github/screenshot.png" alt="Slackers channels" width="220"></a> <a href=".github/screenshot-help.png"><img src=".github/screenshot-help.png" alt="Slackers help" width="220"></a> <a href=".github/screenshot-settings.png"><img src=".github/screenshot-settings.png" alt="Slackers settings" width="220"></a> <a href=".github/screenshot-search.png"><img src=".github/screenshot-search.png" alt="Slackers edit mode" width="220"></a> <a href=".github/screenshot-files.png"><img src=".github/screenshot-files.png" alt="Slackers edit mode" width="220"></a> <a href=".github/screenshot-edit.png"><img src=".github/screenshot-edit.png" alt="Slackers edit mode" width="180"></a>

## Features

- **Real-time messages** -- Socket Mode for instant delivery, with smart polling as a fallback
- **Message search** -- search current or all channels, jump to results with context view
- **File browser** -- upload, download, browse and search files across all channels
- **Mouse support** -- click channels, scroll panels, drag sidebar resize, click files
- **Multi-line editor** -- expandable textarea with normal/edit mode toggle
- **Customizable shortcuts** -- rebind any key in-app, changes take effect immediately
- **Channel management** -- hide, alias, collapse groups, sort by type/name/recent
- **E2E encrypted messaging** -- optional P2P secure mode with X25519 key exchange
- **Friends list** -- private P2P chat with befriended Slackers users, works without a Slack workspace
- **Auto-update** -- new versions downloaded and installed on startup
- **One-command onboarding** -- `slackers join <url>` for team setup, OAuth browser login
- **Single binary** -- cross-platform (Linux, macOS, Windows), no dependencies

For a deep dive into the architecture and design decisions, see [How_It_Works.md](How_It_Works.md).

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

All shortcuts are fully customizable. Open **Settings** (`Ctrl-S`) > **Keyboard Shortcuts** to rebind any key in-app (changes take effect immediately). You can also edit `~/.config/slackers/shortcuts.json` directly -- only overridden keys need to be listed; defaults fill in the rest. See [internal/shortcuts/defaults.json](internal/shortcuts/defaults.json) for the full default mapping. The in-app help panel (`Ctrl-H`) always shows your current bindings including any overrides.

| Key | Action |
|-----|--------|
| `Tab` / `Shift-Tab` | Cycle focus between panels |
| `Esc` | Toggle between sidebar and input |
| `Enter` | Select channel or send message |
| `i` or `/` | Focus input |
| `Ctrl-K` | Search channels |
| `Ctrl-F` | Search messages (Tab toggles scope) |
| `Ctrl-N` | Next unread channel |
| `Ctrl-U` | Attach file (sidebar/input) or half-page up (messages) |
| `Ctrl-D` | Cancel download (if active) or half-page down (messages) |
| `Ctrl-L` | Browse all files |
| `f` (messages) | Toggle file select mode |
| `Ctrl-Up` | Enter file select mode from anywhere |
| `Ctrl-Down` | Exit file select, focus input |
| `Ctrl-X` | Hide channel |
| `Ctrl-G` | Unhide channels |
| `Ctrl-O` | Toggle hidden visible |
| `Ctrl-A` | Rename/alias channel |
| `Ctrl-W` | Toggle full screen chat |
| `Ctrl-R` | Refresh channels |
| `PgUp` / `PgDn` | Page scroll (messages, overlays) |
| `Home` / `End` | Jump to top / bottom |
| `Ctrl-\` | Toggle input mode (normal/edit) |
| `Alt-Enter` | New line (normal) or send (edit) |
| `Shift-Enter` | Insert new line (both modes) |
| `Up` / `Down` (input) | Browse sent message history |
| `Enter` / `Space` (header) | Collapse/expand channel group |
| `Ctrl-B` | Send friend request to current DM user |
| `Ctrl-H` | Help (shows current bindings) |
| `Ctrl-S` | Settings |
| `Ctrl-Q` / `Ctrl-C` | Quit |

All overlay panels (help, settings, search, hidden channels) are scrollable with arrow keys, PgUp/PgDn, and mouse wheel.

</details>

<details>
<summary>Settings (Ctrl-S)</summary>

| Setting | Options | Description |
|---------|---------|-------------|
| Sidebar Width | 10-80 | Sidebar width in characters |
| Timestamp Format | Go format | e.g. `15:04`, `3:04 PM` |
| Auto Update | on / off | Auto-update on startup |
| Away Timeout | 0+ seconds | Auto-away after idle (0 = disabled) |
| Mouse | on / off | Mouse support (restart required) |
| Notifications | on / off | Terminal bell + desktop notifications |
| Poll Interval | 1-300s | Current channel poll frequency (default 10) |
| Bg Poll Interval | 5-600s | Background channel checks (default 30) |
| Priority Channels | 0-10 | Extra channels polled when socket is down |
| Input History | 1-200 | Sent messages to remember |
| Download Path | folder | File download location |
| Sort By | type / name / recent | Channel sorting mode |
| Sort Direction | asc / desc | Sort order |
| Secure Mode | on / off | E2E encrypted P2P messaging (restart required) |
| P2P Port | 1024-65535 | Local port for P2P connections (default 9900) |
| Secure Whitelist | Manage... | Users allowed for encrypted messaging |
| Keyboard Shortcuts | Customize... | Rebind any key in-app |

Fields with fixed options cycle with Enter/Tab.

</details>

### CLI

```
slackers              Launch the TUI
slackers --debug      Launch with debug logging enabled
slackers setup        Interactive setup
slackers login        OAuth browser login
slackers join <url>   One-command team onboarding
slackers update       Check for and install latest version
slackers config       Show current config
slackers friends      P2P friends setup guide (platform-specific)
slackers version      Print version
```

### Debugging

Run with `--debug` to log all Slack API calls, socket events, and poll activity to a file:

```bash
slackers --debug
```

In another terminal, tail the log to watch requests in real time:

```bash
tail -f ~/.config/slackers/debug.log
```

The log shows timestamped entries for every API request (with channel IDs and batch sizes), Socket Mode connect/disconnect/message events, poll tick triggers, and rate limit errors. Useful for diagnosing connectivity issues, unexpected API usage, or verifying that Socket Mode is delivering events. No performance overhead when `--debug` is not passed.

### Friends & Private Chat

Slackers includes a peer-to-peer friends system that works independently of Slack. Friends communicate directly through encrypted libp2p connections -- messages never pass through Slack's servers.

**Adding a friend:** Open a DM with someone and press `Ctrl+B`. If they're running Slackers with Secure Mode enabled, a friend request is sent over P2P. If they're not using Slackers, they'll receive a Slack DM inviting them to try it.

**Accepting a request:** When someone sends you a friend request, a popup appears with their name. Accept to exchange connection info and add them to your friends list.

**Chatting:** Friends appear in a collapsible "Friends" section at the top of the sidebar. Click a friend to open a private chat -- messages are sent directly over libp2p, stored locally, and never touch Slack. Online friends appear in green, offline in grey.

**No workspace required:** If you have friends in your list but no Slack workspace configured, Slackers starts in friends-only mode. You can chat with any online friend without needing a Slack account.

Friend data is stored in `~/.config/slackers/friends.json`. See [How_It_Works.md](How_It_Works.md#friends--private-chat) for the technical details.

### Uninstall

```bash
rm ~/.local/bin/slackers
rm -rf ~/.config/slackers
```

---

<details>
<summary>Friends — Private P2P Communication</summary>

Slackers includes a complete peer-to-peer friends system for private, encrypted chat that operates independently of Slack.

**Key features:**
- **Direct P2P messaging** -- messages sent over libp2p, never through Slack servers
- **P2P file transfer** -- send and receive files directly between friends, no cloud storage
- **X25519 + ChaCha20-Poly1305 encryption** -- per-friend-pair encryption keys
- **Contact card sharing** -- JSON format for easy friend exchange via any channel
- **Friends-only mode** -- works without a Slack workspace configured
- **Online/offline detection** -- periodic pings with green/grey sidebar indicators
- **Import/export** -- bulk friend list management with conflict resolution
- **Profile management** -- edit your own and friends' connection info in-app

**Quick start:**
1. Enable Secure Mode in Settings
2. Open a DM and press `Ctrl+B` to befriend
3. Or exchange contact cards: Settings > Friends Config > Share My Info

**Configuration:**
- Friends stored in `~/.config/slackers/friends.json`
- P2P port default: 9900 (configurable in Settings or Friends Config > Edit My Info)
- Run `slackers friends` for a complete setup guide including firewall/port forwarding instructions

See [How_It_Works.md](How_It_Works.md#friends--private-chat) for the technical deep dive.

</details>

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
  secure/           E2E encryption, key management, libp2p P2P node
  shortcuts/        Customizable keyboard shortcuts with embedded defaults
  slack/            SlackService + SocketService interfaces and implementations
  tui/              Bubbletea model, UI components, overlays
  types/            Shared domain types
scripts/            Install/uninstall/cleanup scripts
configs/            Default config, Slack app manifest
```

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
