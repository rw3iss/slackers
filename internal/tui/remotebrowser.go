package tui

// Remote file browser overlay — lets the user browse a friend's
// shared folder and download files. Data comes from P2P
// browse_request/browse_response messages; the overlay sends
// requests and renders responses as a navigable file list.
//
// Navigation:
//   - Up/Down: select entries
//   - Enter on a directory: navigate into it (sends new request)
//   - Enter on a file: prompt to download
//   - Backspace: navigate up (sends request for parent dir)
//   - Esc: close (or navigate up if not at root)

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/secure"
)

// Message types for the remote browser overlay.

// RemoteBrowseOpenMsg opens the browser for a friend.
type RemoteBrowseOpenMsg struct {
	PeerUID  string
	PeerName string
}

// RemoteBrowseResultMsg delivers a browse response from P2P.
type RemoteBrowseResultMsg struct {
	PeerUID  string
	Response secure.BrowseResponse
}

// RemoteBrowseCloseMsg closes the browser overlay.
type RemoteBrowseCloseMsg struct{}

// RemoteBrowseDownloadMsg requests downloading a file.
type RemoteBrowseDownloadMsg struct {
	PeerUID      string
	RelativePath string
	FileName     string
	FileSize     int64
}

// RemoteBrowseSendRequestMsg tells the model to send a browse
// request to the peer for the given path. The model handles the
// actual P2P send since it owns the p2pNode.
type RemoteBrowseSendRequestMsg struct {
	PeerUID string
	Path    string
	SortBy  string
	SortDir string
}

// RemoteBrowserModel is the overlay state.
type RemoteBrowserModel struct {
	peerUID     string
	peerName    string
	currentPath string // relative path (empty = root)
	entries     []secure.BrowseEntry
	selected    int
	scrollOff   int
	loading     bool
	err         string
	confirmDL   bool   // true when prompting for download
	dlEntry     string // file name being prompted
	width       int
	height      int
	sortBy      string // "name", "size", "modified", "type"
	sortAsc     bool
	filter      textinput.Model
	filtered    []secure.BrowseEntry // entries after filter applied
}

// NewRemoteBrowser creates the overlay for the given friend.
func NewRemoteBrowser(peerUID, peerName, sortBy string, sortAsc bool) RemoteBrowserModel {
	if sortBy == "" {
		sortBy = "name"
	}
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 64
	ti.Focus()
	return RemoteBrowserModel{
		peerUID:  peerUID,
		peerName: peerName,
		loading:  true,
		sortBy:   sortBy,
		sortAsc:  sortAsc,
		filter:   ti,
	}
}

func (m RemoteBrowserModel) sortDirStr() string {
	if m.sortAsc {
		return "asc"
	}
	return "desc"
}

func (m *RemoteBrowserModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// ApplyResult populates the browser with a browse response.
func (m *RemoteBrowserModel) ApplyResult(resp secure.BrowseResponse) {
	m.loading = false
	if resp.Error != "" {
		m.err = resp.Error
		m.entries = nil
		m.filtered = nil
		return
	}
	m.err = ""
	m.currentPath = resp.Path
	m.entries = resp.Entries
	m.filter.SetValue("")
	m.rebuildFiltered()
	m.selected = 0
	m.scrollOff = 0
}

func (m *RemoteBrowserModel) rebuildFiltered() {
	q := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if q == "" {
		m.filtered = m.entries
		return
	}
	m.filtered = nil
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			m.filtered = append(m.filtered, e)
		}
	}
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
		if m.selected < 0 {
			m.selected = 0
		}
	}
}

func (m RemoteBrowserModel) Update(msg tea.Msg) (RemoteBrowserModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Download confirmation.
		if m.confirmDL {
			switch v.String() {
			case "y", "Y", "enter":
				m.confirmDL = false
				entry := m.selectedEntry()
				if entry != nil && !entry.IsDir {
					relPath := filepath.Join(m.currentPath, entry.Name)
					dl := RemoteBrowseDownloadMsg{
						PeerUID:      m.peerUID,
						RelativePath: relPath,
						FileName:     entry.Name,
						FileSize:     entry.Size,
					}
					return m, func() tea.Msg { return dl }
				}
				return m, nil
			default:
				m.confirmDL = false
				m.dlEntry = ""
				return m, nil
			}
		}

		switch v.String() {
		case "esc":
			if m.filter.Value() != "" {
				m.filter.SetValue("")
				m.rebuildFiltered()
				return m, nil
			}
			if m.currentPath != "" && m.currentPath != "." {
				parent := filepath.Dir(m.currentPath)
				if parent == "." {
					parent = ""
				}
				m.loading = true
				m.err = ""
				uid := m.peerUID
				sb, sd := m.sortBy, m.sortDirStr()
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: parent, SortBy: sb, SortDir: sd}
				}
			}
			return m, func() tea.Msg { return RemoteBrowseCloseMsg{} }
		case "ctrl+up":
			if m.currentPath != "" && m.currentPath != "." {
				parent := filepath.Dir(m.currentPath)
				if parent == "." {
					parent = ""
				}
				m.loading = true
				m.err = ""
				uid := m.peerUID
				sb, sd := m.sortBy, m.sortDirStr()
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: parent, SortBy: sb, SortDir: sd}
				}
			}
			return m, nil
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			m.ensureVisible()
			return m, nil
		case "down":
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			m.ensureVisible()
			return m, nil
		case "pgup":
			m.selected -= 10
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
			return m, nil
		case "pgdown":
			m.selected += 10
			if m.selected >= len(m.filtered) {
				m.selected = len(m.filtered) - 1
			}
			m.ensureVisible()
			return m, nil
		case "enter":
			entry := m.selectedEntry()
			if entry == nil {
				return m, nil
			}
			if entry.IsDir {
				subPath := filepath.Join(m.currentPath, entry.Name)
				m.loading = true
				m.err = ""
				uid := m.peerUID
				sb, sd := m.sortBy, m.sortDirStr()
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: subPath, SortBy: sb, SortDir: sd}
				}
			}
			m.confirmDL = true
			m.dlEntry = entry.Name
			return m, nil
		case "alt+s":
			// Cycle sort mode and re-request.
			m.sortBy = nextFileSortBy(m.sortBy)
			m.loading = true
			uid := m.peerUID
			path := m.currentPath
			sb, sd := m.sortBy, m.sortDirStr()
			return m, func() tea.Msg {
				return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: path, SortBy: sb, SortDir: sd}
			}
		case "alt+d":
			// Toggle sort direction and re-request.
			m.sortAsc = !m.sortAsc
			m.loading = true
			uid := m.peerUID
			path := m.currentPath
			sb, sd := m.sortBy, m.sortDirStr()
			return m, func() tea.Msg {
				return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: path, SortBy: sb, SortDir: sd}
			}
		}
		// All other keys go to the filter input.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildFiltered()
		return m, cmd
	}
	return m, nil
}

