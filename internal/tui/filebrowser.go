package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	name  string
	isDir bool
	size  int64
}

// FileBrowserConfig holds options for creating a FileBrowserModel.
type FileBrowserConfig struct {
	StartDir    string   // Starting directory (default: user home)
	Title       string   // Overlay title (default: "Select File")
	ShowFiles   bool     // Show files in listing (default: true)
	ShowFolders bool     // Show folders in listing (default: true)
	FileTypes   []string // If non-empty, only show files with these extensions (e.g. [".png", ".jpg"])
	Favorites   []string // Initial list of favorite folder paths
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
	// fbPaneOuter is the initial top-level mode where the user picks
	// between the favorites list and the files list using up/down.
	// Right/Enter enters the chosen list.
	fbPaneOuter fbPane = iota
	// fbPaneFavorites is active when the user is navigating within the
	// favorites list. Esc/Left returns to the outer mode.
	fbPaneFavorites
	// fbPaneFiles is active when the user is navigating within the
	// directory listing.
	fbPaneFiles
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

	m := FileBrowserModel{
		currentDir:  startDir,
		title:       title,
		showFiles:   showFiles,
		showFolders: showFolders,
		fileTypes:   cfg.FileTypes,
		favorites:   append([]string(nil), cfg.Favorites...),
		// Start in outer-selection mode. Default to highlighting the
		// favorites list when the user has any saved, otherwise fall
		// back to the files list so existing single-Enter workflows
		// still work.
		pane: fbPaneOuter,
	}
	if len(m.favorites) > 0 {
		m.outerSel = 0
	} else {
		m.outerSel = 1
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

		if de.IsDir() {
			if m.showFolders {
				dirs = append(dirs, fileEntry{
					name:  de.Name(),
					isDir: true,
					size:  0,
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
				name:  de.Name(),
				isDir: false,
				size:  info.Size(),
			})
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].name) < strings.ToLower(files[j].name)
	})

	m.entries = nil
	if m.currentDir != "/" {
		m.entries = append(m.entries, fileEntry{name: "..", isDir: true})
	}
	m.entries = append(m.entries, dirs...)
	m.entries = append(m.entries, files...)

	m.fileSel = 0
	m.scrollOff = 0
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
		return m.updateOuter(keyMsg)
	case fbPaneFavorites:
		return m.updateFavorites(keyMsg)
	case fbPaneFiles:
		return m.updateFiles(keyMsg)
	}
	return m, nil
}

// updateOuter handles keys in the top-level mode where up/down picks
// between the favorites list and the files list.
func (m FileBrowserModel) updateOuter(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.outerSel > 0 {
			m.outerSel--
		}
	case "down", "j":
		// On the Files row, pressing down auto-enters the file list
		// with the first entry selected (same as pressing right/Enter).
		if m.outerSel == 1 && len(m.entries) > 0 {
			m.pane = fbPaneFiles
			m.fileSel = 0
			m.ensureVisible()
			return m, nil
		}
		if m.outerSel < 1 {
			m.outerSel++
		}
	case "right", "l", "enter", "tab", " ", "space":
		if m.outerSel == 0 {
			// Enter favorites only if there are any.
			if len(m.favorites) == 0 {
				return m, nil
			}
			m.pane = fbPaneFavorites
			if m.favSel < 0 || m.favSel >= len(m.favorites) {
				m.favSel = 0
			}
		} else {
			m.pane = fbPaneFiles
			if m.fileSel < 0 || m.fileSel >= len(m.entries) {
				m.fileSel = 0
			}
			m.ensureVisible()
		}
	case "esc":
		// Outer-mode esc closes the browser.
		return m, func() tea.Msg { return FileBrowserCancelMsg{} }
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
		} else if len(m.entries) > 0 {
			// At the bottom of favorites — seamlessly drop into the
			// files list with the first entry selected.
			m.pane = fbPaneFiles
			m.fileSel = 0
			m.ensureVisible()
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
	case "left", "h", "esc":
		m.pane = fbPaneOuter
		m.outerSel = 0
	}
	return m, nil
}

// updateFiles handles keys when navigating inside the directory listing.
func (m FileBrowserModel) updateFiles(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.fileSel > 0 {
			m.fileSel--
			m.ensureVisible()
		} else if len(m.favorites) > 0 {
			// At the top of the files list — seamlessly jump up into
			// the favorites list with the last favorite selected.
			m.pane = fbPaneFavorites
			m.favSel = len(m.favorites) - 1
		}
	case "down", "j":
		if m.fileSel < len(m.entries)-1 {
			m.fileSel++
			m.ensureVisible()
		}
	case "enter":
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.fileSel]
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
	case "tab", "right":
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.fileSel]
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
	case "f", "F":
		// Add the highlighted directory to favorites. If the highlighted
		// row is "..", favorite the current directory itself.
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.fileSel]
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
	case "backspace", "ctrl+up":
		if m.currentDir != "/" {
			m.currentDir = filepath.Dir(m.currentDir)
			m.loadDir()
		}
	case "left", "h":
		// Back out to the outer pane selector.
		m.pane = fbPaneOuter
		m.outerSel = 1
	case "esc":
		m.pane = fbPaneOuter
		m.outerSel = 1
	case "pgup":
		visible := m.visibleLines()
		m.fileSel -= visible
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
	case "pgdown":
		visible := m.visibleLines()
		m.fileSel += visible
		if m.fileSel >= len(m.entries) {
			m.fileSel = len(m.entries) - 1
		}
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
	case "home":
		m.fileSel = 0
		m.ensureVisible()
	case "end":
		m.fileSel = len(m.entries) - 1
		if m.fileSel < 0 {
			m.fileSel = 0
		}
		m.ensureVisible()
	}
	return m, nil
}

