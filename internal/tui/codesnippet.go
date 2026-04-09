package tui

// Shared helpers for detecting inline backtick code spans and
// triple-backtick fenced code blocks inside rendered text. Both
// the messages pane (for in-chat code-snippet sub-selection in
// select mode) and the Output view (for /setup share and
// similar structured command output) consume these.
//
// The regexes are intentionally conservative:
//
//   - codeFencePat requires the opening and closing triple
//     backticks to each sit on their own line, matching standard
//     markdown. Multiline content is captured verbatim.
//   - inlineCodePat matches a single-backtick span on a single
//     line with no embedded newline or backtick, same as Slack
//     mrkdwn / GFM inline code.
//
// parseCodeSnippets returns the raw payload of every matched span
// in document order: fenced blocks first, then inline spans. The
// raw payload is what gets copied to the clipboard when the user
// activates `c` on a selected snippet.

import (
	"regexp"
	"strings"
)

// codeFencePat matches a triple-backtick fenced block. Capture 1
// is the body between the fences, stripped of leading/trailing
// whitespace by the caller when needed.
var codeFencePat = regexp.MustCompile("(?s)```\\s*\\n(.*?)\\n\\s*```")

// inlineCodePat matches a single-backtick inline code span. The
// body may not contain newlines or other backticks.
var inlineCodePat = regexp.MustCompile("`([^`\\n]+)`")

// parseCodeSnippets scans the given text for both fenced blocks
// and inline spans and returns the raw payloads in document
// order. Fenced blocks are trimmed; inline spans are returned
// verbatim (minus the wrapping backticks).
//
// Fenced regions are carved out before the inline scan runs, so
// backticks inside a fenced block aren't double-counted as
// inline spans.
func parseCodeSnippets(text string) []string {
	var out []string
	if !strings.Contains(text, "`") {
		return nil
	}
	// Fenced first.
	for _, m := range codeFencePat.FindAllStringSubmatch(text, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	// Strip fenced regions before the inline pass so their
	// inner backticks don't leak into the inline match.
	stripped := codeFencePat.ReplaceAllString(text, "\x00FENCE\x00")
	for _, m := range inlineCodePat.FindAllStringSubmatch(stripped, -1) {
		out = append(out, m[1])
	}
	return out
}
