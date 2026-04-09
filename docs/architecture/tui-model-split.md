# TUI model split (post–Phase C)

`internal/tui/model.go` used to be ~6,200 lines with 88 top-level functions. Phase C (landed in commits `d3a0906` → `1b296ce`) split it into focused files. This doc is the map of *where things live now* so you don't have to re-derive it each session.

## Files and ownership

| File | Lines (approx) | Owns |
|---|---|---|
| `model.go` | ~4,500 | `Model` struct, `NewModel`, `Init`, top-level `Update`, `View`, overlay dispatch switch, shared helpers that touch too many subsystems to live elsewhere. |
| `handlers_slack.go` | ~510 | Slack + shared "message actions": edit / delete / react / notif-store helpers / `channelByID`. Routes between Slack and P2P per active chat. |
| `handlers_p2p.go` | ~635 | Friend add / remove / rename / handshake, P2P send + receive, friend-card click flow, pending-message resend, profile sync. |
| `handlers_ui.go` | ~470 | Overlay open/close, focus cycling, sidebar drag, window resize, status bar updates. |
| `cmds.go` | — | Background `tea.Cmd` constructors (poll ticks, friend pings, timers, one-shot re-connect helpers). |
| `persist.go` | ~41 | Save/load helpers that cross `config` + `friends` + `notifications` stores. |
| `messages.go` | ~2,400 | `MessageViewModel` — render pipeline, formatted-text cache, hit-tests, file rows, reactions, reply rendering, friend-card collapse/rewrite. |
| `channels.go` | ~790 | `ChannelListModel` — sidebar groups, rename/alias, sort, drag resize, friend section at top, collapse state. |
| `input.go` | ~500 | Multi-line textarea, normal/edit modes, history, Konsole `\eOM` detection. |

## Shared primitives (use these, don't reinvent)

- **`selectablelist.go`** — `SelectableList` struct with `HandleKey`, `Navigate`, `SetCount`, `Home`/`End`/`PageUp`/`PageDown`, `Current`. Already used by `hidden.go` and `notifications_overlay.go`; the remaining six overlays (`shortcutseditor`, `settings`, `friendsconfig`, `fileslist`, `friendrequest`, `themes_ui`) still have their own cursor code and are candidates for migration.
- **`overlayscaffold.go`** — `OverlayScaffold{Title, Footer, Width, Height, MaxBoxWidth}.Render(body)`. Use for new modal overlays.
- **`overlayhelpers.go`** — `OverlayBox`, `OverlayBoxSized` for overlays that need custom body preprocessing before boxing (see `about.go`).
- **`styles.go`** — all themed styles hoisted to package level, rebound by `rebuildDerivedStyles()`. Named colors: `ColorKeyBindText`, `ColorDescText`, `ColorStatusOn`, `ColorUnread`, etc. Footer hint constants: `HintSep`, `FooterHintClose`, `FooterHintBack`, `FooterHintCancel`.

## Conventions when adding code

1. **New Slack message action?** → `handlers_slack.go`.
2. **New P2P / friend action?** → `handlers_p2p.go`.
3. **New overlay open/close path?** → `handlers_ui.go`.
4. **New background tick / timer?** → `cmds.go`.
5. **New overlay file?** → follow `hidden.go` as a template; embed `SelectableList`, render via `OverlayScaffold`, add overlay constant to `model.go` enum.
6. **New style or color?** → add to `styles.go`, wire through `rebuildDerivedStyles()`. Never inline `lipgloss.Color("229")` in a call site.
7. **New footer hint?** → use the `HintSep` separator and the canonical `"Key: action"` format. Pick `close` / `back` / `cancel` per overlay type.
8. **Don't let `model.go` grow back.** If you find yourself adding 100+ lines to `model.go` for a single feature, it probably belongs in one of the `handlers_*.go` files or a new overlay file.
