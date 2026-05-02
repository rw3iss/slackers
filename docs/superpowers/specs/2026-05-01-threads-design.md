# Threads — Design Spec

**Date:** 2026-05-01
**Branch:** `feat/threads`
**Phasing:** Plan A (MVP) ships sidebar + detection + storage; Plan B adds global view + backfill scanner + auto-clear scheduler.

## 1. Goals & non-goals

**Goals**

- Track Slack-style threads the user is involved in across both Slack channels and friend (P2P) chats.
- Surface those threads as first-class items in the sidebar (a new top-level group) and via a global search/browse overlay.
- Auto-track new threads going forward at zero API cost (piggyback on existing socket-mode + per-channel history fetches).
- Optionally backfill older threads from history under a user-configurable scope (Disabled / Local-only / Subscribed channels / All-public channels).
- Auto-clear stale threads on a user-configured interval, OS-agnostic (in-process timer + last-cleared persistence).
- Expose the system as an SDK-shaped Go package (`internal/threads`) so the TUI never reaches into Slack/P2P transports for thread purposes.

**Non-goals**

- Replicating Slack's full thread side-pane UX. Activation lands on the existing inline reply-detail view.
- Pre-populating threads on first launch. Forward-tracking starts immediately; historical backfill is opt-in via the global view's "older" section.
- Sending replies from the Threads view itself. Replies happen inside the existing reply-detail view, which already supports them.
- Indexing every historical message. The thread store holds active references + denormalized snapshots; cache for older results is in-memory only.

## 2. Architecture

Three layers with one-way dependencies:

```
                ┌────────────────────────────────────────────────────┐
                │ UI integration (internal/tui/)                     │
                │  - sidebar Threads group (channels.go)             │
                │  - global Threads overlay (threadsoverlay.go)      │
                │  - context menu (threadoptions.go via PopupMenu)   │
                │  - activation flow → existing reply detail view    │
                └────────────────────────┬───────────────────────────┘
                                         │ calls
                ┌────────────────────────▼───────────────────────────┐
                │ Threads SDK (internal/threads/)                    │
                │  - Detector (rule 1–4 evaluation, pure)            │
                │  - Store (active threads + denormalized snapshot)  │
                │  - BackfillScanner (paged, source-agnostic)        │
                │  - Cleanup scheduler (interval timer + persistence)│
                └──────┬───────────────────┬─────────────────────────┘
                       │ uses              │ uses
                ┌──────▼─────┐      ┌──────▼──────────────┐
                │ Source:    │      │ Source:             │
                │ Slack      │      │ Friend (P2P)        │
                │ provider   │      │ provider            │
                └────────────┘      └─────────────────────┘
```

The TUI never touches Slack/P2P directly for thread purposes; it only calls `threads.Store` methods. The store fires a `tea.Msg` (`ThreadsChangedMsg`) whenever the active set mutates so the sidebar/overlay re-render via the normal Bubbletea loop.

## 3. `internal/threads/` package

```
internal/threads/
  threads.go       Public types (ThreadRef, ThreadSnapshot, ThreadReason,
                   Source interface), package-level helpers
  detector.go      Detection rules — pure function over (msg, replies, me)
                   returning (ThreadReason, isThread). No I/O.
  store.go         ThreadStore: active list, JSON persistence, debounced
                   save, ThreadsChangedMsg notifications
  backfill.go      BackfillScanner: paged, concurrent, cancelable.
                   Calls Source.WalkChannel and aggregates results.
  scheduler.go     Auto-clear scheduler: in-process timer + persistence
  threads_test.go  Unit tests for detector + store + scheduler
```

### 3.1 Public types

```go
type ThreadReason string

const (
    ReasonAuthor       ThreadReason = "author"        // user wrote the parent
    ReasonMentioned    ThreadReason = "mentioned"     // @mentioned in parent
    ReasonReplied      ThreadReason = "replied"       // user is in reply_users[]
    ReasonReplyMention ThreadReason = "reply_mention" // user mentioned in a reply
                                                      // (forward-tracking only)
)

type Source string

const (
    SourceSlack  Source = "slack"
    SourceFriend Source = "friend"
)

type ThreadRef struct {
    Source    Source
    ChannelID string
    ParentTS  string // Slack ts or friend message_id
}

type ThreadSnapshot struct {
    Ref              ThreadRef
    ChannelName      string
    ParentText       string   // truncated preview (~120 chars, no formatting)
    ParentAuthorID   string
    ParentAuthorName string
    Participants     []string // names, dedup'd
    LastActivityTS   string   // most-recent reply ts seen
    Reason           ThreadReason
    AddedAt          time.Time
}

// ThreadsChangedMsg is dispatched whenever the active set mutates.
// Carries the new active set so the UI can rerender without re-querying.
type ThreadsChangedMsg struct{ Active []ThreadSnapshot }
```

