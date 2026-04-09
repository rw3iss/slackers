package tui

// Temporary "Contact Card" view shown when the user picks "View
// Contact Info" on a friend pill. The modal always renders the
// card's properties; the action set on the bottom row is derived
// from the local state of that card:
//
//   - isSelf:           no action buttons (you can't friend yourself)
//   - existing != nil:  Merge missing fields, Overwrite with new
//                       values, Cancel — plus an inline diff of any
//                       fields that differ from the stored record
//   - existing == nil:  Add as new friend, Cancel
//
// All variants render via OverlayScaffold so the chrome stays
// consistent with rename / about / friendsconfig modals.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/friends"
)

// ContactCardImportMsg is dispatched when the user activates "Add as
// new friend" in the temporary contact view. The model's existing
// FriendCardClickedMsg handler runs the standard add-with-conflict
// resolution flow.
type ContactCardImportMsg struct {
	Card friends.ContactCard
}

// ContactCardMergeMsg is dispatched when the user picks "Merge" in
// the contact view (existing-friend variant). The model's
// applyFriendCard merge path handles it.
type ContactCardMergeMsg struct {
	Card friends.ContactCard
}

// ContactCardOverwriteMsg is dispatched when the user picks
// "Overwrite" in the contact view (existing-friend variant).
type ContactCardOverwriteMsg struct {
	Card friends.ContactCard
}

// ContactCardCloseMsg closes the contact view without doing
// anything else. Sent on Esc, Cancel, or by the self-card variant
// when the user dismisses it.
type ContactCardCloseMsg struct{}

// contactCardField is a single (label, value) row rendered in the
// modal. Empty values are skipped at render time.
type contactCardField struct {
	label string
	value string
}

// contactCardAction is a single button row at the bottom of the modal.
type contactCardAction struct {
	label string
	emit  func(friends.ContactCard) tea.Msg
}

// ContactCardViewModel renders a single contact card with action buttons.
type ContactCardViewModel struct {
	card     friends.ContactCard
	existing *friends.Friend // non-nil → friend already in local store
	isSelf   bool            // true → card represents the local user
	actions  []contactCardAction
	selected int
	width    int
	height   int
}

// NewContactCardView builds the contact card view in the appropriate
// variant. existing may be nil; if both existing != nil and isSelf
// are true (which shouldn't happen in practice), isSelf wins so the
// user can't accidentally merge their own card on top of a stale
// stored copy.
func NewContactCardView(card friends.ContactCard, existing *friends.Friend, isSelf bool) ContactCardViewModel {
	var actions []contactCardAction
	switch {
	case isSelf:
		// Own card: nothing to import / merge / overwrite. The
		// modal is a pure read-only view of your own properties.
		actions = []contactCardAction{
			{"Close", func(c friends.ContactCard) tea.Msg { return ContactCardCloseMsg{} }},
		}
	case existing != nil:
		actions = []contactCardAction{
			{"Merge missing fields", func(c friends.ContactCard) tea.Msg { return ContactCardMergeMsg{Card: c} }},
			{"Overwrite with new values", func(c friends.ContactCard) tea.Msg { return ContactCardOverwriteMsg{Card: c} }},
			{"Cancel", func(c friends.ContactCard) tea.Msg { return ContactCardCloseMsg{} }},
		}
	default:
		actions = []contactCardAction{
			{"Add as new friend", func(c friends.ContactCard) tea.Msg { return ContactCardImportMsg{Card: c} }},
			{"Cancel", func(c friends.ContactCard) tea.Msg { return ContactCardCloseMsg{} }},
		}
	}
	return ContactCardViewModel{
		card:     card,
		existing: existing,
		isSelf:   isSelf,
		actions:  actions,
	}
}

func (m *ContactCardViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Card returns the underlying contact card so callers can re-flow
// it into other handlers (e.g. ContactCardImportMsg →
// FriendCardClickedMsg).
func (m ContactCardViewModel) Card() friends.ContactCard { return m.card }

func (m ContactCardViewModel) Update(msg tea.Msg) (ContactCardViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ContactCardCloseMsg{} }
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j", "tab":
			if m.selected < len(m.actions)-1 {
				m.selected++
			}
		case "enter":
			if m.selected >= 0 && m.selected < len(m.actions) {
				act := m.actions[m.selected]
				card := m.card
				return m, func() tea.Msg { return act.emit(card) }
			}
		}
	}
	return m, nil
}

