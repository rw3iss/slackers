package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/secure"
	"github.com/rw3iss/slackers/internal/shortcuts"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
)

// Host implements API by wrapping pointers to the live app services.
// Created once in NewModel and shared with all plugins.
type Host struct {
	version     string
	cfg         *config.Config
	slackSvc    slackpkg.SlackService
	friendStore *friends.FriendStore
	p2pNode     *secure.P2PNode
	cmdRegistry *commands.Registry
	shortcutMap *shortcuts.ShortcutMap
	eventBus    *EventBus

	// cmdQueue carries tea.Cmd values from plugins into the Model's
	// Update loop. The Model drains this on every tick via a custom
	// PluginCmdMsg. Typed as any to avoid importing bubbletea.
	cmdQueue chan any

	mu sync.RWMutex
	// Mutable state set by the Model after each Update cycle.
	width, height int
	warning       string
	currentChID   string

	// Theme state — pushed by the Model after each theme change so
	// we don't need to import tui (which would be circular).
	isDark   bool
	colorMap map[string]string
}

// Compile-time assertion that *Host implements API.
var _ API = (*Host)(nil)

// NewHost creates a new API host. Called once during NewModel.
func NewHost(
	version string,
	cfg *config.Config,
	slackSvc slackpkg.SlackService,
	friendStore *friends.FriendStore,
	p2pNode *secure.P2PNode,
	cmdRegistry *commands.Registry,
) *Host {
	return &Host{
		version:     version,
		cfg:         cfg,
		slackSvc:    slackSvc,
		friendStore: friendStore,
		p2pNode:     p2pNode,
		cmdRegistry: cmdRegistry,
		eventBus:    NewEventBus(),
		cmdQueue:    make(chan any, 64),
		colorMap:    make(map[string]string),
	}
}

// DrainCommands returns all pending plugin commands and clears the queue.
// Called by the Model's Update loop.
func (h *Host) DrainCommands() []any {
	var cmds []any
	for {
		select {
		case cmd := <-h.cmdQueue:
			cmds = append(cmds, cmd)
		default:
			return cmds
		}
	}
}

// UpdateViewState is called by the Model after each Update to keep
// the Host's cached view state in sync.
func (h *Host) UpdateViewState(width, height int, currentChID, warning string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.width = width
	h.height = height
	h.currentChID = currentChID
	h.warning = warning
}

// UpdateThemeState is called by the Model after a theme change to
// push color data into the Host without a circular tui import.
func (h *Host) UpdateThemeState(isDark bool, colors map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isDark = isDark
	h.colorMap = colors
}

// SetCmdRegistry wires the command registry after it's been built.
// Called from NewModel after buildCommandRegistry().
func (h *Host) SetCmdRegistry(r *commands.Registry) {
	h.cmdRegistry = r
}

// ---------------------------------------------------------------------------
// API sub-interface accessors
// ---------------------------------------------------------------------------
// Several sub-interfaces share method names with different signatures
// (List, Get, Register). Host uses small wrapper types for those so
// Go's type system is satisfied. Non-colliding interfaces are
// implemented directly on *Host.

func (h *Host) App() App           { return (*hostApp)(h) }
func (h *Host) Messages() Messages { return (*hostMessages)(h) }
func (h *Host) Channels() Channels { return (*hostChannels)(h) }
func (h *Host) Friends() Friends   { return (*hostFriends)(h) }
func (h *Host) Files() Files       { return (*hostFiles)(h) }
func (h *Host) View() View         { return (*hostView)(h) }
func (h *Host) Shortcuts() Shortcuts { return (*hostShortcuts)(h) }
func (h *Host) Commands() Commands { return (*hostCommands)(h) }
func (h *Host) Theme() Theme       { return (*hostTheme)(h) }
func (h *Host) Events() Events     { return h.eventBus }

// ---------------------------------------------------------------------------
// hostApp implements App
// ---------------------------------------------------------------------------

type hostApp Host

func (a *hostApp) Version() string { return a.version }

