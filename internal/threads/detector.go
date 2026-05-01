package threads

import (
	"strings"

	"github.com/rw3iss/slackers/internal/types"
)

// Detect classifies a parent message against the four detection
// rules. The function is pure — no I/O — so callers can use it from
// either the forward-tracking hook (where new messages arrive with
// their full Replies populated) or the backfill scanner (where
// replies may be omitted to save API calls).
//
// Rules:
//   - Rule 1 (mentioned)      — the parent text contains <@me>.
//   - Rule 2 (author)         — the parent's UserID equals me.
//   - Rule 3 (replied)        — any reply's UserID equals me.
//   - Rule 4 (reply-mention)  — any reply's text contains <@me>.
//
// `forward` toggles rule 4. The backfill scanner sets forward=false
// to skip the only rule that would force a per-thread replies fetch
// (it relies on the cheap ReplyUsers signal from conversations.history
// to evaluate rule 3 instead of walking msg.Replies).
//
// `me` is the local user identity in whatever form the surrounding
// transport uses for UserID and mention syntax — for Slack that's
// the U12345 user ID; for friend chats that's "slacker:<SlackerID>".
// Friend messages have no formal @-mention syntax, so rules 1 and 4
// will not trip for SourceFriend regardless of input.
//
// Returns (reason, true) on a positive match; (zero-value, false)
// when no rule applies. The reasons are evaluated in priority order
// — author > mentioned > replied > reply-mention — so the first
// rule to match wins. The store doesn't compare reasons, but tests
// rely on the priority for stability.
func Detect(msg types.Message, me string, forward bool) (ThreadReason, bool) {
	if me == "" {
		return "", false
	}

	// Rule 2 — author. Cheapest and most specific; check first.
	if msg.UserID == me {
		return ReasonAuthor, true
	}

	// Rule 1 — mention in parent text. Slack-style <@USERID> only.
	mentionToken := "<@" + me + ">"
	if strings.Contains(msg.Text, mentionToken) {
		return ReasonMentioned, true
	}

	// Rules 3 / 4 — walk replies if present.
	for _, r := range msg.Replies {
		if r.UserID == me {
			return ReasonReplied, true
		}
		if forward && strings.Contains(r.Text, mentionToken) {
			return ReasonReplyMention, true
		}
	}

	return "", false
}

// HasReplies returns true when the message has any threaded reply.
// Cheap helper used by the forward-tracking caller to skip Detect
// entirely when the message can't possibly be a thread (no replies
// AND not authored / mentioned). The caller still needs to call
// Detect for rules 1 and 2; HasReplies is for the rules-3-and-4
// short-circuit.
func HasReplies(msg types.Message) bool {
	return len(msg.Replies) > 0
}
