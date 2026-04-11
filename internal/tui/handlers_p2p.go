package tui

// This file holds the friend / P2P helper methods extracted from
// model.go as part of the Phase C "split model.go" pass — see
// docs/phase-c-plan-2026-04-08.md.
//
// Everything here is scoped to the friend P2P subsystem: connection
// lifecycle, profile sync, pending-message resend, friend-side
// history storage, and friend card import. Methods stay on *Model
// so they can reach private fields like m.friendStore,
// m.friendHistory, m.p2pNode, m.friendMessages, etc.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/notifications"
	"github.com/rw3iss/slackers/internal/secure"
	"github.com/rw3iss/slackers/internal/types"
)

// newFileBrowserCfg returns a FileBrowserConfig with the user's
// persisted sort preferences pre-filled. Callers set the remaining
// fields (StartDir, Title, ShowFiles, etc.) on the returned value.
func (m *Model) newFileBrowserCfg() FileBrowserConfig {
	cfg := FileBrowserConfig{
		SortBy:  "name",
		SortAsc: true,
	}
	if m.cfg != nil {
		cfg.Favorites = m.cfg.FavoriteFolders
		if m.cfg.FileSortBy != "" {
			cfg.SortBy = m.cfg.FileSortBy
		}
		if m.cfg.FileSortAsc != nil {
			cfg.SortAsc = *m.cfg.FileSortAsc
		}
	}
	return cfg
}

// effectiveStatus returns the status string to broadcast.
// If HideOnlineStatus is enabled, returns empty strings —
// the caller should skip the broadcast entirely.
func (m *Model) effectiveStatus() (status string, msg string, suppress bool) {
	if m.cfg != nil && m.cfg.HideOnlineStatus {
		return "", "", true // suppress all broadcasts
	}
	if m.cfg != nil && m.cfg.AwayEnabled {
		return "away", m.cfg.AwayMsg, false
	}
	return "online", "", false
}

// sharedFolderName returns the basename of the user's shared folder
// (for inclusion in status broadcasts), or "" if none is configured.
func (m *Model) sharedFolderName() string {
	if m.cfg == nil || m.cfg.SharedFolder == "" {
		return ""
	}
	return filepath.Base(m.cfg.SharedFolder)
}

// ---- Plain helpers (package-level, no *Model receiver) -----------------

// deleteFriendMessage removes a message (top-level or nested reply) from a slice.
func deleteFriendMessage(msgs []types.Message, messageID string) []types.Message {
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			return append(msgs[:i], msgs[i+1:]...)
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				msgs[i].Replies = append(msgs[i].Replies[:j], msgs[i].Replies[j+1:]...)
				return msgs
			}
		}
	}
	return msgs
}

// findFriendMsgPtr returns a pointer to a friend message by ID, walking nested replies.
func findFriendMsgPtr(msgs []types.Message, messageID string) *types.Message {
	for i := range msgs {
		if msgs[i].MessageID == messageID {
			return &msgs[i]
		}
		for j := range msgs[i].Replies {
			if msgs[i].Replies[j].MessageID == messageID {
				return &msgs[i].Replies[j]
			}
		}
	}
	return nil
}

// hostPortFromMultiaddr parses a /ip4/<ip>/tcp/<port>/p2p/<id> string
// and returns just the host + port, used by the test-connection
// fallback to do a raw TCP socket dial when libp2p refuses to dial
// our own peer ID.
func hostPortFromMultiaddr(maddr string) (string, int) {
	parts := strings.Split(strings.TrimPrefix(maddr, "/"), "/")
	if len(parts) < 4 || parts[0] != "ip4" || parts[2] != "tcp" {
		return "", 0
	}
	port, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0
	}
	return parts[1], port
}

