package tui

// Built-in slash command registrations.
//
// All commands are registered here as closures that capture *Model
// so they have full access to the friend store, slack service,
// config, P2P node, and so on. The commands package provides the
// dictionary, trie, fuzzy match, and Result type but knows
// nothing about the Model — keeping the framework reusable and
// unit-testable.
//
// Adding a new command is just a matter of:
//
//   1. Append a Command{} entry to buildCommandRegistry below
//   2. Implement its closure (or call out to an existing helper)
//   3. Optionally add a help/<name>.md file for /help <name>
//
// The registry is built once at NewModel time and reused for the
// lifetime of the process.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/theme"
	"github.com/rw3iss/slackers/internal/types"
)

// buildCommandRegistry constructs the slackers slash-command
// registry. Called from NewModel before any UI is shown so the
// dictionary is fully cached by the time the splash screen
// appears.
//
// The Model pointer is captured by every closure so commands can
// reach into the rest of the app state. The same model pointer
// stays alive for the whole process; commands invoked at any
// later time still see the current state.
func (m *Model) buildCommandRegistry() *commands.Registry {
	r := commands.NewRegistry()

	register := func(c commands.Command) {
		if err := r.Register(c); err != nil {
			// Duplicate / empty-name programming errors — surface
			// loudly so the regression is obvious.
			panic("commands: " + err.Error())
		}
	}

	// ---- General ---------------------------------------------------

	register(commands.Command{
		Name:        "commands",
		Aliases:     []string{"cmds", "cmd"},
		Description: "Open the Command List browser",
		Usage:       "/commands",
		Run: func(*commands.Context) commands.Result {
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    func() tea.Msg { return CommandListOpenMsg{} },
			}
		},
	})

	register(commands.Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Description: "Show help (optionally on a specific topic)",
		Usage:       "/help [topic]",
		Args: []commands.ArgSpec{{
			Name: "topic", Kind: commands.ArgHelpTopic, Optional: true,
			Help: "one of the topics from /help",
		}},
		Run: func(ctx *commands.Context) commands.Result {
			topic := "main"
			if len(ctx.Args) > 0 {
				topic = ctx.Args[0]
			}
			body, ok := commands.Topic(topic)
			if !ok {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Help topic not found: " + topic + " (try /help)",
				}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Title:  "Help — " + topic,
				Body:   body,
			}
		},
	})

	register(commands.Command{
		Name:        "version",
		Aliases:     []string{"ver"},
		Description: "Show the running slackers version",
		Usage:       "/version",
		Run: func(*commands.Context) commands.Result {
			return commands.Result{
				Status:    commands.StatusOK,
				StatusBar: "slackers v" + m.version,
			}
		},
	})

	register(commands.Command{
		Name:        "quit",
		Aliases:     []string{"exit", "q"},
		Description: "Exit slackers",
		Usage:       "/quit",
		Run: func(*commands.Context) commands.Result {
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    tea.Quit,
			}
		},
	})

	register(commands.Command{
		Name:        "me",
		Description: "Show your own contact info",
		Usage:       "/me",
		Run: func(*commands.Context) commands.Result {
			var b strings.Builder
			b.WriteString("# Your Contact Info\n\n")
			if m.cfg != nil {
				if m.cfg.MyName != "" {
					b.WriteString("- **Name:** " + m.cfg.MyName + "\n")
				}
				if m.cfg.MyEmail != "" {
					b.WriteString("- **Email:** " + m.cfg.MyEmail + "\n")
				}
				if m.cfg.SlackerID != "" {
					b.WriteString("- **SlackerID:** " + m.cfg.SlackerID + "\n")
				}
			}
			if m.secureMgr != nil {
				if pub := m.secureMgr.OwnPublicKeyBase64(); pub != "" {
					b.WriteString("- **PublicKey:** " + pub + "\n")
				}
			}
			if m.p2pNode != nil {
				if maddr := m.p2pNode.Multiaddr(); maddr != "" {
					b.WriteString("- **Multiaddr:** " + maddr + "\n")
				}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Title:  "Me",
				Body:   b.String(),
			}
		},
	})

	// ---- Friends ---------------------------------------------------

	register(commands.Command{
		Name:        "friends",
		Description: "List your friends",
		Usage:       "/friends",
		Run: func(*commands.Context) commands.Result {
			if m.friendStore == nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "Friends store not available"}
			}
			all := m.friendStore.All()
			if len(all) == 0 {
				return commands.Result{
					Status: commands.StatusOK,
					Title:  "Friends",
					Body:   "_No friends yet. Use `/add-friend <hash|json>` to add one._\n",
				}
			}
			sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
			var b strings.Builder
			b.WriteString(fmt.Sprintf("# Friends (%d)\n\n", len(all)))
			for _, f := range all {
				status := "offline"
				if f.Online {
					status = "online"
				}
				b.WriteString("- **" + f.Name + "** — " + status + "\n")
				if f.Email != "" {
					b.WriteString("    - Email: " + f.Email + "\n")
				}
				if f.Multiaddr != "" {
					b.WriteString("    - Multiaddr: " + f.Multiaddr + "\n")
				}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Title:  "Friends",
				Body:   b.String(),
			}
		},
	})

	register(commands.Command{
		Name:        "add-friend",
		Aliases:     []string{"addfriend", "befriend"},
		Description: "Import a friend by contact card hash or JSON",
		Usage:       "/add-friend <hash|json|[FRIEND:marker]>",
		Args: []commands.ArgSpec{{
			Name: "card", Kind: commands.ArgFriendCard,
			Help: "an SLF1/SLF2 hash, raw JSON contact card, or a [FRIEND:...] marker",
		}},
		Run: func(ctx *commands.Context) commands.Result {
			raw := strings.TrimSpace(ctx.Raw)
			if raw == "" {
				return commands.Result{Status: commands.StatusError, StatusBar: "Usage: /add-friend <hash|json>"}
			}
			// Allow paste of "[FRIEND:...]" with the marker.
			raw = strings.TrimPrefix(raw, "[FRIEND:")
			raw = strings.TrimSuffix(raw, "]")
			card, err := friends.ParseAnyContactCard(raw)
			if err != nil {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Could not parse contact card: " + err.Error(),
				}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    func() tea.Msg { return FriendCardClickedMsg{Card: card} },
			}
		},
	})

	register(commands.Command{
		Name:        "remove-friend",
		Aliases:     []string{"rmfriend", "unfriend"},
		Description: "Remove a friend by name or user ID",
		Usage:       "/remove-friend <name|id>",
		Args: []commands.ArgSpec{{
			Name: "friend", Kind: commands.ArgFriendID,
			Help: "the friend's display name, user id, or slacker id",
		}},
		Run: func(ctx *commands.Context) commands.Result {
			if m.friendStore == nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "Friends store not available"}
			}
			if len(ctx.Args) == 0 {
				return commands.Result{Status: commands.StatusError, StatusBar: "Usage: /remove-friend <name|id>"}
			}
			needle := strings.ToLower(strings.TrimSpace(strings.Join(ctx.Args, " ")))
			var match *friends.Friend
			for _, f := range m.friendStore.All() {
				if strings.ToLower(f.UserID) == needle ||
					strings.ToLower(f.SlackerID) == needle ||
					strings.ToLower(f.Name) == needle ||
					strings.ToLower(f.Email) == needle {
					f := f
					match = &f
					break
				}
			}
			if match == nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "No friend matched: " + needle}
			}
			m.pendingFriendRemoveID = match.UserID
			return commands.Result{
				Status:    commands.StatusOK,
				StatusBar: "Remove friend " + match.Name + "? y=yes, any other key=cancel",
			}
		},
	})

	// ---- Channels & messages ---------------------------------------

	register(commands.Command{
		Name:        "channels",
		Aliases:     []string{"chans"},
		Description: "List every channel and friend chat",
		Usage:       "/channels",
		Run: func(*commands.Context) commands.Result {
			all := m.channels.AllChannels()
			if len(all) == 0 {
				return commands.Result{Status: commands.StatusOK, Title: "Channels", Body: "_No channels._"}
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("# Channels (%d)\n\n", len(all)))
			for _, ch := range all {
				prefix := "#"
				switch {
				case ch.IsFriend:
					prefix = "👤"
				case ch.IsDM:
					prefix = "@"
				case ch.IsPrivate, ch.IsGroup:
					prefix = "🔒"
				}
				b.WriteString("- " + prefix + " " + ch.Name + "\n")
			}
			return commands.Result{Status: commands.StatusOK, Title: "Channels", Body: b.String()}
		},
	})

	register(commands.Command{
		Name:        "clear-history",
		Aliases:     []string{"clear"},
		Description: "Clear the current friend chat's history (with prompt)",
		Usage:       "/clear-history",
		Run: func(*commands.Context) commands.Result {
			if m.currentCh == nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "No channel selected"}
			}
			if !m.currentCh.IsFriend {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Only friend chat history can be cleared from here",
				}
			}
			m.pendingClearFriendHistoryID = m.currentCh.UserID
			return commands.Result{
				Status:    commands.StatusOK,
				StatusBar: "Clear chat history with " + m.currentCh.Name + "? y=yes, any other key=cancel",
			}
		},
	})

	register(commands.Command{
		Name:        "settings",
		Aliases:     []string{"prefs"},
		Description: "Open the settings overlay",
		Usage:       "/settings",
		Run: func(*commands.Context) commands.Result {
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    func() tea.Msg { return openSettingsMsg{} },
			}
		},
	})

	register(commands.Command{
		Name:        "shortcuts",
		Aliases:     []string{"keys", "keybinds"},
		Description: "Open the keyboard shortcuts editor",
		Usage:       "/shortcuts",
		Run: func(*commands.Context) commands.Result {
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    func() tea.Msg { return ShortcutsEditorOpenMsg{} },
			}
		},
	})

	// ---- Appearance ------------------------------------------------

	register(commands.Command{
		Name:        "themes",
		Description: "List installed themes",
		Usage:       "/themes",
		Run: func(*commands.Context) commands.Result {
			all := theme.LoadAll()
			var b strings.Builder
			b.WriteString("# Themes\n\n")
			cur := ""
			if m.cfg != nil {
				cur = m.cfg.Theme
			}
			for _, t := range all {
				marker := "  "
				if t.Name == cur {
					marker = "→ "
				}
				b.WriteString(marker + t.Name + "\n")
			}
			return commands.Result{Status: commands.StatusOK, Title: "Themes", Body: b.String()}
		},
	})

	register(commands.Command{
		Name:        "theme",
		Description: "Switch to a theme by name",
		Usage:       "/theme <name>",
		Args: []commands.ArgSpec{{
			Name: "name", Kind: commands.ArgThemeName,
			Help: "an installed theme name (see /themes)",
		}},
		Run: func(ctx *commands.Context) commands.Result {
			if len(ctx.Args) == 0 {
				return commands.Result{Status: commands.StatusError, StatusBar: "Usage: /theme <name>"}
			}
			name := ctx.Args[0]
			t, ok := theme.FindByName(name)
			if !ok {
				return commands.Result{Status: commands.StatusError, StatusBar: "Theme not found: " + name}
			}
			ApplyTheme(t)
			if m.cfg != nil {
				m.cfg.Theme = name
			}
			m.messages.Refresh()
			return commands.Result{Status: commands.StatusOK, StatusBar: "Theme: " + name}
		},
	})

	// ---- Diagnostics -----------------------------------------------

	register(commands.Command{
		Name:        "config",
		Description: "Show the current configuration",
		Usage:       "/config",
		Run: func(*commands.Context) commands.Result {
			if m.cfg == nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "No config loaded"}
			}
			// Marshal a redacted view — drop tokens.
			redacted := *m.cfg
			redacted.BotToken = mask(redacted.BotToken)
			redacted.AppToken = mask(redacted.AppToken)
			redacted.UserToken = mask(redacted.UserToken)
			redacted.ClientSecret = mask(redacted.ClientSecret)
			raw, err := json.MarshalIndent(redacted, "", "  ")
			if err != nil {
				return commands.Result{Status: commands.StatusError, StatusBar: "Marshal failed: " + err.Error()}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Title:  "Config",
				Body:   "```json\n" + string(raw) + "\n```",
			}
		},
	})

	return r
}

