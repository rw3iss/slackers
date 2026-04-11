package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FileBrowserSelectMsg is sent when the user selects a file or folder.
type FileBrowserSelectMsg struct {
	Path  string
	IsDir bool
}

// FileBrowserFavoritesChangedMsg is dispatched whenever the user adds or
// removes a favorite folder. The model receives this and persists the
// updated list to the user's config.
type FileBrowserFavoritesChangedMsg struct {
	Favorites []string
}

// fileEntry represents a single file or directory in the listing.
type fileEntry struct {
	name        string
	isDir       bool
	size        int64
	modTime     int64 // unix seconds
	createdTime int64 // unix seconds (falls back to modTime on most Linux fs)
	ext         string
}

// FileBrowserConfig holds options for creating a FileBrowserModel.
type FileBrowserConfig struct {
	StartDir    string   // Starting directory (default: user home)
	Title       string   // Overlay title (default: "Select File")
	ShowFiles   bool     // Show files in listing (default: true)
	ShowFolders bool     // Show folders in listing (default: true)
	FileTypes   []string // If non-empty, only show files with these extensions (e.g. [".png", ".jpg"])
	Favorites     []string // Initial list of favorite folder paths
	SortBy        string   // "name", "size", "modified", "type" (default: "name")
	SortAsc       bool     // true = ascending (default: true)
	HideFavorites bool     // true = don't show favorites section (for remote browsers)
}

// fbHitKind identifies what was at a given clickable row.
type fbHitKind int

const (
	fbHitNone fbHitKind = iota
	fbHitFavorite
	fbHitFile
)

// fbHit records a clickable row inside the file browser content area.
// rowOffset is the line offset relative to the inner content area
// (top-left of the box minus border + padding).
type fbHit struct {
	kind  fbHitKind
	index int // index into m.favorites or m.entries depending on kind
	row   int // 0-based row inside the inner content area
}

// fbPane identifies which inner list is currently active.
type fbPane int

const (
	// fbPaneOuter is legacy — treated as fbPaneFiles.
	fbPaneOuter fbPane = iota
	// fbPaneFavorites is active when navigating the favorites list.
	fbPaneFavorites
	// fbPaneFiles is active when navigating the directory listing.
	fbPaneFiles
	// fbPaneFilter is active when the filter input has focus.
	fbPaneFilter
)

// FileBrowserModel provides a file/folder browser overlay.
type FileBrowserModel struct {
	currentDir  string
	entries     []fileEntry
	fileSel     int // selected index in entries
	favSel      int // selected index in favorites
	scrollOff   int
	width       int
	height      int
	title       string
	showFiles   bool
	showFolders bool
	fileTypes   []string
	err         error

	// Favorites navigation state.
	favorites []string
	pane      fbPane
	outerSel  int // 0 = favorites highlighted, 1 = files highlighted (only used in fbPaneOuter)

	// Mouse hit-test data, rebuilt every View() call. Each entry maps a
	// row inside the inner content area to a clickable item.
	hits []fbHit

	// Sorting state.
	sortBy  string // "name", "size", "modified", "created", "type"
	sortAsc bool

	// Filter state.
	filter   textinput.Model
	filtered []fileEntry // entries after filter applied (nil = use entries)

	hideFavorites bool // true for remote browsers
}

// NewFileBrowser creates a new file browser from the given config.
func NewFileBrowser(cfg FileBrowserConfig) FileBrowserModel {
	startDir := cfg.StartDir
	if startDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			startDir = "/"
		} else {
			startDir = home
		}
	}
	startDir, _ = filepath.Abs(startDir)

	title := cfg.Title
	if title == "" {
		title = "Select File"
	}

	showFiles := cfg.ShowFiles
	showFolders := cfg.ShowFolders
	if !showFiles && !showFolders {
		showFiles = true
		showFolders = true
	}

	sortBy := cfg.SortBy
	if sortBy == "" {
		sortBy = "name"
	}

	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 64
	// Starts blurred — focus is on the file list initially.

	m := FileBrowserModel{
		currentDir:  startDir,
		title:       title,
		showFiles:   showFiles,
		showFolders: showFolders,
		fileTypes:   cfg.FileTypes,
		favorites:   append([]string(nil), cfg.Favorites...),
		sortBy:        sortBy,
		sortAsc:       cfg.SortAsc,
		filter:        ti,
		hideFavorites: cfg.HideFavorites,
		pane:          fbPaneFiles,
	}
	m.loadDir()
	return m
}

