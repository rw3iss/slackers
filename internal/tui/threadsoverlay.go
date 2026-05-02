package tui

// Global Threads overlay — a full-screen list of every active
// thread the user is part of, with a search bar at the top and
// the same dual-row entry layout the sidebar group uses
// (channel + time-since on row 1, participants on row 2).
//
// Plan A scope: shows only "active" threads (the ones already
// surfaced in the sidebar by forward-tracking). The "older
// threads" section that walks history is gated on the
// BackfillScanner — that lands separately.
//
// Activation:
//   - Alt+T (configurable shortcut: view_threads).
//   - /threads slash command.
//   - Both routes emit threads.OpenThreadsViewMsg, which the root
//     model's handler converts into ThreadsOverlayOpenMsg.
//
// Inside the overlay:
//   - Type text (when input is focused) → live filter on
//     channel name + parent text + participant names.
//   - ↑/↓ navigates the list (auto-defocuses the input).
//   - Tab toggles focus between input and list.
//   - Enter opens the highlighted thread in the same flow as the
//     sidebar Enter — switches to the parent's channel and
//     auto-enters the existing reply detail view.
//   - x removes the highlighted thread from the active store.
//   - Esc dismisses.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/format"
	"github.com/rw3iss/slackers/internal/threads"
)

// ThreadsOverlayOpenMsg requests the global Threads overlay.
// Carries the active snapshots so the overlay doesn't need a
// reference back to the store. Aliases come along for label
// resolution (matches the sidebar's threadDisplayName precedence).
type ThreadsOverlayOpenMsg struct {
	Active   []threads.ThreadSnapshot
	Aliases  map[string]string
	Resolver map[string]string
}

// ThreadsOverlayCloseMsg dismisses the overlay without acting on
// any thread.
type ThreadsOverlayCloseMsg struct{}

// ThreadsOverlayRemoveMsg requests removal of a thread from the
// store — emitted when the user presses 'x' on a row. The model
// handler removes from the store, refreshes the sidebar, and
// updates this overlay's snapshot list in place.
type ThreadsOverlayRemoveMsg struct {
	Ref threads.ThreadRef
}

// rowHitArea records the absolute Y range a rendered thread row
// occupies inside the overlay box. Used by the mouse handler to
// translate a click anywhere on a row's lines into the index of
// the thread that was clicked.
type rowHitArea struct {
	startLine int
	endLine   int
}

// ThreadsOverlayModel renders the global Threads list.
type ThreadsOverlayModel struct {
	all        []threads.ThreadSnapshot
	filtered   []threads.ThreadSnapshot
	aliases    map[string]string
	resolver   map[string]string
	filter     textinput.Model
	list       SelectableList
	focusInput bool
	width      int
	height     int
	// rowHits is rebuilt every View() — maps each rendered row to
	// its index inside m.filtered so a click can resolve back to
	// the right snapshot. Keys are the row's start Y inside the
	// content area (before lipgloss.Place centres it).
	rowHits []rowHitArea
	// contentRowOffset is the Y offset from the box top to the
	// first row of the list (header + filter + blank lines).
	// Captured during View() so click hit-test can subtract it.
	contentRowOffset int
}

// NewThreadsOverlay constructs the overlay from a snapshot of
// active threads. Pass a fresh snapshot each open so the list
// reflects current state. `resolver` is the same id→name map
// FormatMessage consumes — used to render `<@…>` mentions in
// parent previews as readable names at render time, in case the
// stored snapshot text predates the build-time mention resolution.
func NewThreadsOverlay(active []threads.ThreadSnapshot, aliases map[string]string, resolver map[string]string) ThreadsOverlayModel {
	ti := textinput.New()
	ti.Placeholder = "Search threads (channel, parent text, participants)..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 80
	ti.Focus()

	m := ThreadsOverlayModel{
		all:        active,
		filtered:   active,
		aliases:    aliases,
		resolver:   resolver,
		filter:     ti,
		focusInput: true,
		list: SelectableList{
			WrapAround: false,
			PageSize:   5,
		},
	}
	m.list.SetCount(len(m.filtered))
	return m
}

