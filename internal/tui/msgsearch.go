package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/friends"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// MsgSearchSelectMsg is sent when the user selects a search result.
type MsgSearchSelectMsg struct {
	ChannelID string
	MessageID string
	Timestamp time.Time
}

// MsgSearchResultsMsg carries search results back to the model.
type MsgSearchResultsMsg struct {
	Results []types.SearchResult
}

// MsgSearchModel provides a message search overlay.
//
// Searches both Slack channels (via the search.messages API) and the
// local friend-chat history. Slack's search query syntax natively
// supports "quoted phrases" — those are passed through verbatim. The
// friend-chat scanner uses the local parseSearchQuery helper to apply
// the same phrase + token semantics.
type MsgSearchModel struct {
	input     textinput.Model
	results   []types.SearchResult
	selected  int
	scopeAll  bool // false = current channel, true = all channels
	channelID string
	// isFriendScope is true when the current channel (the one the
	// user was in when they hit Ctrl+F) is a friend chat, so the
	// "Current channel" scope should limit the friend scan to just
	// that friend and skip the Slack API entirely.
	isFriendScope  bool
	loading        bool
	noResults      bool
	slackSvc       slackpkg.SlackService
	friendStore    *friends.FriendStore
	friendHistory  *friends.ChatHistoryStore
	width          int
	height         int
	channelResolve func(string) string
}

// NewMsgSearchModel creates a new message search overlay.
//
// If currentChannelID is empty, the overlay defaults to the
// "All channels" scope. If it's a Slack channel ID, "Current channel"
// means that Slack channel only. If it's a friend-chat ID
// (prefix "friend:"), "Current channel" means that friend only.
func NewMsgSearchModel(
	svc slackpkg.SlackService,
	friendStore *friends.FriendStore,
	friendHistory *friends.ChatHistoryStore,
	currentChannelID string,
	channelResolve func(string) string,
) MsgSearchModel {
	ti := textinput.New()
	ti.Placeholder = `Search messages...  (use "quotes" for exact phrase)`
	ti.Focus()
	ti.CharLimit = 256

	return MsgSearchModel{
		input:          ti,
		channelID:      currentChannelID,
		isFriendScope:  strings.HasPrefix(currentChannelID, "friend:"),
		slackSvc:       svc,
		friendStore:    friendStore,
		friendHistory:  friendHistory,
		scopeAll:       currentChannelID == "",
		channelResolve: channelResolve,
	}
}

// SetSize sets the overlay dimensions.
func (m *MsgSearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles key events in the message search overlay.
func (m MsgSearchModel) Update(msg tea.Msg) (MsgSearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil
		case "tab":
			m.scopeAll = !m.scopeAll
			m.results = nil
			m.noResults = false
			m.selected = 0
			// Re-search with new scope
			if m.input.Value() != "" {
				m.loading = true
				return m, m.searchCmd()
			}
			return m, nil
		case "enter":
			if len(m.results) > 0 && m.selected < len(m.results) {
				r := m.results[m.selected]
				return m, func() tea.Msg {
					return MsgSearchSelectMsg{
						ChannelID: r.ChannelID,
						MessageID: r.Message.MessageID,
						Timestamp: r.Message.Timestamp,
					}
				}
			}
			return m, nil
		}

	case MsgSearchResultsMsg:
		m.loading = false
		m.results = msg.Results
		m.noResults = len(msg.Results) == 0 && m.input.Value() != ""
		m.selected = 0
		return m, nil
	}

	prevValue := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Trigger search when input changes (simple debounce: search on every change)
	if m.input.Value() != prevValue {
		query := m.input.Value()
		if query == "" {
			m.results = nil
			m.noResults = false
			m.loading = false
			return m, cmd
		}
		m.loading = true
		return m, tea.Batch(cmd, m.searchCmd())
	}

	return m, cmd
}

