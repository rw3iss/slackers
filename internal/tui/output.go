package tui

// Output view — a temporary "console" pane that replaces the
// messages pane while a command's output is being displayed. Used
// by /help, /friends, /channels, /setup share, etc.
//
// The pane uses the same outer dimensions as the messages pane so
// the surrounding chrome (sidebar, input, status bar) doesn't
// reflow when the user toggles into the output view. It is a
// *pane state* (m.outputActive), NOT an overlay, so Tab still
// cycles focus, sidebar clicks still switch channels (which
// auto-closes the output), and the input still accepts new
// commands and chat messages.
//
// # Structured output
//
// Commands can return either a plain `Body` string (rendered as a
// single text block) or a list of `commands.Section`s via
// `Result.Sections`. Sections are mapped into OutputItems, where
// each item is a selectable "message" inside the pane. Items may
// also contain embedded code snippets — either inline backticks
// `like this` or fenced triple-backtick blocks — which become
// sub-selectable cursor targets alongside the item itself.
//
// # Cursor model
//
// The output view has two-level cursor state that mirrors the
// message select mode in the chat pane:
//
//   - selectedItem: the currently-highlighted item (-1 = none)
//   - selectedSnippet: sub-selection inside the current item
//     (-1 = whole item, 0..n-1 = a specific code snippet)
//
// Up/Down walks items. Right arrow enters snippet sub-select if
// the current item has snippets; Left arrow exits snippet mode.
// 'c' copies the current selection (snippet text if one is
// selected, otherwise the whole item text).
//
// # Esc behaviour
//
// Esc unwinds one level at a time: snippet → item → close. This
// mirrors the select-mode Esc in the message pane so the muscle
// memory is consistent.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/commands"
)

// OutputCloseMsg is dispatched when the user presses Esc to leave
// the Output view. The model handler clears m.outputActive and
// restores the message pane.
type OutputCloseMsg struct{}

// outputItem is one selectable paragraph / block inside the
// Output view. Items are produced either from the simple
// Body-string path (one item containing the whole body) or from
// the commands.Result.Sections structured path.
type outputItem struct {
	title      string
	text       string
	selectable bool
	// snippets are the code spans parsed out of text (backtick
	// inline or triple-backtick fenced). Each snippet records
	// its raw copy-paste payload; the highlight substitution
	// happens at render time by re-running the regex, so we
	// don't need to track byte positions here.
	snippets []outputSnippet
}

// outputSnippet is one code block / inline code span parsed out
// of an item's text. Used for sub-selection.
type outputSnippet struct {
	raw string // the actual code payload (no backticks)
}

// OutputViewModel is the temporary console pane.
type OutputViewModel struct {
	title    string
	items    []outputItem
	viewport viewport.Model
	width    int
	height   int
	focused  bool

	// Cursor state.
	selectedItem    int // -1 = no selection (browse mode)
	selectedSnippet int // -1 = whole item, 0..n-1 = snippet

	// Cached legacy body string — kept so SetBody backwards
	// compat keeps working if some caller mutates body and
	// then calls SetBody again.
	body string
}

// NewOutputView builds a new output pane with the given title and
// plain body string. For structured multi-item output use
// SetItems after construction.
//
// The pane starts UNFOCUSED — running a command from the input
// bar leaves focus on the input by default (so the user can chain
// more commands without an extra Tab). Commands that prefer the
// output pane to be focused on completion can set
// commands.Result.FocusOutput, which the host honours by calling
// SetFocused after activation.
func NewOutputView(title, body string) OutputViewModel {
	vp := viewport.New(0, 0)
	m := OutputViewModel{
		title:           title,
		body:            body,
		viewport:        vp,
		focused:         false,
		selectedItem:    -1,
		selectedSnippet: -1,
	}
	m.items = bodyToItems(body)
	return m
}

