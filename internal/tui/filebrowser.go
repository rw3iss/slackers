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

// fileEntry represents a single file or directory in the listing.
type fileEntry struct {
	name  string
	isDir bool
	size  int64
}

// FileBrowserConfig holds options for creating a FileBrowserModel.
type FileBrowserConfig struct {
	StartDir   string   // Starting directory (default: user home)
	Title      string   // Overlay title (default: "Select File")
	ShowFiles  bool     // Show files in listing (default: true)
	ShowFolders bool    // Show folders in listing (default: true)
	FileTypes  []string // If non-empty, only show files with these extensions (e.g. [".png", ".jpg"])
}

// FileBrowserModel provides a file/folder browser overlay.
type FileBrowserModel struct {
	currentDir string
	entries    []fileEntry
	selected   int
	scrollOff  int
	width      int
	height     int
	title      string
	showFiles  bool
	showFolders bool
	fileTypes  []string
	err        error
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
	// Resolve to absolute path.
	startDir, _ = filepath.Abs(startDir)

	title := cfg.Title
	if title == "" {
		title = "Select File"
	}

	showFiles := cfg.ShowFiles
	showFolders := cfg.ShowFolders
	// Default both to true if neither is set.
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
			// Filter by extension if fileTypes is set.
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
	// Add parent directory entry unless at root.
	if m.currentDir != "/" {
		m.entries = append(m.entries, fileEntry{name: "..", isDir: true})
	}
	m.entries = append(m.entries, dirs...)
	m.entries = append(m.entries, files...)

	m.selected = 0
	m.scrollOff = 0
}

// SetSize sets the overlay dimensions.
func (m *FileBrowserModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the file browser overlay.
func (m FileBrowserModel) Update(msg tea.Msg) (FileBrowserModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.ensureVisible()
			}

		case "down", "j":
			if m.selected < len(m.entries)-1 {
				m.selected++
				m.ensureVisible()
			}

		case "enter":
			if len(m.entries) == 0 {
				break
			}
			entry := m.entries[m.selected]
			if entry.name == ".." {
				m.currentDir = filepath.Dir(m.currentDir)
				m.loadDir()
			} else if entry.isDir {
				// Enter always navigates into the directory.
				m.currentDir = filepath.Join(m.currentDir, entry.name)
				m.loadDir()
			} else {
				// File selected.
				path := filepath.Join(m.currentDir, entry.name)
				return m, func() tea.Msg {
					return FileBrowserSelectMsg{Path: path, IsDir: false}
				}
			}

		case "tab", "right":
			if len(m.entries) == 0 {
				break
			}
			entry := m.entries[m.selected]
			if entry.isDir && entry.name != ".." {
				// Select/confirm the highlighted folder.
				path := filepath.Join(m.currentDir, entry.name)
				return m, func() tea.Msg {
					return FileBrowserSelectMsg{Path: path, IsDir: true}
				}
			} else if !entry.isDir {
				// Select a file too via tab/right.
				path := filepath.Join(m.currentDir, entry.name)
				return m, func() tea.Msg {
					return FileBrowserSelectMsg{Path: path, IsDir: false}
				}
			}

		case "backspace", "left":
			if m.currentDir != "/" {
				m.currentDir = filepath.Dir(m.currentDir)
				m.loadDir()
			}

		case "pgup":
			visible := m.visibleLines()
			m.selected -= visible
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()

		case "pgdown":
			visible := m.visibleLines()
			m.selected += visible
			if m.selected >= len(m.entries) {
				m.selected = len(m.entries) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()

		case "home":
			m.selected = 0
			m.ensureVisible()

		case "end":
			m.selected = len(m.entries) - 1
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
		}
	}
	return m, nil
}

// visibleLines returns the number of entry lines visible in the viewport.
func (m FileBrowserModel) visibleLines() int {
	// Account for: border (2) + padding (2) + title (1) + blank (1) + path (1) + blank (1) + footer (1) + blank (1)
	overhead := 10
	v := m.height - 2 - overhead
	if v < 1 {
		v = 1
	}
	return v
}

// ensureVisible adjusts scrollOff to keep the selected item in view.
func (m *FileBrowserModel) ensureVisible() {
	visible := m.visibleLines()
	if m.selected < m.scrollOff {
		m.scrollOff = m.selected
	}
	if m.selected >= m.scrollOff+visible {
		m.scrollOff = m.selected - visible + 1
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// View renders the file browser overlay.
func (m FileBrowserModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

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

	var b strings.Builder

	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(pathStyle.Render(m.currentDir))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n")
	} else if len(m.entries) == 0 {
		b.WriteString(dimStyle.Render("  (empty directory)"))
		b.WriteString("\n")
	} else {
		visible := m.visibleLines()
		end := m.scrollOff + visible
		if end > len(m.entries) {
			end = len(m.entries)
		}

		for i := m.scrollOff; i < end; i++ {
			entry := m.entries[i]

			if i == m.selected {
				b.WriteString(selectedPrefix.Render("> "))
			} else {
				b.WriteString("  ")
			}

			if entry.isDir {
				name := entry.name
				if name != ".." {
					name += "/"
				}
				if i == m.selected {
					b.WriteString(lipgloss.NewStyle().
						Foreground(ColorAccent).Bold(true).Render(name))
				} else {
					b.WriteString(dirStyle.Render(name))
				}
			} else {
				if i == m.selected {
					b.WriteString(lipgloss.NewStyle().
						Foreground(ColorPrimary).Bold(true).Render(entry.name))
				} else {
					b.WriteString(fileStyle.Render(entry.name))
				}
				b.WriteString("  ")
				b.WriteString(sizeStyle.Render(formatFileSize(entry.size)))
			}
			b.WriteString("\n")
		}

		// Scroll indicators.
		if m.scrollOff > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more above", m.scrollOff)))
			b.WriteString("\n")
		}
		if end < len(m.entries) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more below", len(m.entries)-end)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.showFiles {
		b.WriteString(dimStyle.Render("  Enter: open/select | Tab/→: select | Backspace: parent | Esc: cancel"))
	} else {
		b.WriteString(dimStyle.Render("  Enter: open folder | Tab/→: choose folder | Backspace: parent | Esc: cancel"))
	}

	content := b.String()

	boxWidth := m.width - 4
	if boxWidth < 30 {
		boxWidth = 30
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth).
		MaxHeight(m.height - 2)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// formatFileSize returns a human-readable file size string.
func formatFileSize(size int64) string {
	const (
		_         = iota
		kB  int64 = 1 << (10 * iota)
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
