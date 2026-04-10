package api

// API is the root interface provided to plugins. It gives access
// to all slackers subsystems through focused sub-interfaces.
// Each sub-interface is a stable contract — plugins depend on
// these, not on Model internals.
type API interface {
	App() App
	Messages() Messages
	Channels() Channels
	Friends() Friends
	Files() Files
	View() View
	Shortcuts() Shortcuts
	Commands() Commands
	Theme() Theme
	Events() Events
}

// App provides app lifecycle, config, and status bar access.
type App interface {
	Version() string
	Config() map[string]any        // read-only config snapshot
	SetConfig(key string, val any) // debounced save
	SetStatusBar(text string)      // bottom status message
	SetWarning(text string)        // warning message
	ClearWarning()
	IsSlackConnected() bool
	IsP2PEnabled() bool
	SlackerID() string
	MyName() string
}

// Messages provides chat message operations.
type Messages interface {
	Send(channelID, text string) error
	SendReply(channelID, parentTS, text string) error
	Edit(channelID, messageTS, newText string) error
	Delete(channelID, messageTS string) error
	React(channelID, messageTS, emoji string) error
	FetchHistory(channelID string, limit int) ([]MessageInfo, error)
	CurrentChannel() *ChannelInfo
	CurrentMessages() []MessageInfo
}

// Channels provides channel listing and management.
type Channels interface {
	List() []ChannelInfo
	Get(channelID string) *ChannelInfo
	Hide(channelID string)
	Unhide(channelID string)
	Rename(channelID, newName string)
	MarkUnread(channelID string)
	ClearUnread(channelID string)
	SelectByID(channelID string)
}

// Friends provides friend management and P2P operations.
type Friends interface {
	List() []FriendInfo
	Get(userID string) *FriendInfo
	IsOnline(userID string) bool
	SendMessage(userID, text string) error
	SendPluginMessage(userID, pluginName, data string) error
}

// Files provides file upload/download operations.
type Files interface {
	Upload(channelID, filePath string) error
	Download(url, destPath string) error
	DownloadPath() string
	SharedFolder() string
}

// View provides overlay and window management.
type View interface {
	ShowOverlay(id string, model OverlayModel)
	CloseOverlay()
	CurrentOverlay() string
	SetFocus(pane FocusPane)
	ScreenSize() (width, height int)
}

// Shortcuts provides keyboard shortcut management.
type Shortcuts interface {
	Register(action string, keys []string)
	KeysForAction(action string) []string
	AllActions() map[string][]string
}

// Commands provides slash command registration.
type Commands interface {
	Register(name, description string, run func(args []string) error)
	Run(input string) error
	List() []CommandInfo
}

// CommandInfo describes a registered command.
type CommandInfo struct {
	Name        string
	Description string
	Usage       string
}

// Theme provides read-only access to current theme colors.
type Theme interface {
	IsDark() bool
	Color(name string) string // returns the color value for a named color key
	AllColors() map[string]string
}

// Events provides pub/sub for app lifecycle events.
type Events interface {
	Subscribe(eventType string, handler EventHandler) UnsubscribeFunc
	Emit(event Event)
}
