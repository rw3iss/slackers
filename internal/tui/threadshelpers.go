package tui

// Shared rendering helpers for tracked threads. Both the sidebar
// Threads group (channels.go) and the global Threads overlay
// (threadsoverlay.go) consume these so the two surfaces stay
// visually consistent — same name precedence, same relative-time
// formatting, same participant label rules.

import (
	"fmt"
	"strings"
	"time"

	"github.com/rw3iss/slackers/internal/threads"
)

// threadDisplayName returns the row-1 label for a thread item.
// Prefers the channel alias from cfg.ChannelAliases when one is
// set; otherwise falls back to the transport-prefixed channel
// name ("#general" for Slack, "@friend" for friend chats). Friend
// channels never gain a "#" prefix and Slack channels never gain
// an "@" prefix.
//
// `aliases` may be nil — caller passes m.cfg.ChannelAliases or
// m.channels.aliases depending on which surface is rendering.
func threadDisplayName(snap threads.ThreadSnapshot, aliases map[string]string) string {
	if aliases != nil {
		if alias, ok := aliases[snap.Ref.ChannelID]; ok && alias != "" {
			return alias
		}
	}
	name := snap.ChannelName
	if name == "" {
		name = snap.Ref.ChannelID
	}
	switch snap.Ref.Source {
	case threads.SourceSlack:
		if !strings.HasPrefix(name, "#") {
			name = "#" + name
		}
	case threads.SourceFriend:
		if !strings.HasPrefix(name, "@") {
			name = "@" + name
		}
	}
	return name
}

// joinParticipantNames returns a comma-joined truncated list of
// participant first names (everything up to the first whitespace
// in each entry). When the result is too wide, the tail is
// replaced with "+N" so the cell still reads as a list. Empty
// input returns "".
func joinParticipantNames(names []string, maxWidth int) string {
	if len(names) == 0 || maxWidth <= 0 {
		return ""
	}
	firsts := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if i := strings.IndexAny(n, " \t"); i > 0 {
			firsts = append(firsts, n[:i])
		} else {
			firsts = append(firsts, n)
		}
	}
	if len(firsts) == 0 {
		return ""
	}
	full := strings.Join(firsts, ", ")
	if len(full) <= maxWidth {
		return full
	}
	for take := len(firsts) - 1; take >= 1; take-- {
		head := strings.Join(firsts[:take], ", ")
		extra := len(firsts) - take
		candidate := fmt.Sprintf("%s, +%d", head, extra)
		if len(candidate) <= maxWidth {
			return candidate
		}
	}
	if len(firsts[0]) > maxWidth-1 {
		return firsts[0][:maxWidth-1] + "…"
	}
	return firsts[0]
}

// formatRelativeTime renders a time as a compact "time since"
// badge ("now", "5m", "3h", "2d", "1w", "4mo", "1y"). Returns
// "" for the zero time so the renderer can omit the badge.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// threadActivityTime returns the time used for sorting and
// "time-since" display. Falls back to AddedAt when LastActivityAt
// is zero (older snapshots from before LastActivityAt was added).
func threadActivityTime(snap threads.ThreadSnapshot) time.Time {
	if !snap.LastActivityAt.IsZero() {
		return snap.LastActivityAt
	}
	return snap.AddedAt
}