// SetSize records the available render area.
func (m *ThreadsOverlayModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetActive replaces the active-threads slice (used by the
// remove path so the overlay stays in sync without needing a
// reopen).
func (m *ThreadsOverlayModel) SetActive(active []threads.ThreadSnapshot) {
	m.all = active
	m.applyFilter()
}

// Selected returns the highlighted snapshot, or nil if the list
// is empty.
func (m ThreadsOverlayModel) Selected() *threads.ThreadSnapshot {
	idx := m.list.Current()
	if idx < 0 || idx >= len(m.filtered) {
		return nil
	}
	s := m.filtered[idx]
	return &s
}

// applyFilter rebuilds m.filtered from m.all using the current
// query, sorted newest-activity-first. Empty query passes
// everything through.
func (m *ThreadsOverlayModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		m.filtered = m.all
	} else {
		out := make([]threads.ThreadSnapshot, 0, len(m.all))
		for _, snap := range m.all {
			if matchThreadFilter(snap, m.aliases, q) {
				out = append(out, snap)
			}
		}
		m.filtered = out
	}
	m.list.SetCount(len(m.filtered))
}

// matchThreadFilter checks whether a snapshot's display name,
// parent preview, or participant list contains the lowercased
// query. Empty query is the caller's responsibility.
func matchThreadFilter(snap threads.ThreadSnapshot, aliases map[string]string, q string) bool {
	if strings.Contains(strings.ToLower(threadDisplayName(snap, aliases)), q) {
		return true
	}
	if strings.Contains(strings.ToLower(snap.ParentText), q) {
		return true
	}
	for _, p := range snap.Participants {
		if strings.Contains(strings.ToLower(p), q) {
			return true
		}
	}
	return false
}

// Update handles key + mouse events.
func (m ThreadsOverlayModel) Update(msg tea.Msg) (ThreadsOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ThreadsOverlayCloseMsg{} }
		case "tab":
			m.focusInput = !m.focusInput
			if m.focusInput {
				m.filter.Focus()
			} else {
				m.filter.Blur()
			}
			return m, nil
		case "up", "down", "pgup", "pgdown", "home", "end":
			// Force focus onto the list — typing arrows shouldn't
			// stay in input mode.
			m.focusInput = false
			m.filter.Blur()
			m.list.SetCount(len(m.filtered))
			m.list.HandleKey(msg)
			return m, nil
		case "enter":
			sel := m.Selected()
			if sel == nil {
				return m, nil
			}
			ref := sel.Ref
			return m, tea.Batch(
				func() tea.Msg { return ThreadsOverlayCloseMsg{} },
				func() tea.Msg {
					return threads.OpenThreadMsg{Ref: ref, OpenReplyView: true}
				},
			)
		case "x":
			if !m.focusInput {
				sel := m.Selected()
				if sel == nil {
					return m, nil
				}
				ref := sel.Ref
				return m, func() tea.Msg { return ThreadsOverlayRemoveMsg{Ref: ref} }
			}
		}
		// Anything else routes to the filter input when focused.
		if m.focusInput {
			prev := m.filter.Value()
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			if m.filter.Value() != prev {
				m.applyFilter()
			}
			return m, cmd
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.list.Navigate(-1)
		case tea.MouseButtonWheelDown:
			m.list.Navigate(1)
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress {
				return m, nil
			}
			idx := m.rowAtScreenY(msg.Y)
			if idx < 0 || idx >= len(m.filtered) {
				return m, nil
			}
			// Move the cursor to the clicked row, then activate.
			m.list.SetCount(len(m.filtered))
			for m.list.Current() < idx {
				m.list.Navigate(1)
			}
			for m.list.Current() > idx {
				m.list.Navigate(-1)
			}
			ref := m.filtered[idx].Ref
			return m, tea.Batch(
				func() tea.Msg { return ThreadsOverlayCloseMsg{} },
				func() tea.Msg {
					return threads.OpenThreadMsg{Ref: ref, OpenReplyView: true}
				},
			)
		}
	}
	return m, nil
}