// searchCmd fans out to both Slack and the friend-history scanner,
// merges the results, sorts them by timestamp descending, and caps
// the combined list at the same limit the Slack API uses (20).
//
// Scope rules:
//   - scopeAll == true           → Slack (global) + every friend chat
//   - scopeAll == false          → follow m.channelID:
//   - empty                 → identical to scopeAll
//   - Slack channel         → Slack-only, scoped with in:<#…>
//   - "friend:<uid>" prefix → that friend's chat only, no Slack call
//
// Slack's search.messages natively honours "quoted phrases" in its
// query syntax, so the raw query is forwarded to it untouched. The
// friend scanner uses parseSearchQuery so the same phrase + token
// semantics apply to both backends.
func (m *MsgSearchModel) searchCmd() tea.Cmd {
	query := m.input.Value()
	svc := m.slackSvc
	friendStore := m.friendStore
	friendHistory := m.friendHistory
	scopeAll := m.scopeAll
	channelID := m.channelID
	isFriendScope := m.isFriendScope

	const limit = 20

	return func() tea.Msg {
		var combined []types.SearchResult

		// --- Slack side ---
		slackChannelID := ""
		if !scopeAll {
			if isFriendScope {
				// Current scope is a friend chat — skip Slack.
				goto friendsSearch
			}
			slackChannelID = channelID
		}
		if svc != nil {
			results, err := svc.SearchMessages(query, slackChannelID, limit)
			if err == nil {
				combined = append(combined, results...)
			}
		}

	friendsSearch:
		// --- Friend side (local) ---
		if friendStore != nil && friendHistory != nil {
			scopeFriend := ""
			if !scopeAll && isFriendScope {
				scopeFriend = strings.TrimPrefix(channelID, "friend:")
			}
			fr := searchFriendHistory(friendStore, friendHistory, query, scopeFriend, limit)
			combined = append(combined, fr...)
		}

		// --- Merge / sort / cap ---
		sort.Slice(combined, func(i, j int) bool {
			return combined[i].Message.Timestamp.After(combined[j].Message.Timestamp)
		})
		if len(combined) > limit {
			combined = combined[:limit]
		}
		return MsgSearchResultsMsg{Results: combined}
	}
}