func (m *RemoteBrowserModel) selectedEntry() *secure.BrowseEntry {
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.selected]
}

func (m *RemoteBrowserModel) ensureVisible() {
	visible := m.visibleRows()
	if m.selected < m.scrollOff {
		m.scrollOff = m.selected
	}
	if m.selected >= m.scrollOff+visible {
		m.scrollOff = m.selected - visible + 1
	}
}

func (m *RemoteBrowserModel) visibleRows() int {
	v := m.height - 10
	if v < 3 {
		v = 3
	}
	return v
}

// browseEntryToFileEntry converts a BrowseEntry to a fileEntry for the
// shared renderFileRow helper.
func browseEntryToFileEntry(e secure.BrowseEntry) fileEntry {
	return fileEntry{
		name:        e.Name,
		isDir:       e.IsDir,
		size:        e.Size,
		modTime:     e.ModTime.Unix(),
		createdTime: e.CreateTime.Unix(),
		ext:         strings.ToLower(filepath.Ext(e.Name)),
	}
}

func (m RemoteBrowserModel) View() string {
	pathStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	dirStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	fileStyle := lipgloss.NewStyle().Foreground(ColorDescText)
	selPfx := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	sizeStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	errStyle := lipgloss.NewStyle().Foreground(ColorError)

	contentW := m.width - 16
	if contentW < 30 {
		contentW = 30
	}

	var lines []string

	// Header: path (left) + sort label (right)
	pathLabel := m.peerName + ":/"
	if m.currentPath != "" && m.currentPath != "." {
		pathLabel = m.peerName + ":/" + m.currentPath
	}
	sortRight := "sorted by " + m.sortBy
	if m.sortAsc {
		sortRight += " (asc)"
	} else {
		sortRight += " (desc)"
	}
	maxPathW := contentW - len(sortRight) - 4
	if maxPathW < 10 {
		maxPathW = 10
	}
	if len(pathLabel) > maxPathW {
		pathLabel = "…" + pathLabel[len(pathLabel)-maxPathW+1:]
	}
	pad := contentW - lipgloss.Width(pathLabel) - lipgloss.Width(sortRight)
	if pad < 2 {
		pad = 2
	}
	lines = append(lines, pathStyle.Render(pathLabel)+strings.Repeat(" ", pad)+dimStyle.Render(sortRight))
	lines = append(lines, "")

	// Filter bar
	lines = append(lines, "  "+m.filter.View())
	lines = append(lines, "")

	showCreated := m.sortBy == "created"

	if m.loading {
		lines = append(lines, dimStyle.Render("  Loading..."))
	} else if m.err != "" {
		lines = append(lines, errStyle.Render("  Error: "+m.err))
	} else if len(m.filtered) == 0 {
		lines = append(lines, dimStyle.Render("  (empty folder)"))
	} else {
		visible := m.visibleRows()
		start := m.scrollOff
		end := start + visible
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := start; i < end; i++ {
			fe := browseEntryToFileEntry(m.filtered[i])
			selected := i == m.selected
			row := renderFileRow(fe, selected, showCreated, contentW,
				selPfx, dirStyle, fileStyle, sizeStyle, dimStyle)
			lines = append(lines, row)
		}

		if m.scrollOff > 0 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more above", m.scrollOff)))
		}
		if end < len(m.filtered) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d more below", len(m.filtered)-end)))
		}
	}

	if m.confirmDL {
		lines = append(lines, "")
		lines = append(lines, selPfx.Render(fmt.Sprintf("  Download %s? y=yes, any key=cancel", m.dlEntry)))
	}

	lines = append(lines, "")
	sortLabel := fileSortLabel(m.sortBy, m.sortAsc)
	lines = append(lines, dimStyle.Render("  ↑↓: navigate"+HintSep+"Enter: open/download"+HintSep+"Ctrl+↑: up"+HintSep+"Alt+S: sort ("+sortLabel+")"+HintSep+"Alt+D: dir"+HintSep+FooterHintClose))

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
