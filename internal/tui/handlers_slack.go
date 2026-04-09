package tui

// This file holds the Slack / message-action methods that used to live
// in model.go. They were extracted as part of the Phase C "split
// model.go" pass — see docs/phase-c-plan-2026-04-08.md.
//
// Most of these have both a Slack code path and a friend / P2P code
// path — they're the user-level "operate on a message" actions (edit,
// delete, react, etc.) that route to whichever backend owns the
// currently-selected chat. Notification-store helpers and the small
// channel-lookup utility also live here because they're consumed
// primarily from the same code paths.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/notifications"
	"github.com/rw3iss/slackers/internal/secure"
	"github.com/rw3iss/slackers/internal/shortcuts"
	"github.com/rw3iss/slackers/internal/types"
)

// ---- Message actions ---------------------------------------------------

func (m *Model) editMessage(messageID, newText string) {
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Edit failed: message not found"
		return
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only edit your own messages"
		return
	}
	if strings.TrimSpace(newText) == "" {
		m.warning = "Edit cancelled (empty body)"
		return
	}
	if m.currentCh == nil {
		return
	}

	if m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		// Optimistically update local state.
		m.messages.EditMessageLocal(messageID, newText)
		if m.friendHistory != nil {
			m.friendHistory.EditMessage(peerUID, messageID, newText)
			go m.friendHistory.Save(peerUID)
		}
		if msgs, ok := m.friendMessages[peerUID]; ok {
			if target := findFriendMsgPtr(msgs, messageID); target != nil {
				target.Text = newText
			}
		}
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:        secure.MsgTypeEdit,
			TargetMsgID: messageID,
			Text:        newText,
			SenderID:    peerUID,
			Timestamp:   time.Now().Unix(),
		})
		m.warning = "Message edited"
		return
	}

	if m.slackSvc != nil {
		channelID := m.currentCh.ID
		go m.slackSvc.UpdateMessage(channelID, messageID, newText)
		m.messages.EditMessageLocal(messageID, newText)
		m.warning = "Message edited"
	}
}

// isMyMessage returns true if the given message was authored by the local user.
// Delegates to MessageViewModel.IsMyUserID so the same three-way check
// (Slack UID, slacker ID, legacy "me") is used everywhere — this is
// the single source of truth for "is this me" across all contexts.
func (m *Model) isMyMessage(msg types.Message) bool {
	return m.messages.IsMyUserID(msg.UserID)
}

// requestMessageDelete handles a user-initiated delete: validates authorship,
// then prompts in the status bar for confirmation.
func (m *Model) requestMessageDelete(messageID string) {
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Message not found"
		return
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only delete your own messages"
		return
	}
	m.pendingDeleteMsgID = messageID
	m.warning = "Delete this message? (y to confirm, Esc to cancel)"
}

// cancelUpload performs the actual cancellation for an in-flight file
// upload identified by "<msgID>|<fileID>". For Slack uploads it cancels
// the context (the HTTP request may complete in the background but the
// result is discarded). For P2P uploads it removes the file from the
// node's serving table and notifies the peer so they can clean up.
// In both cases the file is removed from the local message and the view
// is refreshed.
func (m *Model) cancelUpload(key string) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 {
		return
	}
	msgID, fileID := parts[0], parts[1]

	// Slack: cancel the context if any.
	if cancel, ok := m.uploadCancels[key]; ok {
		if cancel != nil {
			cancel()
		}
		delete(m.uploadCancels, key)
	}

	// P2P friend channel: tell the peer to drop the offer.
	if m.currentCh != nil && m.currentCh.IsFriend && m.p2pNode != nil {
		_ = m.p2pNode.CancelFileOffer(m.currentCh.UserID, fileID)
		// Also update the friend message cache so a re-render shows
		// the file removed.
		if msgs, ok := m.friendMessages[m.currentCh.UserID]; ok {
			for i := range msgs {
				if msgs[i].MessageID != msgID {
					continue
				}
				for fi := range msgs[i].Files {
					if msgs[i].Files[fi].ID == fileID {
						msgs[i].Files = append(msgs[i].Files[:fi], msgs[i].Files[fi+1:]...)
						break
					}
				}
				break
			}
			m.friendMessages[m.currentCh.UserID] = msgs
			if m.friendHistory != nil {
				go m.friendHistory.Save(m.currentCh.UserID)
			}
		}
	}

	m.messages.RemoveFile(msgID, fileID)
	m.warning = "Upload cancelled"
}

