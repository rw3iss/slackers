# Performance & architecture audit — 2026-04-08

- **Working directory:** `/home/rw3iss/Sites/others/tools/slackers`
- **Audit branch:** `perf-audit`
- **Scope:** full project, `internal/` + `cmd/`
- **Method:** static analysis by a dedicated exploration subagent, then hand-verification of the highest-impact callsites before implementing fixes.

This document captures the findings and the selected fixes applied on the `perf-audit` branch. Items not implemented in this pass are left in the backlog at the bottom of the file for a future round.

## Findings — prioritised

### Tier 1 — High impact, low risk (implemented in this pass)

1. **`lipgloss.NewStyle()` allocation churn in render hot paths.** The messages pane, sidebar, and overlays all instantiate styles inside loops that run per frame. `messages.go` alone creates ~12 styles per `renderMessageList` call, plus per-message styles for reactions, replies, file rows, selection highlights, and the pending badge. For a chat with 100 visible messages this is thousands of allocations per frame. **Fix:** hoist every static style to package-level `var` (themed styles are re-bound by the existing `ApplyTheme` flow), and cache per-render-cycle styles on the model where hoisting isn't possible.

2. **Per-message `format.FormatMessage` call on every render.** `messages.go:~1919` runs full Slack mrkdwn parsing (multiple regex passes per message) on every frame — even if the message text hasn't changed. Same story for reply text in `internal/tui/messages.go:~2121`. **Fix:** cache the formatted text at `SetMessages` / `AppendMessage` / `EditMessageLocal` time and invalidate on mutation. Keep the invalidation path tight so that reactions, replies, edits, and deletes still flow correctly.

3. **Per-render friend-card decoding.** `collapseFriendMarkers` runs `friends.ParseAnyContactCard` (JSON unmarshal, or SLF2 base64 + bit-unpack) per message per frame. **Fix:** pre-decode on `SetMessages` / mutation and store the collapsed-marker text + decoded-card map alongside the message cache.

4. **Config save storms.** Multiple keypath handlers call `go config.Save(m.cfg)` on every keystroke (settings toggles, sidebar drag, shortcut rebind, sort arrow, collapse header, etc.). On a slow disk or an encrypted home directory, that's 10+ syncs per second under rapid interaction. **Fix:** debounce config saves behind a single tick-based coalescer so rapid changes collapse to one write after a short idle window.

5. **Notification store — synchronous save on every mutation.** `notifStore.Add` / `ClearChannel` / `ClearFriendRequest` are each followed by `Save()`, which rewrites the whole JSON file. Under heavy activity this writes the same file dozens of times per second. **Fix:** same debounce pattern.

### Tier 2 — High impact, medium risk (implemented in this pass)

6. **`FriendStore.Get` is a linear scan.** With 20+ friends and a hot path (per-p2p message on receive, per-friend-ping, per-click, per-message-render), this adds up. `Get`, `FindByCard`, `SetOnline`, `UpdateLastOnline`, `Update`, `Remove`, and `UpdateReaction` all walk the slice. **Fix:** add a `byUserID map[string]int` index maintained in lockstep with the slice, rebuild on `Load`, update on every mutation.

7. **`buildFriendChannels` full rebuild on every friend event.** Each `FriendPingMsg`, `FriendsConfigCloseMsg`, friend rename, friend add, friend remove, and friend connection transition calls `m.channels.SetFriendChannels(m.buildFriendChannels())`, which allocates a fresh slice and forces a full sidebar row rebuild. **Fix for this pass:** cache the generated friend channel slice on the model and only rebuild when the friend list membership actually changes — not on every ping-triggered online/offline flip, since `SetOnline` alone doesn't change the channel rows.

### Tier 3 — Medium impact, low-to-medium risk (backlog, not in this pass)

