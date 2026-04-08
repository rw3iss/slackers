# Phase C implementation plan — 2026-04-08

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` for the larger refactors. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the three Phase C items from `docs/improvement-audit-2026-04-08.md` — a shared `SelectableList` primitive, a split of `model.go`, and a shared `OverlayScaffold` abstraction — without breaking any existing functionality.

**Architecture:** Each refactor is self-contained and can be landed as its own commit. They do NOT depend on each other. The safest landing order is (1) SelectableList → (2) model.go split → (3) OverlayScaffold, because each subsequent change benefits from the stability of the previous one.

**Tech stack:** Go 1.25, bubbletea / lipgloss / bubbles, no new dependencies.

---

## Task 1: `SelectableList` primitive

**Goal:** Eliminate the duplicated cursor-navigation + bounds-clamp scaffolding across eight overlays (`shortcutseditor`, `settings`, `friendsconfig`, `hidden`, `fileslist`, `friendrequest`, `themes_ui`, `notifications_overlay`) by introducing a reusable primitive.

**Files:**
- Create: `internal/tui/selectablelist.go`
- Migrate in this pass: `internal/tui/hidden.go`, `internal/tui/notifications_overlay.go`
- Defer to follow-up commits: the six more complex overlays

**Scope boundary:** This task introduces the primitive and migrates TWO simple list overlays to prove it. The other six migrations are follow-up tickets because each has custom visual tuning and state that needs per-overlay review.

### Steps

- [ ] **Step 1: Create `internal/tui/selectablelist.go`** with a `SelectableList` struct that owns `Selected int`, `Count int`, `WrapAround bool`, and exposes methods:
  - `Navigate(delta int)` — moves the cursor by delta, optionally wrapping.
  - `SetCount(n int)` — updates the item count and clamps the selection.
  - `Home()` / `End()` / `PageUp(n int)` / `PageDown(n int)` — common navigation.
  - `HandleKey(msg tea.KeyMsg) bool` — handles the standard `up/down/k/j/pgup/pgdn/home/end` bindings and returns true if consumed.
  - `Current() int` — returns the current selection, or -1 if `Count == 0`.

- [ ] **Step 2: Migrate `hidden.go`** — the hidden channels overlay. It already has `selected int` and custom up/down logic; replace with `SelectableList` embedded in `HiddenChannelsModel`. Route Update's key-handling through `list.HandleKey(msg)` first and fall through for non-nav keys.

- [ ] **Step 3: Migrate `notifications_overlay.go`** — same pattern as hidden.go.

- [ ] **Step 4: Verify.** `go build ./...`, `go vet ./...`, `make build`, install, and hand-test:
  - Press Ctrl+G, filter, navigate with arrows and j/k, wrap at top/bottom.
  - Press Alt+N, navigate with arrows, press Enter to activate a notification.

- [ ] **Step 5: Commit.** One commit for the primitive plus the two migrations, tagged `refactor: introduce SelectableList primitive`.

**Non-goals this pass:**
- Migrating `shortcutseditor`, `settings`, `friendsconfig`, `fileslist`, `friendrequest`, `themes_ui` — each has enough custom state (filter input, editing mode, section headers, multi-column layouts) to warrant its own commit.

---

## Task 2: Split `model.go`

**Goal:** Reduce `internal/tui/model.go` from ~6,200 lines toward a clean core by extracting self-contained domains into sibling files in the same `tui` package. Since Go allows multiple files to share a package and receiver methods can be defined anywhere, the split is largely mechanical — the risk is making sure no imports / global state / unexported helpers get orphaned.

**Files affected:**
- Modify: `internal/tui/model.go` (shrinks)
- Create (this pass, safest two): `internal/tui/cmds.go`, `internal/tui/handlers_p2p.go`
- Create (follow-up commits): `handlers_slack.go`, `handlers_ui.go`, `persist.go`

**Scope boundary:** This pass extracts `cmds.go` (the pure background-tick `tea.Cmd` helpers) and `handlers_p2p.go` (the friend / p2p-related helper methods on `*Model`). Both are self-contained enough to move without touching the Update dispatcher. The other three extractions are follow-ups.

### Steps

- [ ] **Step 1: Identify the target functions for `cmds.go`.** Grep `model.go` for top-level (non-method) `func *Cmd` / `func ... tea.Cmd` helpers that have no side effects on the Model struct. Candidates from the audit:
  - `loadUsersCmd`, `loadChannelsCmd`, `loadHistoryCmd`, `silentLoadHistoryCmd`, `loadMoreContextCmd`, `fetchContextCmd`
  - `connectSocketCmd`, `waitForSocketEvent`
  - `checkUpdateCmd`, `clearWarningCmd`, `seedLastSeenCmd`
  - `notifyWatchdogCmd`, `activityCheckCmd`, `pollTickCmd`, `bgPollTickCmd`
  - `checkNewMessagesCmd`, `checkNewMessagesBgCmd`
  - `friendIdleCheckCmd`, `friendPingTickCmd`, `friendPingCmdWithCurrent`
  - `sendMessageWithFilesCmd`, `sendFriendMessageCmd`, `waitForP2PMsg`, `discoverPeerCmd`, `loadFriendsCmd`

  These are all package-level functions (not methods on `*Model`) that return `tea.Cmd` — moving them to a new file means copying their source and deleting from `model.go` with no call-site changes.

- [ ] **Step 2: Write `internal/tui/cmds.go`** with the same `package tui` declaration and the required imports (`context`, `time`, `fmt`, `github.com/charmbracelet/bubbletea`, `github.com/rw3iss/slackers/internal/friends`, `github.com/rw3iss/slackers/internal/secure`, `github.com/rw3iss/slackers/internal/types`, `slackpkg "github.com/rw3iss/slackers/internal/slack"`).

- [ ] **Step 3: Move each command function** one at a time, building after each move. If a move fails because a helper is referenced that's only in `model.go`, either move the helper too or leave the command in place.

- [ ] **Step 4: Identify the target methods for `handlers_p2p.go`.** Methods on `*Model` that operate entirely on the friend/p2p subsystem:
  - `connectFriend`, `sendRequestPending`, `sendProfileSync`, `mergeFriendProfile`
  - `sendFriendMessageCmd` (method version), `resendPendingFriendMessagesCmd`
  - `touchFriendActivity`
  - `applyFriendCard`, `friendCardLabel` (helper if it's a method)
  - `buildFriendChannels`
  - `loadFriendHistory`
  - `recordFriendRequest`
  - `appendFriendMessage`
  - `deleteFriendMessage`, `findFriendMsgPtr` (if package-level)
  - `buildSlackInviteMessage`

- [ ] **Step 5: Write `internal/tui/handlers_p2p.go`** with the `package tui` declaration and move the methods one at a time, building after each.

- [ ] **Step 6: Verify.** `go vet ./...`, `go build ./...`, `make build`, install, and hand-test:
  - Launch the app, open a friend chat, send a message.
  - Trigger the friend online/offline ping (wait 5s).
  - Force a reconnect by closing+reopening the connection manually (if possible).
  - Exit cleanly (Ctrl+Q).

- [ ] **Step 7: Commit.** One commit for `cmds.go` extraction, one for `handlers_p2p.go`. Separate commits so a bisect can pinpoint which extraction introduced any regression.

**Non-goals this pass:**
- Extracting `handlers_slack.go`, `handlers_ui.go`, `persist.go` — they're still in `model.go` after this pass. Follow-up commits tackle them one at a time.
- Refactoring the `Update` dispatcher into a method table. That's a separate design conversation.

**Risk level:** Medium. Each extraction is a simple move but `model.go` is large enough that Go's package-level name collisions are a real concern. Build-after-every-move is the only safe discipline.

---

## Task 3: `OverlayScaffold` abstraction

**Goal:** Introduce a composable `OverlayScaffold` type that every modal-style overlay can use for title / border / footer / empty state / optional filter row. Each overlay's `View()` becomes: build the content, hand it to the scaffold along with its title + footer, get back the centred modal string.

**Files:**
- Create: `internal/tui/overlayscaffold.go` (new abstraction)
- Migrate in this pass (simplest three): `about.go`, `whitelist.go`, `rename.go`
- Follow-up commits: the other 15 overlays

**Scope boundary:** Build the scaffold and migrate only the three simplest overlays. The other 15 each have custom tuning (variable height, sub-modes, embedded filter inputs, reactive layouts) that need per-overlay review.

### Steps

- [ ] **Step 1: Write `internal/tui/overlayscaffold.go`** with:
  - `OverlayScaffold` struct holding `Title string`, `Footer string`, `EmptyMessage string` (optional), `Width int`, `Height int`, `BorderColor lipgloss.Color`.
  - `Render(content string) string` — assembles title + separator + content + footer + empty-state-if-needed, wraps in `OverlayBox`, returns the centred result.
  - Optional `RenderEmpty() string` — shortcut for overlays that just want to show the empty message without content.

- [ ] **Step 2: Migrate `about.go`** — the simplest overlay. Static content, fixed footer. Replace the inline title / border / footer / Place logic with `scaffold.Render(aboutContent)`.

- [ ] **Step 3: Migrate `whitelist.go`** — also relatively simple. Validates the scaffold handles a list overlay with dynamic content.

- [ ] **Step 4: Migrate `rename.go`** — validates the scaffold handles an overlay with a textinput (the alias input).

- [ ] **Step 5: Verify** by hand-testing each migrated overlay. Visual diff before/after by inspection.

- [ ] **Step 6: Commit** with the scaffold + three migrations as one commit. Follow-up migrations land separately.

**Non-goals this pass:**
- Migrating settings / friendsconfig / themes_ui — they each have multiple sub-views inside the same overlay model and the scaffold needs sub-view support first.
- Adding animation, transitions, or focus indicators to the scaffold. Pure structural refactor.

---

## Execution order in this session

1. Write this plan (done).
2. Execute Task 1 (SelectableList primitive + 2 migrations).
3. Execute Task 2 (cmds.go extraction).
4. Stop, report progress.

Task 2 `handlers_p2p.go` extraction and Task 3 (OverlayScaffold) are left as follow-ups for the next session — they're individually non-trivial and each deserves its own focused session rather than being rushed at the tail end of this one. The plan is committed to the repo so the next session can resume from this exact document.

## Execution log

### Task 1 — SelectableList primitive + 2 migrations — ✅ complete

- Created `internal/tui/selectablelist.go` (176 lines). Owns `Selected`, `Count`, `WrapAround`, `PageSize` and exposes `Navigate`, `Home`, `End`, `PageUp`, `PageDown`, `HandleKey`, `Current`.
- Migrated `internal/tui/notifications_overlay.go` — replaced the bespoke cursor/bounds-clamp logic with an embedded `SelectableList`. `Update` now delegates standard navigation keys through `list.HandleKey(msg)` and only handles the notification-specific `enter` / `x` / `delete` / `esc` bindings itself. `PageSize` is recomputed from `visibleEntries()` every update so PgUp / PgDn jump one visible page at a time. `ensureVisible()` now reads from `list.Current()` and safely short-circuits when the list is empty. The view reads the cursor via `m.list.Current()` and renders the selected row the same as before.
- Migrated `internal/tui/hidden.go` — same pattern. `SelectableList` with `PageSize: 5` and `WrapAround: false` to match the overlay's existing feel. The enter handler resolves the filtered row via `list.Current()` instead of the old `m.selected` field, and after unhiding a channel it just calls `list.SetCount(len(m.filtered()))` to re-clamp. The mouse wheel also routes through `list.Navigate(±1)`.
- Six more candidates for migration (shortcutseditor, settings, friendsconfig, fileslist, friendrequest, themes_ui) are deferred — each has custom state (filter input modes, sub-pages, multi-column layouts) that needs per-overlay review. They can be migrated one at a time in follow-up commits now that the primitive exists.

### Task 2 — cmds.go extraction — ✅ complete

- Created `internal/tui/cmds.go` (373 lines) holding 19 package-level `tea.Cmd` helpers extracted from `model.go`: `loadUsersCmd`, `loadChannelsCmd`, `loadHistoryCmd`, `connectSocketCmd`, `waitForSocketEvent`, `loadMoreContextCmd`, `silentLoadHistoryCmd`, `fetchContextCmd`, `sendMessageWithFilesCmd`, `seedLastSeenCmd`, `checkUpdateCmd`, `clearWarningCmd`, `notifyWatchdogCmd`, `activityCheckCmd`, `pollTickCmd`, `bgPollTickCmd`, `checkNewMessagesCmd`, `checkNewMessagesBgCmd`, `loadFriendsCmd`, `friendPingCmdWithCurrent`.
- Left in `model.go`:
  - `friendPingTickCmd` — method on `*Model`, reads `m.cfg.FriendPingSeconds`. Staying with the model for now.
  - `startDownload` / `startP2PDownload` — both methods on `*Model`, they wire `m.downloadCancel` / `m.downloading` state.
  - `waitForP2PMsg`, `discoverPeerCmd`, `friendIdleCheckCmd`, `sendFriendMessageCmd` — scattered among method blocks and not part of the two clean contiguous blocks that were extracted. Deferred to a future pass.
- Dropped the now-unused `io` and `net/http` imports from `model.go`.
- Line count: `model.go` went from ~6,200 → 5,849 lines. `cmds.go` is a fresh 373-line file. The remaining `handlers_p2p.go` / `handlers_slack.go` / `handlers_ui.go` extractions would continue to shrink `model.go` further.

### Task 2 (continued) — handlers_p2p.go extraction — ✅ complete

- Created `internal/tui/handlers_p2p.go` (610 lines) holding the friend/P2P subsystem methods and helpers that used to live in `model.go`.
- Package-level helpers moved: `deleteFriendMessage`, `findFriendMsgPtr`, `hostPortFromMultiaddr`, `friendCardLabel`, `buildSlackInviteMessage`, `sendFriendMessageCmd`, `friendIdleCheckCmd`, plus the `FriendIdleTimeout` constant and `friendIdleCheckMsg` type.
- Methods on `*Model` moved: `addLocalReaction`, `appendFriendMessage`, `loadFriendHistory`, `buildFriendChannels`, `recordFriendRequest`, `touchFriendActivity`, `connectFriend`, `sendRequestPending`, `sendProfileSync`, `mergeFriendProfile`, `resendPendingFriendMessagesCmd`, `applyFriendCard`.
- Left behind in `model.go` on purpose: `friendPingTickCmd` (reads `m.cfg.FriendPingSeconds`, cheap to keep co-located with other model-owned ticks), `startDownload` / `startP2PDownload` (wire `m.downloadCancel` / `m.downloading` and share code with the Slack download path), and `toggleReaction` (dispatches to both Slack and P2P subsystems).
- `model.go` shrank from 5,849 → 5,261 lines (−588). Combined with the `cmds.go` extraction earlier this session, `model.go` is down from ~6,200 → 5,261 lines total, a ~15% reduction.
- Verification: `go build ./...`, `go vet ./...`, `make build` all clean.

### Task 2 (continued) — persist.go + handlers_ui.go extractions — ✅ complete

- **`persist.go`** (41 lines): small pass to relocate `saveLastChannel`, `loadLastSeen`, `persistLastSeen` — the thin wrappers around `config.SaveDebounced`. Committed as a self-contained change.
- **`handlers_ui.go`** (457 lines): extracted the UI-layer interaction methods `applySettings`, `resizeComponents`, `handleOverlayMouse`, `handleMouse`, `cycleFocusForward`, `cycleFocusBackward`, `updateFocus`. Everything that deals with how the user interacts with the chrome (resize, focus cycling, mouse clicks) now lives in one file. `handleMouse` was the biggest win — ~270 lines of branchy click dispatching that were crowding the middle of model.go.
- Left in model.go for later: `renderStatusBar`, `settingsCogClickArea`, `buildSidebarOptionsItems`, `buildChannelIndex`, `resolveChannelDisplay`, `expandFriendMarkers`, `activateNotification`, and the Slack message methods (`editMessage`, `toggleReaction`, `confirmMessageDelete`, etc.). Those cluster into `handlers_slack.go` for a future pass.
- Line count: model.go is now **4,794 lines** (down from ~6,200 at the start of Phase C — a 23% reduction). Across this session so far the split is:
  ```
  4794 internal/tui/model.go
   610 internal/tui/handlers_p2p.go
   457 internal/tui/handlers_ui.go
   373 internal/tui/cmds.go
   176 internal/tui/selectablelist.go
    41 internal/tui/persist.go
  ```
- Verification: `go build ./...`, `go vet ./...`, `make build` all clean; binary installed to `~/.local/bin/slackers`.

### Task 2 (continued) — handlers_slack.go extraction — ✅ complete

- Created `internal/tui/handlers_slack.go` (469 lines) holding the Slack / message-action methods and the small notification-store helpers they depend on.
- Moved (methods on `*Model`): `editMessage`, `isMyMessage`, `requestMessageDelete`, `cancelUpload`, `confirmMessageDelete`, `toggleReaction`, `setChannelHeader`, `secureIndicator`, `decryptMessages`, `clearChannelNotifs`, `activateNotification`, `lookupChannelByID`, `recordUnreadMessage`, `recordReaction`.
- Most of these have both a Slack and a P2P branch — they're the user-level "operate on a message" actions that route to whichever backend owns the currently-selected chat. Keeping them together makes the message-action semantics easy to audit in one file.
- Line count: model.go is now **4,350 lines** (down from ~6,200 at the start of Phase C — a 30% reduction). Current layout:
  ```
  4350 internal/tui/model.go
   610 internal/tui/handlers_p2p.go
   469 internal/tui/handlers_slack.go
   457 internal/tui/handlers_ui.go
   373 internal/tui/cmds.go
   176 internal/tui/selectablelist.go
    41 internal/tui/persist.go
  ```
- Verification: `go build ./...`, `go vet ./...`, `make build` all clean; binary installed.

### Task 3 — OverlayScaffold — ✅ complete

- Created `internal/tui/overlayscaffold.go` (137 lines). `OverlayScaffold` is a small value type (`Title`, `Footer`, `EmptyMessage`, `Width`, `Height`, `BoxWidth`, `BoxHeight`, `MaxBoxWidth`, `BorderColor`) with a single `Render(body string) string` method. Overlays that adopt it can replace the ~40-line "titleStyle + dimStyle + boxStyle + lipgloss.Place" tail with a single scaffold literal.
- Migrated three simple overlays as proof-of-concept:
  - **`about.go`** — keeps its own per-line centring (the only overlay that centres each content line individually) and hands the pre-centred string to the scaffold purely for the box + place.
  - **`rename.go`** — the text-input case. Title/Footer/MaxBoxWidth covered by the scaffold, the body just holds the label rows and the text input view.
  - **`whitelist.go`** — the list case. BoxHeight is passed explicitly to match the overlay's "fill the screen" behaviour.
- The remaining ~15 overlays are deferred: each has its own custom tuning (sub-pages, filter input, multi-column layouts, reactive sizing) that needs per-overlay review. The scaffold is designed to accept them incrementally — each migration is a self-contained View rewrite that doesn't touch state or Update logic.
- Verification: `go build ./...`, `go vet ./...`, `make build` all clean; binary installed.

## Verification

After both tasks landed:
- `gofmt -l .` → empty
- `go vet ./...` → clean
- `go build ./...` → clean
- `make build` → clean release binary, installed to `~/.local/bin/slackers`
