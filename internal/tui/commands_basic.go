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
	"github.com/rw3iss/slackers/internal/config"
	_ "github.com/rw3iss/slackers/internal/emotes"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/setup"
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
		Run: func(ctx *commands.Context) commands.Result {
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
		Run: func(ctx *commands.Context) commands.Result {
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
			m := ctx.Host.(*Model)
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
		Run: func(ctx *commands.Context) commands.Result {
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
		Run: func(ctx *commands.Context) commands.Result {
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
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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
			m := ctx.Host.(*Model)
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
		Name:        "share",
		Description: "Share your contact card, setup, a friend's card, or a theme in the current chat",
		Usage:       "/share <me|profile|setup|friend <id|name>|theme <name>>",
		Args: []commands.ArgSpec{
			{Name: "target", Help: "one of: me, profile, setup, friend, theme"},
			{Name: "value", Optional: true, Help: "second-arg value for friend / theme subcommands"},
		},
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
			if len(ctx.Args) == 0 {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Usage: /share <me|profile|setup|friend <name>|theme <name>>",
				}
			}
			target := findShareTarget(ctx.Args[0])
			if target == nil {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Unknown /share target: " + ctx.Args[0] + " (try me, profile, setup, friend, theme)",
				}
			}
			if target.needsArg && len(ctx.Args) < 2 {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "/share " + target.name + " requires a second argument (e.g. /share " + target.name + " <name>)",
				}
			}
			// Build the message body for the selected target.
			body, err := m.buildShareBody(*target, ctx.Args)
			if err != nil {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Share failed: " + err.Error(),
				}
			}
			if body == "" {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Share produced no content — nothing to send",
				}
			}
			// If a channel is selected, send the share content
			// directly into the chat via the normal send path
			// (friend vs slack, expandFriendMarkers, etc.).
			// Otherwise fall back to showing it in the Output
			// view — this covers the "no channel open yet"
			// and "output view is active" cases so the user
			// always sees useful output instead of an error.
			if m.currentCh != nil {
				chName := m.currentCh.Name
				tgtName := target.name
				return commands.Result{
					Status:    commands.StatusOK,
					StatusBar: "Shared " + tgtName + " in " + chName,
					Cmd: func() tea.Msg {
						return InputSendMsg{Text: body}
					},
				}
			}
			// No channel — show in Output view with selectable
			// sections so the user can arrow-select + copy.
			switch target.name {
			case "setup":
				return m.buildShareSetupOutput()
			case "me", "profile":
				return m.buildShareProfileOutput()
			default:
				return commands.Result{
					Status: commands.StatusOK,
					Title:  "Share: " + target.name,
					Sections: []commands.Section{
						{Text: body, Selectable: true},
					},
					StatusBar: "No channel selected — select an item and press 'c' to copy",
				}
			}
		},
	})

	register(commands.Command{
		Name:        "setup",
		Description: "Import or share workspace credentials",
		Usage:       "/setup <json|hash|--flags> · /setup share [hash|json]",
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
			if len(ctx.Args) == 0 {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Usage: /setup <json|hash|--flags>  or  /setup share [hash|json]",
				}
			}
			// `/setup share [hash|json]`
			if strings.EqualFold(ctx.Args[0], "share") {
				format := "hash"
				if len(ctx.Args) > 1 {
					format = strings.ToLower(ctx.Args[1])
				}
				if format != "hash" && format != "json" {
					return commands.Result{
						Status:    commands.StatusError,
						StatusBar: "Usage: /setup share [hash|json]",
					}
				}
				return m.buildSetupShareResult(format)
			}
			// Import path — reuse the unified parser.
			parsed, err := setup.ParseAny(ctx.Raw)
			if err != nil {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: "Setup: " + err.Error(),
				}
			}
			// Kick off the confirmation-aware import. The
			// Cmd isn't nil-safe here — importSetupConfig may
			// mutate m directly (stage a pending prompt) and
			// return nil, OR return applySetupConfig's cmd.
			cmd := m.importSetupConfig(parsed)
			if m.pendingSetupImport != nil {
				// Confirmation staged — StatusBar is already
				// populated by importSetupConfig.
				return commands.Result{Status: commands.StatusOK}
			}
			return commands.Result{
				Status: commands.StatusOK,
				Cmd:    cmd,
			}
		},
	})

	register(commands.Command{
		Name:        "config",
		Description: "Show the current configuration",
		Usage:       "/config",
		Run: func(ctx *commands.Context) commands.Result {
			m := ctx.Host.(*Model)
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

	// ---- Emotes -------------------------------------------------------
	// Register every emote from the merged dictionary as a
	// KindEmote command. They show in the suggestion popup and
	// the Command List tagged [emote]. The generic /emote takes
	// free-form $text.
	if m.emoteStore != nil {
		for _, e := range m.emoteStore.All() {
			emote := e // capture for closure
			desc := emote.Text
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			cmd := commands.Command{
				Name:        emote.Command,
				Kind:        commands.KindEmote,
				Description: desc,
				Usage:       "/" + emote.Command,
				Run: func(ctx *commands.Context) commands.Result {
					m := ctx.Host.(*Model)
					return m.executeEmote(emote.Command, ctx)
				},
			}
			if emote.Command == "emote" {
				cmd.Args = []commands.ArgSpec{{
					Name: "text", Kind: commands.ArgString,
					Help: "custom emote text (used as $text)",
				}}
			}
			_ = r.Register(cmd) // ignore duplicates with builtins
		}
	}

	return r
}

// EmoteSendMsg is dispatched by the emote command's Run closure.
// The model handler routes it through the appropriate send path
// (Slack or P2P) and creates a local optimistic Message with
// IsEmote=true so the renderer applies the emote style.
type EmoteSendMsg struct {
	FormattedText string
}

// executeEmote formats an emote template and returns a Result
// that dispatches EmoteSendMsg into the current chat.
func (m *Model) executeEmote(command string, ctx *commands.Context) commands.Result {
	if m.emoteStore == nil {
		return commands.Result{Status: commands.StatusError, StatusBar: "Emote system not available"}
	}
	if m.currentCh == nil {
		return commands.Result{Status: commands.StatusError, StatusBar: "No channel selected"}
	}
	// Determine sender name.
	sender := "You"
	if m.cfg != nil && m.cfg.MyName != "" {
		sender = m.cfg.MyName
	}
	// Determine receiver name.
	receiver := ""
	if m.currentCh.IsFriend || m.currentCh.IsDM {
		if m.friendStore != nil && m.currentCh.IsFriend {
			if f := m.friendStore.Get(m.currentCh.UserID); f != nil && f.Name != "" {
				receiver = f.Name
			}
		}
		if receiver == "" {
			if u, ok := m.users[m.currentCh.UserID]; ok {
				if u.DisplayName != "" {
					receiver = u.DisplayName
				} else if u.RealName != "" {
					receiver = u.RealName
				}
			}
		}
		if receiver == "" {
			receiver = m.currentCh.Name
		}
	}
	// Free text for the generic /emote command.
	freeText := ctx.Raw
	formatted, err := m.emoteStore.FormatText(command, sender, receiver, "", freeText)
	if err != nil {
		return commands.Result{Status: commands.StatusError, StatusBar: err.Error()}
	}
	return commands.Result{
		Status: commands.StatusOK,
		Cmd: func() tea.Msg {
			return EmoteSendMsg{FormattedText: formatted}
		},
	}
}

// rebuildCommandRegistry rebuilds the slash-command registry
// from scratch. Called after emote edits/deletes so the trie
// and lookup tables reflect the latest dictionary.
func (m *Model) rebuildCommandRegistry() {
	m.cmdRegistry = m.buildCommandRegistry()
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

// shareTarget describes one valid first-argument to the /share
// command. Kept at package scope so both the dispatcher and the
// argument-completion popup pull from the same source of truth,
// and adding a new share target is a one-line change here.
type shareTarget struct {
	name        string
	description string
	// needsArg, when true, means the target requires a second
	// positional argument (friend id, theme name, etc.) and the
	// dispatcher should error on a missing second arg.
	needsArg bool
	// secondArgKind, when needsArg is true, is used by the
	// context-aware arg completion popup to fetch the second-
	// level pool (friend list, theme list, …).
	secondArgKind commands.ArgKind
}

// shareTargets is the ordered list of /share subcommands. The
// order is the default popup order when the user types "/share "
// with no partial. Add new entries here to expose them in both
// the command dispatcher and the suggestion popup in one step.
var shareTargets = []shareTarget{
	{
		name:        "me",
		description: "Share your own contact card in the current chat",
	},
	{
		name:        "profile",
		description: "Alias for /share me",
	},
	{
		name:        "setup",
		description: "Share your workspace setup credentials (no user token)",
	},
	{
		name:          "friend",
		description:   "Share a friend's contact card — needs a friend id/name",
		needsArg:      true,
		secondArgKind: commands.ArgFriendID,
	},
	{
		name:          "theme",
		description:   "Share a theme's JSON in the current chat — needs a theme name",
		needsArg:      true,
		secondArgKind: commands.ArgThemeName,
	},
}

// findShareTarget returns the shareTarget matching the given
// name (case-insensitive), or nil if the name isn't a known
// subcommand.
func findShareTarget(name string) *shareTarget {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range shareTargets {
		if shareTargets[i].name == name {
			return &shareTargets[i]
		}
	}
	return nil
}

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
	hasOutput := res.Body != "" || len(res.Sections) > 0
	if hasOutput {
		// If the output view is already active, swap the
		// content in place so /help → /friends → /channels all
		// re-use the same pane (and the surrounding chrome
		// doesn't reflow on every command). Otherwise create a
		// fresh view and activate the pane state.
		if m.outputActive {
			m.outputView.SetTitle(res.Title)
		} else {
			m.outputView = NewOutputView(res.Title, "")
			m.outputActive = true
		}
		if len(res.Sections) > 0 {
			// Structured path — each section becomes one
			// selectable item, code snippets parsed for sub
			// selection.
			m.outputView.SetItems(res.Sections)
		} else {
			m.outputView.SetBody(res.Body)
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
// the current input bar value. Three states:
//
//  1. Input doesn't start with "/" → hide popup.
//  2. Input is "/cmd" with no space yet → command mode (fuzzy
//     match against the registry).
//  3. Input is "/cmd <prior args…> <partial>" → arg mode. Prior
//     args are parsed out of the input and passed to
//     argCompletionsForContext so commands like /share whose
//     second-arg type depends on the first-arg value can
//     dispatch the right candidate pool.
//
// Uses the raw (non-trimmed) input value for space-boundary
// detection so "/theme " (trailing space) enters arg mode even
// though TrimSpace would collapse it.
func (m *Model) refreshCmdSuggest() {
	raw := m.input.Value()
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") || m.cmdRegistry == nil {
		m.cmdSuggest.Hide()
		return
	}
	// Use the raw (not-trimmed) input for space boundary
	// detection so a trailing space enters arg mode with an
	// empty partial, showing the full pool.
	rawBody := strings.TrimPrefix(strings.TrimLeft(raw, " \t"), "/")
	spaceIdx := strings.IndexAny(rawBody, " \t")
	if spaceIdx < 0 {
		// Command mode — no space yet.
		matches := m.cmdRegistry.Lookup(trimmed, 8)
		m.cmdSuggest.SetMatches(matches)
		m.cmdSuggest.SetWidth(m.width - 2)
		return
	}

	// Arg mode. Split the body into (cmdName, rest-after-name).
	name := rawBody[:spaceIdx]
	rest := rawBody[spaceIdx+1:]

	// Tokenize prior args (everything except the final in-
	// progress token). The final token may be empty (trailing
	// space → user just hit space, no partial yet).
	var priorArgs []string
	var partial string
	if lastSpace := strings.LastIndexAny(rest, " \t"); lastSpace >= 0 {
		// Split on every whitespace run before the final one.
		head := rest[:lastSpace]
		priorArgs = strings.Fields(head)
		partial = rest[lastSpace+1:]
	} else {
		// Exactly one arg so far — the current in-progress one.
		partial = rest
	}

	pool := m.argCompletionsForContext(name, priorArgs)
	if len(pool) == 0 {
		m.cmdSuggest.Hide()
		return
	}
	ranked := rankArgCandidates(partial, pool, 8)
	if len(ranked) == 0 {
		m.cmdSuggest.Hide()
		return
	}
	// Prefix prepended on Tab-complete is everything up to
	// (and including) the final whitespace. The completion
	// replaces only the partial token at the end.
	var prefix string
	if idx := strings.LastIndexAny(raw, " \t"); idx >= 0 {
		prefix = raw[:idx+1]
	} else {
		prefix = "/" + name + " "
	}
	m.cmdSuggest.SetArgMatches(ranked, prefix)
	m.cmdSuggest.SetWidth(m.width - 2)
}

// buildShareProfileOutput returns a multi-section Output view with
// the local user's full contact card JSON as a selectable item.
// Used by /share me and /share profile when no channel is selected.
func (m *Model) buildShareProfileOutput() commands.Result {
	pubKey := ""
	if m.secureMgr != nil {
		pubKey = m.secureMgr.OwnPublicKeyBase64()
	}
	multiaddr := ""
	if m.p2pNode != nil {
		multiaddr = m.p2pNode.Multiaddr()
	}
	slackerID := ""
	myName := ""
	myEmail := ""
	if m.cfg != nil {
		slackerID = m.cfg.SlackerID
		myName = m.cfg.MyName
		myEmail = m.cfg.MyEmail
	}
	card := friends.MyContactCard(slackerID, myName, myEmail, pubKey, multiaddr)
	pretty, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "Failed to build profile: " + err.Error(),
		}
	}
	// Also build the compact hash for a second selectable option.
	hash, _ := friends.EncodeContactCard(card)

	sections := []commands.Section{
		{
			Text:       "Your contact card. Select an item and press 'c' or Enter to copy.",
			Selectable: false,
		},
		{
			Title:      "JSON (full profile)",
			Text:       string(pretty),
			Selectable: true,
		},
	}
	if hash != "" {
		sections = append(sections, commands.Section{
			Title:      "Hash (compact)",
			Text:       hash,
			Selectable: true,
		})
	}
	return commands.Result{
		Status:    commands.StatusOK,
		Title:     "Share: profile",
		Sections:  sections,
		StatusBar: "Select an item and press 'c' to copy",
	}
}

// buildShareSetupOutput returns a multi-section Output view result
// showing both the JSON and hash forms of the workspace setup
// credentials. Each form is a selectable section so the user can
// arrow onto it, press 'c' to copy, or Enter to copy to clipboard.
func (m *Model) buildShareSetupOutput() commands.Result {
	if m.cfg == nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "No config loaded",
		}
	}
	share := setup.Config{
		ClientID:     m.cfg.ClientID,
		ClientSecret: m.cfg.ClientSecret,
		AppToken:     m.cfg.AppToken,
	}
	if share.IsEmpty() {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "No workspace credentials configured — run /setup first",
		}
	}
	hash, err := setup.Encode(share)
	if err != nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "Encode hash failed: " + err.Error(),
		}
	}
	js, err := share.ToJSON()
	if err != nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "Encode JSON failed: " + err.Error(),
		}
	}
	return commands.Result{
		Status: commands.StatusOK,
		Title:  "Share: setup",
		Sections: []commands.Section{
			{
				Text:       "Select an item below and press 'c' to copy it to the clipboard.",
				Selectable: false,
			},
			{
				Title:      "JSON format",
				Text:       js,
				Selectable: true,
			},
			{
				Title:      "Hash format (compact)",
				Text:       hash,
				Selectable: true,
			},
			{
				Text:       "User OAuth token (xoxp-) is NOT included.",
				Selectable: false,
			},
		},
		StatusBar: "No channel selected — select an item and press 'c' to copy",
	}
}

