package format

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reCodeBlock  = regexp.MustCompile("(?s)```.*?```")
	reInlineCode = regexp.MustCompile("`[^`]+`")

	reBold          = regexp.MustCompile(`\*([^*]+)\*`)
	reItalic        = regexp.MustCompile(`\b_([^_]+)_\b`)
	reStrikethrough = regexp.MustCompile(`~([^~]+)~`)

	// reUserMention matches both Slack-style mentions (<@U12345>) and
	// friend-style mentions (<@slacker:...>). The friend SlackerID
	// segment is permissive enough to cover the common formats:
	// machine-generated 32-char hex, custom human-readable handles
	// like "rw-pc", emails (allowing "." and "@" inside the id is
	// not currently supported because that breaks the unambiguous
	// `>` terminator), and underscored handles.
	reUserMention    = regexp.MustCompile(`<@(U[A-Z0-9]+|slacker:[A-Za-z0-9._\-]+)>`)
	reChannelMention = regexp.MustCompile(`<#C[A-Z0-9]+\|([^>]+)>`)
	reLabeledLink    = regexp.MustCompile(`<([^>|]+)\|([^>]+)>`)
	reBareLink       = regexp.MustCompile(`<([^>|]+)>`)

	reBroadcast = regexp.MustCompile(`<!(\w+)>`)
)

// Mention captures one resolved @mention extracted from a message
// during formatting. The renderer uses it to swap the inline marker
// emitted by FormatMessageWithMentions into a styled, clickable pill.
type Mention struct {
	// ID is the canonical user identifier — a Slack user id like
	// "U12345" or a friend id like "slacker:<SlackerID>".
	ID string
	// Name is the resolved display name (without the leading "@").
	Name string
}

// FormatMessage converts Slack mrkdwn markup to plain terminal text.
// The users map provides user ID to display name lookups for
// @mentions, including both Slack ids ("U...") and friend ids
// ("slacker:..."). Equivalent to FormatMessageWithMentions but
// discards the mention sidecar; kept for callers that don't need
// the rich form.
func FormatMessage(text string, users map[string]string) string {
	out, _ := FormatMessageWithMentions(text, users)
	return out
}

// FormatMessageWithMentions formats `text` and returns both the
// rendered string and a sidecar slice describing each @mention that
// appeared. Each mention is replaced inline with a short marker:
//
//	[MENTION:#m-N]
//
// where N is the index into the returned []Mention. The renderer is
// responsible for swapping the marker for a styled pill at render
// time (see messages.rewriteMentionPills) — this keeps mention
// width predictable for word-wrap and lets the renderer record per-
// line click hit positions.
func FormatMessageWithMentions(text string, users map[string]string) (string, []Mention) {
	var codeBlocks []string
	var inlineCodes []string

	text = reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		idx := len(codeBlocks)
		codeBlocks = append(codeBlocks, match)
		return placeholder("CB", idx)
	})

	text = reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		idx := len(inlineCodes)
		inlineCodes = append(inlineCodes, match)
		return placeholder("IC", idx)
	})

	text = reBold.ReplaceAllString(text, "$1")
	text = reItalic.ReplaceAllString(text, "$1")
	text = reStrikethrough.ReplaceAllString(text, "$1")

	var mentions []Mention
	text = reUserMention.ReplaceAllStringFunc(text, func(match string) string {
		parts := reUserMention.FindStringSubmatch(match)
		id := parts[1]
		name, ok := users[id]
		if !ok || name == "" {
			name = "unknown"
		}
		idx := len(mentions)
		mentions = append(mentions, Mention{ID: id, Name: name})
		return fmt.Sprintf("[MENTION:#m-%d]", idx)
	})

	text = reChannelMention.ReplaceAllString(text, "#$1")
	text = reLabeledLink.ReplaceAllString(text, "$2")
	text = reBareLink.ReplaceAllString(text, "$1")

	text = reBroadcast.ReplaceAllStringFunc(text, func(match string) string {
		parts := reBroadcast.FindStringSubmatch(match)
		switch parts[1] {
		case "channel", "here", "everyone":
			return "@" + parts[1]
		default:
			return match
		}
	})

	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")

	// Replace :emoji: shortcodes with Unicode emoji.
	text = ReplaceEmoji(text)

	for i := len(inlineCodes) - 1; i >= 0; i-- {
		text = strings.Replace(text, placeholder("IC", i), inlineCodes[i], 1)
	}
	for i := len(codeBlocks) - 1; i >= 0; i-- {
		text = strings.Replace(text, placeholder("CB", i), codeBlocks[i], 1)
	}

	return text, mentions
}

// MentionMarkerRE matches the inline marker emitted by
// FormatMessageWithMentions. Exposed so the renderer can scan each
// wrapped line for markers and swap them for styled pills.
var MentionMarkerRE = regexp.MustCompile(`\[MENTION:#m-(\d+)\]`)

func placeholder(kind string, idx int) string {
	return "\x00" + kind + string(rune(idx)) + "\x00"
}
