# Dead-code audit — 2026-04-08

- **Working directory:** `/home/rw3iss/Sites/others/tools/slackers`
- **Project:** Go (`go.mod`, `go1.25`)
- **Tools used:**
  - `staticcheck -checks 'U1000,U1001' ./...` (honnef.co/go/tools, built against go1.25.7) for unused declarations.
  - `go vet ./...` via `make lint` for baseline correctness.
  - `go build ./...` after each removal to confirm nothing downstream broke.
  - Repository-wide `grep` sweeps for reflection (`reflect.`), codegen directives (`go:generate`), and cross-file references to each candidate symbol.
- **Safety baseline:** no `reflect.` uses inside `internal/tui/`, no `go:generate` directives anywhere in the tree, no wire-format / serialization tags on any of the removed symbols, every candidate was an unexported (lowercase) identifier.

## Bucket A — Removed automatically

All six entries were reported by `staticcheck` as `U1000` (unused). Each was confirmed by a follow-up `grep` to have **zero** references anywhere in the repository (tests, docs, scripts, CI config, comments excluded). Every removal was followed by `go build ./...` — the build stayed green through each one.

- **`tui.shareInfoMaxIdx`** — `internal/tui/friendsconfig.go:848` — package-level `var` intended to cache the highest valid index into `shareInfoOptions()`; left over from a previous version of the Share My Info screen that used a fixed max index. Current `handleShareInfoKey` computes `len(opts)` inline per keystroke, so this variable was never read.
- **`tui.Model.pollOffset`** — `internal/tui/model.go:315` — unused `int` field on the root `Model` struct. Previously used for round-robin polling, now replaced by the `pollChannels` slice iteration. Not referenced anywhere.
- **`tui.sendMessageCmd`** — `internal/tui/model.go:5049` — a tea.Cmd wrapper around `SlackService.SendMessage` returning `MessageSentMsg` on success. All current send paths build their own command inline with per-channel file / reply / edit handling, so this helper was never called.
- **`tui.friendPingCmd`** — `internal/tui/model.go:6000` — a trivial wrapper that called `friendPingCmdWithCurrent(store, p2p, "")`. Every caller now uses `friendPingCmdWithCurrent` directly (with the active friend passed in), so the zero-argument wrapper was obsolete. The docstring on `friendPingCmdWithCurrent` was rephrased so it no longer references the removed wrapper.
- **`tui.MsgSearchModel.debounce`** — `internal/tui/msgsearch.go:38` — `time.Time` field intended for a debounce window on the search-as-you-type flow; never written or read. The simple "search on every change" path noted in the source comment made it unnecessary. The package's `time` import stays (the adjacent `Timestamp time.Time` field still uses it).
- **`tui.max`** — `internal/tui/settings.go:985` — local helper shim predating Go 1.21. The project is now on go1.25, which ships a built-in generic `max`, so the local shim was shadowing the builtin and never referenced directly by name.

## Bucket B — Kept for review

*(empty)*

No candidates survived the analyzer + grep sweep without being cleanly removable. `staticcheck` with `U1000,U1001` returns zero findings after the removals.

## Verification steps taken

1. `staticcheck -checks 'U1000,U1001' ./...` → 6 findings.
2. `grep -rn '<symbol>' --include='*.go' .` for each finding, to rule out comment-only references and cross-file usage. (Only one symbol — `friendPingCmd` — had a comment reference inside `model.go:3710`; that comment was rewritten to use the remaining `friendPingCmdWithCurrent` name.)
3. `grep -rn 'reflect\.' --include='*.go' internal/tui/` → none.
4. `grep -rn 'go:generate' --include='*.go' .` → none.
5. After each removal: `go build ./...` → clean.
6. Final re-run: `staticcheck -checks 'U1000,U1001' ./...` → **zero findings**.
7. `make build` (release build with version ldflags) → clean.
8. `go vet ./...` via `make lint` → clean.

## Files touched

- `internal/tui/friendsconfig.go`
- `internal/tui/model.go`
- `internal/tui/msgsearch.go`
- `internal/tui/settings.go`
- `docs/dead-code-audit-2026-04-08.md` (this file)