// buildShareBody constructs the chat message body for one /share
// invocation. Returns ("", err) if the target is recognised but
// the payload can't be built (e.g. friend not found, theme not
// found, setup credentials missing).
//
// All output flows through InputSendMsg so the existing send
// pipeline (friend vs slack, expandFriendMarkers, file parsing)
// runs unchanged. In particular, [FRIEND:me] and [FRIEND:<id>]
// markers are expanded by the send path, not by this function.
func (m *Model) buildShareBody(target shareTarget, args []string) (string, error) {
	switch target.name {
	case "me", "profile":
		// Send the local user's contact card marker — the
		// existing expandFriendMarkers pass in InputSendMsg
		// replaces it with the JSON or SLF2 hash based on
		// ShareMyInfoFormat.
		return "[FRIEND:me]", nil

	case "setup":
		// Export client_id / client_secret / app_token (no
		// user token) as a compact hash and wrap it in a
		// prefixed command line so the recipient sees what to
		// do with it.
		if m.cfg == nil {
			return "", fmt.Errorf("no config loaded")
		}
		share := setup.Config{
			ClientID:     m.cfg.ClientID,
			ClientSecret: m.cfg.ClientSecret,
			AppToken:     m.cfg.AppToken,
		}
		if share.IsEmpty() {
			return "", fmt.Errorf("no workspace credentials configured yet — run /setup first")
		}
		hash, err := setup.Encode(share)
		if err != nil {
			return "", fmt.Errorf("encode hash: %w", err)
		}
		return "To set up the same workspace, run:\n`slackers setup " + hash + "`", nil

	case "friend":
		// Look up the friend by the provided second arg and
		// emit a [FRIEND:<userID>] marker. expandFriendMarkers
		// in the send path resolves it to the friend's
		// contact card hash/JSON.
		if m.friendStore == nil {
			return "", fmt.Errorf("friend store not available")
		}
		needle := strings.ToLower(strings.TrimSpace(args[1]))
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
			return "", fmt.Errorf("no friend matched: %s", args[1])
		}
		// Reference the friend by UserID so expandFriendMarkers
		// finds them in the store.
		return "[FRIEND:" + match.UserID + "]", nil

	case "theme":
		// Export the named theme's JSON as a code block so the
		// recipient can paste it into their own themes dir.
		name := strings.TrimSpace(args[1])
		t, ok := theme.FindByName(name)
		if !ok {
			return "", fmt.Errorf("theme not found: %s", name)
		}
		raw, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal theme: %w", err)
		}
		return "Here's the `" + t.Name + "` theme:\n```\n" + string(raw) + "\n```", nil
	}
	return "", fmt.Errorf("unhandled share target: %s", target.name)
}

