package tui

// This file holds the package-level tea.Cmd helpers that power the
// bubbletea Update loop's background work. They were extracted from
// model.go as part of the Phase C "split model.go" pass — see
// docs/phase-c-plan-2026-04-08.md.
//
// Every function in this file is pure (no *Model receiver) and can
// be called from anywhere in the tui package without introducing
// circular dependencies. Methods on *Model stay in model.go (or
// future handlers_*.go files) because they need access to the
// god-object's private fields.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/secure"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// ---- Slack subsystem ---------------------------------------------------

func loadUsersCmd(svc slackpkg.SlackService) tea.Cmd {
	return func() tea.Msg {
		// AuthTest first to cache the local user ID.
		_, _ = svc.AuthTest()
		users, err := svc.ListUsers()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return UsersLoadedMsg{Users: users}
	}
}

func loadChannelsCmd(svc slackpkg.SlackService) tea.Cmd {
	return func() tea.Msg {
		channels, err := svc.ListChannels()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return ChannelsLoadedMsg{Channels: channels}
	}
}

func loadHistoryCmd(svc slackpkg.SlackService, channelID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := svc.FetchHistory(channelID, 50)
		if err != nil {
			// Return empty history so the channel still opens,
			// plus the error to display in the status bar.
			return HistoryLoadedMsg{Messages: nil, Err: err}
		}
		return HistoryLoadedMsg{Messages: msgs}
	}
}

func connectSocketCmd(socketSvc slackpkg.SocketService, eventCh chan slackpkg.SocketEvent) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := socketSvc.Connect(ctx, eventCh)
		if err != nil {
			return ConnStatusMsg{Status: types.StatusError, Err: err}
		}
		return ConnStatusMsg{Status: types.StatusConnected}
	}
}

func waitForSocketEvent(ch chan slackpkg.SocketEvent) tea.Cmd {
	return func() tea.Msg {
		event := <-ch
		return SlackEventMsg{Event: event}
	}
}