// mask hides sensitive token values for /config display.
func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// openSettingsMsg is a private message used by the /settings
// command to ask the model to open the Settings overlay. The
// model.go Update handler matches on it and routes to the same
// path Ctrl-S takes.
type openSettingsMsg struct{}

// applyCommandResult turns a commands.Result into the right side
// effects on the model: open the Output view if a Body is set,
// surface the StatusBar message, and dispatch the follow-up
// tea.Cmd. Returns the tea.Cmd to be scheduled (or nil).
//
// This is the single funnel for every command's outcome so the
// behaviour is consistent across slash invocation, the Command
// List "Enter" path, and any future programmatic dispatch.
func (m *Model) applyCommandResult(res commands.Result) tea.Cmd {
	if res.StatusBar != "" {
		m.warning = res.StatusBar
	}
	if res.Body != "" {
		// If the output view is already active, swap the
		// content in place so /help → /friends → /channels all
		// re-use the same pane (and the surrounding chrome
		// doesn't reflow on every command). Otherwise create a
		// fresh view and activate the pane state.
		if m.outputActive {
			m.outputView.SetTitle(res.Title)
			m.outputView.SetBody(res.Body)
		} else {
			m.outputView = NewOutputView(res.Title, res.Body)
			m.outputActive = true
		}
		m.outputView.SetSize(m.messages.width, m.messages.height)
		// Default: focus stays on whichever pane it was on
		// before the command (usually the input). Commands
		// whose output is interactive can opt into auto-focus
		// by setting Result.FocusOutput.
		if res.FocusOutput {
			m.focus = types.FocusMessages
		}
		m.updateFocus()
	}
	if res.Cmd != nil {
		if c, ok := res.Cmd.(tea.Cmd); ok {
			return c
		}
		// Allow plain func() tea.Msg as a convenience.
		if fn, ok := res.Cmd.(func() tea.Msg); ok {
			return tea.Cmd(fn)
		}
	}
	return nil
}