// confirmMessageDelete performs the actual deletion that was requested via
// requestMessageDelete. Routes to Slack API or P2P delete request as appropriate.
func (m *Model) confirmMessageDelete() tea.Cmd {
	messageID := m.pendingDeleteMsgID
	m.pendingDeleteMsgID = ""
	m.warning = ""
	if messageID == "" {
		return nil
	}
	mm := m.messages.MessageByID(messageID)
	if mm == nil {
		m.warning = "Message not found"
		return nil
	}
	if !m.isMyMessage(*mm) {
		m.warning = "You can only delete your own messages"
		return nil
	}

	// Friend / P2P channel: send delete request, wait for ack to delete locally.
	if m.currentCh != nil && m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:        secure.MsgTypeDelete,
			TargetMsgID: messageID,
			SenderID:    peerUID,
			Timestamp:   time.Now().Unix(),
		})
		// Optimistically delete locally — the peer's ack confirms persistence
		// on their side, but we don't want to leave the message hanging on
		// our screen if the peer is briefly unreachable.
		m.messages.DeleteMessageLocal(messageID)
		if m.friendHistory != nil {
			m.friendHistory.DeleteMessage(peerUID, messageID)
			go m.friendHistory.Save(peerUID)
		}
		// Also drop from in-memory cache.
		if msgs, ok := m.friendMessages[peerUID]; ok {
			m.friendMessages[peerUID] = deleteFriendMessage(msgs, messageID)
		}
		m.warning = "Message deleted"
		return nil
	}

	// Slack channel: call API, then drop locally.
	if m.slackSvc != nil && m.currentCh != nil {
		channelID := m.currentCh.ID
		go m.slackSvc.DeleteMessage(channelID, messageID)
		m.messages.DeleteMessageLocal(messageID)
		m.warning = "Message deleted"
		return nil
	}
	return nil
}

// toggleReaction adds or removes the user's reaction on a message.
//
// The detection step uses MessageViewModel.RemoveMyReactionsFromEmoji,
// which walks every reaction group for the given emoji and strips
// entries whose user ID matches any of the three identities the local
// user is known by (Slack UID, slacker ID, legacy "me"). Its return
// value tells us whether the user had already reacted — and, if so,
// the actual stored alias(es) we removed, which the persistence layer
// needs to mirror the same cleanup.
//
// The add path uses a canonical storeID derived from the strongest
// identity currently available: Slack workspace UID if authed,
// otherwise the slacker ID, otherwise the legacy "me" fallback.
// Writing new reactions with a canonical ID (instead of whichever
// was set at the moment) means future toggles will find them via
// IsMyUserID regardless of whether the Slack auth state has changed
// between sessions.
func (m *Model) toggleReaction(messageID, emoji string) {
	if m.currentCh == nil {
		return
	}

	// Collapse any existing reaction groups for this emoji that
	// contain one of my identities. If anything was removed, this
	// is an "unreact" — otherwise it's a fresh add.
	removedIDs := m.messages.RemoveMyReactionsFromEmoji(messageID, emoji)
	hasReacted := len(removedIDs) > 0

	// Pick the canonical storeID for a new reaction. Prefer the Slack
	// workspace UID (matches what Slack returns in API responses, so
	// a subsequent history reload keeps the reaction owned by us),
	// fall back to the slacker ID (stable across friend-only installs),
	// then to the legacy "me" alias.
	storeID := "me"
	if m.myUserID != "" {
		storeID = m.myUserID
	} else if m.cfg != nil && m.cfg.SlackerID != "" {
		storeID = m.cfg.SlackerID
	}

	if m.currentCh.IsFriend && m.p2pNode != nil {
		peerUID := m.currentCh.UserID
		if hasReacted {
			if m.friendHistory != nil {
				// Mirror the view-level cleanup into the persisted
				// cache. One call strips every matching alias in one
				// pass so historical duplicates collapse correctly.
				m.friendHistory.RemoveAllReactionAliases(peerUID, messageID, emoji, removedIDs)
				go m.friendHistory.Save(peerUID)
			}
			go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
				Type:          secure.MsgTypeReactionRemove,
				TargetMsgID:   messageID,
				ReactionEmoji: emoji,
				SenderID:      peerUID,
				Timestamp:     time.Now().Unix(),
			})
			return
		}
		m.messages.AddReactionLocal(messageID, emoji, storeID)
		if m.friendHistory != nil {
			m.friendHistory.UpdateReaction(peerUID, messageID, emoji, storeID)
			go m.friendHistory.Save(peerUID)
		}
		go m.p2pNode.SendMessage(peerUID, secure.P2PMessage{
			Type:          secure.MsgTypeReaction,
			TargetMsgID:   messageID,
			ReactionEmoji: emoji,
			SenderID:      peerUID,
			Timestamp:     time.Now().Unix(),
		})
		return
	}

	if m.slackSvc != nil {
		channelID := m.currentCh.ID
		if hasReacted {
			// The view has already been cleaned up above; just tell
			// Slack to drop its server-side copy. Slack dedupes by
			// (channel, ts, emoji, user) so we only need the one call.
			go m.slackSvc.RemoveReaction(channelID, messageID, emoji)
			return
		}
		m.messages.AddReactionLocal(messageID, emoji, storeID)
		go m.slackSvc.AddReaction(channelID, messageID, emoji)
	}
}

