# CLAUDE.md — slackers

Terminal-based Slack client in Go with an optional libp2p peer-to-peer friends layer. TUI is built on [Bubbletea](https://github.com/charmbracelet/bubbletea) (Elm architecture).

Project version: **0.23.0** · Go: **1.25.7** · entry: `cmd/slackers/main.go`

## Quick commands

```bash
make build         # → build/slackers
make run           # build and launch the TUI
make install       # installs to ~/.local/bin via scripts/install.sh
make test          # go test ./...
make lint          # go vet ./...
make build-all     # cross-compile linux/amd64, darwin/arm64, windows/amd64
./build/slackers --debug     # tail ~/.config/slackers/debug.log in another shell
```

There is **no separate formatter step** — `go vet` is the only lint gate. No golangci-lint config is committed.

## ⚠️ Always build AND install after any change

After **every** code edit, run `make build && make install` — not just `make build`. The user launches slackers via `slackers` on `$PATH`, which resolves to `~/.local/bin/slackers` (placed there by a prior `make install`). `make build` only updates `./build/slackers`, so without the install step the user is still running the stale binary and any debugging / testing they do reflects old code.

Symptoms of forgetting this: the user reports your changes "don't work" or "have no effect", debug logs you added never appear, and `ls -la build/slackers ~/.local/bin/slackers` shows the installed copy is older than the build. Don't skip the install unless the user explicitly says they're running `./build/slackers` directly.

Also remind the user to quit (Ctrl-Q) and relaunch any running slackers process — disk replacement doesn't hot-reload an already-running binary.

## Architecture at a glance

Elm architecture via Bubbletea: one root `Model` (`internal/tui/model.go`) owns all state, `Update` handles all `tea.Msg`s, and `View` renders the terminal frame. No mutation happens outside the update loop.

```
cmd/slackers/main.go       Cobra CLI: root, login, join, setup, update, export,
                           import, import-friend, import-theme, config, friends,
                           version. Also hosts resetTerminal(), auto-update logic.
internal/
  auth/          OAuth2 browser flow, team-ID verification.
  backup/        Export/import the whole ~/.config/slackers dir as a zip.
  config/        Single JSON at ~/.config/slackers/config.json (0600 perms).
                 DebouncedSave() coalesces rapid settings changes into one write.
  debug/         --debug log sink (no overhead when disabled).
  format/        Slack mrkdwn → styled terminal text, emoji rendering.
  friends/       Friend store, contact cards (JSON + SLF2 hash), chat history
                 persistence (encrypted per-friend files).
  notifications/ Persistent notifications store (unread / reaction / friend req).
  secure/        X25519 keys, ECDH+HKDF+ChaCha20-Poly1305, libp2p P2P node.
                 P2PNode owns peer discovery, streams, file transfer, pings.
  shortcuts/     Customisable keybindings; defaults.json is go:embedded.
  slack/         SlackService + SocketService interfaces; Slack SDK wrappers.
  theme/         11 built-in themes (go:embed) + custom themes dir.
  tui/           All UI: root Model, overlays, panels, rendering.
  types/         Shared domain types: Channel, Message, User, etc.
```

### TUI file layout (post-Phase-C split)

`internal/tui/model.go` was split in Phase C (commits `47a9221`, `c505e26`, `17cdd2e`, `7f926f1`). Keep the split intact when adding new code:

| File | Owns |
|------|------|
| `model.go` (~4.5k lines) | `Model` struct, `NewModel`, `Init`, top-level `Update`, `View`, overlay dispatch |
| `handlers_slack.go` | Message actions (edit/delete/react), notif-store helpers, channel lookup |
| `handlers_p2p.go` | Friend add/remove/rename, P2P send/receive, friend-card click flow |
| `handlers_ui.go` | Overlay open/close, focus cycling, sidebar drag, window resize |
| `cmds.go` | Background `tea.Cmd` constructors (poll ticks, pings, timers) |
| `persist.go` | Save/load state helpers that straddle config + friends + notifs |
| `messages.go` (~2.4k) | `MessageViewModel` — rendering, scroll, hit-tests, cached format |
| `channels.go` (~800) | `ChannelListModel` — sidebar groups, sorting, drag resize |
| `input.go` (~500) | Multi-line textarea with normal/edit mode toggle |
| `selectablelist.go` | Shared cursor/nav primitive — use for new list overlays |
| `overlayscaffold.go` / `overlayhelpers.go` | `OverlayBox`, `OverlayScaffold` — use these instead of inline `lipgloss.Place` |
| `styles.go` | All themed styles. Theme changes flow through `rebuildDerivedStyles()`. |

Overlays (each one file): `about`, `emojipicker`, `filebrowser`, `fileslist`, `friendrequest`, `friendsconfig`, `help`, `hidden`, `msgoptions`, `msgsearch`, `notifications_overlay`, `rename`, `search`, `settings`, `shortcutseditor`, `sidebaroptions`, `splash`, `themes_ui`, `whitelist`.

## Non-obvious conventions (read before editing)

- **Token fallback pattern.** Slack calls prefer the user token (`xoxp-`); on failure they retry with the bot token via `tryWithFallback()` in `internal/slack/client.go`. Never call the SDK directly from `tui/`.
- **Don't import the Slack SDK from `tui/`.** TUI depends on the `SlackService` / `SocketService` interfaces. Keeping this boundary lets the services be swapped out (see friends-only mode where they're nil).
- **Friends-only mode.** If `config.Validate()` fails but `friends.json` has entries, the app launches with `m.slackSvc == nil` / `m.socketSvc == nil`. Every Slack-dependent command in `Init()` and elsewhere must nil-check these before dispatching.
- **Debounced saves.** `config.Save` and `notifications.Store.Save` are debounced — do not add synchronous calls in hot paths (keystrokes, per-frame updates). Use the debounced entry points. The perf-audit pass fixed four direct `_ = config.Save(m.cfg)` leaks; don't reintroduce them.
- **Formatted-text cache.** `format.FormatMessage` is expensive (multi-pass mrkdwn regex). Cached on `SetMessages` / `AppendMessage` / `EditMessageLocal`. If you add a new message-mutation path, invalidate the cache or the UI will render stale text.
- **Friend marker two-pass pipeline.** `[FRIEND:<blob>]` tokens are collapsed to `[FRIEND:#fc-N]` refs **before** word-wrap (so long JSON blobs survive wrapping), then rewritten into clickable pills at render time per visual line. See `messages.go` `collapseFriendMarkers` / `rewriteFriendCards`. Don't touch one without the other.
- **Outgoing friend-message ordering.** Pending friend resends use `tea.Sequence`, not batched sends — each `FriendSendResultMsg` must be processed before the next send starts, otherwise reconnect-replay can deliver out of order.
- **Shift-Enter on Konsole.** Konsole emits `\eOM` instead of the standard sequence. `input.go` detects the two-part `Alt+O` + `M` pattern. Test any input key-handling changes on both Konsole and a VT100-style terminal.
- **Footer hint format.** Canonical: `"Key: action · Key: action · Esc: close"` using `HintSep = " · "` and the `FooterHintClose` / `FooterHintBack` / `FooterHintCancel` constants from `styles.go`. Use **close** for leaf overlays, **back** for nested, **cancel** only for destructive confirmations.
- **No magic colors outside `styles.go`.** Use `ColorKeyBindText` (229), `ColorDescText` (252), `ColorStatusOn` (`#00ff00`) or add a new named color in `styles.go` and wire it into `rebuildDerivedStyles()`.
- **New list overlays** should embed `SelectableList` rather than reimplementing `selected int` + bounds clamp + up/down/pgup/pgdn.
- **New modal overlays** should render via `OverlayScaffold` (or the `OverlayBox` / `OverlayBoxSized` helpers for overlays that need custom centring first).
- **`staticcheck U1000` is clean.** Don't add dead code; if you do, the dead-code audit will catch it (see `docs/archive/dead-code-audit-2026-04-08.md`).

