// Package threads tracks Slack-style thread conversations the local
// user is involved in across both Slack channels and friend (P2P)
// chats. It is shaped as an SDK: the TUI never reaches into the
// underlying transports for thread purposes; it only calls
// ThreadStore methods and consumes ThreadsChangedMsg notifications.
//
// The package has four pieces:
//
//   - Detector — pure rule evaluation: given a parent message and
//     the local user identity, decides whether the message represents
//     a thread the user is part of.
//   - ThreadStore — persistent set of active thread snapshots
//     (~/.config/slackers/threads.json), with debounced saves and a
//     change-notification fan-out.
//   - Scheduler — auto-clear timer that removes inactive threads
//     older than a user-configured cutoff.
//   - BackfillScanner — paged historical scan of older threads
//     across multiple Sources (Plan B; constructor is exported but
//     the implementation is filled in alongside the global Threads
//     overlay).
//
// The Source interface abstracts away "where do candidate parent
// messages come from" so Slack and friend (P2P) chats both feed the
// same detection pipeline.
package threads

import (
	"os"
	"path/filepath"
	"time"
)

// Source identifies which transport a thread came from. Determines
// how the activation flow re-opens the parent message later.
type Source string

const (
	SourceSlack  Source = "slack"
	SourceFriend Source = "friend"
)

// ThreadReason explains why a parent message was classified as a
// thread the local user is involved in. Stored on the snapshot for
// debugging / sort tie-breaks; not currently rendered.
type ThreadReason string

const (
	// ReasonAuthor — the local user wrote the parent message.
	ReasonAuthor ThreadReason = "author"

	// ReasonMentioned — the local user is @mentioned in the parent's
	// text. Slack mention syntax: <@U12345>. Friend chats have no
	// formal mention syntax, so this rule never trips for SourceFriend.
	ReasonMentioned ThreadReason = "mentioned"

	// ReasonReplied — the local user replied to the parent (their
	// user-id appears in the reply list).
	ReasonReplied ThreadReason = "replied"

	// ReasonReplyMention — someone @mentioned the local user in a
	// reply. Forward-tracking only; the backfill scanner skips this
	// rule because evaluating it for older parents would require an
	// additional conversations.replies API call per parent.
	ReasonReplyMention ThreadReason = "reply_mention"
)

// ScanScope controls how aggressively the BackfillScanner walks
// channel history when populating the global Threads view's "older"
// section. Stored as a config value; the scanner reads it per-page.
type ScanScope string

const (
	// ScopeDisabled — never scan history. Only forward-tracked
	// threads (auto-detected from new activity) appear.
	ScopeDisabled ScanScope = "disabled"

	// ScopeLocal — scan only the channel history already in memory
	// (fetched into the running session). Zero extra API calls.
	// Default.
	ScopeLocal ScanScope = "local"

	// ScopeSubscribed — walk every channel in the user's sidebar.
	// Moderate API cost, paged.
	ScopeSubscribed ScanScope = "subscribed"

	// ScopeAllPublic — walk every public channel in the workspace.
	// High cost on big workspaces; user can cancel mid-scan.
	ScopeAllPublic ScanScope = "all_public"
)

// ThreadRef uniquely identifies a thread. Source disambiguates Slack
// channel IDs from friend channel IDs (which are arbitrary strings
// derived from the friend's SlackerID).
type ThreadRef struct {
	Source    Source `json:"source"`
	ChannelID string `json:"channel_id"`
	ParentTS  string `json:"parent_ts"` // Slack ts or friend MessageID
}

// ThreadSnapshot is a denormalized view of a tracked thread. The
// store keeps these in memory and persists them so the sidebar can
// render without any per-frame transport calls.
type ThreadSnapshot struct {
	Ref              ThreadRef    `json:"ref"`
	ChannelName      string       `json:"channel_name"`
	ParentText       string       `json:"parent_text"` // truncated preview
	ParentAuthorID   string       `json:"parent_author_id,omitempty"`
	ParentAuthorName string       `json:"parent_author_name,omitempty"`
	Participants     []string     `json:"participants,omitempty"` // names, dedup'd
	LastActivityTS   string       `json:"last_activity_ts,omitempty"`
	Reason           ThreadReason `json:"reason"`
	AddedAt          time.Time    `json:"added_at"`
}

// ThreadsChangedMsg is dispatched on the Bubbletea channel whenever
// the active thread set mutates (Add, Remove, Touch, ClearOlderThan).
// Carries the new active set so the UI can render without re-querying.
type ThreadsChangedMsg struct {
	Active []ThreadSnapshot
}

// OpenThreadMsg is the activation request — produced by the sidebar
// click, the global view's Enter, the right-click "Go to Channel"
// option, or the /threads slash command. The root model handles it
// by switching to the parent's channel and (optionally) auto-opening
// the existing reply-detail view.
type OpenThreadMsg struct {
	Ref           ThreadRef
	OpenReplyView bool // true = also auto-enter the reply-detail view
}

// OpenThreadsViewMsg requests opening the global Threads overlay
// (Plan B). Plan A leaves the overlay unimplemented; this type is
// declared in advance so the routing wiring can land alongside the
// shortcut and slash-command without a follow-up rename later.
type OpenThreadsViewMsg struct{}

// DefaultPath returns the standard threads file location:
// $XDG_CONFIG_HOME/slackers/threads.json.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "threads.json")
}

// truncateForPreview shortens a parent message to a fixed-width
// preview, replacing any newline characters with single spaces so
// the snapshot stays renderable in a single sidebar row.
func truncateForPreview(text string, maxRunes int) string {
	if text == "" {
		return ""
	}
	out := make([]rune, 0, maxRunes+1)
	count := 0
	for _, r := range text {
		if r == '\n' || r == '\r' {
			r = ' '
		}
		out = append(out, r)
		count++
		if count >= maxRunes {
			out = append(out, '…')
			break
		}
	}
	return string(out)
}