// buildSetupShareResult constructs the output for `/setup share`.
// The result is a multi-section Output view: an intro paragraph
// explaining what the output does, then the CLI import commands,
// then the in-app import commands, each rendered as its own
// selectable section with code-fenced body. The user can
// right-arrow into each section to select the code snippet and
// copy it to their clipboard with 'c'.
//
// User OAuth token is deliberately not included — only client id
// / client secret / app token are exported, matching the CLI
// `slackers setup share` behaviour.
func (m *Model) buildSetupShareResult(format string) commands.Result {
	if m.cfg == nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "No config loaded",
		}
	}
	share := setup.Config{
		ClientID:     m.cfg.ClientID,
		ClientSecret: m.cfg.ClientSecret,
		AppToken:     m.cfg.AppToken,
	}
	if share.IsEmpty() {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "No workspace credentials configured yet — run /setup first",
		}
	}
	hash, err := setup.Encode(share)
	if err != nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "Encode hash failed: " + err.Error(),
		}
	}
	js, err := share.ToJSON()
	if err != nil {
		return commands.Result{
			Status:    commands.StatusError,
			StatusBar: "Encode JSON failed: " + err.Error(),
		}
	}

	sections := []commands.Section{
		{
			Text: "Share these commands with teammates to set up slackers with your current workspace. " +
				"Either format works — the hash is shorter and obfuscated (but still fully decodable, not encrypted).",
			Selectable: false,
		},
		{
			Title:      "CLI — JSON form",
			Text:       "```\nslackers setup '" + js + "'\n```",
			Selectable: true,
		},
		{
			Title:      "CLI — hash form",
			Text:       "```\nslackers setup " + hash + "\n```",
			Selectable: true,
		},
		{
			Text:       "Or from inside a running slackers instance:",
			Selectable: false,
		},
		{
			Title:      "In-app — JSON form",
			Text:       "```\n/setup " + js + "\n```",
			Selectable: true,
		},
		{
			Title:      "In-app — hash form",
			Text:       "```\n/setup " + hash + "\n```",
			Selectable: true,
		},
		{
			Text: "Your user OAuth token (xoxp-) is NOT included in this output. " +
				"Teammates running the import will still need to authorize with their own Slack account via " +
				"`slackers login`.",
			Selectable: false,
		},
	}
	// Drop the format hint onto the status bar too so users
	// running /setup share json see their choice acknowledged.
	statusBar := "Setup share: both formats shown, " + format + " is the default"
	return commands.Result{
		Status:    commands.StatusOK,
		Title:     "Setup Share",
		Sections:  sections,
		StatusBar: statusBar,
	}
}