// friendCardLabel returns a short human-friendly name for a contact
// card — used in confirmation prompts when a card is clicked/imported.
// Mirrors the same Name → Email → ShortPeerID priority used by the
// in-chat pill renderer so confirmations and pills match.
func friendCardLabel(card friends.ContactCard) string {
	if s := strings.TrimSpace(card.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(card.Email); s != "" {
		return s
	}
	if s := friends.ShortPeerID(card); s != "" {
		return s
	}
	return "unknown"
}

// buildSlackInviteMessage returns a Slack-mrkdwn-formatted invite
// string that embeds the local user's contact card JSON inside a
// code span on its own line, with a linkified "Slackers" word
// pointing at the project repo.
//
// Falls back to a simpler plain-text message if the local P2P
// identity isn't available (Secure Mode off).
func buildSlackInviteMessage(m Model) string {
	const slackersURL = "https://github.com/rw3iss/slackers"
	// Two \n produce a blank line before the marker so the JSON
	// always lands on its own paragraph in the recipient's Slack
	// client (single \n is treated as a soft wrap by some
	// renderers).
	const preface = "Hey! I'm using <" + slackersURL + "|Slackers> — paste this contact info into its Add Friend screen so we can chat privately over P2P:\n\n"

	if m.secureMgr == nil || m.p2pNode == nil {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	pub := m.secureMgr.OwnPublicKeyBase64()
	maddr := m.p2pNode.Multiaddr()
	if pub == "" || maddr == "" {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		pub,
		maddr,
	)
	raw, err := json.Marshal(card)
	if err != nil {
		return "Hey! I'm using Slackers (" + slackersURL + "). Install it to chat with me privately over P2P."
	}
	return preface + "[FRIEND:" + string(raw) + "]"
}

// sendFriendMessageCmd returns a tea.Cmd that attempts to deliver
// a P2P text message to a friend. The peer is always (re)dialed
// before the send — libp2p.Connect is idempotent and this
// guarantees the P2PNode's internal peerMap is populated even when
// the friend came online by dialing us (inbound-only, we never
// dialed back).
//
// On a send failure the command does one more reconnect-and-retry
// pass with a short backoff before giving up. The returned
// FriendSendResultMsg.Success is true only if the final wire send
// returned no error.
func sendFriendMessageCmd(node *secure.P2PNode, store *friends.FriendStore, peerUID string, fm secure.P2PMessage) tea.Cmd {
	return func() tea.Msg {
		debug.Log("[friend-send] start uid=%s type=%s msgID=%s", peerUID, fm.Type, fm.MessageID)
		if f := store.Get(peerUID); f != nil && f.Multiaddr != "" {
			if dialErr := node.ConnectToPeer(peerUID, f.Multiaddr); dialErr != nil {
				debug.Log("[friend-send] pre-dial failed uid=%s err=%v", peerUID, dialErr)
			}
		}
		err := node.SendMessage(peerUID, fm)
		if err != nil {
			debug.Log("[friend-send] first attempt failed uid=%s err=%v — retrying", peerUID, err)
			// Brief pause before retry — lets any in-flight
			// dial / handshake finish if the first send raced
			// the new connection.
			time.Sleep(150 * time.Millisecond)
			if f := store.Get(peerUID); f != nil && f.Multiaddr != "" {
				if dialErr := node.ConnectToPeer(peerUID, f.Multiaddr); dialErr != nil {
					debug.Log("[friend-send] retry-dial failed uid=%s err=%v", peerUID, dialErr)
				}
			}
			err = node.SendMessage(peerUID, fm)
			if err != nil {
				debug.Log("[friend-send] retry attempt failed uid=%s err=%v — giving up", peerUID, err)
			}
		}
		if err == nil {
			debug.Log("[friend-send] ok uid=%s type=%s msgID=%s", peerUID, fm.Type, fm.MessageID)
		}
		result := FriendSendResultMsg{
			PeerUID:   peerUID,
			MessageID: fm.MessageID,
			Success:   err == nil,
		}
		if err != nil {
			result.Err = err.Error()
		}
		return result
	}
}

// ---- Friend-side history and local state (methods on *Model) ----------

// addLocalReaction updates in-memory friend message reactions and refreshes the view.
func (m *Model) addLocalReaction(friendUID, messageID, emoji string) {
	msgs := m.friendMessages[friendUID]
	if target := findFriendMsgPtr(msgs, messageID); target != nil {
		found := false
		for j, r := range target.Reactions {
			if r.Emoji == emoji {
				target.Reactions[j].Count++
				target.Reactions[j].UserIDs = append(target.Reactions[j].UserIDs, "me")
				found = true
				break
			}
		}
		if !found {
			target.Reactions = append(target.Reactions, types.Reaction{
				Emoji: emoji, UserIDs: []string{"me"}, Count: 1,
			})
		}
	}
	friendChID := "friend:" + friendUID
	if m.currentCh != nil && m.currentCh.ID == friendChID {
		m.messages.SetMessages(msgs)
	}
}

// appendFriendMessage adds a message to a friend's history and persists it.
func (m *Model) appendFriendMessage(userID string, msg types.Message) {
	m.friendMessages[userID] = append(m.friendMessages[userID], msg)
	if m.friendHistory != nil {
		pairKey := ""
		if f := m.friendStore.Get(userID); f != nil {
			pairKey = f.PairKey
		}
		m.friendHistory.Append(userID, msg, pairKey)
		go m.friendHistory.Save(userID)
	}
}

// loadFriendHistory loads a friend's persisted chat history into the
// message view. Used by both the keyboard Enter handler and the mouse
// click handler.
func (m *Model) loadFriendHistory(friendUserID string) {
	// Opening / clicking a friend is the user's signal that they
	// want to talk to this person — try to (re)connect now, ping
	// for a fresh status update, and refresh the sidebar + header
	// so the online/away/offline indicator is current, not stale
	// from the last periodic ping cycle.
	m.connectFriend(friendUserID)
	// Async status probe: connect (if needed), check connection
	// state, and immediately update the friend store + sidebar.
	// This runs in a goroutine so it doesn't block the UI while
	// the 3-second dial timeout runs.
	if m.p2pNode != nil && m.friendStore != nil {
		go func() {
			f := m.friendStore.Get(friendUserID)
			if f == nil || f.Multiaddr == "" {
				return
			}
			// Best-effort dial — if already connected this is
			// nearly instant (libp2p short-circuits).
			_ = m.p2pNode.ConnectToPeer(friendUserID, f.Multiaddr)
			on := m.p2pNode.IsConnected(friendUserID)
			m.friendStore.SetOnline(friendUserID, on)
			if on {
				m.friendStore.UpdateLastOnline(friendUserID)
			}
		}()
	}
	if m.friendHistory != nil {
		pairKey := ""
		if m.friendStore != nil {
			if f := m.friendStore.Get(friendUserID); f != nil {
				pairKey = f.PairKey
			}
		}
		msgs := m.friendHistory.GetDecrypted(friendUserID, pairKey)
		m.friendMessages[friendUserID] = msgs
		m.messages.SetMessages(msgs)
		return
	}
	if history, ok := m.friendMessages[friendUserID]; ok {
		m.messages.SetMessages(history)
	} else {
		m.messages.SetMessages(nil)
	}
}

// buildFriendChannels creates Channel entries from the friend store.
func (m *Model) buildFriendChannels() []types.Channel {
	if m.friendStore == nil {
		return nil
	}
	var channels []types.Channel
	for _, f := range m.friendStore.All() {
		channels = append(channels, types.Channel{
			ID:       "friend:" + f.UserID,
			Name:     f.Name,
			IsFriend: true,
			IsDM:     true,
			UserID:   f.UserID,
		})
	}
	return channels
}

// recordFriendRequest drops a TypeFriendRequest notification.
func (m *Model) recordFriendRequest(senderID, senderName, pubKey, multiaddr string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:            notifications.TypeFriendRequest,
		ChannelID:       "friend:" + senderID,
		UserID:          senderID,
		UserName:        senderName,
		FriendPublicKey: pubKey,
		FriendMultiaddr: multiaddr,
	})
}