### 3.2 `Source` interface

```go
type Source interface {
    // Kind identifies which transport this source represents.
    Kind() threads.Source

    // ResolveChannelName returns the human-readable channel name, or
    // empty if unknown. Used when populating snapshots.
    ResolveChannelName(channelID string) string

    // ResolveUserName returns the human-readable name for a user ID
    // (Slack U-id or "slacker:..." friend ID).
    ResolveUserName(userID string) string

    // ListCandidateChannels returns the set of channel IDs the
    // backfill scanner should walk. Slack provider returns the user's
    // sidebar channels (or all public, depending on scope). Friend
    // provider returns active friend channels.
    ListCandidateChannels(scope ScanScope) []string

    // WalkChannel pages through history of one channel and yields any
    // parent that satisfies rules 1, 2, or 3. Backfill skips rule 4
    // (reply-mention) — that requires a per-thread replies fetch.
    // The walker returns (parents, hasMore, error).
    WalkChannel(ctx context.Context, channelID string, before string, limit int) ([]ParentInfo, string, error)
}

type ParentInfo struct {
    ParentTS       string
    AuthorID       string
    Text           string
    ReplyUserIDs   []string
    LastActivityTS string
}

type ScanScope string

const (
    ScopeDisabled  ScanScope = "disabled"   // no historical scan
    ScopeLocal     ScanScope = "local"      // scan in-memory channel history
    ScopeSubscribed ScanScope = "subscribed" // sidebar channels only
    ScopeAllPublic ScanScope = "all_public" // every public channel
)
```

### 3.3 Detector

`detector.go` exposes a pure, I/O-free function:

```go
func Detect(parent ParentInfo, me string, replies []types.Message, forward bool) (ThreadReason, bool)
```

- `forward=true` activates rule 4 (reply-mention).
- `forward=false` (backfill mode) skips rule 4.
- `replies` is optional — when `nil`, only the cheap signals (author, parent text, `reply_users[]`) are evaluated.

Mention detection uses the existing `<@USERID>` pattern from `internal/format`; for friend chats, mention is `<@slacker:SLACKER_ID>` (current friend mention syntax).

### 3.4 Store