// SetSize matches the messages-pane sizing exactly so toggling in
// and out of the output view doesn't reflow the surrounding chrome.
func (o *OutputViewModel) SetSize(w, h int) {
	o.width = w
	o.height = h
	innerW := w - 4
	if innerW < 10 {
		innerW = 10
	}
	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}
	o.viewport.Width = innerW
	o.viewport.Height = innerH
	o.rebuildRender()
}

// SetTitle replaces the pane title.
func (o *OutputViewModel) SetTitle(t string) { o.title = t }

// SetBody replaces the pane with a single plain-text item
// wrapping the entire body. Backwards-compatible with the
// pre-structured Output view API.
func (o *OutputViewModel) SetBody(b string) {
	o.body = b
	o.items = bodyToItems(b)
	o.selectedItem = -1
	o.selectedSnippet = -1
	o.rebuildRender()
	o.viewport.GotoTop()
}

// SetItems replaces the pane with the given sections, mapping
// each one to a selectable outputItem and parsing out any
// embedded code snippets as sub-items.
func (o *OutputViewModel) SetItems(sections []commands.Section) {
	o.items = sectionsToItems(sections)
	o.selectedItem = -1
	o.selectedSnippet = -1
	o.rebuildRender()
	o.viewport.GotoTop()
}

// SetFocused syncs the pane's focused state with the model's
// focus cursor. Updated from updateFocus so the active border
// reflects where keystrokes go.
func (o *OutputViewModel) SetFocused(f bool) {
	o.focused = f
	// Leaving focus resets select state so the next enter is
	// fresh (matches message-pane behaviour — you don't stay
	// selected on a row after tabbing away).
	if !f {
		o.selectedItem = -1
		o.selectedSnippet = -1
		o.rebuildRender()
	}
}

// bodyToItems wraps a plain body string in a single non-
// selectable item. Used by SetBody / NewOutputView so the legacy
// "just render a string" path still works.
func bodyToItems(body string) []outputItem {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	it := outputItem{
		text:       body,
		selectable: false,
	}
	it.snippets = parseSnippets(body)
	return []outputItem{it}
}

// sectionsToItems converts a slice of commands.Sections to
// outputItems. Each section becomes one item; empty-text sections
// are skipped.
func sectionsToItems(sections []commands.Section) []outputItem {
	out := make([]outputItem, 0, len(sections))
	for _, s := range sections {
		if strings.TrimSpace(s.Text) == "" && s.Title == "" {
			continue
		}
		it := outputItem{
			title:      s.Title,
			text:       s.Text,
			selectable: s.Selectable,
		}
		it.snippets = parseSnippets(s.Text)
		out = append(out, it)
	}
	return out
}

// parseSnippets builds outputSnippet records for every code
// span found in the given text. The raw payload comes from the
// shared parseCodeSnippets helper; row positions are filled in
// by rebuildRender once the item has been styled and wrapped.
func parseSnippets(text string) []outputSnippet {
	raws := parseCodeSnippets(text)
	if len(raws) == 0 {
		return nil
	}
	out := make([]outputSnippet, len(raws))
	for i, r := range raws {
		out[i] = outputSnippet{raw: r}
	}
	return out
}

// Body returns the plain-text body of the pane as originally
// set. For Sections-based output this is the empty string.
func (o OutputViewModel) Body() string {
	return strings.TrimSpace(o.body)
}

// SelectedCopyText returns the text that the 'c' key should
// copy to the clipboard based on the current cursor position.
// Returns "" if nothing is selected.
func (o OutputViewModel) SelectedCopyText() string {
	if o.selectedItem < 0 || o.selectedItem >= len(o.items) {
		return ""
	}
	it := o.items[o.selectedItem]
	if o.selectedSnippet >= 0 && o.selectedSnippet < len(it.snippets) {
		return it.snippets[o.selectedSnippet].raw
	}
	// Fall back to the full item text, with code fences
	// stripped so a "copy item" gives back something useful.
	t := codeFencePat.ReplaceAllStringFunc(it.text, func(s string) string {
		m := codeFencePat.FindStringSubmatch(s)
		if len(m) >= 2 {
			return strings.TrimSpace(m[1])
		}
		return s
	})
	return strings.TrimSpace(t)
}

