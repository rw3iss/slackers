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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// codeFencePat matches a triple-backtick fenced block. Capture 1
// is the body between the fences, stripped of leading/trailing
// whitespace by the caller when needed.
var codeFencePat = regexp.MustCompile("(?s)```\\s*\\n(.*?)\\n\\s*```")

// inlineCodePat matches a single-backtick inline code span. The
// body may not contain newlines or other backticks.
var inlineCodePat = regexp.MustCompile("`([^`\\n]+)`")

// styleCodeSpansPostWrap applies CodeSnippetStyle to backtick-delimited
// code spans in the already-wrapped text lines. Unlike the per-line
// rewriteCodeSnippets, this processes all lines together so it can track
// backtick open/close state across line boundaries — handling the case
// where word-wrap splits a long `code span` across multiple lines.
//
// It also records codeSnippetHits for select-mode navigation.
func (m *MessageViewModel) styleCodeSpansPostWrap(textLines []string) []string {
	// Quick check: any backticks at all?
	hasTick := false
	for _, tl := range textLines {
		if strings.Contains(tl, "`") {
			hasTick = true
			break
		}
	}
	if !hasTick {
		return textLines
	}

	// Join all lines, process as one string, then split back.
	// This lets us handle backtick spans that cross line boundaries.
	joined := strings.Join(textLines, "\n")

	// We'll walk the string character by character, tracking backtick
	// state. This handles ```, `, and their interactions properly.
	result := m.applyCodeStyles(joined)

	return strings.Split(result, "\n")
}

// codeSnippetSelSGR returns the ANSI SGR sequences for a SELECTED code
// snippet (bold + snippet fg + subtle selection background). Uses raw
// ANSI to avoid lipgloss \x1b[0m resets.
func codeSnippetSelSGR() (on, off string) {
	// Use the selection foreground color (ColorSelection) + bold + underline
	// + selection background so it's clearly distinct from normal snippets.
	fg := string(ColorSelection)
	if fg == "" {
		fg = string(ColorAccent)
	}
	bg := string(ColorSubtleBgAlt)
	on = "\x1b[1;4m" + fgSGR(fg) // bold + underline + selection fg
	if bg != "" {
		on += bgSGR(lipgloss.Color(bg))
	}
	// "off" restores: unbold + no underline + message text fg + pane bg.
	off = "\x1b[22;24m" + fgSGR(string(ColorMessageText))
	paneBg := bgSGR(ColorBackgroundBg)
	if paneBg != "" {
		off += paneBg
	} else if bg != "" {
		off += "\x1b[49m"
	}
	return
}

// codeSnippetSGR returns the ANSI SGR sequences to turn code snippet
// styling on and off. Uses the theme's codeSnippet background if set,
// otherwise auto-computes a subtle shift from the pane background
// (2 shades lighter for dark themes, 2 shades darker for light).
// Avoids \x1b[0m so the pane background is never cleared mid-line.
func codeSnippetSGR() (on, off string) {
	fg := string(ColorCodeSnippet)
	if fg == "" {
		fg = string(ColorAccent)
	}
	// Use the theme's codeSnippet background if explicitly set.
	// If empty/cleared, no background is applied — the pane bg shows through.
	bg := string(ColorCodeSnippetBg)
	on = fgSGR(fg)
	if bg != "" {
		on += bgSGR(lipgloss.Color(bg))
	}
	// "off" restores the message text foreground and the pane background.
	off = fgSGR(string(ColorMessageText))
	paneBg := bgSGR(ColorBackgroundBg)
	if paneBg != "" {
		off += paneBg
	} else if bg != "" {
		off += "\x1b[49m"
	}
	return
}


// fgSGR returns the ANSI SGR to set a foreground color. Supports 256-color
// index ("12") and truecolor hex ("#ff8800").
func fgSGR(c string) string {
	if c == "" {
		return "\x1b[39m" // default fg
	}
	if strings.HasPrefix(c, "#") && len(c) == 7 {
		r, _ := strconv.ParseInt(c[1:3], 16, 32)
		g, _ := strconv.ParseInt(c[3:5], 16, 32)
		b, _ := strconv.ParseInt(c[5:7], 16, 32)
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	}
	return fmt.Sprintf("\x1b[38;5;%sm", c)
}

// applyCodeStyles walks the text and applies code snippet styling to all
// backtick-delimited spans. Uses raw ANSI fg-only codes instead of
// lipgloss.Style.Render() to avoid emitting \x1b[0m resets that would
// clear the pane background. Records snippet hits.
func (m *MessageViewModel) applyCodeStyles(text string) string {
	on, off := codeSnippetSGR()
	selOn, selOff := codeSnippetSelSGR()

	selKind, selIdx := m.selectedItemKind()
	// Only highlight snippets in the CURRENTLY SELECTED message.
	isSelectedMsg := m.reactMode && m.renderingMsgIdx == m.reactIdx
	snippetSelectedInMsg := isSelectedMsg && selKind == ItemCodeSnippet

	var out strings.Builder
	out.Grow(len(text) * 2)
	i := 0
	for i < len(text) {
		// Check for triple backtick (fenced block).
		if i+2 < len(text) && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`' {
			end := strings.Index(text[i+3:], "```")
			if end == -1 {
				out.WriteString("```")
				i += 3
				continue
			}
			inner := text[i+3 : i+3+end]
			inner = strings.TrimSpace(inner)
			m.codeSnippetHits = append(m.codeSnippetHits, codeSnippetHit{
				raw:      inner,
				msgIdx:   m.renderingMsgIdx,
				localIdx: m.renderingSnippetCount,
			})
			isSel := snippetSelectedInMsg && selIdx == m.renderingSnippetCount
			m.renderingSnippetCount++
			for li, line := range strings.Split(inner, "\n") {
				if li > 0 {
					out.WriteByte('\n')
				}
				if isSel {
					out.WriteString(selOn + line + selOff)
				} else {
					out.WriteString(on + line + off)
				}
			}
			i += 3 + end + 3
			continue
		}
		// Check for single backtick.
		if text[i] == '`' {
			end := strings.IndexByte(text[i+1:], '`')
			if end == -1 {
				out.WriteByte('`')
				i++
				continue
			}
			inner := text[i+1 : i+1+end]
			m.codeSnippetHits = append(m.codeSnippetHits, codeSnippetHit{
				raw:      inner,
				msgIdx:   m.renderingMsgIdx,
				localIdx: m.renderingSnippetCount,
			})
			isSel := snippetSelectedInMsg && selIdx == m.renderingSnippetCount
			m.renderingSnippetCount++
			for li, line := range strings.Split(inner, "\n") {
				if li > 0 {
					out.WriteByte('\n')
				}
				if isSel {
					out.WriteString(selOn + line + selOff)
				} else {
					out.WriteString(on + line + off)
				}
			}
			i += 1 + end + 1
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

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
