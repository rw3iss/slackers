package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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
	allFiles       []types.FileInfo // unfiltered
	files          []types.FileInfo // filtered view
	selected       int
	loading        bool
	scopeAll       bool
	channelID      string
	filter         textinput.Model
	filtering      bool
	width          int
	height         int
	channelResolve func(string) string
	slackSvc       slackpkg.SlackService
}

// NewFilesListModel creates a new files list overlay.
func NewFilesListModel(svc slackpkg.SlackService, channelID string, channelResolve func(string) string) FilesListModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter files..."
	ti.CharLimit = 64

	return FilesListModel{
		loading:        true,
		scopeAll:       channelID == "",
		channelID:      channelID,
		channelResolve: channelResolve,
		slackSvc:       svc,
		filter:         ti,
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
		m.allFiles = msg.Files
		m.loading = false
		m.applyFilter()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			if m.channelID != "" {
				m.scopeAll = !m.scopeAll
				m.loading = true
				m.allFiles = nil
				m.files = nil
				m.selected = 0
				m.filter.SetValue("")
				chID := ""
				if !m.scopeAll {
					chID = m.channelID
				}
				return m, loadFilesCmd(m.slackSvc, chID)
			}
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.files)-1 {
				m.selected++
			}
			return m, nil
		case "pgup":
			m.selected -= 10
			if m.selected < 0 {
				m.selected = 0
			}
			return m, nil
		case "pgdown":
			m.selected += 10
			if m.selected >= len(m.files) {
				m.selected = len(m.files) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
			return m, nil
		case "enter":
			if len(m.files) > 0 && m.selected < len(m.files) {
				f := m.files[m.selected]
				return m, func() tea.Msg {
					return FilesListDownloadMsg{File: f}
				}
			}
			return m, nil
		}

		// All other keys go to the filter input.
		if !m.filtering && msg.String() != "esc" && msg.String() != "ctrl+l" {
			m.filtering = true
			m.filter.Focus()
		}

		prevVal := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != prevVal {
			m.applyFilter()
		}
		return m, cmd
	}
	return m, nil
}

func (m *FilesListModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if query == "" {
		m.files = m.allFiles
	} else {
		m.files = nil
		for _, f := range m.allFiles {
			name := strings.ToLower(f.Name)
			if strings.Contains(name, query) {
				m.files = append(m.files, f)
			}
		}
	}
	m.selected = 0
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
		Foreground(ColorDescText)

	selectedNameStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	metaStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	channelStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	scopeStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	inactiveScopeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Files"))
	b.WriteString("\n\n")

	if m.channelID != "" {
		currentLabel := "Current Channel"
		allLabel := "All Channels"
		if m.scopeAll {
			b.WriteString("  " + inactiveScopeStyle.Render(currentLabel) + "  " + scopeStyle.Render("["+allLabel+"]"))
		} else {
			b.WriteString("  " + scopeStyle.Render("["+currentLabel+"]") + "  " + inactiveScopeStyle.Render(allLabel))
		}
		b.WriteString("    " + dimStyle.Render("(Tab to toggle)"))
		b.WriteString("\n\n")
	}

	// Filter input
	if m.filtering || m.filter.Value() != "" {
		b.WriteString("  ")
		b.WriteString(m.filter.View())
		b.WriteString("\n\n")
	} else if !m.loading && len(m.allFiles) > 0 {
		b.WriteString(dimStyle.Render("  Start typing to filter files..."))
		b.WriteString("\n\n")
	}

	if m.loading {
		b.WriteString(dimStyle.Render("  Loading files..."))
	} else if len(m.files) == 0 {
		b.WriteString(dimStyle.Render("  No files found"))
	} else {
		// Reserve rows for the chrome above and below the file list:
		//   box border + padding   ~4
		//   title + margin         ~2
		//   scope toggle row       ~2 (only when channelID is set)
		//   filter / hint row      ~2
		//   "N files total" line   ~1
		//   blank + footer hint    ~2
		// Each rendered file consumes 2 rows (line + trailing blank).
		reserved := 4 + 2 + 2 + 1 + 2
		if m.channelID != "" {
			reserved += 2
		}
		maxVisible := (m.height - reserved) / 2
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
			chDisplay := ""
			if m.channelResolve != nil && f.ChannelName != "" {
				chDisplay = m.channelResolve(f.ChannelName)
			} else if f.ChannelName != "" {
				chDisplay = "#" + f.ChannelName
			}
			if chDisplay != "" {
				line += "  " + channelStyle.Render(chDisplay)
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
	if m.channelID != "" {
		b.WriteString(dimStyle.Render("  Enter: download | Tab: toggle scope | Esc: close"))
	} else {
		b.WriteString(dimStyle.Render("  Enter: download | Esc: close"))
	}

	content := b.String()

	boxWidth := m.width - 4
	boxHeight := m.height - 2
	if boxHeight < 8 {
		boxHeight = 8
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
		box)
}

func loadFilesCmd(svc slackpkg.SlackService, channelID string) tea.Cmd {
	return func() tea.Msg {
		files, err := svc.ListFiles(channelID, 100)
		if err != nil {
			return FilesListLoadedMsg{}
		}
		return FilesListLoadedMsg{Files: files}
	}
}