func loadMoreContextCmd(svc slackpkg.SlackService, channelID, oldestTS string) tea.Cmd {
	return func() tea.Msg {
		params := 25
		msgs, err := svc.FetchHistory(channelID, params)
		if err != nil {
			return ErrMsg{Err: err}
		}
		// FetchHistory returns chronological. We need messages BEFORE oldestTS.
		// Use FetchHistoryAround with the oldest timestamp to get earlier messages.
		olderMsgs, _, err := svc.FetchHistoryAround(channelID, oldestTS, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		// Filter to only messages older than oldestTS.
		_ = msgs // unused, we use FetchHistoryAround directly
		var filtered []types.Message
		for _, m := range olderMsgs {
			ts := fmt.Sprintf("%d.%06d", m.Timestamp.Unix(), m.Timestamp.Nanosecond()/1000)
			if ts < oldestTS {
				filtered = append(filtered, m)
			}
		}
		return MoreContextLoadedMsg{Messages: filtered}
	}
}

func silentLoadHistoryCmd(svc slackpkg.SlackService, channelID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := svc.FetchHistory(channelID, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return SilentHistoryMsg{Messages: msgs}
	}
}

func fetchContextCmd(svc slackpkg.SlackService, channelID, timestamp, channelName string) tea.Cmd {
	return func() tea.Msg {
		msgs, targetIdx, err := svc.FetchHistoryAround(channelID, timestamp, 50)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return ContextHistoryMsg{
			Messages:    msgs,
			TargetIdx:   targetIdx,
			ChannelName: channelName,
		}
	}
}

// sendMessageWithFilesCmd parses [FILE:<path>] patterns from the message,
// uploads any files, and sends the remaining text as a message.
func sendMessageWithFilesCmd(svc slackpkg.SlackService, channelID, text string) tea.Cmd {
	return func() tea.Msg {
		matches := filePattern.FindAllStringSubmatch(text, -1)
		cleanText := strings.TrimSpace(filePattern.ReplaceAllString(text, ""))

		// Upload files
		uploadCount := 0
		for _, match := range matches {
			if len(match) >= 2 {
				path := match[1]
				if err := svc.UploadFile(channelID, path); err == nil {
					uploadCount++
				}
			}
		}

		// Send remaining text if any
		if cleanText != "" {
			if err := svc.SendMessage(channelID, cleanText); err != nil {
				return ErrMsg{Err: err}
			}
		}

		if uploadCount > 0 {
			return FileUploadedMsg{Count: uploadCount}
		}
		return MessageSentMsg{}
	}
}

// seedLastSeenCmd fetches baseline timestamps for unseeded channels.
// Batches requests to avoid rate limits (5 channels per batch with delays).
//
// For cold-start read-state sync: after fetching the latest message ts
// per channel, we also pull Slack's server-side `last_read` cursor via
// GetChannelLastRead and use `max(last_read, latest_ts)` as the baseline.
// This means if the user has already read the channel from the official
// Slack app while slackers was offline, slackers starts up already
// aware of it and doesn't flash a stale unread indicator.
func seedLastSeenCmd(svc slackpkg.SlackService, lastSeen map[string]string) tea.Cmd {
	channelIDs := make([]string, 0)
	for id, ts := range lastSeen {
		if ts == "0" {
			channelIDs = append(channelIDs, id)
		}
	}

	return func() tea.Msg {
		timestamps := make(map[string]string)
		// Process in small batches to respect rate limits.
		for i := 0; i < len(channelIDs); i += 5 {
			end := i + 5
			if end > len(channelIDs) {
				end = len(channelIDs)
			}
			batch := make(map[string]string)
			for _, id := range channelIDs[i:end] {
				batch[id] = "0"
			}
			_, resultTS, _ := svc.CheckNewMessages(batch)
			for id, ts := range resultTS {
				// Reconcile with Slack's server-side last_read — if
				// the user read this channel in another client while
				// slackers was offline, use the higher cursor.
				lastRead, err := svc.GetChannelLastRead(id)
				if err == nil && lastRead != "" && lastRead > ts {
					ts = lastRead
				}
				timestamps[id] = ts
			}
			// Small delay between batches to stay under rate limits.
			if end < len(channelIDs) {
				time.Sleep(2 * time.Second)
			}
		}
		return SeedLastSeenMsg{Timestamps: timestamps}
	}
}

// ReconcileReadStateMsg carries the result of a background sync with
// Slack's server-side read cursor. Channels whose `last_read` has
// advanced past the local seen timestamp are listed in ReadChannels —
// the TUI clears their unread indicators and updates lastSeen in
// response.
type ReconcileReadStateMsg struct {
	// ReadChannels maps channelID → the new (higher) last-read
	// timestamp Slack has on file. Only entries where Slack's cursor
	// is AHEAD of the local lastSeen are included.
	ReadChannels map[string]string
}

// reconcileReadStateCmd asks Slack what the user's current `last_read`
// cursor is for each of the given channels. Any channel where the
// server cursor is ahead of the local one is reported back as read —
// meaning it was marked read from another client (official Slack app,
// web, mobile). The TUI clears those local unread badges in response.
//
// The caller should only pass channels that are currently showing
// unread locally, so we only spend extra API calls on channels that
// would actually benefit from the reconcile. Empty input returns a
// zero-value message immediately.
func reconcileReadStateCmd(svc slackpkg.SlackService, unreadChannels map[string]string) tea.Cmd {
	// Defensive copy so the caller's map can't be mutated from under us.
	local := make(map[string]string, len(unreadChannels))
	for id, ts := range unreadChannels {
		local[id] = ts
	}
	return func() tea.Msg {
		if len(local) == 0 {
			return ReconcileReadStateMsg{}
		}
		readNow := make(map[string]string)
		// Process in small batches with delays, same shape as
		// seedLastSeenCmd, to stay under rate limits.
		ids := make([]string, 0, len(local))
		for id := range local {
			ids = append(ids, id)
		}
		for i := 0; i < len(ids); i += 5 {
			end := i + 5
			if end > len(ids) {
				end = len(ids)
			}
			for _, id := range ids[i:end] {
				lastRead, err := svc.GetChannelLastRead(id)
				if err != nil {
					continue
				}
				if lastRead == "" {
					continue
				}
				if lastRead > local[id] {
					readNow[id] = lastRead
				}
			}
			if end < len(ids) {
				time.Sleep(2 * time.Second)
			}
		}
		return ReconcileReadStateMsg{ReadChannels: readNow}
	}
}

// reconcileReadStateTickCmd schedules the next periodic read-state
// reconcile. Runs independently of the message-polling tick so it
// can cadence separately (1 minute default) without interfering with
// unread detection latency.
func reconcileReadStateTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 10 {
		intervalSec = 60
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return ReconcileReadStateTickMsg{}
	})
}

// ReconcileReadStateTickMsg is fired by reconcileReadStateTickCmd.
// The TUI collects the currently-unread channels from m.lastSeen and
// dispatches reconcileReadStateCmd in response, then re-schedules.
type ReconcileReadStateTickMsg struct{}

// ---- Update check ------------------------------------------------------

func checkUpdateCmd(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get("https://api.github.com/repos/rw3iss/slackers/releases/latest")
		if err != nil {
			return UpdateAvailableMsg{} // silently fail
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return UpdateAvailableMsg{}
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return UpdateAvailableMsg{}
		}
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return UpdateAvailableMsg{}
		}
		latest := strings.TrimPrefix(release.TagName, "v")
		if latest != currentVersion && latest > currentVersion {
			return UpdateAvailableMsg{Version: release.TagName}
		}
		return UpdateAvailableMsg{}
	}
}

func clearWarningCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return ClearWarningMsg{}
	})
}

// ---- Background ticks --------------------------------------------------

// notifyWatchdogCmd schedules the next NotifyWatchdogMsg. The watchdog
// fires roughly once a second, so any non-empty status warning is aged
// out within 1 second of exceeding the user-configured timeout.
func notifyWatchdogCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return NotifyWatchdogMsg{}
	})
}

func activityCheckCmd(awayTimeoutSec int) tea.Cmd {
	if awayTimeoutSec <= 0 {
		// Away detection disabled — check again in 30s in case setting changes.
		return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
			return ActivityCheckMsg{}
		})
	}
	// Check at half the timeout interval for responsiveness.
	interval := time.Duration(awayTimeoutSec) * time.Second / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return ActivityCheckMsg{}
	})
}

func pollTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 1 {
		intervalSec = 10
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return PollTickMsg{}
	})
}

func bgPollTickCmd(intervalSec int) tea.Cmd {
	if intervalSec < 5 {
		intervalSec = 30
	}
	return tea.Tick(time.Duration(intervalSec)*time.Second, func(t time.Time) tea.Msg {
		return BgPollTickMsg{}
	})
}

func checkNewMessagesCmd(svc slackpkg.SlackService, lastSeen map[string]string, intervalSec int) tea.Cmd {
	seen := make(map[string]string, len(lastSeen))
	for k, v := range lastSeen {
		seen[k] = v
	}

	return func() tea.Msg {
		ids, latestTS, err := svc.CheckNewMessages(seen)
		if err != nil {
			return UnreadChannelsMsg{LatestTS: latestTS}
		}
		return UnreadChannelsMsg{ChannelIDs: ids, LatestTS: latestTS}
	}
}

func checkNewMessagesBgCmd(svc slackpkg.SlackService, lastSeen map[string]string, intervalSec int) tea.Cmd {
	seen := make(map[string]string, len(lastSeen))
	for k, v := range lastSeen {
		seen[k] = v
	}

	return func() tea.Msg {
		ids, latestTS, err := svc.CheckNewMessages(seen)
		if err != nil {
			return UnreadChannelsMsg{LatestTS: latestTS, IsBackground: true}
		}
		return UnreadChannelsMsg{ChannelIDs: ids, LatestTS: latestTS, IsBackground: true}
	}
}

// ---- Friend subsystem --------------------------------------------------

// loadFriendsCmd loads friend channels and pings each friend for online status.
func loadFriendsCmd(store *friends.FriendStore, p2p *secure.P2PNode) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return FriendsLoadedMsg{}
		}
		all := store.All()
		var channels []types.Channel
		online := make(map[string]bool)
		for _, f := range all {
			ch := types.Channel{
				ID:       "friend:" + f.UserID,
				Name:     f.Name,
				IsFriend: true,
				IsDM:     true,
				UserID:   f.UserID,
			}
			channels = append(channels, ch)
			if p2p != nil && f.Multiaddr != "" {
				_ = p2p.ConnectToPeer(f.UserID, f.Multiaddr)
				if p2p.IsConnected(f.UserID) {
					online[f.UserID] = true
					store.SetOnline(f.UserID, true)
				}
			}
		}
		return FriendsLoadedMsg{Channels: channels, Online: online}
	}
}

// friendPingCmdWithCurrent pings every friend for liveness and knows
// which friend the user is currently viewing — that connection is
// (re)dialed proactively if it dropped, so the active chat stays
// connected. Other friends remain observe-only.
func friendPingCmdWithCurrent(store *friends.FriendStore, p2p *secure.P2PNode, currentFriend string) tea.Cmd {
	return func() tea.Msg {
		if store == nil || p2p == nil {
			return FriendPingMsg{}
		}
		online := make(map[string]bool)
		for _, f := range store.All() {
			if f.Multiaddr == "" {
				continue
			}
			before := p2p.IsConnected(f.UserID)
			// Active chat: re-dial if the underlying libp2p
			// connection dropped (e.g. NAT eviction). Log the
			// result regardless of current-friend status so the
			// debug trace shows each friend's state per tick.
			if currentFriend != "" && f.UserID == currentFriend && !before {
				if err := p2p.ConnectToPeer(f.UserID, f.Multiaddr); err != nil {
					debug.Log("[friend-ping] re-dial failed uid=%s err=%v", f.UserID, err)
				}
			}
			on := p2p.IsConnected(f.UserID)
			if on != before {
				debug.Log("[friend-ping] uid=%s state %v → %v", f.UserID, before, on)
			}
			online[f.UserID] = on
			store.SetOnline(f.UserID, on)
			if on {
				store.UpdateLastOnline(f.UserID)
			}
		}
		return FriendPingMsg{Online: online}
	}
}
