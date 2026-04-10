package tui

// Emotes List overlay — full-screen browser of every registered
// emote. Opened by /emotes or its keybinding. Has a filter input
// at the top, two sections (System / Custom), and renders each
// row as "/command — preview text". Enter inserts the highlighted
// emote's command into the input bar. 'e' edits, 'n' creates
// new, 'd' deletes (custom only, with confirmation).

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
	allFiltered   []emoteRow // combined filtered list for rendering
	filter        textinput.Model
	list          SelectableList
	width, height int
	confirmDelete bool
	deleteTarget  string
}

// emoteRow is one row in the combined filtered list. It carries
// the section header flag so the renderer can draw group labels.
type emoteRow struct {
	isHeader bool
	label    string        // header text or "/command"
	preview  string        // abbreviated emote text
	emote    *emotes.Emote // nil for headers
}

// NewEmoteList builds the overlay from the emote store.
func NewEmoteList(store *emotes.Store) EmoteListModel {
	ti := textinput.New()
	ti.Placeholder = "Filter emotes..."
	ti.Prompt = "/ "
	ti.CharLimit = 64
	ti.Focus()

	m := EmoteListModel{
		store:  store,
		filter: ti,
	}
	m.rebuildList()
	return m
}

func (m *EmoteListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// rebuildList regenerates the filtered display rows from the
// store's system and custom emote lists.
func (m *EmoteListModel) rebuildList() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.systemEmotes = m.store.Defaults()
	m.customEmotes = m.store.Custom()

	m.allFiltered = nil
	// System emotes section.
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
		sysRows = append(sysRows, emoteRow{
			label:   "/" + e.Command,
			preview: preview,
			emote:   &e,
		})
	}
	if len(sysRows) > 0 {
		m.allFiltered = append(m.allFiltered, emoteRow{isHeader: true, label: "System Emotes"})
		m.allFiltered = append(m.allFiltered, sysRows...)
	}
	// Custom emotes section.
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
		cusRows = append(cusRows, emoteRow{
			label:   "/" + e.Command,
			preview: preview,
			emote:   &e,
		})
	}
	if len(cusRows) > 0 {
		m.allFiltered = append(m.allFiltered, emoteRow{isHeader: true, label: "Custom Emotes"})
		m.allFiltered = append(m.allFiltered, cusRows...)
	}
	m.list.SetCount(len(m.allFiltered))
}

// selectedEmote returns the emote at the current cursor, or nil
// if the cursor is on a header or out of range.
func (m *EmoteListModel) selectedEmote() *emotes.Emote {
	idx := m.list.Current()
	if idx < 0 || idx >= len(m.allFiltered) {
		return nil
	}
	return m.allFiltered[idx].emote
}

func (m EmoteListModel) Update(msg tea.Msg) (EmoteListModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Confirmation mode for delete.
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

		switch v.String() {
		case "esc":
			return m, func() tea.Msg { return EmoteListCloseMsg{} }
		case "enter":
			if e := m.selectedEmote(); e != nil {
				cmd := e.Command
				return m, func() tea.Msg { return EmoteListSelectMsg{Command: cmd} }
			}
			return m, nil
		case "e":
			// Edit selected emote.
			if e := m.selectedEmote(); e != nil {
				cmd := e.Command
				return m, func() tea.Msg { return EmoteEditRequestMsg{Command: cmd} }
			}
			return m, nil
		case "n":
			// New emote.
			return m, func() tea.Msg { return EmoteEditRequestMsg{} }
		case "d":
			// Delete (custom only).
			if e := m.selectedEmote(); e != nil && e.IsCustom {
				m.confirmDelete = true
				m.deleteTarget = e.Command
				return m, nil
			}
			return m, nil
		}
		// Navigation keys go to SelectableList, skipping headers.
		if v.String() == "up" || v.String() == "k" {
			m.moveSkippingHeaders(-1)
			return m, nil
		}
		if v.String() == "down" || v.String() == "j" {
			m.moveSkippingHeaders(1)
			return m, nil
		}
		if v.String() == "pgup" || v.String() == "pgdown" || v.String() == "home" || v.String() == "end" {
			if m.list.HandleKey(v) {
				// After page nav, skip to the nearest non-header.
				m.skipToNonHeader(1)
			}
			return m, nil
		}
		// Everything else goes to the filter input.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildList()
		return m, cmd
	}
	return m, nil
}

// moveSkippingHeaders moves the cursor by delta, skipping header rows.
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

// skipToNonHeader ensures the cursor isn't sitting on a header.
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

func (m EmoteListModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	cmdStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)

	var b strings.Builder

	// Filter bar.
	b.WriteString("  " + m.filter.View())
	b.WriteString("\n\n")

	if m.confirmDelete {
		b.WriteString(titleStyle.Render("  Delete /" + m.deleteTarget + "? y=yes, any key=cancel"))
		b.WriteString("\n\n")
	}

	if len(m.allFiltered) == 0 {
		b.WriteString(dimStyle.Render("  No emotes match your filter."))
	} else {
		// Compute longest command name for column alignment.
		maxCmdW := 0
		for _, r := range m.allFiltered {
			if !r.isHeader && len(r.label) > maxCmdW {
				maxCmdW = len(r.label)
			}
		}
		maxCmdW += 2

		// Window around cursor.
		visible := 20
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
				b.WriteString("\n")
				b.WriteString(headerStyle.Render("  " + r.label))
				b.WriteString("\n")
				continue
			}
			cursor := "  "
			style := ChannelItemStyle
			if i == cur {
				cursor = "> "
				style = ChannelSelectedStyle
			}
			padded := r.label + strings.Repeat(" ", maxCmdW-len(r.label))
			row := cursor + cmdStyle.Render(padded) + " " + dimStyle.Render(r.preview)
			// If selected and the emote is custom, show a tag.
			if r.emote != nil && r.emote.IsCustom && i == cur {
				row += "  " + style.Render("[custom]")
			}
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	footer := "Enter: insert" + HintSep + "e: edit" + HintSep + "n: new" + HintSep + "d: delete (custom)" + HintSep + FooterHintClose

	scaffold := OverlayScaffold{
		Title:       "Emotes",
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 85,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