API (mirrors `notifications.Store`'s pattern: `sync.Mutex` + debounced save with 750 ms idle window):

```go
type ThreadStore struct { /* unexported */ }

func NewStore(path string) *ThreadStore
func (s *ThreadStore) Load() error
func (s *ThreadStore) Save() error
func (s *ThreadStore) FlushPending()                         // shutdown
func (s *ThreadStore) Add(snap ThreadSnapshot) bool          // dedup by Ref
func (s *ThreadStore) Remove(ref ThreadRef) bool
func (s *ThreadStore) Get(ref ThreadRef) (ThreadSnapshot, bool)
func (s *ThreadStore) Active() []ThreadSnapshot              // sorted newest first
func (s *ThreadStore) Count() int
func (s *ThreadStore) Touch(ref ThreadRef, lastActivityTS string)
func (s *ThreadStore) ClearOlderThan(cutoff time.Time) int
func (s *ThreadStore) LastClearedAt() time.Time
func (s *ThreadStore) SetLastClearedAt(time.Time)
func (s *ThreadStore) ChangedSub() <-chan struct{}            // fan-out for tea.Cmd subscriber
```

The TUI subscribes to `ChangedSub()` once at boot and emits `ThreadsChangedMsg` on each tick.

### 3.5 BackfillScanner

```go
type BackfillScanner struct { /* sources, store, semaphore */ }

func NewBackfillScanner(sources []Source, store *ThreadStore, rateLimit int) *BackfillScanner

// NextPage fetches up to `pageSize` *new* threads (not already in the
// store) older than `cursor`. Returns the discovered snapshots plus a
// new cursor. cursor=="" means "start from newest".
//
// Internally:
//   - Calls each Source.ListCandidateChannels(scope) once per page.
//   - Worker pool fans out per-channel WalkChannel calls.
//   - Worker pool size = min(rateLimit, len(channels)).
//   - For each parent returned, runs Detect(forward=false).
//   - Builds ThreadSnapshot if matched and not already in store.
//   - Bails when pageSize new snapshots are collected or all sources
//     report no more channels to walk.
func (b *BackfillScanner) NextPage(ctx context.Context, scope ScanScope, pageSize int, cursor string) ([]ThreadSnapshot, string, error)

// ProgressMsg is published periodically (one per 5 channels walked)
// for the UI's "Loading older threads..." indicator.
type ProgressMsg struct { ChannelsWalked, ChannelsRemaining int }
```

Caching: a per-process LRU keyed by `(channelID, beforeTS)` returning the last `ParentInfo` page result. The cache is invalidated for a channel when the detector runs forward-tracking on a new message in that channel (i.e., we just learned of new activity).

### 3.6 Scheduler

```go
type Scheduler struct {
    store    *ThreadStore
    interval time.Duration   // 0 = disabled
    timer    *time.Timer
}

// Start runs the catch-up clear immediately (if last_cleared is older
// than interval) and arms the in-process timer.
func (s *Scheduler) Start()

// SetInterval reconfigures the interval. If running, stops the timer,
// runs an immediate sweep with the new cutoff, and re-arms.
func (s *Scheduler) SetInterval(d time.Duration)

// Stop cancels the timer.
func (s *Scheduler) Stop()
```

"Older than X hours" is measured against `LastActivityTS` (most recent reply we know about), not user view time.

Logging: every sweep emits `[threads] auto-clear cutoff=… removed=N total=M` via `internal/debug`. Settings change emits `[threads] scheduler interval=…`. All under the existing `--debug` flag.

### 3.7 On-disk format

`~/.config/slackers/threads.json` (perms 0600, debounced saves like notifications):

```json
{
  "version": 1,
  "active": [
    {
      "ref": { "source": "slack", "channel_id": "C123", "parent_ts": "1714502400.001" },
      "channel_name": "general",
      "parent_text": "Hey, can someone review …",
      "parent_author_id": "U456",
      "parent_author_name": "Alice",
      "participants": ["Alice", "Bob"],
      "last_activity_ts": "1714502500.002",
      "reason": "mentioned",
      "added_at": "2026-05-01T12:34:56Z"
    }
  ],
  "last_cleared_at": "2026-05-01T12:00:00Z"
}
```

The auto-clear interval setting lives in `config.json`, not here — separation of policy (config) from state (threads).

## 4. UI integration

### 4.1 Sidebar Threads group (`internal/tui/channels.go`)

- New top-level group `Threads`, rendered above all existing groups.
- Hidden when `len(active) == 0`. Otherwise expandable/collapsible like other groups; expand state persisted to `config.json` as `threads_group_expanded`.
- Each thread renders as a **two-row entry**:
  - **Row 1:** channel name. Slack channels in `ColorChannelName`; friend channels in a new `ColorThreadFriendChannel` (added to `styles.go`, rebuilt in `rebuildDerivedStyles`). Optional `#` / `🤝` / `@` prefix to disambiguate visually.
  - **Row 2:** participants — first names of the people in the thread, comma-joined. Truncates to `Alice, Bob, +3` when over width. Rendered in `ColorMuted`.
- Selection highlight covers both rows. Mouse hit-testing covers both rows. Up/Down jumps 2 visual rows = 1 thread item.
- Sort order: newest activity first.
- Right-click → `ThreadOptionsModel` (built on shared `PopupMenu`):
  - **Close Thread** — removes the ref from store; thread can be re-added later if the user is mentioned again.
  - **Go to Channel** — switches to the parent's channel (no auto-drill into reply view).

### 4.2 Activation flow

Pressing **Enter** on a sidebar Threads item or a row in the global Threads view:

1. Switches the active channel to the thread's `ChannelID` (same code path as a regular channel click).
2. Selects the parent message (existing `messages.go` selection by ID).
3. Auto-opens the existing inline reply-detail view (a.k.a. "inside" mode) for that parent. Replies are fetched on demand by the existing `fetchReplies` path — no changes needed there.
4. Closing the detail view returns the user to the channel; a stack-style back gesture isn't introduced.

A new `tea.Msg` (`OpenThreadMsg{Ref: ThreadRef}`) carries the request from any UI surface (sidebar, global view, slash command) into the root model.

### 4.3 Global Threads view (`internal/tui/threadsoverlay.go`)

Keyboard shortcut: `Alt+T`, configurable via the existing shortcuts editor. Slash command: `/threads`. Both routed to `OpenThreadsViewMsg{}`.

Layout (rendered via `OverlayScaffold`):

```
┌─────────────── Threads ────────────────────────────────────────┐
│ 🔍 Search threads…                                             │
│                                                                │
│ Active                                                         │
│   #general                                                     │
│   Alice, Bob                                                   │
│   #design                                                      │
│   Maria, Sam, +2                                               │
│                                                                │
│ Older                                                          │
│   #releases                                                    │
│   Tom, Sue                                                     │
│   …                                                            │
│   Loading older threads…                                       │
└────────────────── Esc: close · Enter: open · Tab: search ─────┘
```

- Built on the shared `SelectableList` primitive — same dual-row item renderer reused from the sidebar.
- **Active section:** straight from `store.Active()`. Solid foreground.
- **Older section:** populated by `BackfillScanner.NextPage`. Dimmer foreground (`ColorMuted`).
- **Search bar:** filters across `ChannelName + ParentText + Participants`. Tab toggles focus between list and search. Live filter (no Enter needed).
- **Scroll-paging:** when the cursor reaches the last loaded "Older" row and the user presses Down, request the next page. Render a `Loading older threads…` placeholder until `NextPage` returns. If `BackfillScanner` returns no further results, render `No older threads found.` instead.
- **Cancel:** Esc cancels the in-flight `NextPage` via context cancellation.

Right-click on any row → same `ThreadOptionsModel` as the sidebar.

Footer hint uses canonical `HintSep` / `FooterHintClose` constants from `styles.go`.

## 5. Settings & scheduler

A new section in the existing settings overlay:

```
Threads
  Backfill scope:        [ Disabled | Local-only | Subscribed | All public ]
  Auto-clear inactive:   [ Off | 24h | 72h | 7d | 30d ]
```

Stored in `config.json` as:

```json
{
  "threads": {
    "scan_scope": "local",
    "auto_clear_hours": 168
  }
}
```

Inline help text under each row in the settings overlay:

> **Backfill scope.** Controls which channels the global Threads view searches for older threads.
> *Disabled* — never search history; only show threads detected from new activity. *Local-only* (default) — search channel history already loaded in this session. Free, but limited. *Subscribed* — search every channel in your sidebar. Slow on first run; uses Slack rate budget. *All public* — search every public channel in the workspace. Very slow on big workspaces.

> **Auto-clear inactive.** Threads with no new replies older than this duration are removed from the active list on a recurring sweep. Cleared threads can be re-added if you're mentioned again.

Changing `scan_scope` invalidates cached older-page results (Plan B only — the setting persists in Plan A but has no effect until the global view ships).
Changing `auto_clear_hours` calls `Scheduler.SetInterval(d)`, which runs an immediate sweep and re-arms.

## 6. Wiring & lifecycle

**Boot order in `model.go`'s `NewModel`:**

1. Existing config + notifications stores load.
2. `threads.NewStore(threads.DefaultPath()).Load()`.
3. Construct sources: `slackSource{m.slackSvc, m.cfg}` and `friendSource{m.friendStore}`. (Both sources are nil-safe; if `slackSvc == nil` because we're in friends-only mode, the source returns empty channel lists.) Sources are needed by both the detector path (for name resolution) and Plan B's scanner.
4. `threads.NewScheduler(store, time.Duration(cfg.Threads.AutoClearHours)*time.Hour).Start()`.
5. Subscribe to `store.ChangedSub()` and convert to `ThreadsChangedMsg`.
6. Wire detector into the existing `SetMessages` / `AppendMessage` paths in `messages.go` — every new message runs `Detect(forward=true)` and, on hit, `store.Add(snapshot)`.
7. **Plan B only:** `threads.NewBackfillScanner(sources, store, rateLimit=40)` constructed and held by the model for the global Threads overlay's paging.

**Shutdown:** `model.go`'s existing shutdown path adds `threadStore.FlushPending()` and `scheduler.Stop()` next to the existing notif-store flush.

## 7. Testing

- **Detector** — pure unit tests in `threads_test.go` covering all four rules with author, mention, reply_users[], and reply-mention fixtures. Friend-source mention fixtures included.
- **Store** — unit tests for Add dedup, Remove, Touch, ClearOlderThan, JSON round-trip, debounced save.
- **Scheduler** — fake clock test that validates immediate sweep on stale `LastClearedAt`, interval re-arm, SetInterval reconfiguration.
- **Backfill scanner** — table-driven tests with a fake `Source` that returns canned `ParentInfo` pages. Validates: page size respected, dedup against existing store, cursor advance.
- **No new integration test for the UI** — manual smoke test on `feat/threads`. Covered by the user's stated "we will test before pushing/merging".

## 8. Phasing

**Plan A (MVP):** ships in the first implementation pass.

- `internal/threads/` package complete except `BackfillScanner`.
- Forward-tracking wired through `SetMessages` / `AppendMessage`.
- Sidebar Threads group with dual-row rendering + `ThreadOptionsModel`.
- Activation flow → existing reply-detail view.
- Settings entry for scan scope (stored, no backfill yet — UI only).
- Auto-clear scheduler + settings entry.
- README + How_It_Works update for the visible surface.

After Plan A, the user has a working sidebar Threads group and auto-tracking.

**Plan B:** second pass.

- `BackfillScanner` implementation across both sources.
- Global Threads view (`threadsoverlay.go`) with search, paging, progress indicator.
- `Alt+T` shortcut + `/threads` slash command.
- README/help updates for the global view.

Plan B is gated on Plan A landing and the user testing it.

## 9. Open risks / things to revisit during implementation

- **Concurrent backfill correctness.** The Slack source needs a per-workspace token-bucket so we don't burst over the tier-3 limit when fanning out workers. We'll borrow the existing `tryWithFallback` retry logic and add a `ratelimit.NewBucket` wrapper.
- **Friend source channel listing.** Friend "channels" are 1:1 with friends in slackers' current model. Confirm during implementation that `friendStore.All()` is the canonical iterator and that channel IDs match `m.activeChannelID` semantics.
- **Sidebar dual-row state machine.** `channels.go` has historically assumed one row per item. The expand/collapse + drag-resize code paths need a "logical row → visual row(s)" indirection. Plan A scope budget is ~150 lines added there.
- **Mention regex for friends.** Confirm the friend-mention syntax (`<@slacker:...>` or other) by grepping `internal/tui` and `internal/format` during implementation; spec assumes `<@slacker:...>` but that's a placeholder.

## 10. Files added / modified (high level)

**Added:**

- `internal/threads/threads.go`
- `internal/threads/detector.go`
- `internal/threads/store.go`
- `internal/threads/backfill.go` (Plan B)
- `internal/threads/scheduler.go`
- `internal/threads/threads_test.go`
- `internal/tui/threadoptions.go` (Plan A)
- `internal/tui/threadsoverlay.go` (Plan B)

**Modified:**

- `internal/types/types.go` — no field changes; `Message.Replies` already covers our needs.
- `internal/tui/model.go` — boot wiring, shutdown wiring, `OpenThreadMsg` / `OpenThreadsViewMsg` / `ThreadsChangedMsg` routing.
- `internal/tui/messages.go` — call `detector.Detect(forward=true)` from `SetMessages` / `AppendMessage` and submit to store.
- `internal/tui/channels.go` — Threads group + dual-row rendering.
- `internal/tui/handlers_ui.go` — Alt+T shortcut, `/threads` command (Plan B).
- `internal/tui/handlers_slack.go` / `handlers_p2p.go` — `OpenThreadMsg` activation.
- `internal/tui/settings.go` — Threads section.
- `internal/tui/styles.go` — `ColorThreadFriendChannel`.
- `internal/config/config.go` — `Threads` config struct.
- `internal/shortcuts/defaults.json` — `Alt+T` binding (Plan B).
- `cmd/slackers/main.go` — `/threads` cobra subcommand wrapper (Plan B).
- `README.md`, `How_It_Works.md` — docs.