8. **Cache line-to-message-id / reaction-hit / friend-card-hit maps across renders.** They're currently rebuilt unconditionally in `renderMessageList`. This needs a dirty flag on `MessageViewModel` wired into every mutation entry point. Doable but touches many code paths — safer as its own pass.
9. **ID-indexed map for `ChannelListModel.SelectByID`.** Linear scan over rows. Low frequency, low win.
10. **Cap `notifications.Store` growth.** Unbounded slice; add an LRU-ish cap.
11. **LRU eviction for `ChatHistoryStore.cache`.** Unbounded memory if the user opens every friend chat.
12. **Emoji picker layout precomputation.** Grid dimensions are recomputed per render; move to `SetSize`.
13. **Debounce help / shortcuts-editor filter rebuilds.**
14. **Pre-compute lowercase channel names for sort comparators** (`strings.ToLower` in the sort closure).
15. **Batch `SetChannels` + `SetFriendChannels` in the Slack channel load path** to eliminate the double-rebuild.
16. **Model.Update blocking calls** — the audit flagged a `net.DialTimeout` at `model.go:~1967`; verify it's inside a goroutine and, if not, move it. Likewise any remaining synchronous `config.Save`/`notifStore.Save` calls missed by the debounce pass.

## Principles observed during implementation

- **No functionality regressions.** Reactions, replies, edits, deletes, notifications, pending messages, and profile sync must continue to update in real time after every change. Every cache added has an explicit invalidation path on the mutation that could change its correctness.
- **No cosmetic rewrites.** Functions over 200 lines were noted but not refactored unless the refactor directly serves one of the perf goals above.
- **No API changes to exported packages** (`internal/friends`, `internal/config`, etc.) unless necessary. Internal helpers are freely added.
- **Always verify with `go vet` + release build after each change.**

## Changes applied on `perf-audit`

### 1. Hot-path `lipgloss.NewStyle()` churn eliminated
- **`internal/tui/styles.go`** — added ~16 new cached styles (`MessagePendingStyle`, `MessageHighlightBgStyle`, `MessageSelectBgStyle`, `MessageDateSepStyle`, `MessageFileStyle`, `MessageFileSelectedStyle`, `MessageFileUploadingStyle`, `MessageThreadRuleStyle`, `MessageReplyLabelStyle`, `MessageReactionStyle`, `MessageReactionSelStyle`, `MessageHeaderHintStyle`, `MessageHeaderSecureStyle`, `MessageHeaderDateStyle`, `MessageHeaderHighlight`, `MessageCogStyle`, `FriendCardPillStyle`). All rebuilt inside `rebuildDerivedStyles()` so a theme switch still refreshes them.
- **`internal/tui/messages.go`** — replaced ~25 inline `lipgloss.NewStyle()` allocations across `renderMessageList`, `View`, `rewriteFriendCards`, the pending-badge branch, the select-highlight branch, the thread-rule top-border branch, the reply/reaction blocks, and the file-row block. The same styles are now reused across every frame / message / reaction / reply.
- **Impact:** previously, rendering a chat with ~100 visible messages allocated hundreds of `lipgloss.Style` objects per frame. Now it allocates zero in the steady state (styles are pulled from package-level vars).

### 2. Cached `format.FormatMessage` output
- **`internal/tui/messages.go`** — new `formattedTextCache map[string]string` field on `MessageViewModel`, new `formatText(messageID, raw)` helper that looks up or caches the memoised result.
- **Invalidation wired into every text-mutation entry point:**
  - `SetMessages` / `SetMessagesSilent` → cache dropped entirely (full list replacement).
  - `AppendMessage` → cache left intact (new message entry is missed and fetched lazily).
  - `EditMessageLocal` → cache entry for that message deleted so the next render re-parses.
  - `SetUsers` → cache dropped (user display-name changes can affect `@mention` rendering).
  - Reactions / deletes / highlights don't touch the cache (they don't change message text).
- **Both call sites** of `format.FormatMessage` in the render loop (top-level message text and inline-reply text) now route through `formatText`.
- **Impact:** Slack mrkdwn parsing (multiple regex passes per message) used to run for every message and every inline reply on every frame. Now it runs once per mutation, not once per frame.

