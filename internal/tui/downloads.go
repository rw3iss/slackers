package tui

// Downloads overlay — shows active, failed, and completed downloads
// in three scrollable sections. Active downloads show live progress
// bars. Users can cancel active downloads, retry failed ones, or
// open completed files.

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/downloads"
)

// DownloadsOpenMsg opens the downloads overlay.
type DownloadsOpenMsg struct{}

// DownloadsCloseMsg closes the downloads overlay.
type DownloadsCloseMsg struct{}

// DownloadStartedMsg notifies the model that a download was added.
type DownloadStartedMsg struct {
	ID       string
	FileName string
}

// DownloadCompleteMsg notifies that a download finished.
type DownloadCompleteMsg struct {
	ID       string
	FileName string
	DestPath string
	Err      error
}

// DownloadRetryMsg requests retrying a failed download.
type DownloadRetryMsg struct {
	ID       string
	FileName string
	DestPath string
	RelPath  string
	PeerUID  string
	PeerName string
	Size     int64
}

// RemoteDownloadRequestMsg requests starting a P2P file download
// (used by retry and the remote browser).
type RemoteDownloadRequestMsg struct {
	PeerUID  string
	PeerName string
	RelPath  string
	FileName string
	FileSize int64
}

// downloadRefreshMsg triggers a UI refresh of the downloads overlay.
type downloadRefreshMsg struct{}

// DownloadsSection identifies which section the cursor is in.
type dlSection int

const (
	dlSectionActive dlSection = iota
	dlSectionFailed
	dlSectionCompleted
)

// dlConfirmAction identifies what the pending confirmation is for.
type dlConfirmAction int

const (
	dlConfirmCancel dlConfirmAction = iota
	dlConfirmRetry
	dlConfirmDelete
)

// DownloadsModel is the overlay state.
type DownloadsModel struct {
	manager       *downloads.Manager
	section       dlSection
	selected      int
	width         int
	height        int
	message       string
	confirmID     string          // ID of download pending confirmation
	confirmAction dlConfirmAction // what the confirmation is for
}

// NewDownloadsModel creates the downloads overlay.
func NewDownloadsModel(mgr *downloads.Manager) DownloadsModel {
	return DownloadsModel{manager: mgr}
}

func (m *DownloadsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m DownloadsModel) sectionItems() []*downloads.Download {
	if m.manager == nil {
		return nil
	}
	switch m.section {
	case dlSectionActive:
		return m.manager.Active()
	case dlSectionFailed:
		return m.manager.Failed()
	case dlSectionCompleted:
		return m.manager.Completed()
	}
	return nil
}

func (m DownloadsModel) Update(msg tea.Msg) (DownloadsModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Confirmation prompt.
		if m.confirmID != "" {
			switch v.String() {
			case "y", "Y", "enter":
				id := m.confirmID
				action := m.confirmAction
				m.confirmID = ""
				switch action {
				case dlConfirmCancel:
					if m.manager != nil {
						m.manager.Cancel(id)
					}
					m.message = "Download cancelled"
				case dlConfirmRetry:
					if m.manager != nil {
						dl := m.manager.Get(id)
						if dl != nil {
							m.manager.Remove(id)
							m.message = "Retrying " + dl.FileName + "..."
							return m, func() tea.Msg {
								return DownloadRetryMsg{
									ID:       id,
									FileName: dl.FileName,
									DestPath: dl.DestPath,
									RelPath:  dl.RelPath,
									PeerUID:  dl.PeerUID,
									PeerName: dl.PeerName,
									Size:     dl.Size,
								}
							}
						}
					}
				case dlConfirmDelete:
					if m.manager != nil {
						dl := m.manager.Get(id)
						if dl != nil {
							os.Remove(dl.DestPath)
							m.manager.Remove(id)
							m.message = "Deleted: " + dl.FileName
						}
					}
				}
			default:
				m.confirmID = ""
				m.message = ""
			}
			return m, nil
		}

		items := m.sectionItems()
		switch v.String() {
		case "esc":
			return m, func() tea.Msg { return DownloadsCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(items)-1 {
				m.selected++
			}
		case "tab":
			m.section = (m.section + 1) % 3
			m.selected = 0
		case "enter":
			// Enter: cancel (active), retry (failed), open (completed).
			if len(items) > 0 && m.selected < len(items) {
				dl := items[m.selected]
				switch m.section {
				case dlSectionActive:
					m.confirmID = dl.ID
					m.confirmAction = dlConfirmCancel
					m.message = fmt.Sprintf("Cancel %s? y=yes, any key=no", dl.FileName)
				case dlSectionFailed:
					m.confirmID = dl.ID
					m.confirmAction = dlConfirmRetry
					m.message = fmt.Sprintf("Retry %s? y=yes, any key=no", dl.FileName)
				case dlSectionCompleted:
					m.message = "File: " + dl.DestPath
				}
			}
		case "d", "delete":
			// d/del: cancel (active), delete file (completed).
			if len(items) > 0 && m.selected < len(items) {
				dl := items[m.selected]
				switch m.section {
				case dlSectionActive:
					m.confirmID = dl.ID
					m.confirmAction = dlConfirmCancel
					m.message = fmt.Sprintf("Cancel %s? y=yes, any key=no", dl.FileName)
				case dlSectionCompleted:
					m.confirmID = dl.ID
					m.confirmAction = dlConfirmDelete
					m.message = fmt.Sprintf("Delete %s from disk? y=yes, any key=no", dl.FileName)
				}
			}
		}

	case downloadRefreshMsg:
		// Just re-render — manager state has changed.
		return m, nil
	}
	return m, nil
}

