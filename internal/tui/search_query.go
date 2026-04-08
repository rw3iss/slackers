package tui

// Search-query parser shared by the message search overlay and the
// local friend-history filter. Supports the following grammar:
//
//	query     := (phrase | token | space)*
//	phrase    := '"' any-non-quote* '"'    // matches as exact substring
//	token     := non-space non-quote+      // matches anywhere in text
//
// Whitespace between tokens and phrases is ignored. Unclosed trailing
// quotes are tolerated: the content after the dangling quote is still
// emitted as additional tokens so a typo like `hello "world` still
// returns sensible results instead of an empty match set.
//
// Quoted phrases go to `phrases`. Unquoted words go to `tokens`. Every
// entry in both slices is lower-cased for fast comparison during match.
//
// Empty / whitespace-only queries produce empty slices. Consumers
// should treat that as "no query" and return no results.
//
// The parser is intentionally tiny: no escape sequences, no regex,
// no field: modifiers. Slack's native query syntax (in:, from:,
// before:, etc.) is still forwarded verbatim to Slack's search API
// by the caller — this parser is only used to drive the local
// friend-history matcher, which has no API of its own.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// parseSearchQuery splits a raw query into its exact-phrase and
// loose-token components. See the file header for the grammar.
// Both return values are lower-cased; the caller should likewise
// lower-case the haystack before testing with matchesQuery.
func parseSearchQuery(q string) (phrases, tokens []string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}

	var (
		inQuote bool
		current strings.Builder
	)

	flushToken := func() {
		s := strings.TrimSpace(current.String())
		current.Reset()
		if s == "" {
			return
		}
		// Loose tokens split further on whitespace so a query
		// like `foo bar "baz qux"` yields tokens [foo, bar] and
		// phrases [baz qux] rather than a single "foo bar" token.
		for _, f := range strings.FieldsFunc(s, unicode.IsSpace) {
			tokens = append(tokens, strings.ToLower(f))
		}
	}
	flushPhrase := func() {
		s := current.String()
		current.Reset()
		// Keep internal whitespace but trim the edges so
		// `"  hello world "` normalises to `hello world`.
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		phrases = append(phrases, strings.ToLower(s))
	}

	for _, r := range q {
		if r == '"' {
			if inQuote {
				flushPhrase()
				inQuote = false
			} else {
				// Flush the loose tokens we'd been building up
				// before switching into phrase mode.
				flushToken()
				inQuote = true
			}
			continue
		}
		current.WriteRune(r)
	}
	// Unclosed phrase → emit whatever we accumulated as tokens so
	// the user still gets useful results.
	if inQuote {
		flushToken()
	} else {
		flushToken()
	}
	return phrases, tokens
}

// SearchPreview is a windowed excerpt of a message body, centred on
// the earliest query match. The window is suitable for display in a
// search-result list; its visible width is approximately the `maxLen`
// argument passed to buildSearchPreview.
//
// MatchStart and MatchLen are RUNE offsets into Text describing the
// anchor match. They let the renderer apply a highlight style to the
// matched substring without re-running the matcher. When no match is
// found MatchStart is -1 and MatchLen is 0.
type SearchPreview struct {
	Text       string
	MatchStart int // rune offset into Text
	MatchLen   int // length of the match in runes
}

