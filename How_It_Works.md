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

### Testing the P2P connections locally, with yourself:

To actually verify the full libp2p handshake (key exchange, ping, friend chat) on a single machine, run a second instance with its own config dir + port:

XDG_CONFIG_HOME=/tmp/slackers-test slackers

### Pending messages & reconnect recovery

Messages sent to an offline friend never fail silently. Every friend P2P send is issued as a `tea.Cmd` that wraps `P2PNode.SendMessage` plus a one-shot reconnect-and-retry. The outcome is reported back to the model as a `FriendSendResultMsg{Success bool}`:

- On `Success == false`, the model flips the corresponding history entry's `Pending` flag to `true` and saves the store. The message view then renders a `⏳ pending` badge next to the timestamp so the user knows it hasn't been delivered.
- On `Success == true`, any pre-existing `Pending` flag is cleared.

Recovery runs on two independent triggers so neither side of a reconnect is dependent on its own ping cycle being the faster one:

1. **Local edge-trigger.** The `friendPingCmd` cycle (interval configurable via `cfg.FriendPingSeconds`, minimum 2s, default 5s) tracks each friend's online state in `friendPrevOnline`. An `offline → online` transition queues a `resendPendingFriendMessagesCmd(peerUID)`, which scans the friend's history newest-first — stopping at the first already-delivered locally-authored message — collects everything still flagged `Pending`, and dispatches them one at a time via `tea.Sequence`. Sequencing (not batching) guarantees receive order: each send's `FriendSendResultMsg` is processed before the next one starts.

2. **Remote pull.** When `connectFriend` successfully transitions a peer from offline to online, it fires a `MsgTypeProfileSync` **and** a `MsgTypeRequestPending` at the peer. The receiver of `request_pending` runs its own `resendPendingFriendMessagesCmd` against the requester, so pending messages also flow in the opposite direction immediately on reconnect — not just when the sender's own ping cycle happens to notice.

To preserve ordering across network races, every regular friend message now carries its original `Timestamp` on the wire, and the receiver stamps the stored message with that value instead of the arrival time. The original send order is therefore preserved even if out-of-order delivery happens.

`P2PNode.ConnectToPeer` is wrapped in a 3-second dial timeout so an offline peer can't block the bubbletea Update loop, and `connectFriend` itself dispatches the dial in a goroutine so typing is never frozen by a stale friend connection.

### Profile auto-sync

A fresh wire message type, `MsgTypeProfileSync`, carries the sender's full contact card as single-line JSON. `connectFriend` fires it on every offline→online transition; the receiver merges non-empty fields into the matching stored friend record (matching by `SlackerID` → `PublicKey` → `Multiaddr`, centralised in `FriendStore.FindByCard`). Merge rules:

- `Email`, `PublicKey`, `Multiaddr`, `SlackerID` overwrite the stored value when they differ. A changed `PublicKey` also clears the cached per-pair `PairKey` so the next handshake re-derives the shared secret.
- `Name` is **only** filled in when the stored name is empty. The user's locally-chosen display name for that friend (their alias for that person) is never overwritten by what the peer calls themselves.

`P2PNode` also exposes a `SetPeerLookup` callback used by the incoming stream handler to resolve an otherwise-unknown remote `peer.ID` against the friend store's multiaddrs, so inbound-only connections (where the remote dialed us first and we never dialed back) still attribute messages to the right friend and auto-populate the `peerMap` / `slackMap` tables for future outbound sends.

### Sharing contact cards in chat

Any outgoing message can embed a contact card inline using the `[FRIEND:me]` token (insert from the input with **Alt-M**, or via the sidebar right-click → **Invite to Slackers** action for non-friend DMs) or `[FRIEND:<friend-id>]` for a friend already in the store. A dedicated `expandFriendMarkers` pass runs on every outgoing text and replaces the token with either:

- A compact **SLF2** binary hash (~109 chars, base64url-encoded, 32-byte X25519 public key + peer ID + ipv4 + port — no name/email), or
- A **single-line JSON** contact card with the full profile (name, email, slacker_id, public key, multiaddr).

The encoding is chosen per-user via `Friends Config → Share Format` (stored as `cfg.ShareMyInfoFormat`). New users default to JSON so the recipient sees a real name instead of a `Friend XXXXXX` placeholder. Already-encoded markers (ones starting with `{`, `SLF1.`, `SLF2.`, or `#` placeholder refs) pass through unchanged, so baked-in invites (e.g. the Slack invite builder) aren't double-processed and `]` characters aren't escaped to `\u005d`.