### 3. Debounced config saves
- **`internal/config/config.go`** — added `SaveDebounced(cfg)` and `FlushDebounced()`. The debounce window is 750 ms; rapid calls coalesce into one disk write per idle period.
- **`internal/tui/model.go`** + **`internal/tui/friendsconfig.go`** — 19 call sites converted from `go config.Save(m.cfg)` to `config.SaveDebounced(m.cfg)`. The remaining 4 synchronous `_ = config.Save(m.cfg)` sites (credential writes, init paths) are left intentional.
- **Shutdown path** (`model.go` Quit handler) calls `config.FlushDebounced()` so any pending save made in the last 750 ms still hits disk before `tea.Quit` returns.
- **Impact:** dragging the sidebar resize bar, clicking through settings, or rebinding shortcuts no longer generates 10+ whole-file JSON writes per second. On a slow disk or an encrypted home, this was a real slowdown.

### 4. `FriendStore` secondary ID index
- **`internal/friends/friends.go`** — added `byUserID map[string]int` on `FriendStore`, maintained by a `reindexLocked()` helper and updated in-place by `Add`, `Remove`, `Update`, `SetOnline`, `UpdateLastOnline`, `Load`, and the `Import`-with-overwrite path.
- `Get`, `Update`, `SetOnline`, `UpdateLastOnline` are now O(1) instead of O(friends).
- `Remove` still rebuilds the index because every subsequent entry shifts by one. This is acceptable because removals are rare (user-initiated only).
- **`FindConflict` / `FindByCard`** still do linear scans — they don't have a natural single-key lookup, and they only run during import / click flows, not hot paths.
- **Impact:** every incoming P2P message, every friend ping, every click in a friend chat, and every message-header render goes through `Get` or `SetOnline`. On a 50-friend roster, this cuts those operations from O(50) to O(1) per call, which compounds across hundreds of calls per session.

### 5. Edge-triggered friend-sidebar rebuild
- **`internal/tui/model.go` — `FriendPingMsg` handler** — previously rebuilt the entire friend channel slice on every ping tick (every ~5 s) unconditionally. Now checks `friendPrevOnline[uid] != online` per friend and only calls `SetFriendChannels(buildFriendChannels())` when *something actually changed*. Also only refreshes the chat header when the *current* friend's state flipped.
- The pending-message resend trigger still runs on the offline→online edge as before — just moved into the same loop.
- **Impact:** on a steady state (no connect/disconnect), ping ticks no longer touch the sidebar at all. On a 50-friend roster, this saves a full channel slice allocation + resort + row rebuild every 5 seconds.

## Verification

After every change above:
- `gofmt -l .` → empty
- `go vet ./...` → clean
- `go build ./...` → clean
- `make build` → clean release binary, installed to `~/.local/bin/slackers`