// fields returns the property rows to render. Empty values are
// skipped so the modal stays compact for SLF2-imported cards that
// don't carry name / email.
func (m ContactCardViewModel) fields() []contactCardField {
	c := m.card
	rows := []contactCardField{
		{"Name", c.Name},
		{"Email", c.Email},
		{"Slacker ID", c.SlackerID},
		{"Public Key", truncMiddle(c.PublicKey, 48)},
		{"Multiaddr", c.Multiaddr},
	}
	out := rows[:0]
	for _, r := range rows {
		if strings.TrimSpace(r.value) != "" {
			out = append(out, r)
		}
	}
	return out
}

// truncMiddle returns s shortened to about maxLen characters by
// replacing the middle with an ellipsis. Used for the Public Key
// row so the modal doesn't blow up to 80+ columns.
func truncMiddle(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen < 8 {
		return s
	}
	half := (maxLen - 1) / 2
	return s[:half] + "…" + s[len(s)-half:]
}

func (m ContactCardViewModel) View() string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(ColorDescText)
	noticeStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Italic(true)

	var b strings.Builder

	switch {
	case m.isSelf:
		b.WriteString(noticeStyle.Render("This is your own contact card."))
		b.WriteString("\n\n")
	case m.existing != nil:
		b.WriteString(noticeStyle.Render("This friend already exists in your local store."))
		b.WriteString("\n")
		diff := m.diffRows()
		if len(diff) > 0 {
			b.WriteString(noticeStyle.Render("The new card has different values for some fields."))
			b.WriteString("\n\n")
			b.WriteString(labelStyle.Render("  Changes"))
			b.WriteString("\n")
			for _, r := range diff {
				b.WriteString("    ")
				b.WriteString(labelStyle.Render(r.label))
				b.WriteString(": ")
				b.WriteString(valueStyle.Render(r.value))
				b.WriteString("\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString(noticeStyle.Render("All visible fields match the stored record."))
			b.WriteString("\n\n")
		}
	}

	for _, f := range m.fields() {
		b.WriteString("  ")
		b.WriteString(labelStyle.Render(fmt.Sprintf("%-11s", f.label+":")))
		b.WriteString(" ")
		b.WriteString(valueStyle.Render(f.value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	for i, a := range m.actions {
		cursor := "  "
		style := ChannelItemStyle
		if i == m.selected {
			cursor = "> "
			style = ChannelSelectedStyle
		}
		b.WriteString(style.Render(cursor + a.label))
		b.WriteString("\n")
	}

	title := "Contact Card"
	switch {
	case m.isSelf:
		title = "Contact Card — Your Profile"
	case m.existing != nil:
		title = "Contact Card — Existing Friend"
	}

	footer := "↑↓: navigate" + HintSep + "Enter: select" + HintSep + FooterHintCancel
	if m.isSelf {
		footer = "Enter / " + FooterHintClose
	}

	scaffold := OverlayScaffold{
		Title:       title,
		Footer:      footer,
		Width:       m.width,
		Height:      m.height,
		MaxBoxWidth: 70,
		BorderColor: ColorPrimary,
	}
	return scaffold.Render(b.String())
}

// diffRows returns label/value pairs for fields that differ between
// the stored friend and the incoming card. Used to render the
// "Changes" block when the contact view is opened on an
// already-imported friend so the user can see exactly which fields
// would be touched by Merge or Overwrite.
func (m ContactCardViewModel) diffRows() []contactCardField {
	if m.existing == nil {
		return nil
	}
	e := m.existing
	c := m.card
	type pair struct{ label, oldV, newV string }
	pairs := []pair{
		{"Name", e.Name, c.Name},
		{"Email", e.Email, c.Email},
		{"SlackerID", e.SlackerID, c.SlackerID},
		{"PublicKey", truncMiddle(e.PublicKey, 32), truncMiddle(c.PublicKey, 32)},
		{"Multiaddr", e.Multiaddr, c.Multiaddr},
	}
	out := make([]contactCardField, 0, len(pairs))
	for _, p := range pairs {
		if strings.TrimSpace(p.newV) == "" {
			continue
		}
		if p.oldV == p.newV {
			continue
		}
		out = append(out, contactCardField{
			label: p.label,
			value: p.oldV + "  →  " + p.newV,
		})
	}
	return out
}
