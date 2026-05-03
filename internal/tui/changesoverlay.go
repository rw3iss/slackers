package tui

// /changes overlay — a paged, searchable list of recent commits
// fetched from GitHub's public commits API. Order is oldest-at-
// top, newest-at-bottom (matches the chat-history scroll-up
// pattern); pressing Up at the top of the loaded set fetches
// the next older page.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/changes"
)

// changesRepoOwner / changesRepoName are the public GitHub repo
// the /changes command pulls from. Hardcoded for now; future
// versions can promote these to a config setting.
const (
	changesRepoOwner = "rw3iss"
	changesRepoName  = "slackers"
	changesPerPage   = 10
)

// ChangesOpenMsg requests the /changes overlay open. Producing
// this from anywhere — slash command, future shortcut, etc. —
// triggers the initial-page fetch.
type ChangesOpenMsg struct{}

// ChangesCloseMsg dismisses the overlay.
type ChangesCloseMsg struct{}

// changesPageLoadedMsg arrives when a fetch resolves.
type changesPageLoadedMsg struct {
	Page    int
	Commits []changes.Commit
	Err     error
}

// changesToastClearMsg fires after the toast TTL to clear the
// transient status banner ("Commit link copied!"). The ID
// discriminator lets a stale tick from an earlier copy be
// ignored when the user copies again before the first toast
// expires.
type changesToastClearMsg struct {
	ID int
}

// ChangesOverlayModel renders the commit list overlay.
type ChangesOverlayModel struct {
	commits     []changes.Commit
	filtered    []int // indexes into commits, after applying search
	expanded    map[string]bool
	selected    int // index into filtered
	scrollOff   int // first visible logical commit index (in filtered)
	currentPage int // last successfully loaded page (1-indexed); 0 = nothing yet
	hasMore     bool
	loading     bool
	err         error

	filter     textinput.Model
	focusInput bool

	toast     string
	toastID   int
	width     int
	height    int
}

// NewChangesOverlay builds an empty overlay. The first fetch is
// kicked off by InitialFetchCmd, returned alongside so the model
// can dispatch it the moment the overlay opens.
func NewChangesOverlay() ChangesOverlayModel {
	ti := textinput.New()
	ti.Placeholder = "Search commits (author, subject, sha)..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 80
	return ChangesOverlayModel{
		expanded: make(map[string]bool),
		hasMore:  true,
		filter:   ti,
	}
}