// ---- Friend idle watchdog ---------------------------------------------

// FriendIdleTimeout is how long a friend chat must sit untouched
// before the inactivity watchdog drops its libp2p connection.
const FriendIdleTimeout = 60 * time.Second

// friendIdleCheckMsg fires periodically (~10s) so the model can
// disconnect any friends whose activity timestamp has aged past
// FriendIdleTimeout.
type friendIdleCheckMsg struct{}

func friendIdleCheckCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return friendIdleCheckMsg{}
	})
}

// touchFriendActivity records that the user just interacted with the
// given friend chat. The inactivity watchdog uses this to decide
// whether to keep the libp2p session open.
func (m *Model) touchFriendActivity(friendUID string) {
	if friendUID == "" {
		return
	}
	if m.friendActivity == nil {
		m.friendActivity = make(map[string]time.Time)
	}
	m.friendActivity[friendUID] = time.Now()
}

// ---- Friend connection lifecycle --------------------------------------

// connectFriend kicks off a (re)dial to the given friend and, on
// success, runs the profile-sync / request-pending follow-up.
//
// IMPORTANT: the dial is dispatched in a goroutine so this method
// ALWAYS returns immediately. Running ConnectToPeer inline on the
// bubbletea Update loop would block the entire UI (including the
// input textarea) for the full dial timeout whenever the friend is
// offline. If the peer is already connected we skip the dial
// entirely and just refresh the activity clock. The send path has
// its own reconnect-and-retry logic in sendFriendMessageCmd, so
// the async dial here is purely opportunistic.
func (m *Model) connectFriend(friendUID string) {
	if m.p2pNode == nil || m.friendStore == nil || friendUID == "" {
		debug.Log("[connect-friend] skip uid=%q (p2p=%v store=%v)", friendUID, m.p2pNode != nil, m.friendStore != nil)
		return
	}
	f := m.friendStore.Get(friendUID)
	if f == nil || f.Multiaddr == "" {
		debug.Log("[connect-friend] skip uid=%s (friend=%v maddr=%q)", friendUID, f != nil, "")
		return
	}
	m.touchFriendActivity(friendUID)
	if m.p2pNode.IsConnected(friendUID) {
		debug.Log("[connect-friend] already connected uid=%s", friendUID)
		m.friendStore.SetOnline(friendUID, true)
		m.friendStore.UpdateLastOnline(friendUID)
		return
	}
	debug.Log("[connect-friend] dispatching dial uid=%s maddr=%s", friendUID, f.Multiaddr)
	// Fire the dial in the background — never block Update.
	// Capture everything we need by value so the goroutine is
	// fully detached from the main model state.
	node := m.p2pNode
	store := m.friendStore
	multiaddr := f.Multiaddr
	uid := friendUID
	sendProfile := m.sendProfileSync
	sendRequest := m.sendRequestPending
	go func() {
		if err := node.ConnectToPeer(uid, multiaddr); err != nil {
			debug.Log("[connect-friend] dial failed uid=%s err=%v", uid, err)
		}
		if node.IsConnected(uid) {
			debug.Log("[connect-friend] online uid=%s", uid)
			store.SetOnline(uid, true)
			store.UpdateLastOnline(uid)
			// These already ship as independent goroutines,
			// but calling them here ensures they only fire
			// on the offline→online edge observed from this
			// call site.
			sendProfile(uid)
			sendRequest(uid)
		} else {
			debug.Log("[connect-friend] still offline uid=%s", uid)
			store.SetOnline(uid, false)
		}
	}()
}