// loadDir reads the current directory and populates entries.
func (m *FileBrowserModel) loadDir() {
	dirEntries, err := os.ReadDir(m.currentDir)
	if err != nil {
		m.err = err
		m.entries = nil
		return
	}
	m.err = nil

	var dirs []fileEntry
	var files []fileEntry

	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}

		mt := info.ModTime().Unix()
		ct := fileCreatedTime(info)
		if de.IsDir() {
			if m.showFolders {
				dirs = append(dirs, fileEntry{
					name:        de.Name(),
					isDir:       true,
					modTime:     mt,
					createdTime: ct,
				})
			}
		} else {
			if !m.showFiles {
				continue
			}
			if len(m.fileTypes) > 0 {
				ext := strings.ToLower(filepath.Ext(de.Name()))
				matched := false
				for _, ft := range m.fileTypes {
					if strings.ToLower(ft) == ext {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			files = append(files, fileEntry{
				name:        de.Name(),
				isDir:       false,
				size:        info.Size(),
				modTime:     mt,
				createdTime: ct,
				ext:         strings.ToLower(filepath.Ext(de.Name())),
			})
		}
	}

	sortFileEntries(dirs, m.sortBy, m.sortAsc)
	sortFileEntries(files, m.sortBy, m.sortAsc)

	m.entries = nil
	if m.currentDir != "/" {
		m.entries = append(m.entries, fileEntry{name: "..", isDir: true})
	}
	m.entries = append(m.entries, dirs...)
	m.entries = append(m.entries, files...)

	m.filter.SetValue("")
	m.rebuildFiltered()
	m.fileSel = 0
	m.scrollOff = 0
}

// rebuildFiltered applies the current filter text to entries.
func (m *FileBrowserModel) rebuildFiltered() {
	q := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if q == "" {
		m.filtered = m.entries
		return
	}
	m.filtered = nil
	for _, e := range m.entries {
		if e.name == ".." || strings.Contains(strings.ToLower(e.name), q) {
			m.filtered = append(m.filtered, e)
		}
	}
	if m.fileSel >= len(m.filtered) {
		m.fileSel = len(m.filtered) - 1
		if m.fileSel < 0 {
			m.fileSel = 0
		}
	}
}

// filteredEntries returns the active entry list (filtered or all).
func (m *FileBrowserModel) filteredEntries() []fileEntry {
	if m.filtered != nil {
		return m.filtered
	}
	return m.entries
}

// fileCreatedTime tries to extract the birth/creation time from a
// FileInfo. On systems where this isn't available (most Linux
// filesystems), it falls back to the modification time.
func fileCreatedTime(info os.FileInfo) int64 {
	// Go's os.FileInfo doesn't expose birth time portably.
	// Fall back to mod time.
	return info.ModTime().Unix()
}

// sortFileEntries sorts a slice of fileEntry in place by the given
// sort key and direction. Used by both local and remote file browsers.
func sortFileEntries(entries []fileEntry, sortBy string, asc bool) {
	sort.Slice(entries, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "size":
			less = entries[i].size < entries[j].size
		case "modified":
			less = entries[i].modTime < entries[j].modTime
		case "created":
			less = entries[i].createdTime < entries[j].createdTime
		case "type":
			if entries[i].ext != entries[j].ext {
				less = entries[i].ext < entries[j].ext
			} else {
				less = strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
			}
		default: // "name"
			less = strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
		}
		if !asc {
			return !less
		}
		return less
	})
}

// fileSortLabel returns a short display label for the current sort mode.
func fileSortLabel(sortBy string, asc bool) string {
	dir := "↑"
	if !asc {
		dir = "↓"
	}
	switch sortBy {
	case "size":
		return "size " + dir
	case "modified":
		return "modified " + dir
	case "created":
		return "created " + dir
	case "type":
		return "type " + dir
	default:
		return "name " + dir
	}
}

// nextFileSortBy cycles to the next sort mode.
func nextFileSortBy(current string) string {
	switch current {
	case "name":
		return "size"
	case "size":
		return "modified"
	case "modified":
		return "created"
	case "created":
		return "type"
	default:
		return "name"
	}
}