On the receiving side the message renderer runs a two-pass pipeline:

1. **`collapseFriendMarkers`** runs BEFORE word-wrap on the full message text. It finds each `[FRIEND:<blob>]`, decodes it with `friends.ParseAnyContactCard`, stores the result in `MessageViewModel.friendCards[key]`, and replaces the long blob with a short `[FRIEND:#fc-N]` reference token. This guarantees the marker survives line wrapping intact — without this, a long JSON card would wrap across two visual lines and the regex (which requires `[FRIEND:` and `]` on the same line) would silently miss it, leaving the raw blob in the chat output.
2. **`rewriteFriendCards`** runs per-rendered-line during message layout. It resolves the `#fc-N` reference (or parses a short hash inline) and substitutes a styled `👤 Friend: <label>` pill, where the label comes from Name → Email → `ShortPeerID` fallback. The pill's click hit area is recorded in `friendCardHits` with its line/column bounds.

The full original `[FRIEND:...]` marker is preserved in the underlying message text and in the persisted chat history — the pill is purely a render-time substitution, so the embedded card is always recoverable on click.

**Click flow.** Clicking a pill dispatches a `FriendCardClickedMsg{Card}`. The handler first checks whether the card represents the local user (by SlackerID, PublicKey, or local Multiaddr) and shows `"That's your own contact card — nothing to import."` if so. Otherwise it calls `FriendStore.FindByCard` to look up an existing match and drives one of three confirmation prompts: **Add as new friend**, **Merge missing fields into existing**, or **Replace existing fields**. On confirm, `applyFriendCard` writes through, kicks off the standard `FriendAddedHandshakeMsg` P2P handshake for brand-new friends, and refreshes the sidebar.

## Notifications view

Beyond the terminal BEL / OSC 9 / X11 urgency hints already used for arrival alerts, Slackers now also maintains a persistent **notifications store** at `~/.config/slackers/notifications.json` backed by `internal/notifications/Store`. Three notification types are tracked:

- `TypeUnreadMessage` — a message arrived in a channel you weren't viewing.
- `TypeReaction` — someone reacted to one of your messages.
- `TypeFriendRequest` — a pending friend-request handshake you haven't responded to.

Entries are added as the app observes the underlying event and cleared when the user opens the referenced channel / friend chat / clears the friend request. The **Notifications view** (`Alt-N`, or via the status bar indicator) renders a scrollable list; activating a notification routes through `activateNotification`, which navigates the UI to the source (channel, friend chat, or friend request modal) and removes the entry.

## Right-click context menus

The TUI exposes three overlay-backed right-click context menus that share the same popup-positioning machinery (`ansiTruncatePad` + computed click-inside hit test):