## Config & runtime state

Everything under `$XDG_CONFIG_HOME/slackers/` (Linux: `~/.config/slackers/`):

- `config.json` — tokens, settings, sort order, theme, shortcuts overrides merge target (0600)
- `shortcuts.json` — user shortcut overrides (merged over embedded `internal/shortcuts/defaults.json`)
- `friends.json` — friend list
- `friend_history/` — per-friend encrypted chat history
- `themes/` — user theme JSON files
- `secure.key` — local X25519 keypair (never log or display)
- `notifications.json` — persistent notifications store
- `debug.log` — `--debug` output only

For a second test instance on the same machine, run with `XDG_CONFIG_HOME=/tmp/slackers-test slackers` and a different `P2P Port` in Settings.

## Testing the P2P stack locally

`internal/secure/secure_test.go` and `internal/friends/friends_test.go` exercise the crypto + friend-store primitives. For manual end-to-end P2P testing, launch a second instance with its own XDG_CONFIG_HOME and a different P2P port, then exchange contact cards between them. See `docs/architecture/p2p-testing.md`.

## When picking up work

1. Read `README.md` for the user-facing feature surface.
2. Read `How_It_Works.md` (extensive) for design rationale.
3. Check `docs/architecture/` for current architecture snapshots.
4. Check `docs/features/` for feature-specific deep dives.
5. `docs/archive/` contains landed audit + plan documents — good context for *why* things are shaped the way they are, but they describe past work, not current tasks.
6. `_todo.md` lists open feature ideas (typing indicators, statuses, games, private group chats, workspaces).

## Security warning

`_info.md` in the repo root currently contains live Slack credentials (client secret, user OAuth token, bot OAuth token, signing secret). **Treat those as compromised**, rotate them in the Slack app admin, and gitignore or remove the file. Never commit `_info.md` or anything derived from it.