// searchFriendHistory walks either one friend's history (if
// scopeFriendUID is set) or every friend's history (if empty),
// filtering messages that satisfy the parsed query. Results are
// wrapped in the same types.SearchResult envelope the Slack API
// produces so the overlay renderer can treat them uniformly.
//
// The scan is bounded by `limit` across the friend set to keep the
// UI responsive even when a friend has tens of thousands of cached
// messages — the result list is sliced down to the limit, sorted
// chronologically descending, so the most recent matches win.
func searchFriendHistory(
	store *friends.FriendStore,
	hist *friends.ChatHistoryStore,
	query string,
	scopeFriendUID string,
	limit int,
) []types.SearchResult {
	phrases, tokens := parseSearchQuery(query)
	if len(phrases) == 0 && len(tokens) == 0 {
		return nil
	}

	type friendScope struct {
		uid     string
		name    string
		pairKey string
	}
	var scopes []friendScope

	if scopeFriendUID != "" {
		f := store.Get(scopeFriendUID)
		if f == nil {
			return nil
		}
		scopes = append(scopes, friendScope{uid: f.UserID, name: f.Name, pairKey: f.PairKey})
	} else {
		for _, f := range store.All() {
			scopes = append(scopes, friendScope{uid: f.UserID, name: f.Name, pairKey: f.PairKey})
		}
	}

	var out []types.SearchResult
	for _, sc := range scopes {
		msgs := hist.GetDecrypted(sc.uid, sc.pairKey)
		for _, msg := range msgs {
			if !matchesQuery(msg.Text, phrases, tokens) {
				continue
			}
			rm := msg
			// Fill in a friend-facing user name so the result
			// list shows "You" or the friend's display name
			// rather than a raw UID.
			if rm.UserName == "" {
				if rm.UserID == "me" {
					rm.UserName = "You"
				} else {
					rm.UserName = sc.name
				}
			}
			out = append(out, types.SearchResult{
				Message:     rm,
				ChannelID:   "friend:" + sc.uid,
				ChannelName: sc.name,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Message.Timestamp.After(out[j].Message.Timestamp)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// View renders the message search overlay.
func (m MsgSearchModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	scopeStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	inactiveScopeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	resultUserStyle := lipgloss.NewStyle().
		Bold(true)

	resultTimeStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	resultChannelStyle := lipgloss.NewStyle().
		Foreground(ColorAccent)

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	matchHighlightStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	var b strings.Builder

	b.WriteString(titleStyle.Render("Search Messages"))
	b.WriteString("\n\n")

	// Scope toggle
	currentLabel := "Current Channel"
	allLabel := "All Channels"
	if m.scopeAll {
		b.WriteString("  " + inactiveScopeStyle.Render(currentLabel) + "  " + scopeStyle.Render("["+allLabel+"]"))
	} else {
		b.WriteString("  " + scopeStyle.Render("["+currentLabel+"]") + "  " + inactiveScopeStyle.Render(allLabel))
	}
	b.WriteString("    ")
	b.WriteString(dimStyle.Render("(Tab to toggle)"))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(`  Tip: wrap an exact phrase in "quotes" — e.g. "deploy pipeline"`))
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("  Searching..."))
	} else if m.noResults {
		hint := "  No results found."
		if len(m.input.Value()) < 2 && m.scopeAll {
			hint = "  No results. Try a longer query (Slack requires 2+ characters for global search)."
		}
		b.WriteString(dimStyle.Render(hint))
	} else if len(m.results) > 0 {
		// Each result takes ~3 lines (header + text + blank). Calculate how many fit.
		linesPerResult := 3
		availableLines := m.height - 14 // header, scope, input, footer
		maxVisible := availableLines / linesPerResult
		if maxVisible < 3 {
			maxVisible = 3
		}
		if maxVisible > len(m.results) {
			maxVisible = len(m.results)
		}

		start := 0
		if m.selected >= maxVisible {
			start = m.selected - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.results) {
			end = len(m.results)
		}

		for i := start; i < end; i++ {
			r := m.results[i]
			cursor := "  "
			nameStyle := resultUserStyle
			if i == m.selected {
				cursor = "> "
				nameStyle = selectedStyle
			}

			// Build a windowed preview centred on the earliest
			// query match so the user can see the actual matching
			// context rather than the message's opening few words.
			// Highlight the match itself so it stands out.
			maxTextLen := m.width - 16
			if maxTextLen < 20 {
				maxTextLen = 20
			}
			preview := buildSearchPreview(r.Message.Text, m.input.Value(), maxTextLen)
			text := preview.Text
			if preview.MatchStart >= 0 && preview.MatchLen > 0 {
				pr := []rune(text)
				endRune := preview.MatchStart + preview.MatchLen
				if endRune > len(pr) {
					endRune = len(pr)
				}
				if preview.MatchStart < len(pr) {
					text = string(pr[:preview.MatchStart]) +
						matchHighlightStyle.Render(string(pr[preview.MatchStart:endRune])) +
						string(pr[endRune:])
				}
			}

			timeStr := r.Message.Timestamp.Format("Jan 2 15:04")

			line := fmt.Sprintf("%s%s  %s",
				cursor,
				nameStyle.Render(r.Message.UserName),
				resultTimeStyle.Render(timeStr),
			)
			if m.scopeAll {
				chDisplay := "#" + r.ChannelName
				if m.channelResolve != nil {
					chDisplay = m.channelResolve(r.ChannelID)
				}
				line += "  " + resultChannelStyle.Render(chDisplay)
			}
			b.WriteString(line)
			b.WriteString("\n")
			b.WriteString("    " + text)
			b.WriteString("\n\n") // blank line between results
		}
	}

	b.WriteString(dimStyle.Render("  Enter: go to message | Tab: toggle scope | Esc: close"))

	content := b.String()

	// Use nearly the full terminal size.
	boxWidth := m.width - 4
	if boxWidth > m.width {
		boxWidth = m.width
	}
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