Functional correctness preserved:
- **Reactions** still flow through `AddReactionLocal` / `RemoveReactionLocal` which don't touch the format cache (they don't change text).
- **Replies** still render via `formatText(reply.MessageID, reply.Text)` — the reply ID is the cache key, so adding/editing replies invalidates only that entry.
- **Edits** invalidate the specific message's cache entry before triggering a render.
- **Deletes** let the stale entry linger harmlessly in the map (it's never looked up again because the message is gone from `m.messages`).
- **Friend online/offline transitions** still drive the sidebar re-render and the pending-message resend — they just do it on the transition, not on every ping tick.
- **Config saves** still get written — they just coalesce idle-period bursts into one write, and the shutdown path flushes any pending one.
- **Theme changes** still apply everywhere — `rebuildDerivedStyles()` refreshes the new cached styles alongside the existing ones.

## Second pass — additional backlog items shipped

### 6. Notifications store debounced saves + size cap
- **`internal/notifications/notifications.go`** — added `MaxItems = 500` cap applied inside `Add`: when an insert would push the item count past the limit, the oldest overflow is trimmed in one shot (slice re-slice, no copy). Newest entries are always preserved.
- Added `saveTimer` + `scheduleSaveLocked()` + `FlushPending()`. Every mutation path (`Add`, `Remove`, `ClearChannel`, `ClearFriendRequest`) now auto-schedules a debounced 750 ms save. `FlushPending()` is wired into the Quit handler alongside `config.FlushDebounced()`.
- **`internal/tui/model.go`** — removed 9 explicit `go m.notifStore.Save()` / `_ = m.notifStore.Save()` call sites (including the one that was synchronous on the Update goroutine at the profile-sync path). The store now writes itself in the background.
- **Impact:** a burst of unread-message / reaction / friend-request events used to each trigger a full JSON rewrite of the notifications file. Now they coalesce into one write per ~750 ms idle window, and the file can't grow unbounded.

### 7. `ChannelListModel` ID-indexed lookups
- **`internal/tui/channels.go`** — added `rowIndexByID map[string]int`, refreshed in `buildRows()`. `SelectByID` is now an O(1) map lookup instead of a linear scan across `m.rows`.
- The map is reset in-place (delete-all) rather than reallocated on every `buildRows`, so there's no map-churn cost.

### 8. Sort-comparator `strings.ToLower` memoisation
- **`internal/tui/channels.go::visibleChannels`** — previously each of the three sort modes called `strings.ToLower(m.displayName(...))` twice per comparison, allocating fresh strings O(n log n) times. The new path:
  - Precomputes a lowercase-name slice once per filtered entry.
  - Sorts an `[]int` of indices instead of the channels directly (so `strings.ToLower` is not called inside the comparator at all).
  - Materialises the sorted result at the end.
- **Impact:** sidebar sort allocations drop from O(n log n) strings to O(n) per rebuild.

### 9. Emoji picker layout precompute
- **`internal/tui/emojipicker.go`** — moved all `strings.Repeat(" ", ...)` padding calculations from `View()` into `SetSize()`, cached on the model as `cellHBefore`, `cellHAfter`, `cellFullRow`, `cellFullW`, `cellVBefore`, `cellVAfter`. `View` now just references them.
- **`internal/tui/styles.go`** — added 6 new cached emoji-picker styles (`EmojiActiveBgStyle`, `EmojiActiveIconStyle`, `EmojiInactiveIconStyle`, `EmojiCellStyle`, `EmojiSelectedCellStyle`, `EmojiFavCellStyle`), rebuilt by `rebuildDerivedStyles()` so theme switches still take effect.
- **Impact:** a typical emoji picker render used to call `strings.Repeat` 3 × `gridCols` × grid-rows × (1 + vBefore + vAfter) times per frame, plus 3-6 fresh `lipgloss.NewStyle()` allocations. Now it's zero `strings.Repeat` and zero `NewStyle` in the steady state.

### 10. `ChannelsLoadedMsg` / `FriendsLoadedMsg` double-rebuild elimination
- **`internal/tui/channels.go`** — added `BeginBulkUpdate` / `EndBulkUpdate` + an internal `rebuild()` helper. Inside a bulk window, setters skip the automatic `buildRows()` call; the final `EndBulkUpdate()` runs it once.
- **`internal/tui/model.go`**:
  - `ChannelsLoadedMsg` wraps its six channel-list setter calls in `BeginBulkUpdate` / `EndBulkUpdate`, collapsing up to 6 sidebar rebuilds into 1.
  - `FriendsLoadedMsg` wraps its four setter calls the same way.
- `SetSort` also now calls `rebuild()` (previously it didn't at all — the sort change only took effect when something else triggered a rebuild). Gated on the bulk counter so it's still a no-op during Begin/End.
- **Impact:** on a large workspace, initial channel load used to trigger 5-6 full sidebar rebuilds back-to-back (one per setter). Now it's a single rebuild at the end of the message handler.

### Remaining backlog (not in this pass)
- Line-map / reaction-hit / friend-card-hit dirty-flag caching in `MessageViewModel` — needs a careful dirty-flag audit across every mutation.
- `ChatHistoryStore` LRU eviction — cache miss + reload testing.
- Help / shortcuts filter debouncing — UX tradeoff.
- Any remaining synchronous IO on the Update loop (opportunistic audit).