// FileBrowserSortChangedMsg is dispatched when the user changes the
// sort mode in the file browser so the model can persist the preference.
type FileBrowserSortChangedMsg struct {
	SortBy  string
	SortAsc bool
}

// hasFavorites returns true if the favorites section should be shown.
func (m *FileBrowserModel) hasFavorites() bool {
	return !m.hideFavorites && len(m.favorites) > 0
}

// SetSize sets the overlay dimensions.
func (m *FileBrowserModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Favorites returns the current list of favorite folder paths.
func (m FileBrowserModel) Favorites() []string {
	return append([]string(nil), m.favorites...)
}

// addFavorite appends a path to the favorites list if it isn't already
// present. Returns true if the list changed.
func (m *FileBrowserModel) addFavorite(path string) bool {
	for _, f := range m.favorites {
		if f == path {
			return false
		}
	}
	m.favorites = append(m.favorites, path)
	return true
}

// removeFavorite removes the favorite at index i. Returns true if the
// list changed.
func (m *FileBrowserModel) removeFavorite(i int) bool {
	if i < 0 || i >= len(m.favorites) {
		return false
	}
	m.favorites = append(m.favorites[:i], m.favorites[i+1:]...)
	if m.favSel >= len(m.favorites) {
		m.favSel = len(m.favorites) - 1
	}
	if m.favSel < 0 {
		m.favSel = 0
	}
	return true
}

// Update handles key events in the file browser overlay.
func (m FileBrowserModel) Update(msg tea.Msg) (FileBrowserModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.pane {
	case fbPaneOuter:
		m.pane = fbPaneFiles
		return m.updateFiles(keyMsg)
	case fbPaneFavorites:
		return m.updateFavorites(keyMsg)
	case fbPaneFilter:
		return m.updateFilter(keyMsg)
	case fbPaneFiles:
		return m.updateFiles(keyMsg)
	}
	return m, nil
}

// updateFavorites handles keys when navigating inside the favorites list.
func (m FileBrowserModel) updateFavorites(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.favSel > 0 {
			m.favSel--
			m.ensureVisible()
		}
	case "down", "j":
		if m.favSel < len(m.favorites)-1 {
			m.favSel++
			m.ensureVisible()
		} else {
			// At the bottom of favorites — drop into filter bar.
			m.pane = fbPaneFilter
			m.filter.Focus()
		}
	case "enter":
		if m.favSel < 0 || m.favSel >= len(m.favorites) {
			return m, nil
		}
		// Navigate the file pane to the favorite folder, then switch
		// active pane to files with the first entry selected.
		path := m.favorites[m.favSel]
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			m.currentDir = path
			m.loadDir()
			m.pane = fbPaneFiles
			m.fileSel = 0
			m.ensureVisible()
		} else {
			m.err = fmt.Errorf("favorite folder not found: %s", path)
		}
	case "d", "delete":
		// Remove the highlighted favorite.
		if m.removeFavorite(m.favSel) {
			favs := m.Favorites()
			// Drop back to outer if we just emptied the list.
			if len(m.favorites) == 0 {
				m.pane = fbPaneOuter
				m.outerSel = 1
			}
			return m, func() tea.Msg {
				return FileBrowserFavoritesChangedMsg{Favorites: favs}
			}
		}
	case "esc":
		// From favorites, esc goes to filter bar.
		m.pane = fbPaneFilter
		m.filter.Focus()
	case "left", "h":
		m.pane = fbPaneFilter
		m.filter.Focus()
	}
	return m, nil
}

// updateFilter handles keys when the filter input has focus.
func (m FileBrowserModel) updateFilter(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.hasFavorites() {
			m.pane = fbPaneFavorites
			m.favSel = len(m.favorites) - 1
			m.filter.Blur()
		}
		return m, nil
	case "down":
		m.pane = fbPaneFiles
		m.filter.Blur()
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
		return m, nil
	case "enter":
		// Enter from filter → jump to files with current filter applied.
		m.pane = fbPaneFiles
		m.filter.Blur()
		m.fileSel = 0
		m.ensureVisible()
		return m, nil
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.rebuildFiltered()
			return m, nil
		}
		return m, func() tea.Msg { return FileBrowserCancelMsg{} }
	case "alt+s":
		m.sortBy = nextFileSortBy(m.sortBy)
		m.loadDir()
		return m, func() tea.Msg {
			return FileBrowserSortChangedMsg{SortBy: m.sortBy, SortAsc: m.sortAsc}
		}
	case "alt+d":
		m.sortAsc = !m.sortAsc
		m.loadDir()
		return m, func() tea.Msg {
			return FileBrowserSortChangedMsg{SortBy: m.sortBy, SortAsc: m.sortAsc}
		}
	}
	// All other keys go to the filter input.
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuildFiltered()
	return m, cmd
}

