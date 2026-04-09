# Keyboard Shortcuts

Slackers' keybindings are fully customizable. Open
**Settings → Keyboard Shortcuts** (or `Alt-K`) to rebind any key.
Changes take effect immediately and persist to
`~/.config/slackers/shortcuts.json`.

## Defaults overview

| Group | Bindings |
|---|---|
| Focus | Tab / Shift-Tab cycle, Esc to input |
| Sidebar | Ctrl-K search, Ctrl-N next unread, Ctrl-X hide, Ctrl-G unhide, Ctrl-A rename |
| Messages | Ctrl-J / s select mode, Ctrl-F search, PgUp/PgDn scroll |
| Files | Ctrl-U attach, Ctrl-D cancel/half-down, Ctrl-L browse, f file select |
| Threads | Enter on a reply, Esc to exit |
| Friends | Ctrl-B befriend, Alt-I friend config, Alt-M insert my card |
| Notifications | Alt-N open notifications view |
| Theme | Ctrl-Y toggle theme/alt theme |
| App | Ctrl-S settings, Ctrl-H help, Ctrl-Q quit |

## Editing a binding

1. `Ctrl-S` → **Keyboard Shortcuts**
2. Pick a binding with arrow keys
3. Press Enter to enter capture mode
4. Press the new key combination
5. Press Esc to cancel a capture mid-edit

Conflicts are detected automatically and warned before overwriting.

## Reset

- `Ctrl-R` in the shortcuts editor restores defaults for the
  highlighted entry.
- Delete `~/.config/slackers/shortcuts.json` and restart to reset
  every binding to its default.

The full default mapping lives in `internal/shortcuts/defaults.json`
in the source repo.
