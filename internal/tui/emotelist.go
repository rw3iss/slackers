package tui

// Emotes List overlay — browseable list of every registered emote.
// Three focus zones cycled by Tab:
//   0 = search filter input
//   1 = emote list (arrow nav, e/n/d shortcuts)
//   2 = "Create New Emote" button at the bottom

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/emotes"
)

// Messages dispatched by the emotes list overlay.
type EmoteListOpenMsg struct{}
type EmoteListCloseMsg struct{}
type EmoteListSelectMsg struct{ Command string }
type EmoteEditRequestMsg struct {
	Command string // empty for new emote
}
type EmoteDeleteDoneMsg struct{ Command string }

// EmoteListModel is the overlay state.
type EmoteListModel struct {
	store         *emotes.Store
	systemEmotes  []emotes.Emote
	customEmotes  []emotes.Emote
	allFiltered   []emoteRow
	filter        textinput.Model
	list          SelectableList
	width, height int
	confirmDelete bool
	deleteTarget  string
	focus         int // 0=search, 1=list, 2=create button
}

type emoteRow struct {
	isHeader bool
	label    string
	preview  string
	emote    *emotes.Emote
}

func NewEmoteList(store *emotes.Store) EmoteListModel {
	ti := textinput.New()
	ti.Placeholder = "Filter emotes..."
	ti.Prompt = "/ "
	ti.CharLimit = 64
	ti.Focus()

	m := EmoteListModel{
		store:  store,
		filter: ti,
		focus:  0, // start on search
	}
	m.rebuildList()
	return m
}

func (m *EmoteListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *EmoteListModel) rebuildList() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.systemEmotes = m.store.Defaults()
	m.customEmotes = m.store.Custom()

	m.allFiltered = nil
	var sysRows []emoteRow
	for _, e := range m.systemEmotes {
		if q != "" && !strings.Contains(strings.ToLower(e.Command), q) &&
			!strings.Contains(strings.ToLower(e.Text), q) {
			continue
		}
		preview := e.Text
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		e := e
		sysRows = append(sysRows, emoteRow{label: "/" + e.Command, preview: preview, emote: &e})
	}
	if len(sysRows) > 0 {
		m.allFiltered = append(m.allFiltered, emoteRow{isHeader: true, label: "System Emotes"})
		m.allFiltered = append(m.allFiltered, sysRows...)
	}
	var cusRows []emoteRow
	for _, e := range m.customEmotes {
		if q != "" && !strings.Contains(strings.ToLower(e.Command), q) &&
			!strings.Contains(strings.ToLower(e.Text), q) {
			continue
		}
		preview := e.Text
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		e := e
		cusRows = append(cusRows, emoteRow{label: "/" + e.Command, preview: preview, emote: &e})
	}
	if len(cusRows) > 0 {
		m.allFiltered = append(m.allFiltered, emoteRow{isHeader: true, label: "Custom Emotes"})
		m.allFiltered = append(m.allFiltered, cusRows...)
	}
	m.list.SetCount(len(m.allFiltered))
}

func (m *EmoteListModel) selectedEmote() *emotes.Emote {
	idx := m.list.Current()
	if idx < 0 || idx >= len(m.allFiltered) {
		return nil
	}
	return m.allFiltered[idx].emote
}

func (m *EmoteListModel) cycleFocus(delta int) {
	m.focus = (m.focus + delta + 3) % 3
	if m.focus == 0 {
		m.filter.Focus()
	} else {
		m.filter.Blur()
	}
}