// updateFiles handles keys when navigating inside the directory listing.
func (m FileBrowserModel) updateFiles(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	fe := m.filteredEntries()
	switch msg.String() {
	case "up":
		if m.fileSel > 0 {
			m.fileSel--
			m.ensureVisible()
		} else {
			// At top of file list — go to filter bar.
			m.pane = fbPaneFilter
			m.filter.Focus()
		}
		return m, nil
	case "down":
		if m.fileSel < len(fe)-1 {
			m.fileSel++
			m.ensureVisible()
		}
		return m, nil
	case "enter":
		if len(fe) == 0 {
			return m, nil
		}
		entry := fe[m.fileSel]
		if entry.name == ".." {
			m.currentDir = filepath.Dir(m.currentDir)
			m.loadDir()
			return m, nil
		}
		if entry.isDir {
			m.currentDir = filepath.Join(m.currentDir, entry.name)
			m.loadDir()
			return m, nil
		}
		path := filepath.Join(m.currentDir, entry.name)
		return m, func() tea.Msg {
			return FileBrowserSelectMsg{Path: path, IsDir: false}
		}
	case "tab", "right", " ":
		if len(fe) == 0 {
			return m, nil
		}
		entry := fe[m.fileSel]
		if entry.isDir && entry.name != ".." {
			path := filepath.Join(m.currentDir, entry.name)
			return m, func() tea.Msg {
				return FileBrowserSelectMsg{Path: path, IsDir: true}
			}
		}
		if !entry.isDir {
			path := filepath.Join(m.currentDir, entry.name)
			return m, func() tea.Msg {
				return FileBrowserSelectMsg{Path: path, IsDir: false}
			}
		}
	case "ctrl+f":
		// Add the highlighted directory to favorites.
		if len(fe) == 0 {
			return m, nil
		}
		entry := fe[m.fileSel]
		var path string
		switch {
		case entry.name == "..":
			path = m.currentDir
		case entry.isDir:
			path = filepath.Join(m.currentDir, entry.name)
		default:
			return m, nil
		}
		if m.addFavorite(path) {
			favs := m.Favorites()
			return m, func() tea.Msg {
				return FileBrowserFavoritesChangedMsg{Favorites: favs}
			}
		}
	case "ctrl+up":
		if m.currentDir != "/" {
			m.currentDir = filepath.Dir(m.currentDir)
			m.loadDir()
		}
		return m, nil
	case "left":
		// Go to filter bar from files.
		m.pane = fbPaneFilter
		m.filter.Focus()
		return m, nil
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.rebuildFiltered()
			return m, nil
		}
		return m, func() tea.Msg { return FileBrowserCancelMsg{} }
	case "pgup":
		visible := m.visibleLines()
		m.fileSel -= visible
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
		return m, nil
	case "pgdown":
		visible := m.visibleLines()
		m.fileSel += visible
		if m.fileSel >= len(fe) {
			m.fileSel = len(fe) - 1
		}
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
		return m, nil
	case "home":
		m.fileSel = 0
		m.ensureVisible()
		return m, nil
	case "end":
		m.fileSel = len(fe) - 1
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
		return m, nil
	case "alt+s":
		m.sortBy = nextFileSortBy(m.sortBy)
		m.loadDir()
		return m, func() tea.Msg {
			return FileBrowserSortChangedMsg{SortBy: m.sortBy, SortAsc: m.sortAsc}
		}
	case "alt+d":
		m.sortAsc = !m.sortAsc
		m.loadDir()
		return m, func() tea.Msg {
			return FileBrowserSortChangedMsg{SortBy: m.sortBy, SortAsc: m.sortAsc}
		}
	}
	// All other keys go to the filter input.
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuildFiltered()
	return m, cmd
}