// markSlackRead propagates the user's read state for a Slack channel
// upstream via `conversations.mark`, so other Slack clients (web,
// mobile, official desktop) see the channel as read immediately.
//
// This is the outbound half of the two-way read-state sync: every
// time slackers clears a channel's unread indicator locally we also
// tell Slack's server where our read cursor now sits. Fire-and-forget
// goroutine — failure to mark is not user-visible and will be retried
// on the next open of the same channel.
//
// Friend channels are no-ops (no Slack server involved). Channels
// without a known latest-message timestamp are skipped because
// Slack's API requires a valid ts to move the cursor.
func (m *Model) markSlackRead(ch *types.Channel) {
	if ch == nil || ch.IsFriend {
		return
	}
	if m.slackSvc == nil {
		return
	}
	ts, ok := m.lastSeen[ch.ID]
	if !ok || ts == "" || ts == "0" {
		return
	}
	channelID := ch.ID
	cursor := ts
	go func() {
		if err := m.slackSvc.MarkConversation(channelID, cursor); err != nil {
			// Log only — no need to surface to the user, the next
			// open of the same channel will retry.
			// (debug.Log happens inside the client wrapper.)
			_ = err
		}
	}()
}

// ---- Channel header / secure indicator --------------------------------

func (m *Model) setChannelHeader() {
	if m.currentCh == nil {
		return
	}
	// Switching channels always closes the output view —
	// the user is going back to chat content. Every channel
	// switch (sidebar click, Ctrl-N next unread, /go command,
	// search-result jump, friend chat open) routes through
	// setChannelHeader, so this single line covers them all.
	m.outputActive = false
	prefix := "#"
	if m.currentCh.IsFriend {
		prefix = ""
	}
	m.messages.SetChannelName(prefix + m.channels.displayName(*m.currentCh))
	m.messages.SetSecureLabel(m.secureIndicator())
	m.messages.SetIsFriendChannel(m.currentCh.IsFriend)
	if m.currentCh.IsFriend {
		// Show the configured shortcut for "friend_details" so the
		// user can see how to open the friend config from the chat.
		hint := ""
		if keys := shortcuts.KeysForAction(m.shortcutMap, "friend_details"); len(keys) > 0 {
			hint = keys[0]
		}
		m.messages.SetFriendDetailsHint(hint)
	} else {
		m.messages.SetFriendDetailsHint("")
	}
}

// secureIndicator returns a status label for the current channel's secure state.
func (m *Model) secureIndicator() string {
	if m.currentCh == nil {
		return ""
	}
	// Friend channels: always end-to-end encrypted over libp2p (P2P).
	// Show online/offline state of the secure tunnel.
	if m.currentCh.IsFriend && m.friendStore != nil {
		f := m.friendStore.Get(m.currentCh.UserID)
		if f != nil {
			if f.Online {
				return " 🔒 secure p2p"
			}
			return " 🔓 p2p offline"
		}
		return ""
	}
	if m.secureMgr == nil || !m.currentCh.IsDM {
		return ""
	}
	if !isWhitelisted(m.cfg.SecureWhitelist, m.currentCh.UserID) {
		return ""
	}
	sess := m.secureMgr.GetSession(m.currentCh.UserID)
	if sess == nil {
		return ""
	}
	return " [" + sess.State.String() + "]"
}

