package tui

// Floating "X Notifications" indicator at the top-center of the chat
// pane. Shown only when notifStore has unread notifications. Clickable
// in mouse mode — opens the notifications panel.
//
// The indicator is composited onto the final view by overlayOnRow,
// which preserves the background on both sides of the button (unlike
// the popup helper in msgoptions.go, which truncates the trailing bg).
// Because the button sits on row 0 (the top border of the message
// pane), it visually replaces a small segment of the border or any
// labels underneath, which is exactly the "show in the center above
// other labels" behaviour the design calls for.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rw3iss/slackers/internal/types"
)

// renderNotificationsButton returns a styled button-like label for
// the given unread count, or an empty string when count <= 0.
func renderNotificationsButton(count int) string {
	if count <= 0 {
		return ""
	}
	word := "Notification"
	if count != 1 {
		word = "Notifications"
	}
	text := fmt.Sprintf(" 🔔 %d %s ", count, word)
	return lipgloss.NewStyle().
		Background(ColorAccent).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		Render(text)
}

// notificationsButtonClickArea returns the rendered bounds of the
// notifications button on screen, along with a `visible` flag. When
// visible is false the other return values should be ignored. The
// bounds describe a horizontal span on row y (half-open on x1):
//   - a click at column c with x0 <= c < x1 and row == y is a hit
//   - visible is true only when the notifStore has >0 notifications,
//     a store is configured, and the screen is wide enough for the
//     button to render
//
// All arithmetic is derived from Model state so it stays in sync
// regardless of when the mouse handler asks. There is no cached
// bounds field on Model, matching the pattern used for
// settingsCogClickArea().
func (m Model) notificationsButtonClickArea() (x0, x1, y int, visible bool) {
	if m.notifStore == nil {
		return 0, 0, 0, false
	}
	count := m.notifStore.Count()
	if count <= 0 {
		return 0, 0, 0, false
	}
	btn := renderNotificationsButton(count)
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}

	// Figure out where the message pane starts and how wide it is.
	// In full mode with sidebar hidden the pane spans the whole width;
	// otherwise it starts just right of the sidebar.
	paneX := 0
	paneW := m.width
	showSidebar := !m.fullMode || m.focus == types.FocusSidebar
	if showSidebar {
		paneX = m.sidebarWidth
		paneW = m.width - m.sidebarWidth
	}
	if paneW <= 0 {
		return 0, 0, 0, false
	}

	// Button too wide for the pane → suppress (no truncation — better
	// to hide than render a chopped label).
	if btnW > paneW {
		return 0, 0, 0, false
	}

	// Horizontal centre within the pane.
	start := paneX + (paneW-btnW)/2
	if start < paneX {
		start = paneX
	}
	return start, start + btnW, 0, true
}

// overlayOnRow composites `content` onto row `y` of `bg`, starting at
// visible column `x`. The left portion of the row (columns 0..x) is
// preserved, content replaces columns x..x+width(content), and the
// right portion (x+width(content)..end) is preserved too. ANSI escape
// sequences are handled correctly in both the left and right splits.
//
// This is the proper overlay that preserves both sides of a row —
// unlike the msgoptions popup helper in msgoptions.go which only
// keeps the left side. Use this one whenever you want a small label
// floating in the middle of a row without wiping out the bg to its
// right.
func overlayOnRow(bg string, y, x int, content string) string {
	if content == "" {
		return bg
	}
	lines := strings.Split(bg, "\n")
	if y < 0 || y >= len(lines) {
		return bg
	}
	row := lines[y]
	contentW := lipgloss.Width(content)

	// Left: truncate/pad to exactly x visible cells. Reuses the
	// existing ansiTruncatePad helper (from msgoptions.go) which
	// already handles wide-rune accounting correctly.
	left := ansiTruncatePad(row, x)

	// Right: skip (x + contentW) visible cells of the original row
	// and keep what's left. A trailing reset is emitted before the
	// right portion to stop the button's background from bleeding.
	right := ansiSkipCells(row, x+contentW)

	lines[y] = left + content + "\x1b[0m" + right
	return strings.Join(lines, "\n")
}

// ansiSkipCells walks s, advances past the first n visible terminal
// cells (respecting wide runes), and returns everything after that
// point. ANSI escape sequences encountered during the skip are
// preserved in the output so the returned suffix renders with the
// correct active SGR state (colours, bold, etc.) even though the
// characters those sequences applied to were dropped.
//
// If n is zero or negative, s is returned unchanged. If the skip
// overruns the end of s, an empty string is returned.
func ansiSkipCells(s string, n int) string {
	if n <= 0 {
		return s
	}
	var kept strings.Builder
	visiblePos := 0
	inEsc := false
	i := 0
	runes := []rune(s)

	for i < len(runes) {
		r := runes[i]
		if r == '\x1b' {
			// ANSI escape start — preserve the whole sequence in
			// `kept` so the trailing portion of the row inherits the
			// active SGR state.
			kept.WriteRune(r)
			inEsc = true
			i++
			continue
		}
		if inEsc {
			kept.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			i++
			continue
		}
		if visiblePos >= n {
			break
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// Zero-width combining mark — consume it with the
			// skipped cell above. Do not emit.
			i++
			continue
		}
		visiblePos += w
		i++
	}
	// Append whatever remains after the skip. This may itself start
	// with more ANSI sequences — that's fine, they layer over the
	// state we preserved above.
	if i < len(runes) {
		kept.WriteString(string(runes[i:]))
	}
	return kept.String()
}