func (m EmoteListModel) Update(msg tea.Msg) (EmoteListModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Delete confirmation mode — takes priority over everything.
		if m.confirmDelete {
			switch v.String() {
			case "y", "Y":
				cmd := m.deleteTarget
				m.confirmDelete = false
				m.deleteTarget = ""
				if m.store != nil {
					_ = m.store.Delete(cmd)
				}
				m.rebuildList()
				return m, func() tea.Msg { return EmoteDeleteDoneMsg{Command: cmd} }
			default:
				m.confirmDelete = false
				m.deleteTarget = ""
				return m, nil
			}
		}

		// Esc always closes.
		if v.String() == "esc" {
			return m, func() tea.Msg { return EmoteListCloseMsg{} }
		}

		// Tab cycles focus: search → list → create button.
		if v.String() == "tab" {
			m.cycleFocus(1)
			return m, nil
		}
		if v.String() == "shift+tab" {
			m.cycleFocus(-1)
			return m, nil
		}

		// --- Focus 0: search input ---
		if m.focus == 0 {
			switch v.String() {
			case "down":
				// Down from search → focus the list.
				m.cycleFocus(1)
				return m, nil
			case "enter":
				// Enter from search → focus the list.
				m.cycleFocus(1)
				return m, nil
			default:
				// All other keys go to the filter input.
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.rebuildList()
				return m, cmd
			}
		}

		// --- Focus 1: emote list ---
		if m.focus == 1 {
			switch v.String() {
			case "enter":
				if e := m.selectedEmote(); e != nil {
					cmd := e.Command
					return m, func() tea.Msg { return EmoteListSelectMsg{Command: cmd} }
				}
				return m, nil
			case "e":
				if e := m.selectedEmote(); e != nil {
					cmd := e.Command
					return m, func() tea.Msg { return EmoteEditRequestMsg{Command: cmd} }
				}
				return m, nil
			case "d":
				if e := m.selectedEmote(); e != nil && e.IsCustom {
					m.confirmDelete = true
					m.deleteTarget = e.Command
					return m, nil
				}
				return m, nil
			case "up", "k":
				// If at the top of the list, move focus to search.
				if m.list.Current() <= 0 || m.firstNonHeaderIdx() == m.list.Current() {
					m.cycleFocus(-1)
					return m, nil
				}
				m.moveSkippingHeaders(-1)
				return m, nil
			case "down", "j":
				// If at the bottom, move focus to create button.
				if m.list.Current() >= len(m.allFiltered)-1 || m.lastNonHeaderIdx() == m.list.Current() {
					m.cycleFocus(1)
					return m, nil
				}
				m.moveSkippingHeaders(1)
				return m, nil
			case "pgup", "pgdown", "home", "end":
				if m.list.HandleKey(v) {
					m.skipToNonHeader(1)
				}
				return m, nil
			}
			return m, nil
		}

		// --- Focus 2: create button ---
		if m.focus == 2 {
			switch v.String() {
			case "enter", " ":
				return m, func() tea.Msg { return EmoteEditRequestMsg{} }
			case "up":
				m.cycleFocus(-1)
				return m, nil
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *EmoteListModel) moveSkippingHeaders(delta int) {
	n := len(m.allFiltered)
	if n == 0 {
		return
	}
	idx := m.list.Current() + delta
	for idx >= 0 && idx < n {
		if !m.allFiltered[idx].isHeader {
			m.list.Selected = idx
			return
		}
		idx += delta
	}
}

func (m *EmoteListModel) skipToNonHeader(dir int) {
	n := len(m.allFiltered)
	idx := m.list.Current()
	for idx >= 0 && idx < n && m.allFiltered[idx].isHeader {
		idx += dir
	}
	if idx >= 0 && idx < n {
		m.list.Selected = idx
	}
}

func (m *EmoteListModel) firstNonHeaderIdx() int {
	for i, r := range m.allFiltered {
		if !r.isHeader {
			return i
		}
	}
	return -1
}

func (m *EmoteListModel) lastNonHeaderIdx() int {
	for i := len(m.allFiltered) - 1; i >= 0; i-- {
		if !m.allFiltered[i].isHeader {
			return i
		}
	}
	return -1
}

func (m EmoteListModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	cmdStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	selBtnStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(ColorHighlight)

	var b strings.Builder

	// Filter bar — highlighted when focused.
	filterPrefix := "  "
	if m.focus == 0 {
		filterPrefix = selBtnStyle.Render("▶ ")
	}
	b.WriteString(filterPrefix + m.filter.View())
	b.WriteString("\n\n")

	if m.confirmDelete {
		b.WriteString(errStyle.Render("  Delete /"+m.deleteTarget+"? y=yes, any key=cancel") + "\n\n")
	}

	if len(m.allFiltered) == 0 {
		b.WriteString(dimStyle.Render("  No emotes match your filter."))
	} else {
		maxCmdW := 0
		for _, r := range m.allFiltered {
			if !r.isHeader && len(r.label) > maxCmdW {
				maxCmdW = len(r.label)
			}
		}
		maxCmdW += 2

		visible := 18
		cur := m.list.Current()
		start := 0
		end := len(m.allFiltered)
		if end > visible {
			start = cur - visible/2
			if start < 0 {
				start = 0
			}
			end = start + visible
			if end > len(m.allFiltered) {
				end = len(m.allFiltered)
				start = end - visible
			}
		}
		for i := start; i < end; i++ {
			r := m.allFiltered[i]
			if r.isHeader {
				b.WriteString("\n" + headerStyle.Render("  "+r.label) + "\n")
				continue
			}
			cursor := "  "
			if m.focus == 1 && i == cur {
				cursor = selBtnStyle.Render("> ")
			}
			padded := r.label + strings.Repeat(" ", maxCmdW-len(r.label))
			row := cursor + cmdStyle.Render(padded) + " " + dimStyle.Render(r.preview)
			if r.emote != nil && r.emote.IsCustom && m.focus == 1 && i == cur {
				row += "  " + dimStyle.Render("[custom]")
			}
			b.WriteString(row + "\n")
		}
	}

	// Create New Emote button.
	b.WriteString("\n")
	createLabel := "  Create New Emote"
	if m.focus == 2 {
		createLabel = selBtnStyle.Render("▶ Create New Emote")
	}
	b.WriteString(createLabel + "\n")

	hints := "Tab: cycle focus"
	if m.focus == 1 {
		hints += HintSep + "Enter: insert" + HintSep + "e: edit" + HintSep + "d: delete"
	}
	hints += HintSep + FooterHintClose

	scaffold := OverlayScaffold{
		Title:       "Emotes",
		Footer:      hints,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 85,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
