---
description: Run pre-release audits, update docs, then build, tag, and publish a new slackers release.
---

# /publish — Slackers release workflow

Perform the following steps in order. Do NOT skip steps. Report findings concisely as you go and only ask the user when a real decision is required (e.g. "is this the next major or minor?"). For purely mechanical fixes (a missing default, a stale README line) just make the edit and continue.

## 1. Shortcut audit

The goal: every keybinding the app responds to should be (a) defined in the
defaults file, (b) editable from the in-app Shortcut Settings screen, and
(c) shown in the in-app help screen.

Files of record:
- Defaults: `internal/shortcuts/defaults.json`
- Shortcut runtime + IDs: `internal/shortcuts/shortcuts.go`
- Settings UI (shortcut editor): `internal/tui/shortcuts_editor.go` (or whichever file builds the editor) and `internal/tui/settings.go`
- Help screen: `internal/tui/help.go`
- Keymap binding: `internal/tui/keymap.go` (look for `BuildKeyMap`)

Steps:
1. Grep the TUI for every place a key is matched: `key.Matches`, hardcoded `keyMsg.String() == "..."`, mouse-mode shortcut handlers, the React/Reply/Thread/Emoji-picker/Settings handlers, and any new modes added recently.
2. For each binding found, confirm:
   - It has an entry in `defaults.json` with the right action ID.
   - The action ID exists in `shortcuts.go`.
   - It is exposed in the shortcut editor list (so users can rebind it).
   - It is documented in the help screen (`help.go`) — either statically or via the dynamic shortcut lookup.
3. If anything is missing, add it. Hardcoded keys that are intentionally fixed (Esc, arrow keys in modal flows) can stay hardcoded but should still be mentioned in help.
4. Run `go build ./...` to confirm.

## 2. Help menu

`internal/tui/help.go` renders the help overlay. Make sure:
- Every shortcut from `defaults.json` shows up in the appropriate section (or is pulled dynamically through the keymap).
- Recently added features have an entry: emoji picker, message react/reply modes, thread view, friends, file browser/transfer, secure channels, settings/shortcut editor — plus anything else added since the last release.
- The help is scrollable (it should already be — verify it still is after edits).

## 3. README.md

Open `README.md`. Update:
- Feature list — add anything new since the last tag, remove anything removed.
- Screenshots section if a major UI area changed (no need to regenerate images, just update captions/links if they're stale).
- Install instructions, version references, and `make` commands if they changed.
- Keep tone consistent with the existing doc — don't rewrite sections that are still accurate.

## 4. How_It_Works.md

Open `How_It_Works.md` and update the relevant subsystem sections in more detail than the README:
- Architecture overviews when modules are added/removed.
- Reaction handling (now supports replies, both Slack threaded replies and friend P2P replies, with inline + thread-view nav).
- Friend system (P2P, encrypted history, file transfer) — keep the most recent state accurate.
- Emoji picker layout/keyboard/mouse model.
- Settings/shortcut editor.
- Anything else that is non-obvious from the code.

## 5. Build & verify

```bash
cd /home/rw3iss/Sites/others/tools/slackers
go build ./...
go vet ./...
make build
```

If the test suite is meaningful, run `go test ./...` too — but only fail the workflow on a regression you introduced, not pre-existing failures.

## 6. Version bump

- Read the current `VERSION` from `Makefile`.
- Ask the user: "Bumping from `vX.Y.Z` to next — minor or patch?" unless the changes since the last tag are obviously minor (new features → minor; bugfix-only → patch).
- Update `VERSION` in `Makefile`.
- If there is a `version` constant elsewhere (e.g. `cmd/slackers/main.go`, `internal/version`), update it to match.
- Update `CHANGELOG.md` if one exists; otherwise prepare a release-notes block to use in step 8.

## 7. Commit & tag

Stage everything that was modified by this workflow. Write a single release commit:

```
release: vX.Y.Z

<one-line summary>

<bullet list of notable changes since previous tag, grouped: Features / Fixes / Internals>
```

Then:
```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
```

Do NOT push or tag without showing the commit message + tag name to the user first and getting explicit confirmation. Pushing tags is a publish action.

## 8. Publish

After confirmation:
```bash
git push origin main
git push origin vX.Y.Z
```

If `gh` is configured and the repo uses GitHub Releases (check with `gh release list`), create one:

```bash
gh release create vX.Y.Z \
  --title "vX.Y.Z" \
  --notes "$(cat <<'EOF'
<release notes here>
EOF
)"
```

Build and attach the cross-platform binaries if previous releases included them:
```bash
make build-all
gh release upload vX.Y.Z build/slackers-linux-amd64 build/slackers-darwin-arm64 build/slackers-windows-amd64.exe
```

## 9. Post-publish

- Reinstall locally: `make build && cp build/slackers ~/.local/bin/slackers`
- Print a short summary to the user with: new version, what's in the release, the release URL (from `gh release view --json url -q .url`).

## Guardrails

- Never push tags or create releases without explicit user confirmation in this same conversation.
- Never bypass hooks (`--no-verify`) or skip signing.
- If a build/lint step fails, stop and report — do not "fix forward" by disabling the failing check.
- Match the scope of the changes: a patch release should not include README rewrites that aren't true, and a minor release should not silently ship unfinished features.