// FileBrowserCancelMsg signals the user wants to close the browser
// without selecting anything (e.g. Esc from the outer pane).
type FileBrowserCancelMsg struct{}

// visibleLines returns the number of entry lines visible in the viewport.
// We always reserve a few lines for the favorites section header and any
// favorite rows so the file list can still scroll cleanly underneath.
func (m FileBrowserModel) visibleLines() int {
	// Overhead: border (2) + padding (2) + header (1) + blank (1) +
	//           favorites header+rows (capped) + spacer (1) +
	//           filter (1) + blank (1) + footer (1) + blank (1)
	favRows := 0
	if m.hasFavorites() {
		favRows = len(m.favorites)
		if favRows > 5 {
			favRows = 5
		}
		favRows += 2 // header + spacer
	}
	overhead := 12 + favRows
	v := m.height - 2 - overhead
	if v < 1 {
		v = 1
	}
	return v
}

// ensureVisible adjusts scrollOff to keep the active selection in view.
func (m *FileBrowserModel) ensureVisible() {
	if m.pane != fbPaneFiles {
		return
	}
	visible := m.visibleLines()
	if m.fileSel < m.scrollOff {
		m.scrollOff = m.fileSel
	}
	if m.fileSel >= m.scrollOff+visible {
		m.scrollOff = m.fileSel - visible + 1
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the file browser overlay. Uses a pointer receiver so it
// can record clickable rows into m.hits as a side-effect of rendering.
func (m *FileBrowserModel) View() string {
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	dirStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	fileStyle := lipgloss.NewStyle().
		Foreground(ColorDescText)

	selectedPrefix := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	sizeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	errStyle := lipgloss.NewStyle().
		Foreground(ColorError)

	sectionHeaderStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	// Build content as an explicit slice of lines so we can record an
	// accurate row → action map for mouse hit-testing.
	m.hits = m.hits[:0]
	var lines []string

	// --- Header: path (left) + sort label (right) ---
	contentW := m.width - 16 // account for box border + padding
	if contentW < 30 {
		contentW = 30
	}
	pathDisplay := collapseHome(m.currentDir)
	sortRight := "sorted by " + m.sortBy
	if m.sortAsc {
		sortRight += " (asc)"
	} else {
		sortRight += " (desc)"
	}
	// Abbreviate path if it won't fit alongside the sort label.
	maxPathW := contentW - len(sortRight) - 4
	if maxPathW < 10 {
		maxPathW = 10
	}
	if len(pathDisplay) > maxPathW {
		pathDisplay = "…" + pathDisplay[len(pathDisplay)-maxPathW+1:]
	}
	pad := contentW - lipgloss.Width(pathDisplay) - lipgloss.Width(sortRight)
	if pad < 2 {
		pad = 2
	}
	headerLine := pathStyle.Render(pathDisplay) + strings.Repeat(" ", pad) + dimStyle.Render(sortRight)
	lines = append(lines, headerLine)
	lines = append(lines, "")

	// --- Favorites section (compact, only if visible) ---
	if m.hasFavorites() {
		lines = append(lines, sectionHeaderStyle.Render("  Favorites"))
		favVisible := 5
		if favVisible > len(m.favorites) {
			favVisible = len(m.favorites)
		}
		favScroll := 0
		if m.pane == fbPaneFavorites && m.favSel >= favVisible {
			favScroll = m.favSel - favVisible + 1
		}
		favEnd := favScroll + favVisible
		if favEnd > len(m.favorites) {
			favEnd = len(m.favorites)
		}
		for i := favScroll; i < favEnd; i++ {
			fav := m.favorites[i]
			label := collapseHome(fav)
			isSelected := m.pane == fbPaneFavorites && i == m.favSel
			var rendered string
			if isSelected {
				rendered = selectedPrefix.Render("  > ") +
					lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(label)
			} else {
				rendered = "    " + dirStyle.Render(label)
			}
			m.hits = append(m.hits, fbHit{kind: fbHitFavorite, index: i, row: len(lines)})
			lines = append(lines, rendered)
		}
		if favScroll > 0 || favEnd < len(m.favorites) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("    (%d / %d)", m.favSel+1, len(m.favorites))))
		}
		lines = append(lines, "")
	}

	// --- Filter bar ---
	filterPrefix := "  "
	if m.pane == fbPaneFilter {
		filterPrefix = selectedPrefix.Render("> ")
	}
	lines = append(lines, filterPrefix+m.filter.View())
	lines = append(lines, "")

	// --- File list (table format) ---
	fe := m.filteredEntries()
	if m.err != nil {
		lines = append(lines, errStyle.Render("Error: "+m.err.Error()))
	} else if len(fe) == 0 {
		lines = append(lines, dimStyle.Render("  (empty directory)"))
	} else {
		visible := m.visibleLines()
		end := m.scrollOff + visible
		if end > len(fe) {
			end = len(fe)
		}

		// Determine which date column to show.
		showCreated := m.sortBy == "created"

		for i := m.scrollOff; i < end; i++ {
			entry := fe[i]
			isSelected := m.pane == fbPaneFiles && i == m.fileSel

			row := renderFileRow(entry, isSelected, showCreated, contentW,
				selectedPrefix, dirStyle, fileStyle, sizeStyle, dimStyle)
			m.hits = append(m.hits, fbHit{kind: fbHitFile, index: i, row: len(lines)})
			lines = append(lines, row)
		}

		if m.scrollOff > 0 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more above", m.scrollOff)))
		}
		if end < len(fe) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more below", len(fe)-end)))
		}
	}

	lines = append(lines, "")
	switch m.pane {
	case fbPaneFavorites:
		lines = append(lines, dimStyle.Render("  ↑/↓: navigate | Enter: open | d: remove | Esc/↓: filter"))
	case fbPaneFilter:
		lines = append(lines, dimStyle.Render("  type to filter | ↑: favorites | ↓/Enter: files | Alt+S: sort | Alt+D: dir | Esc: clear/close"))
	default:
		lines = append(lines, dimStyle.Render("  ↑/↓: nav | Enter: open | →/Space: select | Ctrl+↑: parent | Ctrl+F: ★ | Alt+S: sort | Alt+D: dir | Esc: close"))
	}

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth < 30 {
		boxWidth = 30
	}

	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth).
		Height(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Top,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(ColorOverlayFill),
	)
}