// buildSearchPreview returns a trimmed excerpt of `text` centred on
// the first query match with ellipses where content was clipped. The
// returned preview is built in rune space so multi-byte characters
// (emoji, CJK) are never split.
//
// Algorithm:
//  1. Collapse newlines/whitespace runs in the source text.
//  2. If the whole text fits inside maxLen, return it as-is.
//  3. Parse the query to phrases + tokens, find the earliest match.
//  4. Reserve ~40 runes of left context before the match; anything
//     past the left context becomes right context. If the right tail
//     is shorter than the budget (match near end of text), pull the
//     start cursor further left to keep the window full. Similarly,
//     a match near the start keeps the window at the beginning.
//  5. Prepend "…" when content was trimmed from the left and append
//     "…" when trimmed from the right.
//
// When the query produces no matches in the body (defensive edge
// case — shouldn't happen since callers only pass successful hits)
// the function falls back to returning the head of the text followed
// by an ellipsis, so the caller always gets something reasonable.
func buildSearchPreview(text, query string, maxLen int) SearchPreview {
	if maxLen < 20 {
		maxLen = 20
	}
	// Collapse whitespace so long-form messages don't waste the
	// preview budget on indentation or newline runs.
	oneLine := strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	runes := []rune(oneLine)

	if len(runes) <= maxLen {
		phrases, tokens := parseSearchQuery(query)
		byteIdx, byteLen := findFirstMatchSpan(oneLine, phrases, tokens)
		if byteIdx < 0 {
			return SearchPreview{Text: oneLine, MatchStart: -1}
		}
		return SearchPreview{
			Text:       oneLine,
			MatchStart: utf8.RuneCountInString(oneLine[:byteIdx]),
			MatchLen:   utf8.RuneCountInString(oneLine[byteIdx : byteIdx+byteLen]),
		}
	}

	phrases, tokens := parseSearchQuery(query)
	byteIdx, byteLen := findFirstMatchSpan(oneLine, phrases, tokens)
	if byteIdx < 0 {
		// No query match in the body — truncate from the head.
		head := string(runes[:maxLen-1]) + "…"
		return SearchPreview{Text: head, MatchStart: -1}
	}
	runeIdx := utf8.RuneCountInString(oneLine[:byteIdx])
	matchRunes := utf8.RuneCountInString(oneLine[byteIdx : byteIdx+byteLen])

	// Target ~40 runes of left context so the match lands comfortably
	// in the middle of the visible window rather than butting up
	// against the leading ellipsis.
	const leftPad = 40
	start := runeIdx - leftPad
	end := start + maxLen

	// Clamp right: if the tail is shorter than our budget, pull the
	// window leftwards so we don't waste space on an ellipsis-only
	// right edge.
	if end > len(runes) {
		end = len(runes)
		start = end - maxLen
	}
	// Clamp left: if the match is near the start, keep start at 0 and
	// push the window right. Guarantees the whole head of the message
	// is visible when that's the most useful framing.
	if start < 0 {
		start = 0
		end = maxLen
		if end > len(runes) {
			end = len(runes)
		}
	}

	out := string(runes[start:end])
	// Shift match position into the window's rune space.
	matchInWindow := runeIdx - start
	leadingEllipsis := start > 0
	trailingEllipsis := end < len(runes)
	if leadingEllipsis {
		out = "…" + out
		matchInWindow++ // account for the prepended ellipsis rune
	}
	if trailingEllipsis {
		out = out + "…"
	}
	return SearchPreview{
		Text:       out,
		MatchStart: matchInWindow,
		MatchLen:   matchRunes,
	}
}

// findFirstMatchSpan is like findFirstMatchIndex but also returns the
// byte length of the match. Phrases preserve their full length;
// tokens report the token's length (not the whole word it was found
// within).
func findFirstMatchSpan(text string, phrases, tokens []string) (bytePos, byteLen int) {
	lower := strings.ToLower(text)
	bestPos := -1
	bestLen := 0
	for _, p := range phrases {
		if p == "" {
			continue
		}
		if i := strings.Index(lower, p); i >= 0 {
			if bestPos < 0 || i < bestPos {
				bestPos = i
				bestLen = len(p)
			}
		}
	}
	for _, t := range tokens {
		if t == "" {
			continue
		}
		if i := strings.Index(lower, t); i >= 0 {
			if bestPos < 0 || i < bestPos {
				bestPos = i
				bestLen = len(t)
			}
		}
	}
	return bestPos, bestLen
}

// matchesQuery reports whether `text` satisfies every phrase and
// every token from a parsed query. Matching is case-insensitive
// substring — the caller passes already-lowered phrases/tokens
// (from parseSearchQuery), and this helper lowers the text once
// per call. Empty phrases + empty tokens returns false so callers
// that forget to short-circuit an empty query don't accidentally
// match every message.
func matchesQuery(text string, phrases, tokens []string) bool {
	if len(phrases) == 0 && len(tokens) == 0 {
		return false
	}
	hay := strings.ToLower(text)
	for _, p := range phrases {
		if !strings.Contains(hay, p) {
			return false
		}
	}
	for _, t := range tokens {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}