// SetSize records the available render area.
func (m *ChangesOverlayModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// InitialFetchCmd returns the tea.Cmd that loads page 1.
func (m ChangesOverlayModel) InitialFetchCmd() tea.Cmd {
	return changesFetchCmd(1)
}

// changesFetchCmd issues a paged GitHub API call and wraps the
// result in a changesPageLoadedMsg.
func changesFetchCmd(page int) tea.Cmd {
	return func() tea.Msg {
		commits, err := changes.Fetch(changesRepoOwner, changesRepoName, page, changesPerPage)
		return changesPageLoadedMsg{Page: page, Commits: commits, Err: err}
	}
}

// applyFilter rebuilds m.filtered from m.commits using the
// current search query. Empty query → every commit.
func (m *ChangesOverlayModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.filtered = m.filtered[:0]
	for i, c := range m.commits {
		if q == "" || matchesChangesFilter(c, q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func matchesChangesFilter(c changes.Commit, q string) bool {
	if strings.Contains(strings.ToLower(c.Author), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Message), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.SHA), q) {
		return true
	}
	return false
}

// selectedCommit returns the commit at the cursor or nil when
// the filtered list is empty.
func (m ChangesOverlayModel) selectedCommit() *changes.Commit {
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return nil
	}
	c := m.commits[m.filtered[m.selected]]
	return &c
}

// Update handles key + mouse + page-load messages.
func (m ChangesOverlayModel) Update(msg tea.Msg) (ChangesOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case changesPageLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.err = nil
		// GitHub returns newest-first within a page, which is the
		// order the overlay renders too — newest at top, oldest at
		// bottom. Page 1 anchors the head of the slice; later
		// (older) pages append to the tail so they show below.
		if msg.Page == 1 {
			m.commits = append(m.commits, msg.Commits...)
			m.applyFilter()
			m.selected = 0 // newest
		} else {
			// Preserve the cursor's commit across the merge.
			anchorSHA := ""
			if c := m.selectedCommit(); c != nil {
				anchorSHA = c.SHA
			}
			m.commits = append(m.commits, msg.Commits...)
			m.applyFilter()
			if anchorSHA != "" {
				for i, fi := range m.filtered {
					if m.commits[fi].SHA == anchorSHA {
						m.selected = i
						break
					}
				}
			}
		}
		m.currentPage = msg.Page
		m.hasMore = len(msg.Commits) >= changesPerPage
		return m, nil

	case changesToastClearMsg:
		if msg.ID == m.toastID {
			m.toast = ""
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ChangesCloseMsg{} }
		case "tab":
			m.focusInput = !m.focusInput
			if m.focusInput {
				m.filter.Focus()
			} else {
				m.filter.Blur()
			}
			return m, nil
		case "up", "k":
			m.focusInput = false
			m.filter.Blur()
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down", "j":
			m.focusInput = false
			m.filter.Blur()
			if m.selected < len(m.filtered)-1 {
				m.selected++
				return m, nil
			}
			// At the bottom (oldest loaded) — fetch the next
			// older page. Search filter active → don't auto-load
			// since the filter only matches what's already in
			// memory.
			if m.hasMore && !m.loading && strings.TrimSpace(m.filter.Value()) == "" {
				m.loading = true
				return m, changesFetchCmd(m.currentPage + 1)
			}
			return m, nil
		case "pgup":
			m.focusInput = false
			m.filter.Blur()
			m.selected -= 5
			if m.selected < 0 {
				m.selected = 0
			}
			return m, nil
		case "pgdown":
			m.focusInput = false
			m.filter.Blur()
			m.selected += 5
			if m.selected >= len(m.filtered) {
				m.selected = len(m.filtered) - 1
			}
			return m, nil
		case "home":
			m.focusInput = false
			m.filter.Blur()
			m.selected = 0
			return m, nil
		case "end":
			m.focusInput = false
			m.filter.Blur()
			m.selected = len(m.filtered) - 1
			return m, nil
		case "enter":
			c := m.selectedCommit()
			if c == nil {
				return m, nil
			}
			m.expanded[c.SHA] = !m.expanded[c.SHA]
			return m, nil
		case "y":
			// Copy URL when the list (not the search input) has
			// focus. With the input focused, a literal "y"
			// should still type into the search box.
			if m.focusInput {
				break
			}
			c := m.selectedCommit()
			if c == nil {
				return m, nil
			}
			m.toastID++
			id := m.toastID
			m.toast = "Commit link copied!"
			return m, tea.Batch(
				copyToClipboardCmd(c.HTMLURL),
				tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
					return changesToastClearMsg{ID: id}
				}),
			)
		}
		// Anything else routes to the filter input when focused.
		if m.focusInput {
			prev := m.filter.Value()
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			if m.filter.Value() != prev {
				m.applyFilter()
				m.selected = len(m.filtered) - 1
			}
			return m, cmd
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.selected > 0 {
				m.selected--
			}
		case tea.MouseButtonWheelDown:
			if m.selected < len(m.filtered)-1 {
				m.selected++
			} else if m.hasMore && !m.loading && strings.TrimSpace(m.filter.Value()) == "" {
				m.loading = true
				return m, changesFetchCmd(m.currentPage + 1)
			}
		}
	}
	return m, nil
}