// collapseHome replaces the user's home directory prefix with "~/" for
// shorter favorite labels.
func collapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

// boxOrigin returns the (x, y) screen coordinates of the inner content
// area's top-left cell — i.e. the cell where the first rendered line
// starts (just inside the border + padding).
func (m *FileBrowserModel) boxOrigin() (int, int) {
	boxWidth := m.width - 4
	if boxWidth < 30 {
		boxWidth = 30
	}
	boxHeight := m.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}
	// lipgloss.Place centers — leftover whitespace is split evenly.
	leftPad := (m.width - boxWidth) / 2
	topPad := (m.height - boxHeight) / 2
	// Box border (1) + horizontal padding (3) and vertical padding (1).
	x := leftPad + 1 + 3
	y := topPad + 1 + 1
	return x, y
}

// computeHits builds the row → action map for the *current* state
// without rendering. Must mirror the row layout produced by View().
// Returns the slice of hits.
func (m FileBrowserModel) computeHits() []fbHit {
	var hits []fbHit
	row := 0
	// header line (path + sort)
	row++
	// blank
	row++

	// favorites section (only if visible)
	if m.hasFavorites() {
		row++ // "Favorites" header
		favVisible := 5
		if favVisible > len(m.favorites) {
			favVisible = len(m.favorites)
		}
		favScroll := 0
		if m.pane == fbPaneFavorites && m.favSel >= favVisible {
			favScroll = m.favSel - favVisible + 1
		}
		favEnd := favScroll + favVisible
		if favEnd > len(m.favorites) {
			favEnd = len(m.favorites)
		}
		for i := favScroll; i < favEnd; i++ {
			hits = append(hits, fbHit{kind: fbHitFavorite, index: i, row: row})
			row++
		}
		if favScroll > 0 || favEnd < len(m.favorites) {
			row++ // pagination indicator
		}
		row++ // blank spacer
	}

	// filter bar
	row++
	// blank
	row++

	fe := m.filteredEntries()
	if m.err != nil {
		row++
	} else if len(fe) == 0 {
		row++
	} else {
		visible := m.visibleLines()
		end := m.scrollOff + visible
		if end > len(fe) {
			end = len(fe)
		}
		for i := m.scrollOff; i < end; i++ {
			hits = append(hits, fbHit{kind: fbHitFile, index: i, row: row})
			row++
		}
	}
	return hits
}