// FileBrowserCancelMsg signals the user wants to close the browser
// without selecting anything (e.g. Esc from the outer pane).
type FileBrowserCancelMsg struct{}

// visibleLines returns the number of entry lines visible in the viewport.
// We always reserve a few lines for the favorites section header and any
// favorite rows so the file list can still scroll cleanly underneath.
func (m FileBrowserModel) visibleLines() int {
	// Overhead: border (2) + padding (2) + title (1) + blank (1) +
	//           favorites header (1) + favorite rows (capped) + spacer (1) +
	//           path (1) + blank (1) + footer (1) + blank (1)
	favRows := len(m.favorites)
	if favRows > 5 {
		favRows = 5
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
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	pathStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	dirStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	fileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

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

	outerHighlightStyle := lipgloss.NewStyle().
		Foreground(ColorHighlight).
		Bold(true)

	// Build content as an explicit slice of lines so we can record an
	// accurate row → action map for mouse hit-testing.
	m.hits = m.hits[:0]
	var lines []string

	lines = append(lines, titleStyle.Render(m.title))
	lines = append(lines, "")

	// --- Favorites section ---
	favHeader := "Favorites"
	if m.pane == fbPaneOuter && m.outerSel == 0 {
		lines = append(lines, outerHighlightStyle.Render("▶ "+favHeader))
	} else {
		lines = append(lines, "  "+sectionHeaderStyle.Render(favHeader))
	}

	if len(m.favorites) == 0 {
		lines = append(lines, dimStyle.Render("    (none — press 'f' on a folder to add)"))
	} else {
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
	}

	lines = append(lines, "")

	// --- Files section ---
	filesHeader := "Files"
	if m.pane == fbPaneOuter && m.outerSel == 1 {
		lines = append(lines, outerHighlightStyle.Render("▶ "+filesHeader))
	} else {
		lines = append(lines, "  "+sectionHeaderStyle.Render(filesHeader))
	}
	lines = append(lines, pathStyle.Render("  "+m.currentDir))
	lines = append(lines, "")

	if m.err != nil {
		lines = append(lines, errStyle.Render("Error: "+m.err.Error()))
	} else if len(m.entries) == 0 {
		lines = append(lines, dimStyle.Render("  (empty directory)"))
	} else {
		visible := m.visibleLines()
		end := m.scrollOff + visible
		if end > len(m.entries) {
			end = len(m.entries)
		}

		for i := m.scrollOff; i < end; i++ {
			entry := m.entries[i]

			isSelected := m.pane == fbPaneFiles && i == m.fileSel
			var prefix string
			if isSelected {
				prefix = selectedPrefix.Render("> ")
			} else {
				prefix = "  "
			}

			var rendered string
			if entry.isDir {
				name := entry.name
				if name != ".." {
					name += "/"
				}
				if isSelected {
					rendered = prefix + lipgloss.NewStyle().
						Foreground(ColorAccent).Bold(true).Render(name)
				} else {
					rendered = prefix + dirStyle.Render(name)
				}
			} else {
				var nameStyled string
				if isSelected {
					nameStyled = lipgloss.NewStyle().
						Foreground(ColorPrimary).Bold(true).Render(entry.name)
				} else {
					nameStyled = fileStyle.Render(entry.name)
				}
				rendered = prefix + nameStyled + "  " +
					sizeStyle.Render(formatFileSize(entry.size))
			}
			m.hits = append(m.hits, fbHit{kind: fbHitFile, index: i, row: len(lines)})
			lines = append(lines, rendered)
		}

		if m.scrollOff > 0 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more above", m.scrollOff)))
		}
		if end < len(m.entries) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more below", len(m.entries)-end)))
		}
	}

	lines = append(lines, "")
	switch m.pane {
	case fbPaneOuter:
		lines = append(lines, dimStyle.Render("  ↑/↓: pick list | →/Enter/Tab/Space: enter list | Esc: cancel"))
	case fbPaneFavorites:
		lines = append(lines, dimStyle.Render("  ↑/↓: navigate | Enter: open | d: remove | ←/Esc: back"))
	case fbPaneFiles:
		if m.showFiles {
			lines = append(lines, dimStyle.Render("  ↑/↓: nav | Enter: open/select | Tab/→: pick | Ctrl+↑/⌫: parent | f: ★ favorite | ←/Esc: back"))
		} else {
			lines = append(lines, dimStyle.Render("  ↑/↓: nav | Enter: open | Tab/→: pick folder | Ctrl+↑/⌫: parent | f: ★ favorite | ←/Esc: back"))
		}
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
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
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
	// title
	row++
	// blank
	row++
	// favorites header
	row++

	if len(m.favorites) == 0 {
		// "(none — press 'f' …)" placeholder
		row++
	} else {
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
	}

	// blank between sections
	row++
	// files header
	row++
	// path line
	row++
	// blank
	row++

	if m.err != nil {
		row++
	} else if len(m.entries) == 0 {
		row++
	} else {
		visible := m.visibleLines()
		end := m.scrollOff + visible
		if end > len(m.entries) {
			end = len(m.entries)
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