// sendRequestPending asks a peer to scan its chat history for any
// messages addressed to us that are still flagged Pending and
// re-send them. Safe to call from any goroutine; no-op when the
// P2P layer isn't up.
func (m *Model) sendRequestPending(peerUID string) {
	if m.p2pNode == nil || peerUID == "" {
		return
	}
	_ = m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
		Type:      secure.MsgTypeRequestPending,
		SenderID:  peerUID,
		Timestamp: time.Now().Unix(),
	})
}

// sendProfileSync announces the local user's current contact card
// (as single-line JSON) to the given peer. The receiver merges any
// fresh fields into their stored friend record for us without
// overwriting their locally-chosen display name. Safe to call from
// any goroutine; a nil/offline node is a silent no-op.
func (m *Model) sendProfileSync(peerUID string) {
	if m.p2pNode == nil || m.secureMgr == nil || peerUID == "" {
		return
	}
	pub := m.secureMgr.OwnPublicKeyBase64()
	maddr := m.p2pNode.Multiaddr()
	if pub == "" || maddr == "" {
		return
	}
	card := friends.MyContactCard(
		m.cfg.SlackerID,
		m.cfg.MyName,
		m.cfg.MyEmail,
		pub,
		maddr,
	)
	raw, err := json.Marshal(card)
	if err != nil {
		return
	}
	_ = m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
		Type:      secure.MsgTypeProfileSync,
		Text:      string(raw),
		SenderID:  peerUID,
		Timestamp: time.Now().Unix(),
	})
}

