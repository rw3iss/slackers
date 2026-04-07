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

## Friends & Private Chat

The friends system enables direct peer-to-peer messaging between Slackers users without involving Slack's servers. It is built on top of the existing P2P infrastructure (libp2p, X25519 key exchange) and operates as a fully independent communication layer.

### Identity

Every Slackers installation generates a unique **SlackerID** on first launch -- a 32-character hex string stored in `config.json`. This ID persists across sessions and serves as a stable identifier for the friends system, independent of any Slack user ID. When users don't share a Slack workspace, the SlackerID is the primary way to identify and deduplicate friend records.

Users can also set a **display name** and **email** in their profile (Settings > Friends Config > Edit My Info). The email provides an additional uniqueness check during friend import/merge operations.

### Data model

Friends are stored in `~/.config/slackers/friends.json` as a `FriendStore` -- a thread-safe, JSON-persisted list of `Friend` records. Each friend entry contains:

- **UserID**: Slack user ID or `slacker:<SlackerID>` for non-Slack friends
- **SlackerID**: The friend's unique Slackers installation ID
- **Name**: Display name
- **Email**: Optional email for identity verification
- **PublicKey**: Base64-encoded X25519 public key for encryption
- **PairKey**: Per-pair derived encryption key (base64), generated from ECDH shared secret
- **Multiaddr**: libp2p multiaddress for direct connection (e.g. `/ip4/1.2.3.4/tcp/9900/p2p/<peerID>`)
- **Endpoint**: IP/hostname (human-readable alternative to multiaddr)
- **Port**: P2P port number
- **AddedAt**: Unix timestamp of when the friendship was established
- **LastOnline**: Unix timestamp of last confirmed online status
- **ConnectionType**: `"p2p"` (direct libp2p) or `"e2e"` (encrypted relay via Slack)

The `Online` field is runtime-only (not persisted) and tracks whether the friend's P2P node is currently reachable.

### Contact cards

Friends are exchanged via **contact cards** -- a versioned JSON format containing everything needed to establish a connection:

```json
{
  "version": 1,
  "slacker_id": "a1b2c3...",
  "name": "Alice",
  "email": "alice@example.com",
  "public_key": "base64...",
  "multiaddr": "/ip4/...",
  "endpoint": "1.2.3.4",
  "port": 9900
}
```

Users can generate their contact card from Settings > Friends Config > Share My Info, then send it to a friend via any channel (email, Signal, etc.). The recipient pastes it in Settings > Friends Config > Add a Friend > Ctrl+J.

### Conflict resolution

When importing friends (individually or via bulk import), the system checks for conflicts across multiple fields: UserID, SlackerID, Email, and Endpoint+Port combination. If any match, the entry is flagged as conflicting. During import, users choose to either overwrite matching entries or skip them.

### Startup sequence

Friends are loaded **before** workspace data. The startup order is:

1. Load `friends.json` and create `FriendStore`
2. Build friend channel entries and add to sidebar
3. Attempt P2P connection to each friend's multiaddr
4. Start friend ping cycle (30-second interval)
5. Load workspace data (users, channels, Socket Mode) -- if configured

This means the sidebar shows friends immediately, even before Slack channels finish loading. If no workspace is configured at all, the app enters **friends-only mode** and skips all Slack API initialization.

### Friend request flow

The friend request protocol uses the existing P2P message system with three new message types:

```
friend_request  →  Sender's public key + multiaddr
friend_accept   ←  Receiver's public key + multiaddr
friend_reject   ←  (no payload)
```

**Sending a request (`Ctrl+B`):**
1. User presses `Ctrl+B` while viewing a DM channel
2. A confirmation popup asks "Send friend request to {name}?"
3. On confirm, a `friend_request` P2P message is sent containing the sender's public key and multiaddr, pipe-delimited in the `Text` field
4. If the P2P send fails (peer not connected or not running Slackers), a fallback Slack DM is sent inviting them to install Slackers

**Receiving a request:**
1. The P2P callback detects a `friend_request` message type
2. A `P2PReceivedMsg` is dispatched with the special `__friend_request__` text marker
3. The model opens the friend request popup showing the sender's name with Accept/Reject buttons
4. On accept: the friend is added to `FriendStore`, saved to disk, a `friend_accept` message is sent back with the receiver's own public key and multiaddr, and the friend appears in the sidebar
5. On reject: a `friend_reject` is sent and nothing is stored

**Mutual completion:**
When the original sender receives a `friend_accept` message, they automatically add the peer as a friend (no second confirmation needed since they initiated), save to disk, and update the sidebar.

### Friend channels

Friend channels are represented as regular `types.Channel` entries with `IsFriend: true` and an ID of `"friend:<userID>"`. They participate in the same sidebar rendering system as Slack channels but are grouped into a "Friends" section that renders before all workspace sections.

The `sectionKey()` function checks `IsFriend` first, ensuring friends always group together regardless of other flags. The channel list model's `SetFriendChannels()` method strips existing friend entries and prepends the new set, so friends always appear at the top.

### Message routing

When the current channel is a friend channel, message handling diverges from the normal Slack flow:

