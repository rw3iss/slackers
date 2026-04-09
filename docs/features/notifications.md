# Notifications

## Store

Persistent notifications live in `~/.config/slackers/notifications.json`, backed by `internal/notifications/Store`. Three entry types:

- **`TypeUnreadMessage`** — a message arrived in a channel you weren't viewing
- **`TypeReaction`** — someone reacted to one of your messages
- **`TypeFriendRequest`** — a pending friend-request handshake you haven't responded to yet

Each entry stores enough context to navigate back to the source: channel ID, message ID, friend user ID, timestamp, a rendered preview.

Writes are debounced (see perf audit) — do not call `notifStore.Save` synchronously on every mutation.

## UI surfaces

- **Indicator** — floating top-centre badge over the chat pane (`notifications_indicator.go`) shows the unread total when > 0.
- **Overlay** — `Alt-N` (configurable) opens `notifications_overlay.go`, a scrollable list that embeds `SelectableList`. Enter activates (navigates to the source), `x` dismisses, `Esc` closes.
- **Terminal alerts** — BEL (`\a`) + OSC 9 desktop notification + X11 urgency hint (`\e[?1042h`) all fire on new-message arrival, so the user gets a signal even when slackers is backgrounded.

## Activation routing

`activateNotification` in `handlers_ui.go` dispatches based on entry type:

- `TypeUnreadMessage` → select the channel, scroll to the referenced message ID
- `TypeReaction` → select the channel + message, show reaction context
- `TypeFriendRequest` → open the Friends Config overlay on the friend's accept/reject modal

After successful activation the entry is cleared.

## Auto-clearing

Entries clear themselves when the referenced state is resolved:

- Opening a channel clears all unread-message entries for that channel ID
- Opening a friend chat clears friend-specific unread entries
- Accepting/rejecting a friend request clears the corresponding `TypeFriendRequest`
- Connecting to a friend for the first time via any path clears any stale `TypeFriendRequest` for that pair

## Not yet implemented

- Notification filtering by type inside the overlay
- A "clear all" action
- Desktop-native notifications on platforms without OSC 9 support
- Configurable per-channel notification preferences (mute / @-only)
