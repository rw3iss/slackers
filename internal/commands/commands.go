// Package commands implements slackers' slash-command framework.
//
// The package is intentionally Model-agnostic — it only knows about
// the abstract shapes (commands, args, results, registry, lookup
// trie). The TUI layer registers concrete command handlers as
// closures that capture *Model so they can call the existing app
// services (friend store, slack service, P2P node, theme manager,
// etc.) without this package needing to know about any of them.
//
// The lookup is a hybrid:
//
//   - A character trie indexes every command name. A user typing
//     "/the" walks the trie nodes t→h→e and the registry returns
//     every command underneath that subtree (so "/theme",
//     "/themes", "/them", etc. all surface). This is O(prefix len)
//     lookup, independent of how many commands are registered, and
//     stays fast as the dictionary grows to include emotes and
//     custom commands.
//
//   - A fuzzy matcher rescans the prefix-filtered candidates with
//     a subsequence-with-bonuses score so the dropdown can
//     surface "addfri" → /add-friend and similar typo-tolerant
//     completions. The fuzzy pass only ever runs against the trie
//     prefix subset, never the full registry.
//
// See trie.go and fuzzy.go for the lookup implementations.
package commands

// Kind classifies what category a registered command belongs to.
// Commands and emotes share the same lookup machinery but render
// differently in the suggestion popup and the Command List view.
type Kind int

const (
	KindCommand Kind = iota // built-in or custom command
	KindEmote               // text emote (/laugh, /nod, ...)
)

// String returns a short label for the Kind, used in the Command
// List view's right-aligned tag.
func (k Kind) String() string {
	switch k {
	case KindCommand:
		return "command"
	case KindEmote:
		return "emote"
	default:
		return "?"
	}
}

// ArgKind hints to the suggestion popup what kind of value an
// argument expects, so the input bar can offer fuzzy completions
// against the right pool (channel names, friend ids, help topics).
// The runner doesn't enforce these — they're purely a hint for
// the UI completion layer.
type ArgKind int

const (
	ArgString     ArgKind = iota // free text
	ArgFriendID                  // user id, slacker id, or display name
	ArgChannelID                 // channel name or id
	ArgHelpTopic                 // one of the embedded help filenames
	ArgThemeName                 // installed theme name
	ArgFriendCard                // [FRIEND:<blob>] / json / hash
	ArgFile                      // local filesystem path
)

// ArgSpec describes one positional argument of a command.
type ArgSpec struct {
	Name     string
	Kind     ArgKind
	Optional bool
	Help     string // appears in usage strings and the Command List view
}

// Command is a single registered slash command (or emote).
//
// Run is invoked by the host with a Context populated with the
// raw arg slice the user typed after the command name. Returning a
// Result lets the runner decide whether to surface the output in
// the status bar, the Output view, or as a follow-up tea.Cmd.
type Command struct {
	Name        string   // canonical name without leading slash, e.g. "add-friend"
	Aliases     []string // additional names that resolve to the same command
	Kind        Kind
	Description string // one-liner shown in the suggestion dropdown
	Usage       string // e.g. "/add-friend <hash|json|FRIEND:marker>"
	Args        []ArgSpec
	Run         RunFunc
}

// FullName returns the canonical "/name" form (with leading slash)
// used in suggestion rendering.
func (c Command) FullName() string {
	return "/" + c.Name
}

// Context carries the raw args for an invocation. RunFunc closures
// usually pull additional state from the captured *Model.
type Context struct {
	// Args is the slice of whitespace-separated tokens following
	// the command name. Tokens enclosed in double quotes are
	// preserved as a single arg with the quotes stripped.
	Args []string

	// Raw is the entire user input minus the leading "/cmd "
	// prefix. Useful for commands that want a single free-form
	// value (e.g. /add-friend takes a JSON / hash blob that may
	// itself contain spaces and quotes).
	Raw string
}

// ResultStatus classifies a command's outcome. The host inspects
// this to decide where to render the result.
type ResultStatus int

const (
	StatusOK    ResultStatus = iota // success — show in status bar / output view
	StatusError                     // failure — show as warning + (optionally) output view
	StatusInfo                      // informational — like OK but visually muted
)

// Result is the value a command returns to its caller.
//
// All fields are optional; an empty Result is treated as a silent
// success. Title + Body, when non-empty, open the Output view
// with that content. StatusBar populates the bottom-of-screen
// warning slot. Cmd is a follow-up tea.Cmd that the host should
// run after handling the rest of the result (e.g. dispatching a
// FriendsConfigOpenMsg or starting a download).
//
// FocusOutput is an opt-in. The default UX after running a
// command from the input bar is to leave focus on the input
// (so the user can chain more commands without an extra Tab).
// Commands whose output is *interactive* — for example a list
// the user is meant to navigate with arrow keys, or a dynamic
// view that responds to keystrokes — can set FocusOutput=true
// to ask the host to auto-Tab focus to the output pane on
// activation. Static text commands (/help, /friends, /channels)
// should leave it false.
type Result struct {
	Status      ResultStatus
	Title       string
	Body        string
	StatusBar   string
	FocusOutput bool
	// Cmd is a tea.Cmd-shaped follow-up represented here as an
	// `any` so this package stays independent of bubbletea. The
	// host casts it to tea.Cmd before scheduling.
	Cmd any
}

// RunFunc is the signature every registered command implements.
type RunFunc func(ctx *Context) Result
