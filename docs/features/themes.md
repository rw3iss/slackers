# Themes

## Built-in themes

11 themes ship embedded via `go:embed` in `internal/theme/builtin/`:

`default`, `dracula`, `cyberpunk`, `solarized_light`, `forest`, `nord`, `synthwave`, `monokai`, `paper`, `matrix`, `sunset`

Users can add custom themes to `~/.config/slackers/themes/*.json`.

## Color value syntax

Each theme is a flat map from semantic color name to a terminal color spec:

| Syntax | Meaning |
|---|---|
| `"12"` | 256-color foreground |
| `"12/240"` | fg 12 on bg 240 |
| `"/235"` | bg only, default fg |
| `"12+b"` | 256-color fg, bold |
| `"229+bi"` | bold + italic |
| `"#ff8800"` | truecolor hex |

Parsed by `internal/theme/theme.go`.

## Live preview & editor

- **Settings → Theme** — arrow through the theme list, preview applies to the whole UI as you move.
- **Theme Editor** (`e` on a theme row) — 256-color picker with separate FG and BG slots, bold/italic toggles, reset key (`r`), and an Export row that writes the current theme JSON to `~/Downloads`.
- **Theme Color Picker** — 16×16 grid, mouse-navigable, `Tab` swaps FG/BG, `Alt-B` / `Alt-I` toggle bold/italic.

All three overlays (`ThemePickerModel`, `ThemeEditorModel`, `ThemeColorPickerModel`) live in `internal/tui/themes_ui.go` — the largest single TUI file (~1,260 lines). It's a candidate for a Phase D split into three files if it grows further.

## Alternate theme + toggle

`cfg.Theme` and `cfg.AltTheme` define a pair; `Ctrl-Y` swaps between them anywhere in the app. Useful for dark/light or high-contrast toggles.

## Adding a new built-in theme

1. Drop `<name>.json` in `internal/theme/builtin/` — every color key in `default.json` is required.
2. Rebuild. The `//go:embed` directive picks up new files automatically.
3. It will appear in the Theme picker on next launch.

## Importing custom themes

- In-app: **Theme Picker → `i`** or the **Import…** row opens a file browser, validates the JSON, and copies to `~/.config/slackers/themes/`. Name collisions prompt overwrite vs. add-alongside (with auto-numbered name).
- CLI: `slackers import-theme <file>` does the same headlessly.

## Styling conventions

- **No magic colors** anywhere outside `styles.go`. Use or add a named color there.
- Theme changes flow through `rebuildDerivedStyles()` — any new style needs to be rebound there or it will stay stale after a theme swap.
- Per-render `lipgloss.NewStyle()` calls are a perf smell. Hoist to package-level `var` and rebind in `rebuildDerivedStyles()`.
