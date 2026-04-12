// Package types defines shared domain types used across all packages.
package types

import "time"

// Channel represents a Slack channel, DM, or group conversation.
type Channel struct {
	ID          string
	Name        string
	WorkspaceID string // Slack team ID; empty for friend channels
	IsDM        bool
	IsPrivate   bool
	IsGroup     bool
	IsFriend    bool
	UserID      string // for DMs: the other user's ID
	UnreadCount int    // populated from Slack at load time (DMs/mpims from conversations.list)
}

// FileInfo represents an attached file.
type FileInfo struct {
	ID          string
	Name        string
	Size        int64
	MimeType    string
	URL         string // private download URL
	ChannelName string // channel where the file was shared
	UserName    string // who uploaded it
	Timestamp   time.Time
	// LocalPath is set on the sender side for files that haven't been
	// uploaded/served yet. Used by the cancel/upload tracker.
	LocalPath string `json:"local_path,omitempty"`
	// Uploading is true while the file is being uploaded (Slack) or
	// pending pickup (P2P). Once delivered, this becomes false.
	Uploading bool `json:"uploading,omitempty"`
}

// Reaction represents an emoji reaction on a message.
type Reaction struct {
	Emoji   string   `json:"emoji"` // shortcode (e.g. "thumbsup")
	UserIDs []string `json:"user_ids"`
	Count   int      `json:"count"`
}

// Message represents a single Slack or P2P message.
type Message struct {
	MessageID   string     `json:"message_id,omitempty"` // unique ID (Slack TS or generated UUID)
	UserID      string     `json:"user_id"`
	UserName    string     `json:"user_name"`
	Text        string     `json:"text"`
	Timestamp   time.Time  `json:"timestamp"`
	ChannelID   string     `json:"channel_id,omitempty"`
	Files       []FileInfo `json:"files,omitempty"`
	Reactions   []Reaction `json:"reactions,omitempty"`
	Replies     []Message  `json:"replies,omitempty"`  // child messages (replies)
	ReplyTo     string     `json:"reply_to,omitempty"` // parent message ID if this is a reply
	IsEncrypted bool       `json:"is_encrypted,omitempty"`
	// Pending marks a friend (P2P) message that could not be
	// delivered at send time because the peer was offline. The
	// sender stores it locally with this flag set; when the peer
	// comes back online, any Pending messages are re-sent in order
	// and the flag is cleared on success.
	Pending bool `json:"pending,omitempty"`
	// IsEmote marks a message as an emote action (/laugh, /wave).
	// Rendered with EmoteMessageStyle (italic, purple-ish) and
	// cannot be edited (only deleted). Reactions and replies
	// still work normally.
	IsEmote bool `json:"is_emote,omitempty"`
}

// User represents a Slack workspace member.
type User struct {
	ID          string
	DisplayName string
	RealName    string
}

// SearchResult represents a single message search result.
type SearchResult struct {
	Message     Message
	ChannelID   string
	ChannelName string
	Permalink   string
}

// ConnectionStatus represents the Socket Mode connection state.
type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

// String returns a human-readable connection status.
func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting..."
	case StatusConnected:
		return "connected"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Focus represents which TUI panel currently has keyboard focus.
type Focus int

const (
	FocusSidebar Focus = iota
	FocusMessages
	FocusInput
)