- **Message options** (`MsgOptionsModel`, `overlayMsgOptions`) — right-click any message in the chat view to get React / Reply / Edit / Delete. Edit and Delete only appear when `isMyMessage(msg)` returns true. The same set of actions is available by keyboard via the select-mode hint (`r` / `e` / `d`) so mouse and keyboard users stay in sync.
- **Sidebar channel options** (`SidebarOptionsModel`, `overlaySidebarOptions`) — right-click any channel entry in the sidebar. Every channel gets Hide / Rename; DM / private-group entries whose target user is not already a friend also get **Invite to Slackers** (which switches to that chat and pre-fills the input with the Slack-mrkdwn invite text plus a `[FRIEND:<json>]` payload on its own line); friend channels instead get **View Contact Info** (opens the Friends Config overlay on that friend's Edit Friend page) and **Remove Friend** (with a y/Enter confirmation prompt — the chat history view stays on screen for one last reference read until the user navigates away). The right-click handler uses a non-mutating `ChannelByRow` lookup so the popup does NOT move the sidebar cursor off the user's currently active channel.
- **Friend card pill options** (`FriendCardOptionsModel`, `overlayFriendCardOptions`) — right-click any `[FRIEND:...]` pill rendered inside a chat message. The menu items adapt to the card's relationship to the local user: **self** → View Contact Info / Copy Contact Info; **already a friend** → View Friend Profile / View Contact Info / Copy Contact Info; **non-friend** → Add Friend / View Contact Info / Copy Contact Info. Right-click hit-testing on the chat pane prioritises items in order — friend card → reaction → file → fall back to parent message — so item-specific menus only fire on the item itself, not the whole message line. The pill hit-test allows ±3 cells horizontally and ±1 row vertically to compensate for the wide leading 👤 emoji glyph.

## In-message item navigation (select mode)

The message select mode (`Ctrl-J` or `s`) extends beyond reactions and the inline reply list to cycle through every interactive item in a parent message in priority order. Left/right arrow keys walk a combined index over `[ contact cards | files | reactions | reply list virtual ]`, with the existing "down into replies" navigation preserved at the end.

Each kind of selection swaps the inline keyboard hint shown above the highlighted message:

- **Contact card pill** — `[a: add friend  v: view info  c: copy info  ←/→: navigate]`. Pressing `a` routes through the same `FriendCardOptionsSelectMsg` dispatch the right-click menu uses (so the existing self-check, conflict prompt, and handshake all run unchanged); `v` opens the temporary contact card view modal; `c` marshals the card to JSON and copies it to the clipboard; `Enter` defaults to View Contact Info.
- **File row** — `[Enter: download  c: copy contents  ←/→: navigate]`. Enter routes through the existing `FilesListDownloadMsg` path; `c` triggers the existing `FileCopyRequestMsg` flow (large-file confirmation included).
- **Reaction badge** — Enter toggles, behaviour unchanged.
- **Reply list virtual element** — down arrow enters the reply tree, behaviour unchanged.

The selection state is encoded in the existing `reactionSelIdx` field, reinterpreted via `selectedItemKind() (kind, localIdx)`. A per-message render counter (`renderingCardCount`) is reset at the top of each parent message in `renderMessageList` and incremented for every pill emitted by `rewriteFriendCards`, so the pill style flips to `FriendCardPillSelectedStyle` for whichever card matches the cursor position. File rendering checks the same kind/index pair to switch to `MessageFileSelectedStyle` when the cursor lands on a file row.

## Slash command framework

Slackers exposes a slash-command interface in the input bar — type `/` to start. The architecture is split between a Model-agnostic framework package (`internal/commands`) and the TUI host code that registers concrete handlers as closures over `*Model`.

### Framework (`internal/commands`)

The package is a self-contained dictionary + lookup engine with no Bubbletea or Slackers dependencies. It compiles standalone and is unit-tested.

- **`Command`** — name, aliases, kind (command vs emote), description, usage, args, and a `RunFunc(ctx *Context) Result` handler.
- **`Registry`** — owns the command set, indexed both by name (`map[string]*Command`) and by a character trie for prefix lookup. Aliases share the same canonical Command pointer.
- **`trie`** — each node caches the names of every descendant command on the path from root to that node, so prefix lookup is O(len(prefix)) regardless of how many commands are registered. Empty-prefix lookup returns every name in the registry.
- **`FuzzyScore` / `RankFuzzy`** — subsequence match scorer with bonuses for exact prefix matches (highest), substring matches at word boundaries, and contiguous runs of matching characters. The scorer falls back to a global pass when the trie returns fewer than `topN` candidates so subsequence matches like `addfri` → `add-friend` and `rmfr` → `remove-friend` still surface.
- **`Result`** — `Status`, `StatusBar` (one-line status), `Title` + `Body` (Output view content), `FocusOutput` (opt-in to auto-Tab focus to the output pane), and `Cmd` (a follow-up `tea.Cmd`-shaped value carried as `any` so the package stays Bubbletea-free).
- **`tokenize`** — splits raw arg strings into tokens, honouring `"two words"` quotes for arguments containing whitespace.
- **`embed.FS` over `help/*.md`** — `Topics()` and `Topic(name)` expose the embedded markdown help files. Adding a new topic is just dropping `help/<topic>.md` in the package and rebuilding.

### TUI integration (`internal/tui/commands_basic.go`)

The host file `commands_basic.go` registers every concrete command as a closure that captures `*Model`. The registry is built once in `NewModel` *before* the splash screen, so the trie is fully cached by the time the user sees the loading view. Custom user emote / command JSON files (`~/.config/slackers/emotes.json`, `commands.json`) are merged into the same registry on startup — currently empty stubs but the loader is wired so adding them later is a no-op change.

The 14 foundation commands are:

- **General:** `/commands`, `/help [topic]`, `/version`, `/quit`, `/me`
- **Friends:** `/friends`, `/add-friend <hash|json|[FRIEND:...]>`, `/remove-friend <name|id>`
- **Channels & messages:** `/channels`, `/clear-history`, `/settings`, `/shortcuts`
- **Appearance:** `/themes`, `/theme <name>`
- **Diagnostics:** `/config` (with tokens redacted via `mask`)

Every command flows through `applyCommandResult`, the single funnel that:

1. Sets `m.warning` from `Result.StatusBar`
2. Activates the Output view from `Result.Title` + `Result.Body` (creating a fresh view or swapping content if the view is already active)
3. Honours `Result.FocusOutput` to optionally Tab focus to the messages pane
4. Schedules `Result.Cmd` (typed as `any`, accepts both `tea.Cmd` and `func() tea.Msg`)

### Suggestion popup (`CmdSuggestModel`)

Floats inline above the input bar — it's part of the vertical layout (not a screen overlay) so it doesn't need z-ordering against the message pane. As the user types after `/`, every keystroke calls `refreshCmdSuggest()` which queries `registry.Lookup(input, 8)` and updates the popup. Up/Down navigates with cursor-windowed visible slice. Tab completes the highlighted suggestion into the input bar with a trailing space; Enter runs the highlighted command directly (preserving any args already typed after the command name); Esc dismisses the popup without affecting the input value.

The popup auto-hides as soon as the user types a space after the command name (so it doesn't fight with whatever arg is being typed), or when the input no longer starts with `/`.

### Output view (`OutputViewModel`)

A pane-state — *not* an overlay. When `m.outputActive` is true, `renderBaseView` swaps `m.messages.View()` for `m.outputView.View()`, leaving the sidebar / input / status bar untouched. This means Tab still cycles focus normally, sidebar selection still switches channels (which auto-closes the output via `setChannelHeader`), the input still accepts new commands and chat messages, and key events route to the output viewport only when focus lands on the messages pane.

- Switching channels closes the output (handled by a single line in `setChannelHeader`, which every channel-switch path calls)
- Sending a regular chat message closes the output (the user wants to see their freshly-sent message in the chat)
- Running another command swaps the body in place via `SetTitle` / `SetBody` instead of allocating a fresh view
- Esc on the messages pane closes the output and returns to chat
- Window resize syncs `outputView.SetSize` from `resizeComponents`

The pane's `focused` state is updated by `updateFocus` based on `m.focus == FocusMessages && m.outputActive`, so the active border accurately reflects where keystrokes go.

### Command List overlay (`CommandListModel`)

A full-screen modal opened via `/commands` or the global `Alt-C` shortcut. Embeds `SelectableList`, has a top filter input, and renders each command as `name  description  [emote?]` in two aligned columns. Enter inserts `/<name> ` into the input bar and closes the overlay so the user can fill in arguments before pressing Enter again to run.

### Help system (`/help [topic]`)

`/help` reads from the embedded `internal/commands/help/*.md` files via the `commands.Topic(name)` accessor. Topics: `main`, `commands`, `friends`, `themes`, `setup`, `p2p`, `secure`, `shortcuts`, `debug`. Adding a new topic is just dropping a markdown file in the package directory and rebuilding — no code change needed.

In that second instance set a different P2P Port (e.g. 9901), share its contact card to the primary instance, and you can chat between the two.

## Plugin system

Slackers includes a compiled-in plugin system that lets self-contained features register slash commands, keyboard shortcuts, config fields, and P2P message handlers without touching the core TUI code. Plugins are Go packages compiled into the binary -- there is no dynamic loading -- so they ship with the build and benefit from the same type safety as the rest of the codebase.

### Architecture overview

The plugin system is split across three packages:

```
internal/plugins/        Plugin interface, Manager, manifest/index persistence
internal/api/            API facade (api.API) and sub-interfaces exposed to plugins
internal/api/ui/         UI SDK components (Canvas, VBox, HBox, Label, List)
```

The design principle is **dependency inversion**: plugins depend on stable interfaces (`api.API`, `plugins.Plugin`), never on `tui.Model` or any Bubbletea type. The `api.Host` struct bridges the gap -- it holds pointers to live app services (config, Slack, friends, P2P, commands, shortcuts) and implements every sub-interface by delegating to those services. A `cmdQueue` channel carries plugin-initiated side effects (`StatusBarCmd`, `WarningCmd`) back into the Bubbletea update loop without requiring the plugin to import Bubbletea.

### Plugin interface

Every plugin implements `plugins.Plugin`:

```go
type Plugin interface {
    Manifest() Manifest                         // name, version, author, description
    Init(appAPI api.API) error                  // called on app startup if enabled
    Start() error                               // lazy activation (heavy init goes here)
    Stop() error                                // deactivation
    Destroy() error                             // full uninstall cleanup
    Commands() []*commands.Command              // slash commands to register
    Shortcuts() map[string][]string             // custom keyboard shortcuts
    MessageFilter(senderID, data string) bool   // handle incoming P2P plugin message
    ConfigFields() []ConfigField                // user-editable settings
    SetConfig(key, value string)                // apply a config change
}
```

**Lifecycle stages:**

1. **Register** — `Manager.Register(p)` adds the plugin to the internal map at startup, before any initialization. State: `StateDisabled`.
2. **Init** — `Manager.InitAll(appAPI)` checks the on-disk plugin index (`~/.config/slackers/plugins/plugins.json`). Newly registered plugins are auto-enabled. For each enabled plugin, `Init(appAPI)` is called, giving the plugin access to all app subsystems. State: `StateEnabled`.
3. **Start** — `Manager.Start(name)` activates the plugin's main process (lazy load). State: `StateRunning`. Used for plugins that need a persistent background process.
4. **Stop** — `Manager.Stop(name)` deactivates the running process. State returns to `StateEnabled`.
5. **Destroy** — `Manager.Uninstall(name)` calls `Stop` + `Destroy`, removes the plugin's config directory, and deletes it from the index.

### Plugin Manager

`plugins.Manager` is the central registry and lifecycle controller. Key operations:

- **Register(p Plugin)** — adds a plugin to the map. Called in `NewModel` for each compiled-in plugin.
- **InitAll(appAPI)** — loads the plugin index from disk, initializes all enabled plugins, and persists any index changes (new plugins auto-added).
- **Enable(name) / Disable(name)** — toggles a plugin on or off, persisting the change to `plugins.json`.
- **Uninstall(name)** — full cleanup: stop, destroy, remove config directory, remove from index.
- **RouteMessage(pluginName, senderID, data)** — delivers a P2P plugin message to the named plugin's `MessageFilter`. Returns true if handled.
- **MergeShortcuts(base)** — collects `shortcuts.json` from each enabled plugin's `.config/` directory and merges them into the base shortcut map. Plugin shortcuts are applied BEFORE user overrides, so user settings always win.
- **EnabledPlugins()** — returns all plugins with state >= `StateEnabled`, used by the TUI to collect and register their commands.

The manager also provides `PluginSettings(name)` / `SavePluginSettings(name, settings)` for reading and writing per-plugin `settings.json` files, and `PluginThemeDirs()` for collecting theme directories from enabled plugins.

### Plugin index and config storage

Each plugin gets its own directory under `~/.config/slackers/plugins/<name>/`:

```
~/.config/slackers/plugins/
  plugins.json              # global index: which plugins are enabled
  games/
    .config/
      settings.json         # plugin-specific settings
      shortcuts.json        # plugin-specific shortcut overrides
    themes/                 # optional: plugin-contributed themes
  weather/
    .config/
      settings.json
```

The `PluginIndex` (`plugins.json`) tracks each plugin's enabled state and installation date. It is read at startup and updated when plugins are enabled, disabled, or uninstalled.

### Internal API (`api.API`)

The `api.API` interface is the root facade provided to plugins via `Init()`. It exposes focused sub-interfaces:

| Sub-interface | Purpose |
|---------------|---------|
| `App()` | App lifecycle: version, config read/write, status bar, warnings, connection state |
| `Messages()` | Send, reply, edit, delete, react, fetch history |
| `Channels()` | List, get, hide, unhide, rename, mark/clear unread, select |
| `Friends()` | List friends, check online status, send messages, send plugin messages |
| `Files()` | Upload, download, paths |
| `View()` | Show/close overlays, set focus, screen size |
| `Shortcuts()` | Register/query keyboard shortcuts |
| `Commands()` | Register/run slash commands |
| `Theme()` | Read-only access to current theme colors |
| `Events()` | Pub/sub for app lifecycle events (subscribe, emit) |

The `api.Host` struct implements all of these by delegating to the live services. It uses small wrapper types (`hostApp`, `hostMessages`, etc.) to resolve method name collisions across sub-interfaces. The `Host` is created once in `NewModel` and shared with all plugins.

**Command queue pattern.** Plugins cannot import Bubbletea, so side effects are communicated via a buffered `cmdQueue` channel on the Host. When a plugin calls `api.App().SetStatusBar("...")`, the Host enqueues a `StatusBarCmd`. The Model's update loop drains this queue via `apiHost.DrainCommands()` and processes each item as a `tea.Cmd`.

**Theme state push.** The Host caches theme colors (pushed by the Model after each theme change via `UpdateThemeState`) so plugins can query `api.Theme().Color("primary")` without a circular import on the `tui` package.

### UI SDK (`internal/api/ui`)

The UI SDK provides a small set of components for plugins that need to render custom views:

- **`Component`** — base interface: `ID()`, `Render(w, h)`, `HandleKey(key)`, `SetSize(w, h)`.
- **`Canvas`** — character-addressable grid with per-cell foreground/background colors. Used by the games plugin for pixel-level rendering (snake board, tetris board). Supports `Set(x, y, char, fg, bg)`, `Get(x, y)`, `Clear()`, and renders to a string via `Render()`.
- **`VBox` / `HBox`** — vertical and horizontal layout containers. Add/remove children, automatic size distribution.
- **`Label`** — single-line styled text.
- **`Paragraph`** — multi-line text with word wrapping.
- **`List`** — scrollable, selectable list with item callbacks and configurable styles.
- **`SizePolicy`** — min/max width/height and grow flag for layout negotiation.

The SDK intentionally wraps `lipgloss` for styling rather than exposing raw ANSI, so plugin rendering stays consistent with the app's theme system.

### P2P plugin message routing

Plugins can exchange custom data between friends via the P2P layer. The wire protocol uses `MsgTypePlugin` (`"plugin"`) with two extra fields on `P2PMessage`:

- `PluginName` — identifies the target plugin
- `PluginData` — JSON payload (arbitrary structure, plugin-defined)

**Sending:** A plugin calls `api.Friends().SendPluginMessage(userID, pluginName, data)`, which calls `P2PNode.SendPluginMessage()`, constructing a `P2PMessage{Type: MsgTypePlugin, PluginName: ..., PluginData: ...}` and sending it over the existing libp2p stream.

**Receiving:** The P2P callback in `NewModel` detects `MsgTypePlugin` and dispatches a `P2PReceivedMsg` with the special `"__plugin__"` text marker. The fields are carried on `PubKey` (plugin name) and `Multiaddr` (plugin data), reusing existing message fields to avoid adding new ones. In the `P2PReceivedMsg` handler, the Model checks for the `"__plugin__"` marker and calls `pluginManager.RouteMessage(pluginName, senderID, data)`, which delivers it to the named plugin's `MessageFilter`. If the plugin returns `true`, the message is consumed; otherwise it's silently dropped.

This allows plugins to build multiplayer features (e.g. turn-based games) on top of the existing encrypted P2P transport without any changes to the P2P protocol itself.

### Game overlay system

The games plugin demonstrates a full-screen interactive overlay with exclusive keyboard control. When a game starts via `/games snake` or `/games tetris`, the Model opens `overlayGame` and all key events route to `GameOverlayModel.Update()` instead of the normal TUI handlers.

**Game loop:** A periodic `gameTickMsg` (driven by `tea.Tick`) advances the game state. The tick interval is derived from a configurable speed factor (0.1x to 5.0x). Input is throttled via a pool of 2 pending movement commands -- held keys produce a steady stream tied to the game speed without flooding the event queue.

**In-game settings:** `Ctrl-S` opens a settings menu within the game overlay (board size, speed factor, block scale, halve-vertical compensation). Changes take effect on "Save & Restart". High scores are tracked per game type and persist via `GameSettings` on the Model.

**Background games and taskbar:** When the user presses `Ctrl-Q` during a game, the overlay is hidden rather than destroyed. The game state is saved to `m.backgroundGame`, the game is paused, and the user returns to normal chat. A taskbar indicator appears in the status bar area (top-right of the message pane) showing the paused game name. Clicking the indicator or running `/games` restores the game to the foreground, unpaused, with its full state intact. Running `/games quit` or pressing the Quit option in settings fully destroys the background game.

**Rendering:** Games use the `api/ui.Canvas` component for pixel-level rendering. The snake game renders each cell as one or two characters (depending on the halve-vertical setting), with colored blocks for the snake head, body, food, and walls. The tetris game supports a configurable block scale (1x or 2x) and renders a side panel with score, level, lines, and next piece preview. Both games auto-clamp their logical board dimensions to fit the available terminal space.

### Config merge order

Configuration values from multiple sources are merged in a specific priority order. For keyboard shortcuts:

1. **Embedded defaults** (`internal/shortcuts/defaults.json`, compiled via `go:embed`)
2. **Plugin shortcuts** (each enabled plugin's `.config/shortcuts.json`, merged via `Manager.MergeShortcuts`)
3. **User overrides** (`~/.config/slackers/shortcuts.json`)

Each layer overwrites keys from the previous, so user settings always have the final word. The same pattern applies to plugin-contributed themes (scanned alongside the user's `themes/` directory) and emote definitions (embedded defaults + user custom).

For plugin-specific settings, each plugin owns its own `settings.json` within its config directory. The plugin reads and writes these via `ConfigFields()` / `SetConfig()`, and the Plugin Config overlay (`PluginConfigModel`) provides the user-facing editor.

### Example plugins

**Games** (`internal/plugins/games/`) — Registers the `/games` command (aliases: `/game`, `/play`) with subcommands for `snake` and `tetris`. The command returns a `GameStartRequest{Name}` via the `Result.Cmd` field, which the Model intercepts to open the game overlay. No config fields, no shortcuts, no P2P message handling. The snake and tetris implementations are self-contained game engines using `api/ui.Canvas` for rendering.

**Weather** (`internal/plugins/weather/`) — Registers `/weather [city]` (alias: `/wttr`). Fetches forecasts from `wttr.in` using the compact format for a summary line, then the ASCII-art `?T&n&q` format for a detailed 3-day forecast. Results are displayed in the Output view. Provides one config field (`city` -- default location) and one custom shortcut (`show_weather` bound to `Ctrl+W`). The city is remembered across invocations within a session.

### Plugin management UI

The **Plugin Manager** overlay (`Alt-P` or `/plugins`) shows a table of all registered plugins with name, version, author, and current state (enabled/disabled/running). From here users can:

- **Enter** — open the plugin's config screen (fields from `ConfigFields()`)
- **e** — toggle enable/disable
- **d** — uninstall with confirmation prompt

The **Plugin Config** overlay shows the plugin's metadata and a list of editable settings fields. Each field has a label, current value, and description. Enter starts editing, Enter again saves via `SetConfig()`.

## Hide Online Status

The "Hide Online Status" feature allows users to appear permanently offline to friends while retaining full functionality -- chat, file transfer, and all P2P features continue to work normally. Only the status broadcast is suppressed.

### Configuration

The setting is exposed in Settings as a toggle (`on`/`off`) under the Friends section:

- **Config key:** `hide_online_status` in `config.json`
- **Default:** `off`
- **Description:** "Always appear offline to friends (chat still works, only status is hidden)"

### Broadcast behaviour

When the setting is toggled, the `hideOnlineStatusChangedMsg` is dispatched to the Model, which triggers an immediate broadcast:

- **Enabling (hidden):** A one-shot `BroadcastStatus("offline", "")` is sent to all connected friends, then all further status broadcasts are suppressed.
- **Disabling (visible):** A `BroadcastStatus("online", "")` is sent to resume normal visibility.

The suppression is implemented in `effectiveStatus()` on the Model. Every status broadcast path (ping responses, status updates, away/back transitions) calls this function first. When `cfg.HideOnlineStatus` is `true`, it returns `suppress: true` and the caller skips the broadcast entirely.

### Interaction with other features

- **Away status:** If the user has both `HideOnlineStatus` and a manual away status set, the hide takes precedence -- no broadcasts are sent at all.
- **Ping cycle:** The friend ping cycle still checks connectivity for the local user's benefit (updating the sidebar's online/offline indicators for friends), but the status response to the remote peer is suppressed.
- **Friend store:** The `AwayStatus` field on remote friend records can be `"offline"` when the remote friend has hidden their status. The ping cycle respects this -- even if the transport connection succeeds, a friend whose `AwayStatus` is `"offline"` is treated as offline for sidebar display purposes.