// refreshCmdSuggest re-evaluates the suggestion popup state from
// the current input bar value. Hides the popup if the input
// doesn't start with "/" or has no matches.
func (m *Model) refreshCmdSuggest() {
	val := strings.TrimSpace(m.input.Value())
	if !strings.HasPrefix(val, "/") || m.cmdRegistry == nil {
		m.cmdSuggest.Hide()
		return
	}
	// If the user has already typed a space the command name is
	// settled — drop the popup so it doesn't compete with the
	// arg the user is typing.
	rest := strings.TrimPrefix(val, "/")
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		m.cmdSuggest.Hide()
		return
	}
	matches := m.cmdRegistry.Lookup(val, 8)
	m.cmdSuggest.SetMatches(matches)
	m.cmdSuggest.SetWidth(m.width - 2)
}

// confirmClearFriendHistory wipes the current friend's chat
// history file from disk and refreshes the message view. Called
// from the y/Enter confirmation handler in model.go after the
// user agrees to /clear-history.
func (m *Model) confirmClearFriendHistory(userID string) tea.Cmd {
	if m.friendHistory == nil || userID == "" {
		return nil
	}
	if _, err := m.friendHistory.ClearHistory(userID); err != nil {
		m.warning = "Clear failed: " + err.Error()
		return nil
	}
	delete(m.friendMessages, userID)
	if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID == userID {
		m.messages.SetMessages(nil)
	}
	m.warning = "Chat history cleared"
	return nil
}
