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
}

// NewRemoteBrowser creates the overlay for the given friend.
func NewRemoteBrowser(peerUID, peerName string) RemoteBrowserModel {
	return RemoteBrowserModel{
		peerUID:  peerUID,
		peerName: peerName,
		loading:  true,
	}
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
		return
	}
	m.err = ""
	m.currentPath = resp.Path
	m.entries = resp.Entries
	m.selected = 0
	m.scrollOff = 0
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
			if m.currentPath != "" && m.currentPath != "." {
				// Navigate up.
				parent := filepath.Dir(m.currentPath)
				if parent == "." {
					parent = ""
				}
				m.loading = true
				m.err = ""
				uid := m.peerUID
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: parent}
				}
			}
			return m, func() tea.Msg { return RemoteBrowseCloseMsg{} }
		case "backspace":
			if m.currentPath != "" && m.currentPath != "." {
				parent := filepath.Dir(m.currentPath)
				if parent == "." {
					parent = ""
				}
				m.loading = true
				m.err = ""
				uid := m.peerUID
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: parent}
				}
			}
			return m, nil
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
			m.ensureVisible()
			return m, nil
		case "down", "j":
			if m.selected < len(m.entries)-1 {
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
			if m.selected >= len(m.entries) {
				m.selected = len(m.entries) - 1
			}
			m.ensureVisible()
			return m, nil
		case "enter":
			entry := m.selectedEntry()
			if entry == nil {
				return m, nil
			}
			if entry.IsDir {
				// Navigate into subdirectory.
				subPath := filepath.Join(m.currentPath, entry.Name)
				m.loading = true
				m.err = ""
				uid := m.peerUID
				return m, func() tea.Msg {
					return RemoteBrowseSendRequestMsg{PeerUID: uid, Path: subPath}
				}
			}
			// File: prompt for download.
			m.confirmDL = true
			m.dlEntry = entry.Name
			return m, nil
		}
	}
	return m, nil
}

func (m *RemoteBrowserModel) selectedEntry() *secure.BrowseEntry {
	if m.selected < 0 || m.selected >= len(m.entries) {
		return nil
	}
	return &m.entries[m.selected]
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

func (m RemoteBrowserModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	dirStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	fileStyle := lipgloss.NewStyle().Foreground(ColorDescText)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(ColorError)

	var b strings.Builder

	// Path breadcrumb.
	pathLabel := "/"
	if m.currentPath != "" && m.currentPath != "." {
		pathLabel = "/" + m.currentPath
	}
	b.WriteString(titleStyle.Render("  " + m.peerName + ":" + pathLabel))
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("  Loading..."))
	} else if m.err != "" {
		b.WriteString(errStyle.Render("  Error: " + m.err))
	} else if len(m.entries) == 0 {
		b.WriteString(dimStyle.Render("  (empty folder)"))
	} else {
		visible := m.visibleRows()
		start := m.scrollOff
		end := start + visible
		if end > len(m.entries) {
			end = len(m.entries)
		}
		for i := start; i < end; i++ {
			e := m.entries[i]
			cursor := "  "
			if i == m.selected {
				cursor = selStyle.Render("> ")
			}
			var row string
			if e.IsDir {
				row = cursor + dirStyle.Render("📁 "+e.Name+"/")
			} else {
				size := formatFileSize(e.Size)
				row = cursor + fileStyle.Render("   "+e.Name) + "  " + dimStyle.Render("("+size+")")
			}
			b.WriteString(row + "\n")
		}
	}

	if m.confirmDL {
		b.WriteString("\n")
		b.WriteString(selStyle.Render(fmt.Sprintf("  Download %s? y=yes, any key=cancel", m.dlEntry)))
	}

	footer := "↑↓: navigate" + HintSep + "Enter: open/download" + HintSep + "Backspace: up" + HintSep + FooterHintClose

	scaffold := OverlayScaffold{
		Title:       "Browse Files — " + m.peerName,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 80,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
