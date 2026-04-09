# Themes

Slackers ships with 15 built-in themes (9 dark, 6 light):

**Dark:** `default` · `cyberpunk` · `dracula` · `forest` · `matrix`
· `monokai` · `nord` · `sunset` · `synthwave`

**Light:** `aurora` · `mint_cream` · `paper` · `sakura` · `soft_sun`
· `solarized_light`

Light themes are tagged `(light)` after the name in the picker and
in `Settings → Theme` so you can spot them at a glance.

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
