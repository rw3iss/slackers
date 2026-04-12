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
	"sort"
	"strings"
	"sync"
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
		_, _, _ = svc.AuthTest()
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
		unread := make(map[string]string)
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
				// Reconcile with Slack's server-side last_read.
				lastRead, err := svc.GetChannelLastRead(id)
				if err == nil && lastRead != "" {
					if lastRead >= ts {
						// User already read this channel (via
						// another client). Use the higher cursor.
						ts = lastRead
					} else {
						// last_read < latest_ts → channel has
						// unread messages. Use last_read as the
						// baseline so the unread indicator appears.
						unread[id] = ts
						ts = lastRead
					}
				}
				timestamps[id] = ts
			}
			// Small delay between batches to stay under rate limits.
			if end < len(channelIDs) {
				time.Sleep(2 * time.Second)
			}
		}
		return SeedLastSeenMsg{Timestamps: timestamps, Unread: unread}
	}
}

// StartupUnreadCheckMsg carries channels detected as having unread
// messages at startup. May be sent multiple times (once per batch)
// for progressive updates.
type StartupUnreadCheckMsg struct {
	Unread map[string]string // channelID → count string
}

// startupUnreadCheckCmd checks all previously-seeded Slack channels
// for unread messages using conversations.info. Returns a tea.Cmd
// that sends the first batch's results immediately, then chains
// subsequent batches so the UI updates progressively.
// unreadTarget is a channel to check for unreads, with its lastSeen
// timestamp for priority sorting.
type unreadTarget struct {
	id string
	ts string
}

func startupUnreadCheckCmd(svc slackpkg.SlackService, lastSeen map[string]string) tea.Cmd {
	var targets []unreadTarget
	for id, ts := range lastSeen {
		if ts == "" || ts == "0" || strings.HasPrefix(id, "friend:") {
			continue
		}
		targets = append(targets, unreadTarget{id, ts})
	}
	if len(targets) == 0 {
		return nil
	}
	// Most recently active channels first.
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].ts > targets[j].ts
	})

	// Return a command that processes one batch and chains the next.
	return startupUnreadBatch(svc, targets, 0)
}

// startupUnreadBatch processes one batch of channels and returns a
// StartupUnreadCheckMsg. If more batches remain, a follow-up command
// is chained via tea.Sequence so the UI updates after each batch.
func startupUnreadBatch(svc slackpkg.SlackService, targets []unreadTarget, offset int) tea.Cmd {
	const batchSize = 15
	return func() tea.Msg {
		end := offset + batchSize
		if end > len(targets) {
			end = len(targets)
		}
		if offset == 0 {
			debug.Log("[startup-unread] checking %d channels for unreads", len(targets))
		}
		batch := targets[offset:end]
		unread := make(map[string]string)
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(len(batch))
		for _, t := range batch {
			go func(id string) {
				defer wg.Done()
				count, err := svc.GetChannelUnreadCount(id)
				if err != nil {
					return
				}
				if count > 0 {
					debug.Log("[startup-unread] UNREAD %s count=%d", id, count)
					mu.Lock()
					unread[id] = fmt.Sprintf("%d", count)
					mu.Unlock()
				}
			}(t.id)
		}
		wg.Wait()
		if end >= len(targets) {
			debug.Log("[startup-unread] done (%d/%d)", end, len(targets))
		}
		return startupUnreadBatchResult{
			Unread:    unread,
			targets:   targets,
			nextOff:   end,
			svc:       svc,
			done:      end >= len(targets),
		}
	}
}

// startupUnreadBatchResult is an internal message carrying one batch
// of unread results plus the state needed to chain the next batch.
type startupUnreadBatchResult struct {
	Unread  map[string]string
	targets []unreadTarget
	nextOff int
	svc     slackpkg.SlackService
	done    bool
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

// loadFriendsCmd loads friend channels from the store and builds
// the sidebar entries. It does NOT dial friends — that's handled
// by the startup ping cycle (friendPingCmdWithCurrent) which runs
// immediately after FriendsLoadedMsg. Dialing here would trigger
// libp2p's dial backoff when the friend is offline, which then
// blocks the ping cycle's attempt a moment later, adding a 5-30
// second delay before the first successful connection.
func loadFriendsCmd(store *friends.FriendStore, _ /* p2p */ *secure.P2PNode) tea.Cmd {
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
			// Don't dial here — the startup ping cycle
			// (dispatched right after FriendsLoadedMsg)
			// handles all connections. The peerLookup
			// callback on the P2P node resolves inbound
			// connections from friends we haven't dialed yet.
		}
		return FriendsLoadedMsg{Channels: channels, Online: online}
	}
}

