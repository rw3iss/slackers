// Package types defines shared domain types used across all packages.
package types

import "time"

// Channel represents a Slack channel, DM, or group conversation.
type Channel struct {
	ID        string
	Name      string
	IsDM      bool
	IsPrivate bool
	IsGroup   bool
	UserID    string // for DMs: the other user's ID
}

// FileInfo represents an attached file.
type FileInfo struct {
	ID       string
	Name     string
	Size     int64
	MimeType string
	URL      string // private download URL
}

// Message represents a single Slack message.
type Message struct {
	UserID    string
	UserName  string
	Text      string
	Timestamp time.Time
	ChannelID string
	Files     []FileInfo
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