**Sending:** Instead of calling `SendMessage` on the Slack API, the input handler creates a `P2PMessage` with type `"message"` and sends it via `p2pNode.SendMessage()`. The message is also immediately appended to the local view and stored in `friendMessages[userID]` (an in-memory map of message history per friend).

**Receiving:** When a P2P message arrives from a known friend:
- If the friend's channel is currently open, the message is appended to the viewport and stored in history
- If the friend's channel is not open, the channel is marked as unread (triggering the sidebar indicator)

**History:** Friend message history is stored in-memory only (`friendMessages` map). It survives for the duration of the session but is not persisted to disk. When you select a friend channel, `SetMessages()` loads the stored history into the viewport. Future versions may add persistent local message storage.

### P2P file transfer

Files can be sent and received directly between friends using the same `[FILE:path]` syntax as Slack file attachments.

**Sending:** When a message in a friend channel contains `[FILE:/path/to/file]` patterns, the sender's client:
1. Registers each file locally via `ShareFile()`, generating a unique 16-char hex file ID
2. Stores the `fileID -> localPath` mapping in the P2P node's `sharedFiles` map
3. Sends a `file_offer` message to the friend with the file name, size, and ID
4. Displays the file as a `[FILE:name]` entry in the local chat

**Receiving:** When a `file_offer` arrives, it creates a `FileInfo` entry with a `p2p://` URL scheme (`p2p://<peerUID>/<fileID>`) and appends it to the chat as a regular file attachment. The receiver can browse files with `f` (file select mode) or click them.

**Downloading:** When the receiver selects a file for download, the `FileDownloadMsg` handler detects the `p2p://` URL prefix and calls `startP2PDownload` instead of the Slack API downloader:
1. Opens a new stream on the `/slackers/file/1.0.0` protocol to the sender
2. Sends a `file_request` message with the file ID
3. The sender's `handleFileRequest` looks up the file ID, opens the local file, and streams it raw over the connection
4. The receiver writes the stream to the configured Downloads folder
5. Cancel with `Ctrl+D` works the same as Slack downloads

The file transfer protocol is separate from the messaging protocol, allowing file data to flow without interfering with chat messages. The `FileDownloadCompleteMsg` and `FileDownloadCancelledMsg` handlers are shared between Slack and P2P downloads.

### Online detection

A background goroutine pings all friends every 30 seconds:

1. `friendPingTickMsg` fires every 30 seconds
2. `friendPingCmd` iterates through all friends with a stored multiaddr
3. For each friend, it attempts `ConnectToPeer()` if not already connected, then checks `IsConnected()`
4. The resulting `FriendPingMsg` carries a map of `userID -> bool`
5. The model updates the sidebar: online friends have their channel marked as "unread" (which triggers the green color for friend channels), offline friends have it cleared (grey color)

The friend-specific styling in `renderItem()` checks `IsFriend` before the generic unread check, so the green/grey coloring is specific to friends and doesn't conflict with normal unread indicators for Slack channels.

Connections are **not kept open permanently**. The ping cycle connects, checks, and lets the connection idle. A persistent connection is only established when the user actively opens a friend's chat channel. This avoids holding open sockets for all friends simultaneously.

### Disconnect notifications

When a user shuts down Slackers (or the P2P node closes), `BroadcastDisconnect()` sends a `disconnect` message to all currently connected peers. Recipients update the friend's online status to offline and record the `LastOnline` timestamp. This provides faster offline detection than waiting for the next ping cycle to timeout.

### Friends Config panel

The Friends Config panel (Settings > Friends Config) provides a complete management interface with six sub-pages, all managed by `FriendsConfigModel` with an internal page state machine:

1. **Friends List**: Scrollable list showing each friend with name, online/offline indicator, and last-seen time. Enter to edit, `d` to delete.
2. **Edit My Info**: Edit your display name, email, P2P endpoint/port, and Secure Mode. Changes sync with the main Settings automatically.
3. **Share My Info**: Generates and displays your JSON contact card for copy/paste sharing.
4. **Add a Friend**: Manual form for entering connection details, or `Ctrl+J` to paste a JSON contact card which auto-fills the form. `Ctrl+S` to save.
5. **Export Friends List**: Saves all friends as formatted JSON to the configured Downloads folder.
6. **Import Friends List**: Load a JSON file with conflict resolution -- toggle "Overwrite conflicts" to replace matching entries or skip them.

Each friend's profile is also editable (Friend List > Enter on a friend) showing connection details, added date, last online time, and all editable fields.

### Friends-only mode

If `config.Validate()` fails (no bot token / app token) but the friend store has entries, the app skips the setup wizard and launches with nil Slack services. The `Init()` function guards all workspace-dependent commands (`loadUsersCmd`, `connectSocketCmd`, `pollTickCmd`, etc.) behind nil checks on `m.slackSvc` and `m.socketSvc`. The friend loading, P2P node, and ping cycle all start normally.

This allows Slackers to function as a standalone P2P chat client for users who only want private friend-to-friend communication.

### Network requirements

P2P connections require port 9900/tcp (or the configured P2P port) to be reachable. Slackers uses libp2p with UPnP and NAT hole punching, which often works without manual configuration. For reliable connections behind NAT routers, users may need to set up port forwarding. Run `slackers friends` for platform-specific firewall instructions.