// View renders the overlay using OverlayScaffold for consistent
// chrome with the other modal overlays.
func (m ThreadsOverlayModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).MarginBottom(1)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Threads"))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	// Inner width inside the box — overlay's MaxBoxWidth is 90,
	// reserve 4 for border (2) + padding (2 cols).
	boxW := 90
	if m.width-4 < boxW {
		boxW = m.width - 4
	}
	if boxW < 30 {
		boxW = 30
	}
	innerWidth := boxW - 8 // border 2 + padding 6 (Padding(1, 3))

	// Reset hit tracking — rebuilt this render.
	m.rowHits = m.rowHits[:0]
	// contentRowOffset is the line index where the first thread
	// row starts, relative to the box's top-inside-padding edge.
	// It accounts for: title row, blank, filter row, blank.
	m.contentRowOffset = 4

	if len(m.filtered) == 0 {
		if strings.TrimSpace(m.filter.Value()) != "" {
			b.WriteString(dimStyle.Render("  No matches."))
		} else if len(m.all) == 0 {
			b.WriteString(dimStyle.Render("  No active threads. Threads are auto-tracked when you're @mentioned, when you author a parent that gets replies, or when you reply in someone else's thread."))
		} else {
			b.WriteString(dimStyle.Render("  No active threads."))
		}
		b.WriteString("\n")
	} else {
		// Reserve overhead for chrome:
		//   border + padding   ~4
		//   title + blank      ~2
		//   filter + blank     ~2
		//   footer hint        ~2
		const overhead = 10
		avail := m.height - overhead
		if avail < threadRowHeight {
			avail = threadRowHeight
		}
		maxVisible := avail / threadRowHeight
		if maxVisible < 1 {
			maxVisible = 1
		}
		if maxVisible > len(m.filtered) {
			maxVisible = len(m.filtered)
		}

		sel := m.list.Current()
		start := 0
		if sel >= maxVisible {
			start = sel - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.filtered) {
			end = len(m.filtered)
		}

		// Track render-line position so click hit-test maps clicks
		// back to the right snapshot. line counts from 0 at the
		// top of the rows area (i.e. m.contentRowOffset lines into
		// the box's content).
		line := 0
		for i := start; i < end; i++ {
			b.WriteString(m.renderRow(m.filtered[i], i == sel, innerWidth))
			b.WriteString("\n")
			m.rowHits = append(m.rowHits, rowHitArea{
				startLine: line,
				endLine:   line + threadRowHeight - 1,
			})
			line += threadRowHeight
		}
		if start > 0 {
			b.WriteString(dimStyle.Render("  ... more above\n"))
		}
		if end < len(m.filtered) {
			b.WriteString(dimStyle.Render("  ... more below\n"))
		}
	}

	footer := "Tab: search/list" + HintSep +
		"↑↓: navigate" + HintSep +
		"Enter: open" + HintSep +
		"x: close thread" + HintSep +
		FooterHintClose

	// Pin box height so the click hit-test below has predictable
	// row geometry. Box is centred via lipgloss.Place so the top
	// is at (m.height - boxH) / 2.
	boxH := m.height - 4
	if boxH < 12 {
		boxH = 12
	}

	scaffold := OverlayScaffold{
		Title:       "",
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 90,
		BoxHeight:   boxH,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}

// rowsScreenStart returns the absolute Y coordinate where the
// first thread row starts on screen. Used by the click handler.
// Mirrors the layout used by Render: box centred with margin 2,
// then border (1) + top padding (1) + title (1) + blank (1) +
// filter (1) + blank (1) = 6 lines before the rows.
func (m ThreadsOverlayModel) rowsScreenStart() int {
	boxH := m.height - 4
	if boxH < 12 {
		boxH = 12
	}
	boxTop := (m.height - boxH) / 2
	if boxTop < 0 {
		boxTop = 0
	}
	return boxTop + 2 + m.contentRowOffset
}

