# Debug & Troubleshooting

## Enabling debug logging

```bash
slackers --debug
```

Logs land at `~/.config/slackers/debug.log`. Tail in another
terminal:

```bash
tail -f ~/.config/slackers/debug.log
```

Debug logging has zero overhead when `--debug` is not passed.

## What's in the log

- `[api]` — Slack API calls (channel IDs, batch sizes)
- `[socket]` — Socket Mode connect / disconnect / message events
- `[poll]` — primary + background poll ticks
- `[p2p]` — libp2p dial attempts, stream events, peer connections
- `[friend-ping]` — friend online/offline transitions
- `[connect-friend]` — outbound dial results
- `[handle-mouse]` / `[right-click]` / `[friend-pill]` — input
  routing and hit-testing
- `[msgoptions]` / `[friendcardoptions]` — popup positioning

## Common issues

### "connection refused" on every friend dial

The peer isn't listening on the address in their contact card. See
`/help p2p` for the full diagnostic flow. TL;DR: friend's slackers
isn't running, port has changed, or NAT/firewall is blocking.

### Suggestion popup doesn't appear when typing /

Check that you're focused on the input bar (the bottom border
should highlight). Tab cycles focus.

### Theme didn't apply

Some changes (mouse, secure mode) require a restart. Display
settings (sidebar width, sort order, theme) take effect immediately
on save.

## Reporting bugs

Open an issue at <https://github.com/rw3iss/slackers/issues> with:

1. The output of `slackers version`
2. The relevant section of `~/.config/slackers/debug.log`
3. Your terminal name and version
4. Steps to reproduce