// View renders the overlay.
func (m ChangesOverlayModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	errStyle := lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	toastStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Changes — %s/%s", changesRepoOwner, changesRepoName)))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	if m.toast != "" {
		b.WriteString("  ")
		b.WriteString(toastStyle.Render(m.toast))
		b.WriteString("\n\n")
	}

	if m.err != nil {
		b.WriteString("  ")
		b.WriteString(errStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
	}

	if m.loading && len(m.commits) == 0 {
		b.WriteString(dimStyle.Render("  Loading commits..."))
		b.WriteString("\n")
	} else if len(m.filtered) == 0 {
		if m.err == nil {
			if strings.TrimSpace(m.filter.Value()) != "" {
				b.WriteString(dimStyle.Render("  No matches."))
			} else {
				b.WriteString(dimStyle.Render("  No commits loaded."))
			}
			b.WriteString("\n")
		}
	} else {
		boxW := 90
		if m.width-4 < boxW {
			boxW = m.width - 4
		}
		if boxW < 30 {
			boxW = 30
		}
		innerWidth := boxW - 8

		// Reserve overhead for chrome:
		//   border + padding         ~4
		//   title + blank            ~2
		//   filter + blank           ~2
		//   toast + blank (when on)  ~2
		//   footer hint              ~2
		overhead := 10
		if m.toast != "" {
			overhead += 2
		}
		avail := m.height - overhead
		if avail < 6 {
			avail = 6
		}

		// Render visible commits, accounting for variable height
		// when an entry is expanded vs collapsed.
		// Walk from selected backwards to figure out scrollOff.
		_ = m.scrollOff // simple: just render a window around selected
		linesUsed := 0
		var rendered []string
		// Collect rows for every commit, then window around the cursor.
		rowsPerCommit := make([]string, len(m.filtered))
		heights := make([]int, len(m.filtered))
		for i, fi := range m.filtered {
			c := m.commits[fi]
			expanded := m.expanded[c.SHA]
			row := m.renderCommit(c, i == m.selected, expanded, innerWidth)
			rowsPerCommit[i] = row
			heights[i] = strings.Count(row, "\n") + 1
		}
		// Find first visible index so the selected row stays in view.
		start := m.selected
		used := heights[m.selected]
		// Walk backwards adding earlier rows while there's room.
		for start > 0 {
			next := used + heights[start-1]
			if next > avail {
				break
			}
			used = next
			start--
		}
		// Walk forwards adding later rows up to budget.
		end := m.selected + 1
		for end < len(rowsPerCommit) {
			next := used + heights[end]
			if next > avail {
				break
			}
			used = next
			end++
		}

		if start > 0 {
			rendered = append(rendered, dimStyle.Render("  ↑ ... newer commits above"))
		}
		for i := start; i < end; i++ {
			rendered = append(rendered, rowsPerCommit[i])
		}
		_ = linesUsed
		switch {
		case m.loading:
			rendered = append(rendered, dimStyle.Render("  ↓ Loading older commits..."))
		case end < len(rowsPerCommit):
			rendered = append(rendered, dimStyle.Render("  ↓ ... older commits below"))
		case m.hasMore:
			rendered = append(rendered, dimStyle.Render("  ↓ scroll down for older commits"))
		}

		b.WriteString(strings.Join(rendered, "\n"))
		b.WriteString("\n")
	}

	footer := "↑↓: navigate" + HintSep +
		"Enter: expand" + HintSep +
		"y: copy link" + HintSep +
		"Tab: search/list" + HintSep +
		FooterHintClose

	scaffold := OverlayScaffold{
		Title:       "",
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 90,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}

// renderCommit renders a single commit entry — header line
// (author + relative time + short SHA) and message preview
// (4 lines collapsed, full body when expanded). Selected row
// gets a "▶" caret and highlight; others a 2-col indent.
func (m ChangesOverlayModel) renderCommit(c changes.Commit, selected, expanded bool, innerWidth int) string {
	cursor := "  "
	if selected {
		cursor = "▶ "
	}

	authorStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	metaStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	subjectStyle := lipgloss.NewStyle().Foreground(ColorMessageText).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(ColorMessageText)
	if selected {
		authorStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
		subjectStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
	}

	header := fmt.Sprintf("%s%s  %s  %s",
		cursor,
		authorStyle.Render(c.Author),
		metaStyle.Render(formatRelativeTime(c.Date)),
		metaStyle.Render(c.ShortSHA()),
	)

	indent := "    "
	subject := c.Subject()
	bodyMax := innerWidth - len(indent)
	if bodyMax < 1 {
		bodyMax = 1
	}

	// Subject — single line, truncated.
	if len(subject) > bodyMax {
		subject = subject[:bodyMax-1] + "…"
	}
	subjectLine := indent + subjectStyle.Render(subject)

	// Body lines — everything past the first newline of the
	// commit message. Collapsed view shows up to 3 wrapped
	// lines (combined with the subject = 4 total). Expanded
	// view shows everything.
	rest := ""
	if i := strings.Index(c.Message, "\n"); i >= 0 {
		rest = strings.TrimSpace(c.Message[i+1:])
	}
	var bodyLines []string
	if rest != "" {
		wrapped := strings.Split(wordWrap(rest, bodyMax), "\n")
		if !expanded && len(wrapped) > 3 {
			cut := wrapped[2]
			if len(cut) > bodyMax-1 {
				cut = cut[:bodyMax-1]
			}
			wrapped = []string{wrapped[0], wrapped[1], cut + "…"}
		}
		bodyLines = wrapped
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(subjectLine)
	for _, line := range bodyLines {
		b.WriteString("\n")
		b.WriteString(indent + bodyStyle.Render(line))
	}
	b.WriteString("\n") // trailing blank between commits
	return b.String()
}