// rowAtScreenY returns the index into m.filtered for the row at
// screen Y, or -1 if the click missed every rendered row.
func (m ThreadsOverlayModel) rowAtScreenY(y int) int {
	rowsTop := m.rowsScreenStart()
	if y < rowsTop {
		return -1
	}
	off := y - rowsTop
	idx := off / threadRowHeight
	if idx < 0 || idx >= len(m.rowHits) {
		return -1
	}
	// Map back to filtered index — m.rowHits[i] corresponds to the
	// i'th rendered row, which is filtered[start+i]. We don't track
	// `start` here, but List.Current() / scroll start can be
	// recovered from the hit's startLine. Simpler: rely on the
	// scroll start the View used. Recompute from list state.
	sel := m.list.Current()
	maxVisible := len(m.rowHits)
	start := 0
	if sel >= maxVisible {
		start = sel - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	abs := start + idx
	if abs < 0 || abs >= len(m.filtered) {
		return -1
	}
	return abs
}

// rowHeight is the fixed number of visual lines every thread row
// occupies in the overlay — header (time + channel + members) +
// up to 2 wrapped message-preview lines + trailing blank. Fixed
// because the click hit-test needs predictable row geometry.
const threadRowHeight = 4

// renderRow builds a 4-line entry:
//
//	▸ 5m  #general - Alice, Bob
//	      Hey, can someone review my PR? It's been waiting
//	      for two days now…
//	      (blank)
//
// Row 1 leads with a fixed-width "time since" column then the
// channel label, dash, and participants list — sharing one row
// keeps the list dense. Rows 2-3 indent under the channel name
// (same column as the time col + cursor padding) so the message
// text aligns with the channel-name column on the left. Long
// message previews wrap to a second line and truncate with `…`
// if they overflow that.
//
// Every output line is padded to the overlay's inner width with
// the theme background colour so wrapped/empty cells don't show
// the terminal default — same trick the chat pane uses via
// MessagePaneStyle's Background.
func (m ThreadsOverlayModel) renderRow(snap threads.ThreadSnapshot, selected bool, innerWidth int) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}

	timeStr := formatRelativeTime(threadActivityTime(snap))
	if timeStr == "" {
		timeStr = "—"
	}
	const timeColW = 5
	timePadded := timeStr
	if len(timePadded) < timeColW {
		timePadded += strings.Repeat(" ", timeColW-len(timePadded))
	}
	indent := strings.Repeat(" ", len(cursor)+timeColW)

	// Styles keyed off selection and source.
	var nameStyle lipgloss.Style
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
	} else if snap.Ref.Source == threads.SourceFriend {
		nameStyle = ThreadFriendChannelRowStyle
	} else {
		nameStyle = ThreadSlackChannelRowStyle
	}
	dimStyle := ThreadParticipantsRowStyle
	if selected {
		dimStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Italic(true)
	}
	// Parent message preview is regular grey, distinct from the
	// dim/italic participants colour — matches the chat-pane
	// muted text colour for body content.
	previewStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	if selected {
		previewStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel)
	}

	// --- Row 1: time + channel + " - " + members ----------------
	name := threadDisplayName(snap, m.aliases)
	parts := joinParticipantNames(snap.Participants, max1(innerWidth/2))

	// Width math for row 1 — figure out how much the channel name
	// can use after reserving for cursor, time col, " - ", and
	// participants.
	row1Avail := innerWidth - len(cursor) - timeColW
	if row1Avail < 1 {
		row1Avail = 1
	}
	sep := ""
	if parts != "" {
		sep = " - "
	}
	nameMax := row1Avail - len(sep) - len(parts)
	if nameMax < 1 {
		nameMax = 1
	}
	if len(name) > nameMax {
		clip := nameMax - 1
		if clip < 1 {
			clip = 1
		}
		name = name[:clip] + "~"
	}
	row1Body := cursor + dimStyle.Render(timePadded) + nameStyle.Render(name)
	if parts != "" {
		row1Body += dimStyle.Render(sep + parts)
	}
	row1 := bgPad(row1Body, innerWidth)

	// --- Row 2 / 3: parent message preview, wrapped --------------
	preview := snap.ParentText
	// Resolve any leftover <@U…> / <@slacker:…> markers using
	// the live resolver — covers snapshots that predate the
	// build-time resolution in buildThreadSnapshot.
	if strings.Contains(preview, "<@") && len(m.resolver) > 0 {
		preview = format.FormatMessage(preview, m.resolver)
	}
	if preview == "" {
		preview = "(no preview)"
	}

	previewMax := innerWidth - len(indent)
	if previewMax < 1 {
		previewMax = 1
	}
	wrappedLines := strings.Split(wordWrap(preview, previewMax), "\n")
	// Cap at 2 wrapped lines; truncate the second line with `…`
	// if the body extends further.
	if len(wrappedLines) > 2 {
		second := wrappedLines[1]
		if len(second) > previewMax-1 {
			second = second[:previewMax-1] + "…"
		} else {
			second += "…"
		}
		wrappedLines = []string{wrappedLines[0], second}
	}
	for len(wrappedLines) < 2 {
		wrappedLines = append(wrappedLines, "")
	}

	row2 := bgPad(indent+previewStyle.Render(wrappedLines[0]), innerWidth)
	row3 := bgPad(indent+previewStyle.Render(wrappedLines[1]), innerWidth)

	// Row 4 — blank separator, but still bg-padded so the box
	// background is consistent.
	row4 := bgPad("", innerWidth)

	return row1 + "\n" + row2 + "\n" + row3 + "\n" + row4
}

// bgPad pads `text` with theme-background-coloured spaces out to
// `width` cells so wrapped / short content lines render with a
// consistent background instead of falling through to the
// terminal default. Mirrors the trick MessagePaneStyle uses in
// the main chat pane.
func bgPad(text string, width int) string {
	used := lipgloss.Width(text)
	if used >= width {
		return text
	}
	pad := strings.Repeat(" ", width-used)
	style := lipgloss.NewStyle()
	if ColorBackgroundBg != "" {
		style = style.Background(ColorBackgroundBg)
	}
	return text + style.Render(pad)
}

// max1 returns x or 1, whichever is larger. Used to keep width
// budgets non-negative without dragging in stdlib max generics.
func max1(x int) int {
	if x < 1 {
		return 1
	}
	return x
}