// RefreshCmd returns a command that triggers a UI refresh.
func RefreshDownloadsCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return downloadRefreshMsg{}
	})
}

func (m DownloadsModel) View() string {
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(ColorError)
	successStyle := lipgloss.NewStyle().Foreground(ColorStatusOn)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)

	var b strings.Builder

	if m.manager == nil {
		b.WriteString(dimStyle.Render("  No download manager available."))
		return m.scaffold(b.String())
	}

	active := m.manager.Active()
	failed := m.manager.Failed()
	completed := m.manager.Completed()

	// Section headers with counts.
	sections := []struct {
		title string
		items []*downloads.Download
		sec   dlSection
	}{
		{"Active Downloads", active, dlSectionActive},
		{"Failed / Cancelled", failed, dlSectionFailed},
		{"Completed", completed, dlSectionCompleted},
	}

	for _, sec := range sections {
		// Section header.
		indicator := "  "
		if sec.sec == m.section {
			indicator = selStyle.Render("▶ ")
		}
		header := fmt.Sprintf("%s (%d)", sec.title, len(sec.items))
		b.WriteString(indicator + sectionStyle.Render(header))
		b.WriteString("\n")

		if len(sec.items) == 0 {
			b.WriteString(dimStyle.Render("    (none)"))
			b.WriteString("\n")
		} else {
			// Show items (limit visible to half screen height).
			maxVisible := m.height / 6
			if maxVisible < 3 {
				maxVisible = 3
			}
			if maxVisible > len(sec.items) {
				maxVisible = len(sec.items)
			}
			for i := 0; i < maxVisible; i++ {
				dl := sec.items[i]
				cursor := "    "
				if sec.sec == m.section && i == m.selected {
					cursor = selStyle.Render("  > ")
				}

				// Build row.
				var row string
				switch dl.Status {
				case downloads.StatusDownloading:
					pct := int(dl.Progress() * 100)
					bar := renderProgressBar(dl.Progress(), 20)
					size := downloads.FormatSize(dl.Size)
					row = fmt.Sprintf("%s %s %3d%% %s", dl.FileName, bar, pct, size)
					if dl.PeerName != "" {
						row += dimStyle.Render("  from " + dl.PeerName)
					}
				case downloads.StatusCompleted:
					size := downloads.FormatSize(dl.Size)
					ago := timeSince(dl.CompletedAt)
					row = successStyle.Render("✓ ") + dl.FileName + "  " +
						dimStyle.Render(size+"  "+ago)
					if dl.PeerName != "" {
						row += dimStyle.Render("  from "+dl.PeerName)
					}
				case downloads.StatusFailed, downloads.StatusCancelled:
					row = errStyle.Render("✗ ") + dl.FileName + "  " +
						errStyle.Render(dl.Status.String()+": "+dl.Error)
				}

				b.WriteString(cursor + row + "\n")
			}
			if len(sec.items) > maxVisible {
				b.WriteString(dimStyle.Render(fmt.Sprintf("    ... %d more", len(sec.items)-maxVisible)))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	if m.message != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorHighlight).Render("  " + m.message))
		b.WriteString("\n")
	}

	return m.scaffold(b.String())
}

func (m DownloadsModel) scaffold(body string) string {
	var actions string
	switch m.section {
	case dlSectionActive:
		actions = "Enter/d: cancel"
	case dlSectionFailed:
		actions = "Enter: retry"
	case dlSectionCompleted:
		actions = "Enter: open" + HintSep + "d/Del: delete"
	}
	footer := "↑↓: navigate" + HintSep + "Tab: section" + HintSep +
		actions + HintSep + FooterHintClose
	s := OverlayScaffold{
		Title:       "Downloads",
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 90,
		BorderColor: ColorPrimary,
	}
	return s.Render(body)
}

// renderProgressBar creates a text progress bar.
func renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
	return lipgloss.NewStyle().Foreground(ColorAccent).Render(bar)
}

func timeSince(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("Jan 02")
	}
}