// Update routes key/mouse events through the select-mode state
// machine. Arrow keys navigate items and snippets, 'c' copies,
// Esc unwinds one level at a time, q closes.
func (o OutputViewModel) Update(msg tea.Msg) (OutputViewModel, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			// Unwind snippet → item → close.
			if o.selectedSnippet >= 0 {
				o.selectedSnippet = -1
				o.rebuildRender()
				return o, nil
			}
			if o.selectedItem >= 0 {
				o.selectedItem = -1
				o.rebuildRender()
				return o, nil
			}
			return o, func() tea.Msg { return OutputCloseMsg{} }
		case "q":
			return o, func() tea.Msg { return OutputCloseMsg{} }
		case "down", "j":
			o.moveItem(1)
			return o, nil
		case "up", "k":
			o.moveItem(-1)
			return o, nil
		case "right", "l":
			// Enter snippet sub-select if the current item
			// has snippets.
			if o.selectedItem < 0 {
				return o, nil
			}
			it := o.items[o.selectedItem]
			if len(it.snippets) == 0 {
				return o, nil
			}
			if o.selectedSnippet < 0 {
				o.selectedSnippet = 0
			} else if o.selectedSnippet < len(it.snippets)-1 {
				o.selectedSnippet++
			}
			o.rebuildRender()
			return o, nil
		case "left", "h":
			if o.selectedSnippet > 0 {
				o.selectedSnippet--
				o.rebuildRender()
				return o, nil
			}
			if o.selectedSnippet == 0 {
				o.selectedSnippet = -1
				o.rebuildRender()
				return o, nil
			}
			return o, nil
		case "c":
			// Copy selected item / snippet to clipboard.
			if text := o.SelectedCopyText(); text != "" {
				if copyToClipboard(text) {
					return o, func() tea.Msg {
						return outputCopiedMsg{msg: "Copied to clipboard"}
					}
				}
				return o, func() tea.Msg {
					return outputCopiedMsg{msg: "Copy failed: no clipboard tool"}
				}
			}
			return o, nil
		}
	}
	var cmd tea.Cmd
	o.viewport, cmd = o.viewport.Update(msg)
	return o, cmd
}

// outputCopiedMsg is dispatched by the 'c' copy action so the
// host model can surface a status-bar line. Kept private to the
// tui package since it's an internal signalling type.
type outputCopiedMsg struct {
	msg string
}

// moveItem advances the cursor over selectable items, skipping
// non-selectable ones. delta=+1 moves down, -1 moves up.
func (o *OutputViewModel) moveItem(delta int) {
	if len(o.items) == 0 {
		return
	}
	// First move from "no selection" → first selectable.
	if o.selectedItem < 0 {
		if delta > 0 {
			for i := 0; i < len(o.items); i++ {
				if o.items[i].selectable {
					o.selectedItem = i
					o.selectedSnippet = -1
					o.rebuildRender()
					return
				}
			}
		} else {
			for i := len(o.items) - 1; i >= 0; i-- {
				if o.items[i].selectable {
					o.selectedItem = i
					o.selectedSnippet = -1
					o.rebuildRender()
					return
				}
			}
		}
		return
	}
	// Walk to the next/previous selectable item, wrapping around
	// when we run off the end. Limit the walk to two full passes
	// so an items slice with zero selectables never spins.
	step := delta
	idx := o.selectedItem + step
	for attempts := 0; attempts < len(o.items)*2; attempts++ {
		if idx < 0 {
			idx = len(o.items) - 1
			continue
		}
		if idx >= len(o.items) {
			idx = 0
			continue
		}
		if o.items[idx].selectable {
			o.selectedItem = idx
			o.selectedSnippet = -1
			o.rebuildRender()
			return
		}
		idx += step
	}
}

