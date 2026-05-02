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
	"github.com/rw3iss/slackers/internal/threads"
)

// ThreadsOverlayOpenMsg requests the global Threads overlay.
// Carries the active snapshots so the overlay doesn't need a
// reference back to the store. Aliases come along for label
// resolution (matches the sidebar's threadDisplayName precedence).
type ThreadsOverlayOpenMsg struct {
	Active  []threads.ThreadSnapshot
	Aliases map[string]string
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

// ThreadsOverlayModel renders the global Threads list.
type ThreadsOverlayModel struct {
	all        []threads.ThreadSnapshot
	filtered   []threads.ThreadSnapshot
	aliases    map[string]string
	filter     textinput.Model
	list       SelectableList
	focusInput bool
	width      int
	height     int
}

// NewThreadsOverlay constructs the overlay from a snapshot of
// active threads. Pass a fresh snapshot each open so the list
// reflects current state.
func NewThreadsOverlay(active []threads.ThreadSnapshot, aliases map[string]string) ThreadsOverlayModel {
	ti := textinput.New()
	ti.Placeholder = "Search threads (channel, parent text, participants)..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 80
	ti.Focus()

	m := ThreadsOverlayModel{
		all:        active,
		filtered:   active,
		aliases:    aliases,
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
		// Each thread occupies 3 lines — channel row, participants
		// row, blank separator. Reserve overhead for chrome:
		//   border + padding   ~4
		//   title + blank      ~2
		//   filter + blank     ~2
		//   footer hint        ~2
		const overhead = 10
		avail := m.height - overhead
		if avail < 3 {
			avail = 3
		}
		maxVisible := avail / 3
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

		for i := start; i < end; i++ {
			b.WriteString(m.renderRow(m.filtered[i], i == sel))
			b.WriteString("\n")
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

// renderRow builds a 3-line entry: channel name + right-aligned
// time-since on row 1, participants on row 2, blank spacer on
// row 3. Mirrors the sidebar's two-row entry visually so a user
// recognises the same format from both surfaces.
func (m ThreadsOverlayModel) renderRow(snap threads.ThreadSnapshot, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}

	// Width budget for the row — overlay box width minus border +
	// padding allowance.
	rowWidth := m.width - 12
	if rowWidth < 30 {
		rowWidth = 30
	}

	name := threadDisplayName(snap, m.aliases)
	timeStr := formatRelativeTime(threadActivityTime(snap))

	timeReserve := 0
	if timeStr != "" {
		timeReserve = len(timeStr) + 2 // 2-col gap before time
	}
	nameMax := rowWidth - len(cursor) - timeReserve
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

	var nameStyle lipgloss.Style
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Bold(true)
		if ColorSelectedChannelBg != "" {
			nameStyle = nameStyle.Background(ColorSelectedChannelBg)
		}
	} else if snap.Ref.Source == threads.SourceFriend {
		nameStyle = ThreadFriendChannelRowStyle
	} else {
		nameStyle = ThreadSlackChannelRowStyle
	}

	row1 := nameStyle.Render(cursor + name)
	if timeStr != "" {
		used := len(cursor) + len(name)
		spacer := rowWidth - used - len(timeStr)
		if spacer < 1 {
			spacer = 1
		}
		row1 += strings.Repeat(" ", spacer) + ThreadParticipantsRowStyle.Render(timeStr)
	}

	// Row 2 — participants list.
	indent := "    "
	partsMax := rowWidth - len(indent)
	if partsMax < 1 {
		partsMax = 1
	}
	parts := joinParticipantNames(snap.Participants, partsMax)
	if parts == "" {
		parts = "no other participants"
	}
	partStyle := ThreadParticipantsRowStyle
	if selected {
		partStyle = lipgloss.NewStyle().Foreground(ColorSelectedChannel).Italic(true)
		if ColorSelectedChannelBg != "" {
			partStyle = partStyle.Background(ColorSelectedChannelBg)
		}
	}
	row2 := partStyle.Render(indent + parts)

	return row1 + "\n" + row2 + "\n"
}
