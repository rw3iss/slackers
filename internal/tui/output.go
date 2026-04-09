package tui

// Output view — a temporary "console" pane that replaces the
// messages pane while a command's output is being displayed. Used
// by /help, /friends, /channels, /config, etc. Activated by
// setting m.outputView and m.outputActive — see model.go View()
// for the dispatch.
//
// The pane uses the same outer dimensions as the messages pane so
// the surrounding chrome (sidebar, input, status bar) doesn't
// reflow when the user toggles into the output view. Esc closes
// it back to whatever chat or channel was previously focused.

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// OutputCloseMsg is dispatched when the user presses Esc to leave
// the Output view. The model handler clears m.outputActive and
// restores the message pane.
type OutputCloseMsg struct{}

// OutputViewModel is the temporary console pane.
type OutputViewModel struct {
	title    string
	body     string
	viewport viewport.Model
	width    int
	height   int
	focused  bool
}

// NewOutputView builds a new output pane with the given title and
// body. Body may be multiline; it is rendered as plain text inside
// a scrollable viewport.
//
// The pane starts UNFOCUSED — running a command from the input
// bar leaves focus on the input by default (so the user can chain
// more commands without an extra Tab). Commands that prefer the
// output pane to be focused on completion can set
// commands.Result.FocusOutput, which the host honours by calling
// SetFocused after activation.
func NewOutputView(title, body string) OutputViewModel {
	vp := viewport.New(0, 0)
	vp.SetContent(body)
	return OutputViewModel{
		title:    title,
		body:     body,
		viewport: vp,
		focused:  false,
	}
}

// SetSize matches the messages-pane sizing exactly so toggling in
// and out of the output view doesn't reflow the surrounding chrome.
func (o *OutputViewModel) SetSize(w, h int) {
	o.width = w
	o.height = h
	// Inner viewport: leave room for top/bottom border (2) and
	// header line (1).
	innerW := w - 4 // 2 borders + 2 padding
	if innerW < 10 {
		innerW = 10
	}
	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}
	o.viewport.Width = innerW
	o.viewport.Height = innerH
	o.viewport.SetContent(o.body)
}

// SetTitle / SetBody let callers replace the displayed content
// without recreating the model (useful for /help <topic> swapping
// between topics inside the same view).
func (o *OutputViewModel) SetTitle(t string) { o.title = t }
func (o *OutputViewModel) SetBody(b string) {
	o.body = b
	o.viewport.SetContent(b)
	o.viewport.GotoTop()
}

func (o *OutputViewModel) SetFocused(f bool) { o.focused = f }

// Update routes key/mouse events into the embedded viewport for
// scrolling. Esc emits OutputCloseMsg so the model can dismiss.
func (o OutputViewModel) Update(msg tea.Msg) (OutputViewModel, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			return o, func() tea.Msg { return OutputCloseMsg{} }
		}
	}
	var cmd tea.Cmd
	o.viewport, cmd = o.viewport.Update(msg)
	return o, cmd
}

// View renders the pane: rounded border, title bar, body
// viewport, footer hint. Mirrors MessagePaneStyle so the chrome
// matches the rest of the app.
func (o OutputViewModel) View() string {
	style := MessagePaneStyle
	if o.focused {
		style = MessagePaneActiveStyle
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	hintStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	header := titleStyle.Render("Output: "+o.title) + "\n"
	body := o.viewport.View()
	hint := "\n" + hintStyle.Render("  ↑/↓ scroll · "+FooterHintClose)

	content := header + body + hint
	return style.
		Width(o.width).
		Height(o.height).
		Render(content)
}

// Body returns the current body string. Used by tests and by the
// command runner if it needs to reuse the rendered content.
func (o OutputViewModel) Body() string {
	return strings.TrimSpace(o.body)
}
