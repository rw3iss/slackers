package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// FilesListLoadedMsg carries the file list results.
type FilesListLoadedMsg struct {
	Files []types.FileInfo
}

// FilesListDownloadMsg requests downloading a file from the files list.
type FilesListDownloadMsg struct {
	File types.FileInfo
}

// FilesListModel provides an overlay showing all files across channels.
type FilesListModel struct {
	files    []types.FileInfo
	selected int
	loading  bool
	width    int
	height   int
}

// NewFilesListModel creates a new files list overlay.
func NewFilesListModel() FilesListModel {
	return FilesListModel{
		loading: true,
	}
}

// SetSize sets the overlay dimensions.
func (m *FilesListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the files list overlay.
func (m FilesListModel) Update(msg tea.Msg) (FilesListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case FilesListLoadedMsg:
		m.files = msg.Files
		m.loading = false
		m.selected = 0
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.files)-1 {
				m.selected++
			}
		case "pgup":
			m.selected -= 10
			if m.selected < 0 {
				m.selected = 0
			}
		case "pgdown":
			m.selected += 10
			if m.selected >= len(m.files) {
				m.selected = len(m.files) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
		case "enter":
			if len(m.files) > 0 && m.selected < len(m.files) {
				f := m.files[m.selected]
				return m, func() tea.Msg {
					return FilesListDownloadMsg{File: f}
				}
			}
		}
	}
	return m, nil
}

// View renders the files list overlay.
func (m FilesListModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	selectedNameStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	metaStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	channelStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Files"))
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("  Loading files..."))
	} else if len(m.files) == 0 {
		b.WriteString(dimStyle.Render("  No files found"))
	} else {
		maxVisible := (m.height - 10) / 2
		if maxVisible < 3 {
			maxVisible = 3
		}
		if maxVisible > len(m.files) {
			maxVisible = len(m.files)
		}

		start := 0
		if m.selected >= maxVisible {
			start = m.selected - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.files) {
			end = len(m.files)
		}

		for i := start; i < end; i++ {
			f := m.files[i]
			cursor := "  "
			ns := nameStyle
			if i == m.selected {
				cursor = "> "
				ns = selectedNameStyle
			}

			sizeStr := formatFileSize(f.Size)
			ts := ""
			if !f.Timestamp.IsZero() {
				ts = f.Timestamp.Format("Jan 2 15:04")
			}

			line := fmt.Sprintf("%s%s  %s  %s",
				cursor,
				ns.Render(f.Name),
				metaStyle.Render(sizeStr),
				metaStyle.Render(ts),
			)
			if f.ChannelName != "" {
				line += "  " + channelStyle.Render("#"+f.ChannelName)
			}
			if f.UserName != "" {
				line += "  " + metaStyle.Render("by "+f.UserName)
			}
			b.WriteString(line)
			b.WriteString("\n\n")
		}

		b.WriteString(dimStyle.Render(fmt.Sprintf("  %d files total", len(m.files))))
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Enter: download | Esc: close"))

	content := b.String()

	boxWidth := m.width - 4
	boxHeight := m.height - 2

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(boxWidth).
		MaxHeight(boxHeight)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}

func loadFilesCmd(svc slackpkg.SlackService) tea.Cmd {
	return func() tea.Msg {
		files, err := svc.ListFiles(100)
		if err != nil {
			return FilesListLoadedMsg{}
		}
		return FilesListLoadedMsg{Files: files}
	}
}
