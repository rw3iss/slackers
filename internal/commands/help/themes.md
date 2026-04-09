# Themes

Slackers ships with 11 built-in themes:

`default` · `dracula` · `cyberpunk` · `solarized_light` · `forest`
· `nord` · `synthwave` · `monokai` · `paper` · `matrix` · `sunset`

## Picking a theme

- **Settings → Theme** — arrow through with live preview, Enter to confirm
- **`/theme <name>`** — switch directly from the input bar
- **`/themes`** — list every installed theme

## Alternate theme + toggle

Set both **Theme** and **Alt Theme** in Settings → Appearance, then
press **Ctrl-Y** anywhere in the app to swap between them.

## Color value syntax

| Syntax | Meaning |
|---|---|
| `"12"` | 256-color foreground |
| `"12/240"` | fg 12 on bg 240 |
| `"/235"` | bg only, default fg |
| `"12+b"` | 256-color fg, bold |
| `"229+bi"` | bold + italic |
| `"#ff8800"` | truecolor hex |

## Custom themes

Drop JSON files into `~/.config/slackers/themes/`. Use the existing
`internal/theme/builtin/default.json` as a template — every key is
required.

The in-app **theme editor** (Settings → Theme → `e` on a row) lets
you tweak each color with a 16×16 picker, separate FG / BG slots,
bold / italic toggles, and an Export row that writes the result back
to `~/Downloads/<name>.json`.

CLI: `slackers import-theme <file>` validates and copies a theme
JSON straight into your themes folder.
