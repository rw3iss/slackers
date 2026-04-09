# Friends & Private Chat

Slackers includes a peer-to-peer friends system independent of Slack.
Friends communicate over libp2p with X25519 + ChaCha20-Poly1305 — no
messages pass through Slack's servers.

## Identity

Every install gets a unique 32-character **SlackerID** stored in
`~/.config/slackers/config.json`. You can also set a display name and
email in **Settings → Friends Config → Edit My Info**.

## Adding a friend

There are several paths to befriending someone:

1. **DM right-click → Invite to Slackers** — pre-fills the chat with
   a Slack-formatted invite + your contact card.
2. **Settings → Friends Config → Add a Friend** — paste their card.
3. **`/add-friend <hash|json|[FRIEND:...]>`** — same import flow from
   the slash command.
4. **In a chat** — right-click any `[FRIEND:...]` pill, pick
   *Add Friend*, or hit `a` in select mode.

## Removing a friend

- **Sidebar right-click → Remove Friend**, or
- **`/remove-friend <id|name>`**

Both flows confirm with a status-bar prompt before deleting.
The current chat history view is preserved on screen until you
navigate away — useful for one last reference read.

## Online detection

Online status is checked every few seconds via libp2p ping. The
sidebar entry colours green when reachable, grey when offline.
Pending messages sent while a friend was offline are flagged
`⏳ pending` and auto-resent on reconnect, in original order.

## Profile auto-sync

Connected peers exchange their full contact card on every offline →
online transition. Stale fields (PublicKey, Multiaddr, Email) get
refreshed in place. Your locally-chosen display name for that friend
is never overwritten.

## Network requirements

P2P needs the configured P2P port (default 9900) reachable from your
peer's network. UPnP and NAT hole punching usually handle this; for
strict NATs you may need manual port forwarding. Run
`slackers friends` for platform-specific firewall instructions.