// decryptMessages decrypts any E2E encrypted messages in the list using the secure manager.
func (m *Model) decryptMessages(msgs []types.Message) []types.Message {
	if m.secureMgr == nil {
		return msgs
	}
	for i, msg := range msgs {
		if secure.IsEncryptedMessage(msg.Text) {
			plaintext, err := m.secureMgr.DecryptMessage(msg.UserID, msg.Text)
			if err == nil {
				msgs[i].Text = "🔒 " + plaintext
			} else {
				msgs[i].Text = "🔒 [encrypted message]"
			}
		}
	}
	return msgs
}

// ---- Notification-store helpers ---------------------------------------

// clearChannelNotifs removes any notifications tied to the given
// channel ID and persists. Used when the user opens a channel so the
// notifications view stays in sync with what's actually unread.
func (m *Model) clearChannelNotifs(channelID string) {
	if m.notifStore == nil || channelID == "" {
		return
	}
	if m.notifStore.ClearChannel(channelID) > 0 {
	}
}

// activateNotification navigates the user to the chat / message that
// owns the notification, then clears the notification (and any
// matching unread state on the channel).
func (m *Model) activateNotification(n notifications.Notification) tea.Cmd {
	if m.notifStore == nil {
		return nil
	}

	switch n.Type {
	case notifications.TypeFriendRequest:
		// Re-open the friend request modal with the cached identity.
		if m.friendRequest = NewIncomingFriendRequest(n.UserID, n.UserName, n.FriendPublicKey, n.FriendMultiaddr); true {
			m.friendRequest.SetSize(m.width, m.height)
			m.overlay = overlayFriendRequest
		}
		// The notification will be cleared when the user accepts /
		// rejects from the modal (handled in the FriendRequestSentMsg
		// + friend_accept paths) — we leave it intact for now so it
		// stays in the list if the user just peeks.
		return nil

	case notifications.TypeUnreadMessage, notifications.TypeReaction:
		// Drop the notification and any siblings from the same channel,
		// then switch to that channel.
		m.notifStore.ClearChannel(n.ChannelID)
		ch := m.lookupChannelByID(n.ChannelID)
		if ch == nil {
			m.warning = "Channel not found for notification"
			return nil
		}
		if m.messages.InThreadMode() {
			m.messages.ExitThreadMode()
		}
		m.currentCh = ch
		m.channels.SelectByID(ch.ID)
		m.channels.ClearUnread(ch.ID)
		m.markSlackRead(ch)
		m.clearChannelNotifs(ch.ID)
		m.setChannelHeader()
		m.saveLastChannel(ch.ID)
		if ch.IsFriend {
			m.loadFriendHistory(ch.UserID)
			return nil
		}
		return loadHistoryCmd(m.slackSvc, ch.ID)
	}
	return nil
}

// lookupChannelByID returns the *types.Channel for an ID by walking
// the sidebar's known channel list (Slack + friend channels).
func (m *Model) lookupChannelByID(id string) *types.Channel {
	for _, ch := range m.channels.AllChannels() {
		if ch.ID == id {
			cp := ch
			return &cp
		}
	}
	return nil
}

// recordUnreadMessage drops a TypeUnreadMessage notification into the
// store. Called by message-arrival code paths when the user is not
// currently viewing the originating channel.
func (m *Model) recordUnreadMessage(channelID, messageID, userID, userName, text string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:      notifications.TypeUnreadMessage,
		ChannelID: channelID,
		MessageID: messageID,
		UserID:    userID,
		UserName:  userName,
		Text:      text,
	})
}

// recordReaction drops a TypeReaction notification.
func (m *Model) recordReaction(channelID, messageID, reactorID, reactorName, emoji, targetText string) {
	if m.notifStore == nil {
		return
	}
	m.notifStore.Add(notifications.Notification{
		Type:             notifications.TypeReaction,
		ChannelID:        channelID,
		MessageID:        messageID,
		UserID:           reactorID,
		UserName:         reactorName,
		Emoji:            emoji,
		TargetMessageTxt: targetText,
	})
}
