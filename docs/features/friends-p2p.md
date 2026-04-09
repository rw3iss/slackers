# Friends & P2P — feature reference

This is the working reference for the friends subsystem. For design rationale see `How_It_Works.md` → "Friends & Private Chat". For testing see `docs/architecture/p2p-testing.md`.

## What it is

A fully independent peer-to-peer chat layer built on libp2p. Messages, file transfers, friend requests, pending-message replay, profile sync, and online detection all happen directly between peers — the Slack API is never involved.

It can operate in two modes:

- **Hybrid:** Slack workspace is configured AND friends are present. The sidebar shows a "Friends" section above workspace channels.
- **Friends-only:** `config.Validate()` fails (no tokens) but `friends.json` has entries → the app launches with `m.slackSvc == nil` and only runs the P2P stack.

## Key files

| File | Role |
|---|---|
| `internal/friends/friends.go` | `FriendStore` — thread-safe JSON-persisted friend list with an indexed `byUserID` map for O(1) lookup |
| `internal/friends/encode.go` | Contact card formats: JSON, SLF1, SLF2 (compact binary hash ~109 chars) |
| `internal/friends/history.go` | Per-friend encrypted chat history on disk with LRU-capped in-memory cache |
| `internal/secure/p2p.go` | `P2PNode` — libp2p host, peer discovery, stream protocol (`/slackers/msg/1.0.0`, `/slackers/file/1.0.0`) |
| `internal/secure/crypto.go` | X25519 keys, ECDH, HKDF-SHA256, ChaCha20-Poly1305 |
| `internal/tui/handlers_p2p.go` | All friend/P2P message handling in the TUI (add, remove, rename, send, receive, reconnect) |
| `internal/tui/friendsconfig.go` | 6-page Friends Config overlay state machine |
| `internal/tui/friendrequest.go` | Accept/reject popup for incoming friend requests |

## Wire protocol message types

Defined in `internal/secure/p2p.go`:

- `MsgTypeMessage` — regular chat message (carries original `Timestamp` on the wire to preserve order)
- `MsgTypeFriendRequest` / `MsgTypeFriendAccept` / `MsgTypeFriendReject`
- `MsgTypeFileOffer` / `MsgTypeFileRequest` — file transfer offer + pull
- `MsgTypePing` — keep-alive / online check
- `MsgTypeDisconnect` — graceful shutdown broadcast
- `MsgTypeProfileSync` — full contact card, merged into stored friend on receive
- `MsgTypeRequestPending` — asks the peer to resend anything still flagged `Pending` for us

## Contact card formats

1. **JSON** — full profile (name, email, slacker_id, public_key, multiaddr, endpoint, port). Default for new users so recipients see real names.
2. **SLF2** — binary format (32-byte public key + peer ID + IPv4 + port), base64url-encoded with a `SLF2.` prefix. ~109 chars. No name/email. Chosen for compactness when sharing over e.g. a chat line.
3. **SLF1** — legacy format, still parsed by `ParseAnyContactCard` for backwards compatibility.

Format choice is stored as `cfg.ShareMyInfoFormat` and configured in **Friends Config → Share Format**.

## In-chat contact pills

Outgoing messages can embed `[FRIEND:me]` (alt-M) or `[FRIEND:<friend-id>]`. `expandFriendMarkers` replaces the token on send with either the JSON card or SLF2 hash. On the receiving side, `messages.go` runs a two-pass pipeline:

1. **`collapseFriendMarkers`** (pre-wrap) — finds `[FRIEND:<blob>]`, decodes via `friends.ParseAnyContactCard`, stores the result in `MessageViewModel.friendCards[key]`, and substitutes a short `[FRIEND:#fc-N]` reference. This guarantees the marker survives word-wrap.
2. **`rewriteFriendCards`** (per rendered line) — resolves the reference and renders a `👤 Friend: <label>` pill with click hit-testing stored in `friendCardHits`.

Click flow: `FriendCardClickedMsg{Card}` → self-check → `FriendStore.FindByCard` → one of **Add as new / Merge into existing / Replace existing** confirmation prompts → `applyFriendCard` writes through.

## Pending messages & reconnect recovery

Every friend P2P send returns a `FriendSendResultMsg{Success bool}`. On failure the model flips the history entry's `Pending` flag and renders a `⏳ pending` badge.

Recovery has two independent triggers:

1. **Local edge-trigger.** `friendPingCmd` tracks `friendPrevOnline`; an offline→online transition queues `resendPendingFriendMessagesCmd(peerUID)`, which sweeps the friend's history newest-first (stopping at the first delivered locally-authored message), collects everything still flagged `Pending`, and dispatches them via `tea.Sequence` to guarantee order.
2. **Remote pull.** `connectFriend`, on offline→online, fires `MsgTypeProfileSync` **and** `MsgTypeRequestPending` at the peer. The receiver runs its own resend sweep against the requester.

## Profile auto-sync merge rules

On receive of `MsgTypeProfileSync`, `FriendStore.FindByCard` matches (in order) by SlackerID → PublicKey → Multiaddr. Then:

- `Email`, `PublicKey`, `Multiaddr`, `SlackerID` overwrite on difference. A changed `PublicKey` **clears** the cached per-pair `PairKey` so the next handshake re-derives the shared secret.
- `Name` is only filled when empty locally. The user's local alias for a friend is never clobbered.

## Things that trip people up

- **Don't block Update with dial calls.** `P2PNode.ConnectToPeer` has a 3-second dial timeout and `connectFriend` dispatches in a goroutine. Anything you add on the friend path should do the same.
- **Friend history is encrypted on disk.** Merging encrypted history files during a backup `import --mode merge` is intentionally skipped — merging ciphertext by message ID would corrupt the file.
- **Don't rebuild friend channels on every ping.** The perf pass cached the friend-channel slice on the model. Only rebuild when the friend list *membership* changes, not when online/offline flips.
- **`FriendStore.Get` is O(1).** It's now indexed by a `byUserID` map rebuilt on `Load` and maintained on every mutation. Don't reintroduce linear scans in the hot path.