// importSetupConfig is the shared import entry point for both the
// internal `/setup <arg>` command and any future TUI-invoked CLI
// path. It decides whether the current config already has enough
// credentials to warrant a confirmation prompt — if so, it stages
// the new values in m.pendingSetupImport and surfaces a y/Enter
// confirmation via the status bar. Otherwise it applies directly.
//
// Either way the apply path is applySetupConfig, which is the
// single place tokens actually get written, debounced-saved, and
// reported back to the user.
func (m *Model) importSetupConfig(cfg setup.Config) tea.Cmd {
	if cfg.IsEmpty() {
		m.warning = "Setup payload is empty — nothing to import"
		return nil
	}
	// Detect existing credentials that would be overwritten. We
	// compare each incoming non-empty field against the current
	// cfg.* value; if any is already set, we require confirmation.
	hasExisting := false
	if m.cfg != nil {
		if cfg.ClientID != "" && m.cfg.ClientID != "" && cfg.ClientID != m.cfg.ClientID {
			hasExisting = true
		}
		if cfg.ClientSecret != "" && m.cfg.ClientSecret != "" && cfg.ClientSecret != m.cfg.ClientSecret {
			hasExisting = true
		}
		if cfg.AppToken != "" && m.cfg.AppToken != "" && cfg.AppToken != m.cfg.AppToken {
			hasExisting = true
		}
		if cfg.UserToken != "" && m.cfg.UserToken != "" && cfg.UserToken != m.cfg.UserToken {
			hasExisting = true
		}
	}
	if hasExisting {
		staged := cfg
		m.pendingSetupImport = &staged
		m.warning = "Replace existing Slack credentials? y=yes, any other key=cancel"
		return nil
	}
	return m.applySetupConfig(cfg)
}

