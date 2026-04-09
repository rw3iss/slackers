# Architecture overview

Snapshot of the slackers codebase as of v0.20.0. For design rationale (why decisions were made this way), see `How_It_Works.md` at the repo root.

## Elm / Bubbletea root

One root `Model` in `internal/tui/model.go` holds all app state. Every user event becomes a `tea.Msg`, the single `Update` function maps `(Model, Msg) → (Model, Cmd)`, and `View` renders the frame. There is no mutation outside `Update`.

Sub-models compose the root Model and each own an independent `Update` / `View`:

```
Model (internal/tui/model.go)
├── ChannelListModel       sidebar, groups, drag resize, sorting
├── MessageViewModel       scrollable viewport, format cache, hit-tests
├── InputModel             multi-line textarea, history, normal/edit modes
├── KeyMap                 dynamic bindings from shortcuts.Store
├── overlays:              one per file in internal/tui/
│   ├── SettingsModel
│   ├── SearchModel
│   ├── MsgSearchModel
│   ├── FileBrowserModel / FilesListModel
│   ├── ShortcutsEditorModel
│   ├── FriendsConfigModel (state machine across 6 sub-pages)
│   ├── ThemePickerModel / ThemeEditorModel / ThemeColorPickerModel
│   ├── NotificationsOverlayModel
│   ├── HelpModel / HiddenChannelsModel / RenameModel / WhitelistModel
│   ├── MsgOptionsModel / SidebarOptionsModel (right-click menus)
│   └── FriendRequestModel / EmojiPickerModel / AboutModel / SplashModel
└── services & stores
    ├── slackSvc / socketSvc (interfaces, may be nil in friends-only mode)
    ├── friendStore
    ├── notifStore
    ├── p2pNode (libp2p)
    └── cfg (debounced save)
```

## Package responsibilities

| Package | Purpose | Key types |
|---|---|---|
| `cmd/slackers` | Cobra CLI, process entry, auto-update | `rootCmd` + subcommands |
| `internal/auth` | OAuth2 browser flow | `Flow`, `VerifyTeam` |
| `internal/backup` | Export/import of `$XDG_CONFIG_HOME/slackers` as a zip | `Export`, `Import(mode)` |
| `internal/config` | JSON config at `config.json` with debounced saves and 0600 perms | `Config`, `Save`, `DebouncedSave` |
| `internal/debug` | Lazy debug log sink | `Init`, `Logf`, `Close` |
| `internal/format` | Slack mrkdwn → styled terminal text, emoji rendering | `FormatMessage` |
| `internal/friends` | FriendStore, contact cards (JSON + SLF2 hash), chat history | `FriendStore`, `ContactCard`, `ChatHistoryStore` |
| `internal/notifications` | Persistent notifications store | `Store`, `Entry`, `TypeUnreadMessage` / `TypeReaction` / `TypeFriendRequest` |
| `internal/secure` | X25519 + ECDH + HKDF + ChaCha20-Poly1305, libp2p P2P node | `KeyPair`, `Session`, `P2PNode` |
| `internal/shortcuts` | Embedded defaults + user overrides, builds `key.Binding`s | `Store`, `BuildKeyMap` |
| `internal/slack` | Slack SDK wrappers behind `SlackService` / `SocketService` interfaces | `Client`, `Socket`, `tryWithFallback` |
| `internal/theme` | 11 embedded themes + custom themes dir + color parser | `Theme`, `Load`, `Builtin` |
| `internal/tui` | All UI — see [tui-model-split.md](tui-model-split.md) | `Model`, sub-models, overlays |
| `internal/types` | Shared domain types used across packages | `Channel`, `Message`, `User`, `FileInfo`, `ConnectionStatus` |

## Data flow (receive path)

```
┌──────────────┐  WebSocket  ┌──────────────┐
│  Slack API   ├────────────▶│ SocketService│  real-time events
└──────────────┘             └──────┬───────┘
        ▲                           │ SlackEventMsg
        │ HTTP polls                ▼
        │                    ┌──────────────┐
        └────────────────────┤   Model      │   single Update loop
                             │  (tui/)      │
                             └──────┬───────┘
                                    │ View
                                    ▼
                             ┌──────────────┐
                             │  Terminal    │
                             └──────────────┘
                                    ▲
┌──────────────┐   P2PReceivedMsg   │
│  libp2p P2P  ├────────────────────┘
└──────────────┘
```

Polling runs on two tickers — primary (current channel, default 10s) and background (5 channels per cycle, default 30s). Hidden channels are excluded from rotation. See `How_It_Works.md` → "Real-time message delivery" for rate-limit arithmetic.

## Extension points

- **New overlay:** add `<name>.go` in `internal/tui/`, embed `SelectableList` if it's a list, render through `OverlayScaffold`, add an `overlay<Name>` constant in `model.go`'s enum, dispatch in `handlers_ui.go` open/close routing.
- **New settings field:** add to `config.Config`, surface in `settings.go` under the correct section, use debounced save.
- **New Slack API call:** add to the `SlackService` interface in `internal/slack/`, implement via `tryWithFallback`, consume from `handlers_slack.go`.
- **New P2P message type:** add to the `MsgType*` constants in `internal/secure/p2p.go`, extend `P2PNode` stream handler, consume in `handlers_p2p.go`.
- **New theme color:** add named color to `styles.go`, wire into `rebuildDerivedStyles()`, reference from call sites (no magic colors).