// mergeFriendProfile refreshes an existing friend record with the
// values from a peer-announced contact card. Matching is tried
// first by the sender UID we received the packet over, then via
// FindByCard (SlackerID / PublicKey / Multiaddr). The locally-set
// display name is preserved unless it was empty — it is the user's
// own alias for that friend and shouldn't get clobbered by what the
// peer calls themselves.
//
// Saves the friend store on any change and refreshes the sidebar
// labels so a renamed peer shows up right away.
func (m *Model) mergeFriendProfile(senderUID string, card friends.ContactCard) {
	if m.friendStore == nil {
		return
	}
	var existing *friends.Friend
	if senderUID != "" {
		existing = m.friendStore.Get(senderUID)
	}
	if existing == nil {
		existing = m.friendStore.FindByCard(card)
	}
	if existing == nil {
		return
	}
	updated := *existing
	changed := false
	if card.Name != "" && updated.Name == "" {
		updated.Name = card.Name
		changed = true
	}
	if card.Email != "" && updated.Email != card.Email {
		updated.Email = card.Email
		changed = true
	}
	if card.PublicKey != "" && updated.PublicKey != card.PublicKey {
		updated.PublicKey = card.PublicKey
		// New public key means the ECDH-derived pair key is
		// stale; clear it so the next handshake re-derives.
		updated.PairKey = ""
		changed = true
	}
	if card.Multiaddr != "" && updated.Multiaddr != card.Multiaddr {
		updated.Multiaddr = card.Multiaddr
		changed = true
	}
	if card.SlackerID != "" && updated.SlackerID != card.SlackerID {
		updated.SlackerID = card.SlackerID
		changed = true
	}
	if !changed {
		return
	}
	if err := m.friendStore.Update(updated); err == nil {
		go m.friendStore.Save()
		m.channels.SetFriendChannels(m.buildFriendChannels())
	}
}

// resendPendingFriendMessagesCmd returns a tea.Cmd that queries the
// friend chat history for any messages still marked Pending and
// fires a sendFriendMessageCmd for each in chronological order.
//
// IMPORTANT: uses tea.Sequence rather than tea.Batch so each send
// completes (and its FriendSendResultMsg is processed) before the
// next one starts. A parallel batch lets the wire sends race each
// other, which causes the receiver to observe messages out of
// order — making the chat history appear scrambled on the other
// side. The original send Timestamp also travels on the wire so
// the receiver can stamp each message with its true send time
// instead of the arrival time.
func (m *Model) resendPendingFriendMessagesCmd(peerUID string) tea.Cmd {
	if m.p2pNode == nil || m.friendHistory == nil || m.friendStore == nil || peerUID == "" {
		return nil
	}
	pairKey := ""
	if f := m.friendStore.Get(peerUID); f != nil {
		pairKey = f.PairKey
	}
	pending := m.friendHistory.PendingForResend(peerUID, pairKey)
	if len(pending) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, pm := range pending {
		fm := secure.P2PMessage{
			Type:         secure.MsgTypeMessage,
			Text:         pm.Text,
			SenderID:     peerUID,
			Timestamp:    pm.Timestamp.Unix(),
			MessageID:    pm.MessageID,
			ReplyToMsgID: pm.ReplyTo,
		}
		cmds = append(cmds, sendFriendMessageCmd(m.p2pNode, m.friendStore, peerUID, fm))
	}
	return tea.Sequence(cmds...)
}

// ---- Friend card import -----------------------------------------------