// friendPingCmdWithCurrent pings every friend for liveness and
// knows which friend the user is currently viewing — that
// connection is (re)dialed proactively if it dropped, so the
// active chat stays connected. Other friends remain observe-only.
//
// Friends are batched in groups of 5 to avoid overwhelming the
// network or the libp2p host. Each batch of 5 runs concurrently
// (one goroutine per friend in the batch); batches are processed
// sequentially. This caps the number of simultaneous dial
// attempts at 5 regardless of friend count, keeping latency
// bounded and resource usage predictable even at 50+ friends.
//
// localStatus / localAwayMsg carry the local user's current
// status, piggybacked on each ping so the remote peer learns
// our state without a separate broadcast.
func friendPingCmdWithCurrent(store *friends.FriendStore, p2p *secure.P2PNode, _ /* currentFriend */, localStatus, localAwayMsg, localSharedFolder string) tea.Cmd {
	return func() tea.Msg {
		if store == nil || p2p == nil {
			return FriendPingMsg{}
		}
		all := store.All()
		// Filter to friends with a multiaddr — there's nothing
		// to ping without one.
		type pingTarget struct {
			userID     string
			multiaddr  string
			lastOnline int64
			isOnline   bool
		}
		var targets []pingTarget
		for _, f := range all {
			if f.Multiaddr == "" {
				continue
			}
			targets = append(targets, pingTarget{f.UserID, f.Multiaddr, f.LastOnline, f.Online})
		}
		// Sort by priority: online friends first, then by most
		// recently active. This ensures the friends the user is
		// most likely interacting with get pinged in the first
		// batch.
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].isOnline != targets[j].isOnline {
				return targets[i].isOnline // online first
			}
			return targets[i].lastOnline > targets[j].lastOnline
		})

		online := make(map[string]bool, len(targets))
		status := make(map[string]FriendStatusInfo, len(targets))
		var mu sync.Mutex

		// Process in batches of 5.
		const batchSize = 5
		for i := 0; i < len(targets); i += batchSize {
			end := i + batchSize
			if end > len(targets) {
				end = len(targets)
			}
			batch := targets[i:end]
			var wg sync.WaitGroup
			wg.Add(len(batch))
			for _, t := range batch {
				go func(uid, maddr string) {
					defer wg.Done()
					before := p2p.IsConnected(uid)
					// Always try to connect if not already
					// connected — this covers startup (where
					// no friends are connected yet) and
					// reconnection after NAT eviction / idle
					// timeout. The 3-second dial timeout keeps
					// each attempt bounded.
					if !before {
						if err := p2p.ConnectToPeer(uid, maddr); err != nil {
							debug.Log("[friend-ping] dial failed uid=%s err=%v", uid, err)
						}
					}
					on := p2p.IsConnected(uid)
					if on != before {
						debug.Log("[friend-ping] uid=%s state %v → %v", uid, before, on)
					}
					store.SetOnline(uid, on) // blocked by guard if AwayStatus=="offline"
					if on {
						store.UpdateLastOnline(uid)
						// Send our status so the friend's handler
						// fires and sets our status on their side.
						if localStatus != "" {
							statusMsg := secure.P2PMessage{
								Type:          secure.MsgTypeStatusUpdate,
								Timestamp:     time.Now().Unix(),
								SenderID:      uid,
								StatusType:    localStatus,
								StatusMessage: localAwayMsg,
								SharedFolder:  localSharedFolder,
							}
							_ = p2p.SendMessage(uid, statusMsg)
						}
					}
					// Build status info from the friend store's
					// current AwayStatus/AwayMessage (populated
					// by incoming status_update messages or prior
					// pong responses).
					f := store.Get(uid)
					// Check the friend's stored status — if they
					// sent "offline" (hidden), report offline.
					reportOnline := on
					if f != nil && f.AwayStatus == "offline" {
						reportOnline = false
					}
					info := FriendStatusInfo{Online: reportOnline}
					if f != nil {
						info.AwayStatus = f.AwayStatus
						info.AwayMessage = f.AwayMessage
					}
					mu.Lock()
					online[uid] = reportOnline
					status[uid] = info
					mu.Unlock()
				}(t.userID, t.multiaddr)
			}
			wg.Wait()
		}
		// After all batches complete, do a final broadcast to any
		// peers that may have connected to us inbound during the
		// ping cycle. This catches the case where a friend starts
		// their app and dials us while we were busy pinging others
		// — without this follow-up, they wouldn't learn our status
		// until the *next* ping cycle (up to 10s later).
		if localStatus != "" {
			p2p.BroadcastStatus(localStatus, localAwayMsg, localSharedFolder)
		}

		return FriendPingMsg{Online: online, Status: status}
	}
}
