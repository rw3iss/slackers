# Development workflow

## Branches

The active development branch is `feature/improve` (continuation of `perf-audit` and earlier feature branches). Main is `main`.

## Build / run / test

```bash
make build         # → build/slackers
make run           # build and launch
make install       # to ~/.local/bin
make test          # go test ./...
make lint          # go vet ./...
make build-all     # cross-compile all 3 platforms
make clean         # rm -rf build + go clean
```

No separate formatter — rely on `gofmt`/`goimports` from your editor. `go vet` is the only lint.

`make setup` builds and then runs `slackers setup` interactively if you need to reconfigure credentials against a new workspace.

## Running with debug logging

```bash
slackers --debug
# in another terminal:
tail -f ~/.config/slackers/debug.log
```

Debug logging has zero overhead when `--debug` is not passed (`debug.Init` is never called).

## Running a second "test" instance alongside your main one

For anything touching the P2P / friends stack you need two processes that don't share config, friend stores, keypairs, or port. The trick is to override `XDG_CONFIG_HOME` for the second instance so it reads and writes to a completely separate directory (`/tmp/slackers-test`) while your real instance keeps using `~/.config/slackers`.

```bash
# One-off run
XDG_CONFIG_HOME=/tmp/slackers-test slackers

# With debug logging
XDG_CONFIG_HOME=/tmp/slackers-test slackers --debug
# (log then lives at /tmp/slackers-test/slackers/debug.log)

# Convenient shell alias — drop into ~/.zshrc or ~/.bashrc
alias slackers-test='XDG_CONFIG_HOME=/tmp/slackers-test slackers'
```

First launch creates `/tmp/slackers-test/slackers/` with its own empty `config.json`, `secure.key`, `friends.json`, etc. Run through setup (or skip straight into friends-only mode) and **change the P2P port to something other than 9900** in **Settings → Behavior → P2P Port** (e.g. 9901) — otherwise the two instances will fight over the same libp2p listener and neither will accept inbound connections.

Then exchange contact cards between your primary instance and the test instance to verify friend requests, online detection, pending-message replay, profile sync, and file transfer end-to-end. Full test loop in `docs/architecture/p2p-testing.md`.

To wipe the test instance and start over: `rm -rf /tmp/slackers-test`. It's in `/tmp` specifically so it evaporates on reboot if you forget.

## Adding a new feature — quick checklist

1. **Ask first** — does the feature need a Slack API call, a P2P message type, a new overlay, or all three? That decides which files you'll touch.
2. **Slack API call:** extend `SlackService` in `internal/slack/`, implement via `tryWithFallback`, add the consumer in `handlers_slack.go`.
3. **P2P message type:** add `MsgType*` in `internal/secure/p2p.go`, extend the stream handler, add the receive dispatch in `handlers_p2p.go`.
4. **Overlay:** create `internal/tui/<name>.go`, embed `SelectableList` if it's a list, render through `OverlayScaffold`, add the `overlay<Name>` constant to `model.go`, wire open/close in `handlers_ui.go`.
5. **Settings field:** add to `config.Config`, surface in `settings.go` in the right section, use debounced save.
6. **Keybinding:** add a default in `internal/shortcuts/defaults.json`, reference it through `m.keys.<Binding>` — do not hardcode key strings in handlers.
7. **Theme color:** add a named color to `styles.go`, wire into `rebuildDerivedStyles()`.
8. **Footer hint:** use `HintSep` and the canonical `"Key: action · Esc: close"` format.
9. **Build + lint + test** before committing: `make build && make lint && make test`.
10. **Commit message style** — see `git log` for the house style: `area(subarea): short verb phrase`, e.g. `feat(search): quoted-phrase queries + friend-chat results`.

## Adding a new theme

1. Drop a JSON file in `internal/theme/builtin/<name>.json`, or for user-only themes, drop it in `~/.config/slackers/themes/`.
2. Use the existing `default.json` as a template — every key is required.
3. Values: `"N"` (256-color fg), `"N/M"` (fg/bg), `"/M"` (bg only), `"N+b"` / `"N+i"` / `"N+bi"` (bold/italic), `"#rrggbb"` (truecolor).
4. In-app: open **Settings → Theme**, arrow to preview, `e` to edit, Export row to write back to `~/Downloads`.

## Releasing

See `scripts/` for install/uninstall/cleanup helpers. Releases are cut with `release: vX.Y.Z` commits (see `git log --grep '^release'`). The Makefile `VERSION` var and `var version` in `cmd/slackers/main.go` must match.

Auto-update fetches the matching binary from GitHub releases on startup if enabled.