// applyFriendCard saves an incoming contact card into the friend
// store. Behaviour depends on the flags:
//   - merge=false replace=false: add as a new friend (errors if one
//     with the same SlackerID already exists).
//   - merge=true: fill only missing fields on the existing record,
//     never overwrite present ones.
//   - replace=true: overwrite every persistent field on the existing
//     record (runtime-only fields like Online are preserved by Update).
//
// On success the friend sidebar is refreshed. For brand-new friends
// a FriendAddedHandshakeMsg is dispatched so the existing P2P
// handshake flow runs exactly as if the user had added them from the
// Add a Friend page.
func (m *Model) applyFriendCard(card friends.ContactCard, merge, replace bool) tea.Cmd {
	if m.friendStore == nil {
		m.warning = "Friend store not available"
		return nil
	}
	card.Multiaddr = strings.TrimSpace(card.Multiaddr)
	label := friendCardLabel(card)

	// Locate any existing record by SlackerID, PublicKey, or
	// Multiaddr (in that priority order). FindByCard centralises the
	// matching rules used elsewhere in the import flow.
	existing := m.friendStore.FindByCard(card)

	switch {
	case existing == nil:
		// Brand new friend. SLF2 hash imports arrive with no Name
		// or Email — fill in a synthetic placeholder so the friend
		// list always has something to display until the real name
		// arrives over the wire.
		f := friends.FriendFromCard(card)
		if strings.TrimSpace(f.Name) == "" {
			f.Name = friends.FallbackName(card)
		}
		if err := m.friendStore.Add(f); err != nil {
			m.warning = "Import failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Saved in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Imported friend " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		newUID := f.UserID
		if newUID == "" && f.SlackerID != "" {
			newUID = "slacker:" + f.SlackerID
		}
		if newUID != "" {
			return func() tea.Msg {
				return FriendAddedHandshakeMsg{
					UserID:    newUID,
					Name:      f.Name,
					Multiaddr: f.Multiaddr,
				}
			}
		}
		return nil

	case merge:
		updated := *existing
		if updated.Name == "" && card.Name != "" {
			updated.Name = card.Name
		}
		if updated.Email == "" && card.Email != "" {
			updated.Email = card.Email
		}
		if updated.PublicKey == "" && card.PublicKey != "" {
			updated.PublicKey = card.PublicKey
		}
		if updated.Multiaddr == "" && card.Multiaddr != "" {
			updated.Multiaddr = card.Multiaddr
		}
		if updated.SlackerID == "" && card.SlackerID != "" {
			updated.SlackerID = card.SlackerID
		}
		if err := m.friendStore.Update(updated); err != nil {
			m.warning = "Merge failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Merged in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Merged contact card into " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		return nil

	case replace:
		updated := *existing
		updated.Name = card.Name
		updated.Email = card.Email
		updated.PublicKey = card.PublicKey
		updated.Multiaddr = card.Multiaddr
		if card.SlackerID != "" {
			updated.SlackerID = card.SlackerID
		}
		// Dropping the stale per-pair key forces a fresh ECDH
		// derivation on the next handshake with the new public key.
		updated.PairKey = ""
		if err := m.friendStore.Update(updated); err != nil {
			m.warning = "Replace failed: " + err.Error()
			return nil
		}
		if err := m.friendStore.Save(); err != nil {
			m.warning = "Replaced in memory but failed to persist: " + err.Error()
		} else {
			m.warning = "Replaced contact card for " + label
		}
		m.channels.SetFriendChannels(m.buildFriendChannels())
		return nil
	}

	return nil
}

// confirmFriendRemoval deletes a friend from the local store and
// refreshes the sidebar's friend section. The current chat history
// view (if the user is currently viewing the friend being removed)
// is intentionally NOT cleared — they can keep reading until they
// navigate away. The next time they leave the friend chat, the
// channel will be gone and they won't be able to come back to it.
//
// Also drops any pending friend-request notification for the
// removed peer so the notifications panel doesn't stay stale.
func (m *Model) confirmFriendRemoval(userID string) tea.Cmd {
	if m.friendStore == nil || userID == "" {
		return nil
	}
	f := m.friendStore.Get(userID)
	name := userID
	if f != nil && f.Name != "" {
		name = f.Name
	}
	m.friendStore.Remove(userID)
	if err := m.friendStore.Save(); err != nil {
		m.warning = "Removed in memory but failed to persist: " + err.Error()
	} else {
		m.warning = "Removed friend " + name
	}
	// Refresh the sidebar friend section. The currently-open
	// friend chat is left on screen for reference until the user
	// navigates away.
	m.channels.SetFriendChannels(m.buildFriendChannels())
	if m.notifStore != nil {
		m.notifStore.ClearFriendRequest(userID)
	}
	return nil
}

// updateFriendStatusDisplay builds the friend status map from the
// current friend store and pushes it to the sidebar renderer.
// Called after every FriendPingMsg, FriendStatusUpdateMsg, and
// disconnect to keep the three-state sidebar indicators in sync.
func (m *Model) updateFriendStatusDisplay() {
	if m.friendStore == nil {
		return
	}
	statuses := make(map[string]FriendDisplayStatus)
	for _, f := range m.friendStore.All() {
		chID := "friend:" + f.UserID
		statuses[chID] = FriendDisplayStatus{
			Online:      f.Online,
			AwayStatus:  f.AwayStatus,
			AwayMessage: f.AwayMessage,
		}
	}
	m.channels.SetFriendStatus(statuses)
}

// sortBrowseEntries sorts a slice of BrowseEntry in place.
func sortBrowseEntries(entries []secure.BrowseEntry, sortBy string, asc bool) {
	sort.Slice(entries, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "size":
			less = entries[i].Size < entries[j].Size
		case "modified":
			less = entries[i].ModTime.Before(entries[j].ModTime)
		case "created":
			less = entries[i].CreateTime.Before(entries[j].CreateTime)
		case "type":
			ei := strings.ToLower(filepath.Ext(entries[i].Name))
			ej := strings.ToLower(filepath.Ext(entries[j].Name))
			if ei != ej {
				less = ei < ej
			} else {
				less = strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
			}
		default: // "name"
			less = strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		}
		if !asc {
			return !less
		}
		return less
	})
}

