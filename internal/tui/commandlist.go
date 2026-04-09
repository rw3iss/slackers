package tui

// Command List overlay — full-screen browser of every registered
// command. Opened by /commands or its keybinding. Has a filter
// input at the top, embeds SelectableList for cursor handling, and
// renders each row as the canonical name + description in two
// aligned columns. Enter inserts the highlighted command into the
// input bar (with leading slash and a trailing space) so the user
// can fill in arguments.
//
// Mirrors the look of the existing hidden / fileslist / settings
// overlays so it stays consistent with the rest of the UI.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/commands"
)

// CommandListOpenMsg requests the model to open the Command List
// overlay. Used by both the /commands command itself and any
// future global keybinding.
type CommandListOpenMsg struct{}

// CommandListSelectMsg signals that the user picked a command
// from the list. The model handler inserts "/<name> " into the
// input bar and focuses the input.
type CommandListSelectMsg struct {
	Name string
}

// CommandListCloseMsg dismisses the overlay without selection.
type CommandListCloseMsg struct{}

// CommandListModel is the overlay state.
type CommandListModel struct {
	all      []*commands.Command
	filtered []*commands.Command
	filter   textinput.Model
	list     SelectableList
	width    int
	height   int
}

// NewCommandList builds the overlay over the registry's full
// command set. Pass r.All() in.
func NewCommandList(all []*commands.Command) CommandListModel {
	ti := textinput.New()
	ti.Placeholder = "Filter commands..."
	ti.Prompt = "🔍 "
	ti.CharLimit = 64
	ti.Focus()

	m := CommandListModel{
		all:      all,
		filtered: all,
		filter:   ti,
	}
	m.list.SetCount(len(all))
	m.list.WrapAround = true
	return m
}

func (m *CommandListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// rebuildFiltered rescores the full set against the current filter
// text and updates the SelectableList count.
func (m *CommandListModel) rebuildFiltered() {
	q := strings.TrimSpace(m.filter.Value())
	if q == "" {
		m.filtered = m.all
		m.list.SetCount(len(m.filtered))
		return
	}
	// Lowercase substring + first-letter pass — cheap for the
	// command count we're working with. The full fuzzy scorer
	// from the commands package isn't strictly needed here, but
	// using it keeps the ranking consistent with the suggestion
	// popup.
	type scored struct {
		c     *commands.Command
		score int
	}
	out := make([]scored, 0, len(m.all))
	for _, c := range m.all {
		if s := commands.FuzzyScore(q, c.Name); s > 0 {
			out = append(out, scored{c, s})
		} else if c.Description != "" && strings.Contains(strings.ToLower(c.Description), strings.ToLower(q)) {
			out = append(out, scored{c, 1})
		}
	}
	// Stable sort by score descending then name ascending.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if out[j].score > out[j-1].score ||
				(out[j].score == out[j-1].score && out[j].c.Name < out[j-1].c.Name) {
				out[j], out[j-1] = out[j-1], out[j]
				continue
			}
			break
		}
	}
	m.filtered = make([]*commands.Command, len(out))
	for i, s := range out {
		m.filtered[i] = s.c
	}
	m.list.SetCount(len(m.filtered))
}

func (m CommandListModel) Update(msg tea.Msg) (CommandListModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		// Esc closes.
		if v.String() == "esc" {
			return m, func() tea.Msg { return CommandListCloseMsg{} }
		}
		// Enter selects.
		if v.String() == "enter" {
			cur := m.list.Current()
			if cur >= 0 && cur < len(m.filtered) {
				name := m.filtered[cur].Name
				return m, func() tea.Msg { return CommandListSelectMsg{Name: name} }
			}
			return m, nil
		}
		// Navigation keys go to the SelectableList first.
		if m.list.HandleKey(v) {
			return m, nil
		}
		// Anything else goes to the filter input.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildFiltered()
		return m, cmd
	}
	return m, nil
}

func (m CommandListModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	tagStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Commands"))
	b.WriteString("\n\n")
	b.WriteString("  " + m.filter.View())
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  No commands match your filter."))
	} else {
		// Compute the longest command name for column alignment.
		maxNameW := 0
		for _, c := range m.filtered {
			if l := len(c.FullName()); l > maxNameW {
				maxNameW = l
			}
		}
		maxNameW += 2

		// Window the visible slice around the cursor.
		visible := 18
		start := 0
		end := len(m.filtered)
		cur := m.list.Current()
		if end > visible {
			start = cur - visible/2
			if start < 0 {
				start = 0
			}
			end = start + visible
			if end > len(m.filtered) {
				end = len(m.filtered)
				start = end - visible
			}
		}
		for i := start; i < end; i++ {
			c := m.filtered[i]
			name := c.FullName()
			padded := name + strings.Repeat(" ", maxNameW-len(name))
			cursor := "  "
			style := ChannelItemStyle
			if i == cur {
				cursor = "> "
				style = ChannelSelectedStyle
			}
			row := cursor + style.Render(padded) + "  " + dimStyle.Render(c.Description)
			if c.Kind == commands.KindEmote {
				row += "  " + tagStyle.Render("[emote]")
			}
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	scaffold := OverlayScaffold{
		Title:       "Command List",
		Footer:      "↑↓: navigate" + HintSep + "Enter: select" + HintSep + FooterHintClose,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 80,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}