func (a *hostApp) Config() map[string]any {
	data, err := json.Marshal(a.cfg)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

func (a *hostApp) SetConfig(key string, val any) {
	// TODO: wire to config field setter + debounced save
}

func (a *hostApp) SetStatusBar(text string) {
	select {
	case a.cmdQueue <- StatusBarCmd{Text: text}:
	default:
	}
}

func (a *hostApp) SetWarning(text string) {
	select {
	case a.cmdQueue <- WarningCmd{Text: text}:
	default:
	}
}

func (a *hostApp) ClearWarning() {
	select {
	case a.cmdQueue <- WarningCmd{Text: ""}:
	default:
	}
}

func (a *hostApp) IsSlackConnected() bool { return a.slackSvc != nil }
func (a *hostApp) IsP2PEnabled() bool     { return a.p2pNode != nil }

func (a *hostApp) SlackerID() string {
	if a.cfg == nil {
		return ""
	}
	return a.cfg.SlackerID
}

func (a *hostApp) MyName() string {
	if a.cfg == nil {
		return ""
	}
	return a.cfg.MyName
}

// ---------------------------------------------------------------------------
// hostMessages implements Messages
// ---------------------------------------------------------------------------

type hostMessages Host

func (m *hostMessages) Send(channelID, text string) error {
	if m.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return m.slackSvc.SendMessage(channelID, text)
}

func (m *hostMessages) SendReply(channelID, parentTS, text string) error {
	if m.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return m.slackSvc.SendThreadReply(channelID, parentTS, text)
}

func (m *hostMessages) Edit(channelID, messageTS, newText string) error {
	if m.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return m.slackSvc.UpdateMessage(channelID, messageTS, newText)
}

func (m *hostMessages) Delete(channelID, messageTS string) error {
	if m.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return m.slackSvc.DeleteMessage(channelID, messageTS)
}

func (m *hostMessages) React(channelID, messageTS, emoji string) error {
	if m.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return m.slackSvc.AddReaction(channelID, messageTS, emoji)
}

func (m *hostMessages) FetchHistory(channelID string, limit int) ([]MessageInfo, error) {
	if m.slackSvc == nil {
		return nil, errors.New("slack not connected")
	}
	msgs, err := m.slackSvc.FetchHistory(channelID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]MessageInfo, len(msgs))
	for i, msg := range msgs {
		out[i] = MessageInfo{
			ID:        msg.MessageID,
			ChannelID: channelID,
			UserID:    msg.UserID,
			UserName:  msg.UserName,
			Text:      msg.Text,
			Timestamp: msg.Timestamp,
			IsEmote:   msg.IsEmote,
			ReplyTo:   msg.ReplyTo,
		}
	}
	return out, nil
}

func (m *hostMessages) CurrentChannel() *ChannelInfo {
	// TODO: wire to Model — needs current channel snapshot
	return nil
}

func (m *hostMessages) CurrentMessages() []MessageInfo {
	// TODO: wire to Model — needs current message list snapshot
	return nil
}

// ---------------------------------------------------------------------------
// hostChannels implements Channels
// ---------------------------------------------------------------------------

type hostChannels Host

func (c *hostChannels) List() []ChannelInfo {
	// TODO: wire to Model — needs channel list snapshot
	return nil
}

func (c *hostChannels) Get(channelID string) *ChannelInfo {
	// TODO: wire to Model — needs channel index lookup
	return nil
}

func (c *hostChannels) Hide(channelID string) {
	// TODO: wire to Model
}

func (c *hostChannels) Unhide(channelID string) {
	// TODO: wire to Model
}

func (c *hostChannels) Rename(channelID, newName string) {
	// TODO: wire to Model
}

func (c *hostChannels) MarkUnread(channelID string) {
	// TODO: wire to Model
}

func (c *hostChannels) ClearUnread(channelID string) {
	// TODO: wire to Model
}

func (c *hostChannels) SelectByID(channelID string) {
	// TODO: wire to Model
}

// ---------------------------------------------------------------------------
// hostFriends implements Friends
// ---------------------------------------------------------------------------

type hostFriends Host

func (f *hostFriends) List() []FriendInfo {
	if f.friendStore == nil {
		return nil
	}
	all := f.friendStore.All()
	out := make([]FriendInfo, len(all))
	for i, fr := range all {
		out[i] = FriendInfo{
			UserID:          fr.UserID,
			SlackerID:       fr.SlackerID,
			Name:            fr.Name,
			Email:           fr.Email,
			Online:          fr.Online,
			AwayStatus:      fr.AwayStatus,
			AwayMessage:     fr.AwayMessage,
			HasSharedFolder: fr.HasSharedFolder,
			LastOnline:      fr.LastOnline,
			Multiaddr:       fr.Multiaddr,
			ConnectionType:  fr.ConnectionType,
		}
	}
	return out
}

func (f *hostFriends) Get(userID string) *FriendInfo {
	if f.friendStore == nil {
		return nil
	}
	fr := f.friendStore.Get(userID)
	if fr == nil {
		return nil
	}
	return &FriendInfo{
		UserID:          fr.UserID,
		SlackerID:       fr.SlackerID,
		Name:            fr.Name,
		Email:           fr.Email,
		Online:          fr.Online,
		AwayStatus:      fr.AwayStatus,
		AwayMessage:     fr.AwayMessage,
		HasSharedFolder: fr.HasSharedFolder,
		LastOnline:      fr.LastOnline,
		Multiaddr:       fr.Multiaddr,
		ConnectionType:  fr.ConnectionType,
	}
}