// handleBrowseRequest reads the local shared folder and returns a
// BrowseResponse. Called from the __browse_request__ handler in a
// goroutine so os.ReadDir doesn't block the Update loop.
func (m *Model) handleBrowseRequest(reqPath, sortBy, sortDir string) secure.BrowseResponse {
	if m.cfg == nil || m.cfg.SharedFolder == "" {
		return secure.BrowseResponse{Error: "no shared folder configured"}
	}
	absPath, err := secure.ValidateSharedPath(m.cfg.SharedFolder, reqPath)
	if err != nil {
		return secure.BrowseResponse{Error: err.Error()}
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return secure.BrowseResponse{Error: "cannot read directory: " + err.Error()}
	}
	var out []secure.BrowseEntry
	for _, e := range entries {
		info, _ := e.Info()
		var size int64
		var modTime time.Time
		if info != nil {
			size = info.Size()
			modTime = info.ModTime()
		}
		out = append(out, secure.BrowseEntry{
			Name:       e.Name(),
			Size:       size,
			IsDir:      e.IsDir(),
			ModTime:    modTime,
			CreateTime: modTime, // Go stdlib doesn't expose birth time portably
		})
	}
	// Sort entries: dirs first, then files, each group sorted by the
	// requested key. Default to name ascending.
	if sortBy == "" {
		sortBy = "name"
	}
	asc := sortDir != "desc"
	var dirs, files []secure.BrowseEntry
	for _, e := range out {
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sortBrowseEntries(dirs, sortBy, asc)
	sortBrowseEntries(files, sortBy, asc)
	sorted := make([]secure.BrowseEntry, 0, len(out))
	sorted = append(sorted, dirs...)
	sorted = append(sorted, files...)
	return secure.BrowseResponse{Path: reqPath, Entries: sorted}
}

// isOwnCard reports whether a contact card represents the local
// user. Matches by SlackerID, PublicKey, or Multiaddr against the
// values exposed by the local config / secure manager / P2P node, in
// that priority order. Used by both the left-click import flow and
// the right-click context menu so own cards consistently get the
// "view only" treatment instead of being offered for friending.
func (m *Model) isOwnCard(card friends.ContactCard) bool {
	if m.cfg != nil && card.SlackerID != "" && card.SlackerID == m.cfg.SlackerID {
		return true
	}
	if m.secureMgr != nil {
		if pub := m.secureMgr.OwnPublicKeyBase64(); pub != "" && card.PublicKey == pub {
			return true
		}
	}
	if m.p2pNode != nil {
		if maddr := m.p2pNode.Multiaddr(); maddr != "" && card.Multiaddr == maddr {
			return true
		}
	}
	return false
}