// rebuildRender styles every item into renderedLines, applying
// the selected highlight to the current cursor position and the
// snippet highlight to the active sub-selection.
func (o *OutputViewModel) rebuildRender() {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	selStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	snippetStyle := lipgloss.NewStyle().Foreground(ColorAccent).Italic(true)
	snippetSelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(ColorAccent).
		Bold(true)

	var b strings.Builder
	for i := range o.items {
		it := &o.items[i]
		isSel := i == o.selectedItem
		if i > 0 {
			b.WriteString("\n")
		}
		if it.title != "" {
			if isSel {
				b.WriteString("▶ ")
				b.WriteString(selStyle.Render(it.title))
			} else {
				b.WriteString("  ")
				b.WriteString(titleStyle.Render(it.title))
			}
			b.WriteString("\n")
		}
		rendered := renderItemText(it, isSel, o.selectedSnippet, dimStyle, snippetStyle, snippetSelStyle)
		for _, line := range strings.Split(rendered, "\n") {
			prefix := "  "
			if isSel && it.title == "" {
				prefix = "▶ "
			}
			b.WriteString(prefix)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	o.viewport.SetContent(strings.TrimRight(b.String(), "\n"))
}

// renderItemText styles one item's body with code snippet spans
// highlighted. The snippetCursor parameter is -1 when no snippet
// is actively selected, or the index into it.snippets otherwise.
func renderItemText(
	it *outputItem,
	_ bool,
	snippetCursor int,
	textStyle, codeStyle, codeSelStyle lipgloss.Style,
) string {
	text := it.text
	// First, replace fenced blocks with a styled version.
	fenceIdx := 0
	text = codeFencePat.ReplaceAllStringFunc(text, func(block string) string {
		m := codeFencePat.FindStringSubmatch(block)
		if len(m) < 2 {
			return block
		}
		payload := strings.TrimSpace(m[1])
		style := codeStyle
		if snippetCursor == fenceIdx {
			style = codeSelStyle
		}
		fenceIdx++
		return style.Render(payload)
	})
	// Then inline backticks (starting after all the fences).
	inlineIdx := fenceIdx
	text = inlineCodePat.ReplaceAllStringFunc(text, func(span string) string {
		m := inlineCodePat.FindStringSubmatch(span)
		if len(m) < 2 {
			return span
		}
		payload := m[1]
		style := codeStyle
		if snippetCursor == inlineIdx {
			style = codeSelStyle
		}
		inlineIdx++
		return style.Render(payload)
	})
	// Wrap unstyled content in the muted text style so it reads
	// as informational body.
	return textStyle.Render(text)
}

// View renders the pane: rounded border, title bar, body
// viewport, footer hint. Mirrors MessagePaneStyle so the chrome
// matches the rest of the app. The footer adapts to the current
// cursor state so users know which keys apply.
func (o OutputViewModel) View() string {
	style := MessagePaneStyle
	if o.focused {
		style = MessagePaneActiveStyle
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	hintStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	header := titleStyle.Render("Output: "+o.title) + "\n"
	body := o.viewport.View()

	// Build the dynamic footer hint.
	var hint string
	switch {
	case o.selectedSnippet >= 0:
		hint = "  " + hintStyle.Render(fmt.Sprintf("←/→: snippets · c: copy snippet · Esc: back"))
	case o.selectedItem >= 0:
		cur := o.items[o.selectedItem]
		if len(cur.snippets) > 0 {
			hint = "  " + hintStyle.Render("↑/↓: items · →: enter snippets · c: copy item · Esc: deselect")
		} else {
			hint = "  " + hintStyle.Render("↑/↓: items · c: copy item · Esc: deselect")
		}
	default:
		hint = "  " + hintStyle.Render("↑/↓: select an item · scroll · "+FooterHintClose)
	}

	content := header + body + "\n" + hint
	return style.
		Width(o.width).
		Height(o.height).
		Render(content)
}
