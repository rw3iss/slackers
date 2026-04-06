# How It Works

A deep dive into the architecture, design decisions, and efficiency strategies behind Slackers.

## Architecture

Slackers follows the **Elm architecture** via the [Bubbletea](https://github.com/charmbracelet/bubbletea) framework: a single root `Model` struct holds all application state, an `Update` function processes messages and returns new state, and a `View` function renders the terminal output. No mutation happens outside the update loop, making the data flow predictable and debuggable.

### SOLID design

- **Interfaces for external systems.** `SlackService` and `SocketService` are interfaces, not concrete types. The TUI depends on these abstractions, not the Slack SDK directly. This makes the API layer swappable and testable.
- **Single-responsibility packages.** Each package owns one concern: `config/` handles persistence, `format/` handles text rendering, `secure/` handles cryptography, `slack/` handles the API, `tui/` handles the UI.
- **Dependency inversion.** The TUI model receives its dependencies (services, config) at construction time. It never imports the Slack SDK directly.

### Component structure

The root `Model` composes sub-models for each UI region:

```
Model (root)
  ChannelListModel   -- sidebar with collapsible groups
  MessageViewModel   -- scrollable message viewport with date headers
  InputModel         -- multi-line textarea with history
  KeyMap             -- dynamic shortcut bindings
  SettingsModel      -- settings overlay
  SearchModel        -- channel search overlay
  MsgSearchModel     -- message search overlay
  HiddenChannelsModel -- hidden channel manager
  RenameModel        -- channel alias editor
  FileBrowserModel   -- file/folder browser
  FilesListModel     -- all-files browser with search
  ShortcutsEditorModel -- keyboard shortcut editor
  WhitelistModel     -- secure messaging whitelist
```

Each sub-model has its own `Update` and `View` methods. The root model delegates based on focus state and active overlay. Overlays render on top of the main layout and capture all keyboard input while active.

## Token strategy

**User token first.** Slackers uses the user token (`xoxp-`) as the primary API token. This means messages appear as the actual user (not a bot), and the client sees the same channels and permissions the user has in the Slack web app. The bot token (`xoxb-`) serves as a fallback via the `tryWithFallback()` pattern -- if a user-token call fails, the same request is retried with the bot token. Warnings are collected (not thrown) so the TUI can display them without interrupting the user.

## Real-time message delivery

Slackers uses a two-layer approach for receiving messages:

### Layer 1: Socket Mode (primary)

Slack's Socket Mode delivers events over a WebSocket connection in real time. When a message is posted in any channel the bot has access to, Slackers receives it instantly with zero API calls. The socket handler:

1. Appends the message to the viewport if it's in the current channel
2. Marks the channel as unread (sidebar `*` indicator) if it's a different channel
3. Updates the `lastSeen` timestamp for the current channel to prevent duplicate detection by the poller

Socket Mode handles auto-reconnection with exponential backoff. Connection status changes are displayed in the status bar, and only actual state transitions emit updates (no flicker from redundant reconnect events).

### Layer 2: Polling (fallback)

Polling exists as a safety net for when Socket Mode misses events (network glitches, reconnection gaps). It runs on two independent timers:

**Primary poll** (`poll_interval`, default 10 seconds):
- Always checks the **current channel** by fetching `conversations.history` with `limit=1` (one API call)
- If Socket Mode is **disconnected**, also checks N priority channels (the most recently active ones)
- Refreshes the current channel's content so edits, reactions, and thread replies appear

**Background poll** (`poll_interval_bg`, default 30 seconds):
- Rotates through 5 channels per cycle, picking the ones that were checked longest ago
- Skips the current channel (already handled by the primary poll)
- Ensures every channel gets checked eventually, even if Socket Mode drops events

**Net effect when Socket Mode is connected:** ~1 API call per 10 seconds (just the current channel), plus ~5 calls per 30 seconds for background rotation = roughly 11 calls per minute. When Socket Mode is disconnected, priority channels are added to the primary poll, increasing to ~34 calls per minute -- still well within Slack's ~50 calls/minute rate limit.

### Rate limit awareness

`CheckNewMessages` stops the current batch early if it hits a rate limit error from Slack, preventing cascading failures. The next poll cycle picks up where it left off.

### Hidden channels are excluded

Channels hidden via `Ctrl-X` are removed from the poll rotation entirely. This directly reduces API usage -- hiding 20 low-priority channels in a 100-channel workspace cuts background poll cycles nearly in half.

## Unread detection

On startup, Slackers seeds baseline timestamps for all visible channels. This is done in batches of 5 with 2-second delays between batches to avoid rate limits. The seed uses `SeedLastSeenMsg` which updates timestamps without marking anything as unread -- so you don't see false unread indicators on first launch.

After seeding, unread detection works through two paths:
1. **Socket events** mark channels as unread instantly (zero-latency)
2. **Poll results** compare the latest message timestamp against `lastSeen` and mark channels with newer messages

The `lastSeen` map is persisted to `~/.config/slackers/config.json` as `last_seen_ts`, so unread state survives restarts. Viewing a channel updates its timestamp, clearing the unread marker.

## Overlay navigation and API efficiency

Opening and closing overlays (help, settings, search, shortcuts editor, etc.) triggers **zero API calls**. The overlay system is purely a UI state machine -- setting `m.overlay = overlaySettings` shows the settings panel, and `m.overlay = overlayNone` returns to the main view. No channel data is reloaded, no history is re-fetched. The only time navigation triggers an API call is when you switch to a different channel (which fetches its message history).

Returning from away (idle timeout) only refreshes the current channel's history. Previous behavior polled all channels on wake-up, which could hit rate limits in large workspaces.

## Input handling

### Multi-line textarea

The input bar uses a Bubbletea textarea with two modes:

- **Normal mode**: `Enter` sends the message, `Alt-Enter` and `Shift-Enter` insert newlines. The textarea auto-expands vertically as you type (up to 10 rows) and shrinks back when lines are deleted.
- **Edit mode** (`Ctrl-\`): `Enter` inserts a newline, `Alt-Enter` sends. This mode is better for composing longer multi-line messages.

The `Shift-Enter` handling accounts for terminal escape sequence differences -- Konsole sends `\eOM` instead of the standard sequence, so the input model detects the two-part `Alt+O` followed by `M` pattern.

### Input history

Sent messages are saved (configurable limit, default 20) and navigable with `Up`/`Down` when the input is focused. History persists across sessions via the config file.

## File handling

- **Upload**: Uses Slack's `files.uploadV2` API with the required `FileSize` parameter. Files are selected through the built-in file browser overlay.
- **Download**: Supports context cancellation (`Ctrl-D`) and progress reporting in the status bar. Downloaded to the configurable download path.
- **File browser**: Full directory browser with file type filters, used for both uploads and selecting the download path in settings.
- **File drop detection**: Pasting a file path (detected via heuristic pattern matching) triggers an upload prompt.

## Channel management

### Collapsible groups

Channels are grouped by type (public, private, DM, group). Group headers can be collapsed/expanded with `Enter` or `Space` on the header row. Collapsed state persists across sessions.

### Aliases

Any channel can be given a display alias (`Ctrl-A`). The alias appears in the sidebar and search results. A channel index maps both the real name and alias for quick lookup.

### Sorting

Three sort modes (type, name, recent) with ascending/descending direction. The "recent" mode uses `latestTS` timestamps updated by both Socket Mode events and poll results.

### Hidden channels

Hidden channels are excluded from the sidebar, polling, and unread detection. They can be viewed and unhidden from the hidden channels overlay (`Ctrl-G`).

## Keyboard shortcuts system

Shortcuts are defined in a JSON file with embedded defaults (`go:embed`). User overrides are stored separately in `~/.config/slackers/shortcuts.json` and merged at startup. The `BuildKeyMap()` function constructs Bubbletea `key.Binding` objects from the merged map, making every shortcut dynamic.

The in-app shortcuts editor uses a "capture mode" that suppresses all normal key handling -- when rebinding a key, pressing `Ctrl-Q` captures that key combination instead of quitting the app. Conflict detection warns when a key is already bound to another action.

## Secure messaging (optional)

When Secure Mode is enabled in settings, Slackers adds end-to-end encrypted messaging for DMs with whitelisted users.

### Cryptography

- **Key exchange**: X25519 (Curve25519) keypairs generated and stored locally at `~/.config/slackers/slackers.key`
- **Shared secret**: ECDH key agreement produces a shared secret per peer
- **Key derivation**: HKDF-SHA256 derives a 32-byte encryption key from the shared secret
- **Encryption**: ChaCha20-Poly1305 AEAD with random 12-byte nonces
- **Message format**: `[SLACKERS_E2E:<base64-nonce>:<base64-ciphertext>]` -- sent as regular Slack DMs but unreadable without the key

### P2P direct connections

When both peers have Secure Mode enabled, Slackers can establish direct peer-to-peer connections using libp2p:

- NAT traversal via UPnP port mapping and hole punching
- Messages sent directly between peers bypass Slack entirely
- Falls back to encrypted Slack DM relay when direct connection fails

### Peer discovery

Peers discover each other's public keys via Slack profile status fields. The whitelist controls which users are eligible for encrypted messaging, manageable through the settings overlay.

### Session states

The channel header shows the current encryption state for DMs:
- **(empty)**: No encryption
- **discovering...**: Looking for peer's public key
- **E2E**: End-to-end encrypted via Slack DM relay
- **P2P**: Direct peer-to-peer connection active

## Notifications

Slackers uses three notification mechanisms for maximum terminal compatibility:

1. **BEL character** (`\a`): Works in virtually all terminals
2. **OSC 9**: Desktop notification escape sequence (supported by iTerm2, Windows Terminal, etc.)
3. **Urgency hints**: X11 window urgency flag via `\e[?1042h` (Linux terminals)

Notifications are sent when new messages arrive in non-current channels while the app is running.

## Auto-update

On startup (if enabled), Slackers checks the GitHub releases API for a newer version. If found, it downloads the appropriate binary for the current platform, replaces the running executable, and displays a notification. The update check is non-blocking -- the app is fully usable while it runs in the background.

## Config persistence

All configuration is stored in `~/.config/slackers/config.json` with `0600` permissions (user-read/write only). The config is loaded once at startup and saved incrementally when settings change. Sensitive fields (tokens) are never logged or displayed in the UI beyond masked values in the settings panel.

Config changes from the settings overlay take effect immediately for display settings (sidebar width, sort order, timestamp format) and on next poll cycle for polling settings. Some settings (mouse, secure mode) require a restart.
