package api

import "time"

// ChannelInfo is a read-only snapshot of a channel's state.
type ChannelInfo struct {
	ID        string
	Name      string
	IsDM      bool
	IsPrivate bool
	IsGroup   bool
	IsFriend  bool
	UserID    string // for DMs: the other user's ID
}

// MessageInfo is a read-only snapshot of a chat message.
type MessageInfo struct {
	ID        string
	ChannelID string
	UserID    string
	UserName  string
	Text      string
	Timestamp time.Time
	IsEmote   bool
	ReplyTo   string // parent message ID if this is a reply
}

// FriendInfo is a read-only snapshot of a friend's state.
type FriendInfo struct {
	UserID          string
	SlackerID       string
	Name            string
	Email           string
	Online          bool
	AwayStatus      string // "online", "away", "back", "offline"
	AwayMessage     string
	HasSharedFolder bool
	LastOnline      int64
	Multiaddr       string
	ConnectionType  string // "p2p", "e2e"
}

// FocusPane identifies which UI pane has focus.
type FocusPane int

const (
	FocusSidebar  FocusPane = iota
	FocusMessages
	FocusInput
)

// Event is a generic app event for the pub/sub system.
type Event struct {
	Type string
	Data any
}

// EventHandler processes an event.
type EventHandler func(Event)

// UnsubscribeFunc removes an event subscription.
type UnsubscribeFunc func()

// OverlayModel is the interface a plugin overlay must implement.
// It mirrors the pattern used by all existing overlays in the TUI.
type OverlayModel interface {
	Update(msg any) (OverlayModel, any) // returns updated model + optional tea.Cmd
	View() string
	SetSize(w, h int)
}
