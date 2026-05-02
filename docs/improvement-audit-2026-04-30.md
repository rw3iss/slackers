# Improvement Audit — 2026-04-30

## 1. Summary
- Project: slackers
- Working directory: /home/rw3iss/Sites/others/tools/slackers
- Total findings: 9 (UI: 2, styling: 3, architecture: 4)

## 2. UI & UX improvements

### 2.1 Footer hint inconsistency
- **Location**: search.go, emojipicker.go, awaystatus.go, filebrowser.go, fileslist.go
- **Problem**: Several overlays hardcode hint separators (pipes `|` or plain text) instead of using the canonical `HintSep = " · "` and `FooterHintClose`/`FooterHintCancel`/`FooterHintBack` constants from styles.go.
- **Fix**: Replace hardcoded separators and close/cancel/back labels with the shared constants.
- **Risk**: Low

### 2.2 Hardcoded colors outside styles.go
- **Location**: splash.go:32 (`lipgloss.Color("15")`), themes_ui.go:1029 (`lipgloss.Color("252")`)
- **Problem**: Magic color values bypass the theme system.
- **Fix**: Replace with existing palette colors or add named constants in styles.go.
- **Risk**: Low

## 3. Styling & design system

### 3.1 Design token coverage
- **Problem**: The theme/color system in styles.go is comprehensive (40+ named colors), but a few files still use raw color literals.
- **Fix**: Phase A — replace the two known instances. Future passes can grep for remaining `lipgloss.Color("` outside styles.go.
- **Risk**: Low

### 3.2 Context menu title/dim styles duplicated
- **Location**: msgoptions.go, sidebaroptions.go, chatoptions.go, friendcardoptions.go
- **Problem**: Each popup re-creates identical title and dim styles with the same color values.
- **Fix**: Phase B — extract shared popup styles into styles.go or a popupmenu.go base.
- **Risk**: Medium (touches 4 files)

### 3.3 ANSI helper functions defined in wrong file
- **Location**: `ansiTruncatePad` and `ansiAfterCells` in msgoptions.go:244–327
- **Problem**: These are general-purpose ANSI text utilities used by 5+ files, but defined inside msgoptions.go.
- **Fix**: Move to overlayhelpers.go (their natural home).
- **Risk**: Low (pure move, no logic change)

## 4. Architecture & code quality

### 4.1 Context menu code duplication
- **Location**: msgoptions.go, sidebaroptions.go, chatoptions.go, friendcardoptions.go
- **Problem**: Four separate context menu implementations with duplicated: item rendering, cursor navigation, position clamping, click hit-testing, keyboard handling. Each is ~200-300 lines with ~60% identical logic.
- **Fix**: Phase C — extract a generic `PopupMenu` component (like `SelectableList`) that owns items, selection, positioning, and input, with each overlay providing only its custom items and action dispatch.
- **Risk**: High (4 files, ~1000 lines)

### 4.2 Diagnostic debug.Log statements left in
- **Location**: handlers_ui.go (mouse-wheel-up), messages.go (near-top), model.go (LoadMoreHistoryMsg, MoreHistoryLoadedMsg)
- **Problem**: Diagnostic logging from the scroll-up history debugging session. Noisy in --debug mode.
- **Fix**: Remove the diagnostic lines; keep the API-level logs.
- **Risk**: Low

### 4.3 loadMoreContextCmd inefficiency
- **Location**: cmds.go:88-112
- **Problem**: `loadMoreContextCmd` calls both `FetchHistory` (unused result) and `FetchHistoryAround`. The `FetchHistory` call is wasted.
- **Fix**: Remove the unused `FetchHistory` call and the `_ = msgs` line.
- **Risk**: Low

### 4.4 PrependMessages anchor could use ScrollToMessage
- **Location**: messages.go PrependMessages()
- **Problem**: Minor — the anchor logic works but could be more robust by using the anchor message's ID with ScrollToMessage after rebuild (which it already does). No change needed — noting for completeness.
- **Risk**: N/A

## 5. Recommended execution plan

### Phase A (low risk, apply automatically): **DONE**
- ~~Remove diagnostic debug.Log statements (4.2)~~ — **DONE**
- ~~Remove unused FetchHistory call in loadMoreContextCmd (4.3)~~ — **DONE**
- ~~Move ansiTruncatePad/ansiAfterCells to overlayhelpers.go (3.3)~~ — **DONE**
- ~~Replace hardcoded colors with palette constants (2.2)~~ — **DONE** (2026-05-02): added `ColorSplashBanner` to styles.go and switched splash.go to use it; `ColorDescText` (252) was already living in styles.go.

### Phase B (medium risk, apply after user approval): **DONE**
- ~~Standardize footer hints across all overlays (2.1)~~ — **DONE** (2026-05-02): swept emojipicker, awaystatus, filebrowser, fileslist, search, msgsearch, friendrequest, friendsconfig (5 sites), shortcutseditor, notifications_overlay, hidden, notificationsettings, emoteedit, whitelist, rename, settings (3 sites). All overlay footers now use `HintSep` + `FooterHintClose` / `FooterHintBack` / `FooterHintCancel` constants.
- ~~Extract shared popup title/dim styles to styles.go (3.2)~~ — **DONE**: `PopupTitleStyle` and `PopupDimStyle` live in styles.go and are rebuilt by `rebuildDerivedStyles()`.

### Phase C (high risk, plan separately):
- ~~Extract generic PopupMenu component from context menu overlays (4.1)~~ — **DONE**: created `popupmenu.go` with shared `PopupMenu` type; refactored all 4 popup files (msgoptions, sidebaroptions, chatoptions, friendcardoptions) to use it. Net result: -539 lines of duplicated code.
