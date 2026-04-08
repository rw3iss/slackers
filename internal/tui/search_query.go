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
