package format

import (
	"regexp"
	"strings"
)

var (
	reCodeBlock  = regexp.MustCompile("(?s)```.*?```")
	reInlineCode = regexp.MustCompile("`[^`]+`")

	reBold          = regexp.MustCompile(`\*([^*]+)\*`)
	reItalic        = regexp.MustCompile(`\b_([^_]+)_\b`)
	reStrikethrough = regexp.MustCompile(`~([^~]+)~`)

	reUserMention    = regexp.MustCompile(`<@(U[A-Z0-9]+)>`)
	reChannelMention = regexp.MustCompile(`<#C[A-Z0-9]+\|([^>]+)>`)
	reLabeledLink    = regexp.MustCompile(`<([^>|]+)\|([^>]+)>`)
	reBareLink       = regexp.MustCompile(`<([^>|]+)>`)

	reBroadcast = regexp.MustCompile(`<!(\w+)>`)
)

// FormatMessage converts Slack mrkdwn markup to plain terminal text.
// The users map provides user ID to display name lookups for @mentions.
func FormatMessage(text string, users map[string]string) string {
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

	text = reUserMention.ReplaceAllStringFunc(text, func(match string) string {
		parts := reUserMention.FindStringSubmatch(match)
		if name, ok := users[parts[1]]; ok {
			return "@" + name
		}
		return "@unknown"
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

	for i := len(inlineCodes) - 1; i >= 0; i-- {
		text = strings.Replace(text, placeholder("IC", i), inlineCodes[i], 1)
	}
	for i := len(codeBlocks) - 1; i >= 0; i-- {
		text = strings.Replace(text, placeholder("CB", i), codeBlocks[i], 1)
	}

	return text
}

func placeholder(kind string, idx int) string {
	return "\x00" + kind + string(rune(idx)) + "\x00"
}