func (f *hostFriends) IsOnline(userID string) bool {
	if f.friendStore == nil {
		return false
	}
	fr := f.friendStore.Get(userID)
	if fr == nil {
		return false
	}
	return fr.Online
}

func (f *hostFriends) SendMessage(userID, text string) error {
	// TODO: wire to P2P send
	return errors.New("not implemented")
}

func (f *hostFriends) SendPluginMessage(userID, pluginName, data string) error {
	// TODO: wire to P2P plugin message send
	return errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// hostFiles implements Files
// ---------------------------------------------------------------------------

type hostFiles Host

func (f *hostFiles) Upload(channelID, filePath string) error {
	if f.slackSvc == nil {
		return errors.New("slack not connected")
	}
	return f.slackSvc.UploadFile(channelID, filePath)
}

func (f *hostFiles) Download(url, destPath string) error {
	if f.slackSvc == nil {
		return errors.New("slack not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return f.slackSvc.DownloadFile(ctx, url, destPath)
}

func (f *hostFiles) DownloadPath() string {
	if f.cfg == nil {
		return ""
	}
	return f.cfg.DownloadPath
}

func (f *hostFiles) SharedFolder() string {
	// TODO: wire to config shared folder path
	return ""
}

// ---------------------------------------------------------------------------
// hostView implements View
// ---------------------------------------------------------------------------

type hostView Host

func (v *hostView) ShowOverlay(id string, model OverlayModel) {
	// TODO: wire to Model overlay system
}

func (v *hostView) CloseOverlay() {
	// TODO: wire to Model overlay system
}

func (v *hostView) CurrentOverlay() string {
	// TODO: wire to Model overlay system
	return ""
}

func (v *hostView) SetFocus(pane FocusPane) {
	// TODO: wire to Model focus system
}

func (v *hostView) ScreenSize() (width, height int) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.width, v.height
}

// ---------------------------------------------------------------------------
// hostShortcuts implements Shortcuts
// ---------------------------------------------------------------------------

type hostShortcuts Host

func (s *hostShortcuts) Register(action string, keys []string) {
	if s.shortcutMap != nil {
		(*s.shortcutMap)[action] = keys
	}
}

func (s *hostShortcuts) KeysForAction(action string) []string {
	if s.shortcutMap == nil {
		return nil
	}
	return (*s.shortcutMap)[action]
}

func (s *hostShortcuts) AllActions() map[string][]string {
	if s.shortcutMap == nil {
		return nil
	}
	out := make(map[string][]string, len(*s.shortcutMap))
	for k, v := range *s.shortcutMap {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// hostCommands implements Commands
// ---------------------------------------------------------------------------

type hostCommands Host

func (c *hostCommands) Register(name, description string, run func(args []string) error) {
	if c.cmdRegistry == nil {
		return
	}
	_ = c.cmdRegistry.Register(commands.Command{
		Name:        name,
		Kind:        commands.KindCommand,
		Description: description,
		Run: func(ctx *commands.Context) commands.Result {
			if err := run(ctx.Args); err != nil {
				return commands.Result{
					Status:    commands.StatusError,
					StatusBar: err.Error(),
				}
			}
			return commands.Result{Status: commands.StatusOK}
		},
	})
}

func (c *hostCommands) Run(input string) error {
	if c.cmdRegistry == nil {
		return errors.New("command registry not available")
	}
	result := c.cmdRegistry.Run(input, nil)
	if result.Status == commands.StatusError {
		return errors.New(result.StatusBar)
	}
	return nil
}

func (c *hostCommands) List() []CommandInfo {
	if c.cmdRegistry == nil {
		return nil
	}
	all := c.cmdRegistry.All()
	out := make([]CommandInfo, len(all))
	for i, cmd := range all {
		out[i] = CommandInfo{
			Name:        cmd.Name,
			Description: cmd.Description,
			Usage:       cmd.Usage,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// hostTheme implements Theme
// ---------------------------------------------------------------------------

type hostTheme Host

func (t *hostTheme) IsDark() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isDark
}

func (t *hostTheme) Color(name string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.colorMap[name]
}

func (t *hostTheme) AllColors() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]string, len(t.colorMap))
	for k, v := range t.colorMap {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal command types queued via cmdQueue
// ---------------------------------------------------------------------------

// StatusBarCmd is queued when a plugin calls SetStatusBar.
// The Model reads Text via the exported field.
type StatusBarCmd struct{ Text string }

// WarningCmd is queued when a plugin calls SetWarning/ClearWarning.
// Empty Text means clear the warning.
type WarningCmd struct{ Text string }