// applySetupConfig writes non-empty incoming fields into m.cfg and
// persists. It does NOT attempt to bring slackSvc / socketSvc
// online — the user still needs to restart (or re-run setup) for
// Slack services to be instantiated with the new tokens. A status
// bar line confirms which fields were updated and prompts for a
// restart when needed.
func (m *Model) applySetupConfig(cfg setup.Config) tea.Cmd {
	if m.cfg == nil {
		m.warning = "No config loaded — cannot apply setup"
		return nil
	}
	changed := []string{}
	if cfg.ClientID != "" && m.cfg.ClientID != cfg.ClientID {
		m.cfg.ClientID = cfg.ClientID
		changed = append(changed, "client id")
	}
	if cfg.ClientSecret != "" && m.cfg.ClientSecret != cfg.ClientSecret {
		m.cfg.ClientSecret = cfg.ClientSecret
		changed = append(changed, "client secret")
	}
	if cfg.AppToken != "" && m.cfg.AppToken != cfg.AppToken {
		m.cfg.AppToken = cfg.AppToken
		changed = append(changed, "app token")
	}
	if cfg.UserToken != "" && m.cfg.UserToken != cfg.UserToken {
		m.cfg.UserToken = cfg.UserToken
		changed = append(changed, "user token")
	}
	if len(changed) == 0 {
		m.warning = "Setup: all fields already match current config"
		return nil
	}
	if err := config.Save(m.cfg); err != nil {
		m.warning = "Setup: saved in memory but failed to persist: " + err.Error()
		return nil
	}
	m.warning = "Setup: updated " + strings.Join(changed, ", ") + " — restart to activate Slack services"
	return nil
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