// UpdateMouse handles mouse events. Left-clicks on a clickable row act
// as if the user navigated to that row and pressed Enter.
func (m FileBrowserModel) UpdateMouse(msg tea.MouseMsg) (FileBrowserModel, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	ox, oy := m.boxOrigin()
	row := msg.Y - oy
	col := msg.X - ox
	if row < 0 || col < 0 {
		return m, nil
	}
	hits := m.computeHits()
	for _, h := range hits {
		if h.row != row {
			continue
		}
		switch h.kind {
		case fbHitFavorite:
			if h.index < 0 || h.index >= len(m.favorites) {
				return m, nil
			}
			m.pane = fbPaneFavorites
			m.favSel = h.index
			// Synthesize an Enter so the existing handler navigates
			// the file pane to the favorite and switches focus.
			return m.updateFavorites(tea.KeyMsg{Type: tea.KeyEnter})
		case fbHitFile:
			if h.index < 0 || h.index >= len(m.entries) {
				return m, nil
			}
			m.pane = fbPaneFiles
			m.fileSel = h.index
			return m.updateFiles(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return m, nil
}

// formatFileSize returns a human-readable file size string.
// renderFileRow renders one table-format row for a file entry:
//
//	> name.txt          1.2 KB   Apr 10 14:30
//	  folder/                     Apr 09 11:15
func renderFileRow(
	entry fileEntry, selected, showCreated bool, contentW int,
	selPfx, dirSty, fileSty, sizeSty, dimSty lipgloss.Style,
) string {
	prefix := "  "
	if selected {
		prefix = selPfx.Render("> ")
	}

	// Right columns: size (8 chars) + gap (2) + date (12 chars) = 22
	const sizeW = 8
	const dateW = 12
	const rightW = sizeW + 2 + dateW
	nameW := contentW - 2 - rightW - 2 // 2 for prefix, 2 for gaps
	if nameW < 10 {
		nameW = 10
	}

	// Name
	name := entry.name
	if entry.isDir && name != ".." {
		name += "/"
	}
	if len(name) > nameW {
		name = name[:nameW-1] + "…"
	}

	var nameStyled string
	if entry.isDir {
		if selected {
			nameStyled = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(name)
		} else {
			nameStyled = dirSty.Render(name)
		}
	} else {
		if selected {
			nameStyled = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render(name)
		} else {
			nameStyled = fileSty.Render(name)
		}
	}
	// Pad name to nameW using visible width.
	nameVis := lipgloss.Width(nameStyled)
	namePad := nameW - nameVis
	if namePad < 0 {
		namePad = 0
	}

	// Size column (right-aligned in sizeW chars).
	var sizeStr string
	if !entry.isDir {
		sizeStr = formatFileSize(entry.size)
	}
	sizePad := sizeW - len(sizeStr)
	if sizePad < 0 {
		sizePad = 0
	}
	sizeCol := strings.Repeat(" ", sizePad) + sizeSty.Render(sizeStr)

	// Date column.
	var ts int64
	if showCreated {
		ts = entry.createdTime
	} else {
		ts = entry.modTime
	}
	dateStr := ""
	if ts > 0 {
		dateStr = formatCompactDate(ts)
	}
	datePad := dateW - len(dateStr)
	if datePad < 0 {
		datePad = 0
	}
	dateCol := strings.Repeat(" ", datePad) + dimSty.Render(dateStr)

	return prefix + nameStyled + strings.Repeat(" ", namePad) + "  " + sizeCol + "  " + dateCol
}

// formatCompactDate formats a unix timestamp as a compact date string.
func formatCompactDate(unix int64) string {
	t := time.Unix(unix, 0)
	now := time.Now()
	if t.Year() == now.Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}

func formatFileSize(size int64) string {
	const (
		_        = iota
		kB int64 = 1 << (10 * iota)
		mB
		gB
	)
	switch {
	case size >= gB:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(gB))
	case size >= mB:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mB))
	case size >= kB:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(kB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
