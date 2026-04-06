# Friends List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global Friends system enabling private P2P chat between Slackers users, independent of any Slack workspace.

**Architecture:** Friends are stored locally in config as a list of `Friend` records (user ID, display name, multiaddr, public key). The P2P node handles friend requests, pings, and messaging. A new "friends" channel section renders in the sidebar before workspace channels. Friend channels use the existing message view but route through P2P instead of the Slack API. The app can start in "friends-only" mode when no workspace is configured.

**Tech Stack:** Go, Bubbletea, libp2p (existing P2P node), ChaCha20-Poly1305 (existing crypto), JSON config persistence.

---

## File Structure

### New files
| File | Responsibility |
|------|---------------|
| `internal/friends/friends.go` | Friend struct, FriendStore (load/save/add/remove), friend request protocol |
| `internal/friends/friends_test.go` | Tests for FriendStore, request/accept logic |
| `internal/tui/friendrequest.go` | Friend request popup overlay (send/receive/confirm) |

### Modified files
| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `Friends []Friend` field, relax `Validate()` |
| `internal/types/types.go` | Add `IsFriend bool` to Channel struct |
| `internal/secure/p2p.go` | Add "friend_request", "friend_accept", "friend_ping" message types |
| `internal/tui/model.go` | Friends-first init, friend channels in sidebar, friend chat routing, friend request handling |
| `internal/tui/channels.go` | New "friends" section key/label, render before workspace channels |
| `internal/tui/messages.go` | No changes needed (already supports AppendMessage + SetMessages) |
| `internal/tui/help.go` | Add friend shortcut entry |
| `internal/shortcuts/defaults.json` | Add `befriend` shortcut |
| `internal/tui/keymap.go` | Add `Befriend` binding |
| `cmd/slackers/main.go` | Allow TUI launch without workspace tokens |

---

## Task 1: Friend data model and persistence

**Files:**
- Create: `internal/friends/friends.go`
- Create: `internal/friends/friends_test.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write the Friend struct and FriendStore**

```go
// internal/friends/friends.go
package friends

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Friend represents a befriended Slackers user.
type Friend struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`  // base64 X25519 public key
	Multiaddr string `json:"multiaddr"`  // libp2p multiaddr for direct connection
	AddedAt   int64  `json:"added_at"`   // unix timestamp
	Online    bool   `json:"-"`          // runtime state, not persisted
}

// FriendRequest is the wire format for friend request messages over P2P.
type FriendRequest struct {
	Type      string `json:"type"`       // "friend_request", "friend_accept", "friend_reject"
	Name      string `json:"name"`       // sender's display name
	PublicKey string `json:"public_key"` // sender's public key (base64)
	Multiaddr string `json:"multiaddr"`  // sender's multiaddr
}

// FriendStore manages the local friends list with persistence.
type FriendStore struct {
	friends []Friend
	path    string
	mu      sync.RWMutex
}

// NewFriendStore creates a store that reads/writes to the given JSON file.
func NewFriendStore(path string) *FriendStore {
	return &FriendStore{path: path}
}

// DefaultPath returns the default friends file path.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "friends.json")
}

// Load reads friends from disk. Returns empty list if file doesn't exist.
func (s *FriendStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.friends = nil
			return nil
		}
		return fmt.Errorf("reading friends: %w", err)
	}
	return json.Unmarshal(data, &s.friends)
}

// Save writes friends to disk.
func (s *FriendStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.friends, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// All returns a copy of the friends list.
func (s *FriendStore) All() []Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Friend, len(s.friends))
	copy(out, s.friends)
	return out
}

// Add adds a friend. Returns error if already exists.
func (s *FriendStore) Add(f Friend) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.friends {
		if existing.UserID == f.UserID {
			return fmt.Errorf("friend %s already exists", f.UserID)
		}
	}
	if f.AddedAt == 0 {
		f.AddedAt = time.Now().Unix()
	}
	s.friends = append(s.friends, f)
	return nil
}

// Remove removes a friend by user ID.
func (s *FriendStore) Remove(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			s.friends = append(s.friends[:i], s.friends[i+1:]...)
			return
		}
	}
}

// Get returns a friend by user ID, or nil if not found.
func (s *FriendStore) Get(userID string) *Friend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			return &s.friends[i]
		}
	}
	return nil
}

// SetOnline updates the online status for a friend.
func (s *FriendStore) SetOnline(userID string, online bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.friends {
		if f.UserID == userID {
			s.friends[i].Online = online
			return
		}
	}
}

// Count returns the number of friends.
func (s *FriendStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.friends)
}
```

- [ ] **Step 2: Write tests for FriendStore**

```go
// internal/friends/friends_test.go
package friends

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFriendStore_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "friends.json")
	store := NewFriendStore(path)

	f := Friend{UserID: "U123", Name: "Alice", PublicKey: "abc123"}
	if err := store.Add(f); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := store.Get("U123")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want Alice", got.Name)
	}
	if got.AddedAt == 0 {
		t.Error("AddedAt should be set automatically")
	}
}

func TestFriendStore_DuplicateAdd(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	err := store.Add(Friend{UserID: "U1", Name: "B"})
	if err == nil {
		t.Fatal("expected error on duplicate add")
	}
}

func TestFriendStore_Remove(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	store.Add(Friend{UserID: "U2", Name: "B"})
	store.Remove("U1")
	if store.Count() != 1 {
		t.Errorf("Count = %d, want 1", store.Count())
	}
	if store.Get("U1") != nil {
		t.Error("U1 should be removed")
	}
}

func TestFriendStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "friends.json")

	// Save
	s1 := NewFriendStore(path)
	s1.Add(Friend{UserID: "U1", Name: "Alice", PublicKey: "key1"})
	s1.Add(Friend{UserID: "U2", Name: "Bob", PublicKey: "key2"})
	if err := s1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload
	s2 := NewFriendStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s2.Count() != 2 {
		t.Errorf("Count = %d, want 2", s2.Count())
	}
	if s2.Get("U1").Name != "Alice" {
		t.Error("Alice not found after reload")
	}
}

func TestFriendStore_LoadMissing(t *testing.T) {
	store := NewFriendStore("/nonexistent/path/friends.json")
	if err := store.Load(); err != nil {
		t.Fatalf("Load should not error on missing file: %v", err)
	}
	if store.Count() != 0 {
		t.Errorf("Count = %d, want 0", store.Count())
	}
}

func TestFriendStore_OnlineStatus(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	store.SetOnline("U1", true)
	if !store.Get("U1").Online {
		t.Error("should be online")
	}
	store.SetOnline("U1", false)
	if store.Get("U1").Online {
		t.Error("should be offline")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go test ./internal/friends/ -v`
Expected: All 5 tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/friends/
git commit -m "feat: add Friend data model and FriendStore with persistence"
```

---

## Task 2: P2P protocol extensions for friend requests and pings

**Files:**
- Modify: `internal/secure/p2p.go`

- [ ] **Step 1: Add friend request/ping constants to P2PMessage types**

In `internal/secure/p2p.go`, add after line 22:

```go
const (
	P2PProtocol = protocol.ID("/slackers/msg/1.0.0")

	// P2P message types.
	MsgTypeMessage       = "message"
	MsgTypePing          = "ping"
	MsgTypePong          = "pong"
	MsgTypeFriendRequest = "friend_request"
	MsgTypeFriendAccept  = "friend_accept"
	MsgTypeFriendReject  = "friend_reject"
)
```

- [ ] **Step 2: Add RegisterPeer method to P2PNode**

This allows registering a peer's mapping before they connect to us (needed for friend pings):

```go
// RegisterPeer maps a Slack user ID to a libp2p peer ID without connecting.
// Used when loading saved friends to prepare for incoming connections.
func (n *P2PNode) RegisterPeer(slackUserID string, peerID peer.ID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peerMap[slackUserID] = peerID
	n.slackMap[peerID] = slackUserID
}
```

- [ ] **Step 3: Add PingPeer method to P2PNode**

```go
// PingPeer sends a ping to a peer and returns true if they respond.
func (n *P2PNode) PingPeer(slackUserID string) bool {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return false
	}

	if n.host.Network().Connectedness(peerID) != network.Connected {
		return false
	}

	msg := P2PMessage{Type: MsgTypePing, SenderID: slackUserID, Timestamp: time.Now().Unix()}
	stream, err := n.host.NewStream(n.ctx, peerID, P2PProtocol)
	if err != nil {
		return false
	}
	defer stream.Close()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, err = stream.Write(data)
	return err == nil
}
```

Add `"time"` to the imports.

- [ ] **Step 4: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/secure/p2p.go
git commit -m "feat: add friend request/ping P2P message types and PingPeer method"
```

---

## Task 3: IsFriend channel type and sidebar "Friends" section

**Files:**
- Modify: `internal/types/types.go`
- Modify: `internal/tui/channels.go`

- [ ] **Step 1: Add IsFriend to Channel struct**

In `internal/types/types.go`, add field to Channel:

```go
type Channel struct {
	ID        string
	Name      string
	IsDM      bool
	IsPrivate bool
	IsGroup   bool
	IsFriend  bool   // P2P friend channel (not a Slack channel)
	UserID    string
}
```

- [ ] **Step 2: Add "friends" section to channels.go**

In `internal/tui/channels.go`, update `sectionKey()`:

```go
func sectionKey(ch types.Channel) string {
	switch {
	case ch.IsFriend:
		return "friends"
	case ch.IsDM:
		return "dm"
	case ch.IsGroup:
		return "group"
	case ch.IsPrivate:
		return "private"
	default:
		return "channels"
	}
}
```

Update `sectionLabel()`:

```go
func sectionLabel(key string) string {
	switch key {
	case "friends":
		return "Friends"
	case "channels":
		return "# Channels"
	case "private":
		return "# Private"
	case "dm":
		return "@ Direct Messages"
	case "group":
		return "Group Chats"
	}
	return key
}
```

- [ ] **Step 3: Add SetFriendChannels method to ChannelListModel**

This prepends friend channels to the channel list, ensuring they render first:

```go
// SetFriendChannels sets the friend channels that render at the top of the sidebar.
func (m *ChannelListModel) SetFriendChannels(friends []types.Channel) {
	// Remove existing friend channels.
	var workspace []types.Channel
	for _, ch := range m.channels {
		if !ch.IsFriend {
			workspace = append(workspace, ch)
		}
	}
	// Prepend friends.
	m.channels = append(friends, workspace...)
	m.buildRows()
}
```

- [ ] **Step 4: Update renderItem to show online/offline for friends**

In the `renderItem` method, add friend styling:

```go
// Inside renderItem(), before the existing name rendering:
if ch.IsFriend {
	// Grey out offline friends, bright for online.
	if m.unread[ch.ID] { // we'll use unread as "online" indicator for friends
		name = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Render(name)
	} else {
		name = lipgloss.NewStyle().Foreground(ColorMuted).Render(name)
	}
}
```

- [ ] **Step 5: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/tui/channels.go
git commit -m "feat: add IsFriend channel type and Friends sidebar section"
```

---

## Task 4: Friend request overlay UI

**Files:**
- Create: `internal/tui/friendrequest.go`

- [ ] **Step 1: Create the friend request overlay**

```go
// internal/tui/friendrequest.go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FriendRequestAction represents what happened with a friend request.
type FriendRequestAction int

const (
	FriendRequestNone FriendRequestAction = iota
	FriendRequestSent
	FriendRequestAccepted
	FriendRequestRejected
)

// FriendRequestSentMsg signals that a friend request was initiated.
type FriendRequestSentMsg struct {
	UserID string
	Name   string
}

// FriendRequestReceivedMsg signals an incoming friend request.
type FriendRequestReceivedMsg struct {
	UserID    string
	Name      string
	PublicKey string
	Multiaddr string
}

// FriendRequestRespondMsg signals the user responded to a friend request.
type FriendRequestRespondMsg struct {
	UserID   string
	Name     string
	Accepted bool
	PublicKey string
	Multiaddr string
}

// FriendRequestModel provides the friend request popup overlay.
type FriendRequestModel struct {
	userID    string
	userName  string
	incoming  bool   // true = received request, false = sending confirmation
	selected  int    // 0 = accept/send, 1 = cancel/reject
	width     int
	height    int
	publicKey string
	multiaddr string
}

// NewOutgoingFriendRequest creates a confirmation popup for sending a friend request.
func NewOutgoingFriendRequest(userID, userName string) FriendRequestModel {
	return FriendRequestModel{
		userID:   userID,
		userName: userName,
		incoming: false,
	}
}

// NewIncomingFriendRequest creates a popup for an incoming friend request.
func NewIncomingFriendRequest(userID, userName, pubKey, multiaddr string) FriendRequestModel {
	return FriendRequestModel{
		userID:    userID,
		userName:  userName,
		incoming:  true,
		publicKey: pubKey,
		multiaddr: multiaddr,
	}
}

func (m *FriendRequestModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m FriendRequestModel) Update(msg tea.Msg) (FriendRequestModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			m.selected = 0
		case "right", "l":
			m.selected = 1
		case "tab":
			m.selected = (m.selected + 1) % 2
		case "enter":
			if m.incoming {
				return m, func() tea.Msg {
					return FriendRequestRespondMsg{
						UserID:    m.userID,
						Name:      m.userName,
						Accepted:  m.selected == 0,
						PublicKey: m.publicKey,
						Multiaddr: m.multiaddr,
					}
				}
			}
			if m.selected == 0 {
				return m, func() tea.Msg {
					return FriendRequestSentMsg{UserID: m.userID, Name: m.userName}
				}
			}
			// Cancel — close overlay (handled by model.go via esc)
			return m, func() tea.Msg { return FriendRequestRespondMsg{Accepted: false} }
		}
	}
	return m, nil
}

func (m FriendRequestModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	nameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	var b strings.Builder

	if m.incoming {
		b.WriteString(titleStyle.Render("Friend Request"))
		b.WriteString("\n\n")
		b.WriteString("  " + nameStyle.Render(m.userName) + " wants to be your friend.\n")
		b.WriteString("  Accept to enable private P2P chat.\n")
	} else {
		b.WriteString(titleStyle.Render("Add Friend"))
		b.WriteString("\n\n")
		b.WriteString("  Send a friend request to " + nameStyle.Render(m.userName) + "?\n")
		b.WriteString("  This enables private P2P chat outside Slack.\n")
	}

	b.WriteString("\n")

	yesLabel := "  Send  "
	noLabel := " Cancel "
	if m.incoming {
		yesLabel = " Accept "
		noLabel = " Reject "
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Background(lipgloss.Color("236"))
	inactiveStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if m.selected == 0 {
		b.WriteString("  " + activeStyle.Render("["+yesLabel+"]") + "  " + inactiveStyle.Render(" "+noLabel+" "))
	} else {
		b.WriteString("  " + inactiveStyle.Render(" "+yesLabel+" ") + "  " + activeStyle.Render("["+noLabel+"]"))
	}

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Tab: switch | Enter: confirm | Esc: cancel"))

	content := b.String()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(min(50, m.width-4))

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
```

- [ ] **Step 2: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add internal/tui/friendrequest.go
git commit -m "feat: add friend request overlay UI (send/receive/confirm)"
```

---

## Task 5: Befriend shortcut and keymap

**Files:**
- Modify: `internal/shortcuts/defaults.json`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help.go`

- [ ] **Step 1: Add befriend shortcut**

In `internal/shortcuts/defaults.json`, add before the closing `}`:

```json
  "toggle_input_mode": ["ctrl+\\"],
  "befriend": ["ctrl+b"]
```

(Remove trailing comma from previous line, add comma after `toggle_input_mode`.)

- [ ] **Step 2: Add Befriend to KeyMap**

In `internal/tui/keymap.go`, add to the KeyMap struct:

```go
	ToggleInputMode  key.Binding
	Befriend         key.Binding
```

In `BuildKeyMap()`, add:

```go
	ToggleInputMode: binding(sm, "toggle_input_mode", "ctrl+\\", "input mode"),
	Befriend:        binding(sm, "befriend", "ctrl+b", "befriend user"),
```

- [ ] **Step 3: Add to help layout**

In `internal/tui/help.go`, add to the "App" section entries:

```go
	{"befriend", "Send friend request to current DM user", ""},
```

- [ ] **Step 4: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/shortcuts/defaults.json internal/tui/keymap.go internal/tui/help.go
git commit -m "feat: add Ctrl+B befriend shortcut"
```

---

## Task 6: Wire friends into the TUI model

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `cmd/slackers/main.go`

This is the largest task. It wires together all the pieces:

- [ ] **Step 1: Add friend overlay constant and model fields**

In model.go overlay constants, add:

```go
	overlayWhitelist
	overlayFriendRequest
```

In Model struct, add:

```go
	// Friends
	friendStore     *friends.FriendStore
	friendRequest   FriendRequestModel
	friendMessages  map[string][]types.Message // userID -> message history
```

Add import: `"github.com/rw3iss/slackers/internal/friends"`

- [ ] **Step 2: Update NewModel to accept FriendStore and load friend channels**

Add `friendStore *friends.FriendStore` parameter to `NewModel()`. In the return struct:

```go
	friendStore:    friendStore,
	friendMessages: make(map[string][]types.Message),
```

- [ ] **Step 3: Add helper to build friend channels from store**

```go
// friendChannels builds Channel entries from the friend store.
func (m *Model) friendChannels() []types.Channel {
	if m.friendStore == nil {
		return nil
	}
	var channels []types.Channel
	for _, f := range m.friendStore.All() {
		ch := types.Channel{
			ID:       "friend:" + f.UserID,
			Name:     f.Name,
			IsFriend: true,
			IsDM:     true,
			UserID:   f.UserID,
		}
		channels = append(channels, ch)
	}
	return channels
}
```

- [ ] **Step 4: Load friends on Init, before workspace data**

In `Init()`, add at the start of the cmds list:

```go
	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		splashTimerCmd(),
		loadFriendsCmd(m.friendStore, m.p2pNode),
		// ... existing cmds
	}
```

Add the command function:

```go
// FriendsLoadedMsg carries the friend channels to display.
type FriendsLoadedMsg struct {
	Channels []types.Channel
	Online   map[string]bool
}

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
			// Ping each friend to check online status.
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
```

- [ ] **Step 5: Handle FriendsLoadedMsg in Update**

```go
case FriendsLoadedMsg:
	if len(msg.Channels) > 0 {
		m.channels.SetFriendChannels(msg.Channels)
		// Mark online friends as "unread" (used for online indicator).
		for uid, on := range msg.Online {
			if on {
				m.channels.MarkUnread("friend:" + uid)
			}
		}
	}
	return m, nil
```

- [ ] **Step 6: Handle Befriend shortcut**

In the global shortcuts switch block, add:

```go
case key.Matches(msg, m.keymap.Befriend):
	if m.currentCh != nil && m.currentCh.IsDM && m.currentCh.UserID != "" {
		// Check if already a friend.
		if m.friendStore != nil && m.friendStore.Get(m.currentCh.UserID) != nil {
			m.warning = "Already friends with " + m.currentCh.Name
			return m, nil
		}
		userName := m.currentCh.Name
		m.friendRequest = NewOutgoingFriendRequest(m.currentCh.UserID, userName)
		m.friendRequest.SetSize(m.width, m.height)
		m.overlay = overlayFriendRequest
	} else {
		m.warning = "Select a DM channel to befriend"
	}
	return m, nil
```

- [ ] **Step 7: Handle friend request overlay in key handler**

After the whitelist overlay handler:

```go
if m.overlay == overlayFriendRequest {
	if msg.String() == "esc" {
		m.overlay = overlayNone
		return m, nil
	}
	var cmd tea.Cmd
	m.friendRequest, cmd = m.friendRequest.Update(msg)
	return m, cmd
}
```

- [ ] **Step 8: Handle FriendRequestSentMsg — send request over P2P or Slack DM**

```go
case FriendRequestSentMsg:
	m.overlay = overlayNone
	if m.p2pNode != nil && m.secureMgr != nil {
		// Try P2P friend request.
		pubKey := m.secureMgr.OwnPublicKeyBase64()
		multiaddr := m.p2pNode.Multiaddr()
		go func() {
			req := secure.P2PMessage{
				Type:     secure.MsgTypeFriendRequest,
				Text:     pubKey + "|" + multiaddr,
				SenderID: msg.UserID,
			}
			if err := m.p2pNode.SendMessage(msg.UserID, req); err != nil {
				// Fallback: send invite via Slack DM.
				inviteText := fmt.Sprintf("Hey! I'd like to chat privately using Slackers TUI. "+
					"Check it out: https://github.com/rw3iss/slackers")
				_ = m.slackSvc.SendMessage(m.currentCh.ID, inviteText)
			}
		}()
		m.warning = "Friend request sent to " + msg.Name
	} else {
		m.warning = "P2P not available — enable Secure Mode in settings"
	}
	return m, nil
```

- [ ] **Step 9: Handle incoming friend requests from P2P**

In the `P2PReceivedMsg` handler, check for friend request type:

```go
case P2PReceivedMsg:
	// Check for friend request messages.
	if msg.Text != "" && strings.HasPrefix(msg.Text, "") {
		// Parse based on the P2P message type (handled via the callback).
	}
	// ... existing P2P message handling
```

Actually, the cleaner approach: update the P2P callback in `NewModel` to detect friend request types and emit different tea.Msg types. Add a new message type:

```go
type P2PFriendRequestMsg struct {
	UserID    string
	Name      string
	PublicKey string
	Multiaddr string
}
```

Update the p2p callback in NewModel:

```go
onMsg := func(peerSlackID string, msg secure.P2PMessage) {
	switch msg.Type {
	case secure.MsgTypeFriendRequest:
		parts := strings.SplitN(msg.Text, "|", 2)
		if len(parts) == 2 {
			p2pChan <- P2PReceivedMsg{
				SenderID: peerSlackID,
				Text:     "__FRIEND_REQUEST__",
				PubKey:   parts[0],
				Multiaddr: parts[1],
			}
		}
	default:
		p2pChan <- P2PReceivedMsg{SenderID: peerSlackID, Text: msg.Text}
	}
}
```

Update `P2PReceivedMsg` to include optional fields:

```go
type P2PReceivedMsg struct {
	SenderID  string
	Text      string
	PubKey    string // for friend requests
	Multiaddr string // for friend requests
}
```

In the `P2PReceivedMsg` handler:

```go
case P2PReceivedMsg:
	if msg.Text == "__FRIEND_REQUEST__" {
		// Show incoming friend request popup.
		senderName := msg.SenderID
		if u, ok := m.users[msg.SenderID]; ok {
			senderName = u.DisplayName
		}
		m.friendRequest = NewIncomingFriendRequest(msg.SenderID, senderName, msg.PubKey, msg.Multiaddr)
		m.friendRequest.SetSize(m.width, m.height)
		m.overlay = overlayFriendRequest
		if m.p2pChan != nil {
			return m, waitForP2PMsg(m.p2pChan)
		}
		return m, nil
	}
	// ... existing message handling
```

- [ ] **Step 10: Handle FriendRequestRespondMsg — accept or reject**

```go
case FriendRequestRespondMsg:
	m.overlay = overlayNone
	if msg.Accepted && m.friendStore != nil {
		f := friends.Friend{
			UserID:    msg.UserID,
			Name:      msg.Name,
			PublicKey: msg.PublicKey,
			Multiaddr: msg.Multiaddr,
		}
		_ = m.friendStore.Add(f)
		_ = m.friendStore.Save()
		// Add to sidebar.
		m.channels.SetFriendChannels(m.friendChannels())
		m.warning = msg.Name + " added as friend!"
		// Send accept response over P2P.
		if m.p2pNode != nil && m.secureMgr != nil {
			go func() {
				resp := secure.P2PMessage{
					Type:     secure.MsgTypeFriendAccept,
					Text:     m.secureMgr.OwnPublicKeyBase64() + "|" + m.p2pNode.Multiaddr(),
					SenderID: msg.UserID,
				}
				_ = m.p2pNode.SendMessage(msg.UserID, resp)
			}()
		}
	} else if !msg.Accepted && msg.UserID != "" {
		m.warning = "Friend request declined"
	}
	return m, nil
```

- [ ] **Step 11: Route friend channel messages through P2P**

In the message send handler (`InputSendMsg`), before the existing send logic:

```go
// Check if this is a friend channel — route through P2P.
if m.currentCh != nil && m.currentCh.IsFriend {
	if m.p2pNode != nil {
		friendMsg := secure.P2PMessage{
			Type:     secure.MsgTypeMessage,
			Text:     sendText,
			SenderID: m.currentCh.UserID,
			Timestamp: time.Now().Unix(),
		}
		go m.p2pNode.SendMessage(m.currentCh.UserID, friendMsg)
		// Append to local view immediately.
		localMsg := types.Message{
			UserID:    "me",
			UserName:  "You",
			Text:      text,
			Timestamp: time.Now(),
		}
		m.messages.AppendMessage(localMsg)
		// Store in friend message history.
		m.friendMessages[m.currentCh.UserID] = append(m.friendMessages[m.currentCh.UserID], localMsg)
	}
	return m, nil
}
```

- [ ] **Step 12: Load friend message history when selecting a friend channel**

In the Enter key handler (channel selection), add before `loadHistoryCmd`:

```go
if ch.IsFriend {
	m.currentCh = ch
	m.channels.ClearUnread(ch.ID)
	m.setChannelHeader()
	m.saveLastChannel(ch.ID)
	// Load local friend message history.
	if history, ok := m.friendMessages[ch.UserID]; ok {
		m.messages.SetMessages(history)
	} else {
		m.messages.SetMessages(nil)
	}
	return m, nil
}
```

- [ ] **Step 13: Add friend request overlay to View**

```go
case overlayFriendRequest:
	return m.friendRequest.View()
```

- [ ] **Step 14: Allow TUI to launch without workspace tokens**

In `cmd/slackers/main.go`, update the root command to skip validation when friends exist:

```go
if err := cfg.Validate(); err != nil {
	// Check if we have friends — can run in friends-only mode.
	store := friends.NewFriendStore(friends.DefaultPath())
	_ = store.Load()
	if store.Count() == 0 {
		fmt.Println("Configuration is incomplete. Starting setup...")
		// ... existing setup flow
	} else {
		fmt.Println("Running in friends-only mode (no workspace configured)")
	}
}
```

- [ ] **Step 15: Build and test**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./... && go test ./internal/friends/ -v`
Expected: Clean build, all tests pass

- [ ] **Step 16: Commit**

```bash
git add internal/tui/model.go cmd/slackers/main.go
git commit -m "feat: wire friends into TUI — sidebar, chat, P2P routing, friend requests"
```

---

## Task 7: P2P message routing for friend channels

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/messages.go`

- [ ] **Step 1: Add SetMessages method to MessageViewModel if not present**

Check if `SetMessages` exists. If not, add:

```go
// SetMessages replaces all messages (used for friend channel history).
func (m *MessageViewModel) SetMessages(msgs []types.Message) {
	m.messages = msgs
	m.autoScroll = true
	m.rebuildContent()
}
```

- [ ] **Step 2: Route incoming P2P messages to friend channels**

In the `P2PReceivedMsg` handler, update the existing message display code to also store in friend history:

```go
// Existing P2P message handling — also store in friend history.
if m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID == msg.SenderID {
	p2pMsg := types.Message{
		UserID:    msg.SenderID,
		UserName:  userName,
		Text:      msg.Text,
		Timestamp: time.Now(),
	}
	m.messages.AppendMessage(p2pMsg)
	m.friendMessages[msg.SenderID] = append(m.friendMessages[msg.SenderID], p2pMsg)
} else if msg.SenderID != "" {
	// Not viewing this friend's channel — mark unread.
	m.channels.MarkUnread("friend:" + msg.SenderID)
}
```

- [ ] **Step 3: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go internal/tui/messages.go
git commit -m "feat: route P2P messages to friend channels with history"
```

---

## Task 8: Online detection and friend pinging

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add a FriendPingMsg and ping tick**

```go
type FriendPingMsg struct {
	Online map[string]bool
}

func friendPingCmd(store *friends.FriendStore, p2p *secure.P2PNode) tea.Cmd {
	return func() tea.Msg {
		if store == nil || p2p == nil {
			return FriendPingMsg{}
		}
		online := make(map[string]bool)
		for _, f := range store.All() {
			if f.Multiaddr != "" {
				if !p2p.IsConnected(f.UserID) {
					_ = p2p.ConnectToPeer(f.UserID, f.Multiaddr)
				}
				on := p2p.IsConnected(f.UserID)
				online[f.UserID] = on
				store.SetOnline(f.UserID, on)
			}
		}
		return FriendPingMsg{Online: online}
	}
}

func friendPingTickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return friendPingTickMsg{}
	})
}

type friendPingTickMsg struct{}
```

- [ ] **Step 2: Handle friend ping tick in Update**

```go
case friendPingTickMsg:
	return m, friendPingCmd(m.friendStore, m.p2pNode)

case FriendPingMsg:
	for uid, on := range msg.Online {
		chID := "friend:" + uid
		if on {
			m.channels.MarkUnread(chID) // green = online
		} else {
			m.channels.ClearUnread(chID) // grey = offline
		}
	}
	return m, friendPingTickCmd()
```

- [ ] **Step 3: Start friend ping in Init**

Add to Init cmds:

```go
if m.friendStore != nil && m.friendStore.Count() > 0 {
	cmds = append(cmds, friendPingTickCmd())
}
```

- [ ] **Step 4: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: periodic friend ping for online/offline detection"
```

---

## Task 9: Update main.go to create FriendStore and pass to TUI

**Files:**
- Modify: `cmd/slackers/main.go`

- [ ] **Step 1: Create FriendStore in root command**

In the root command's RunE, after config loading and before TUI creation:

```go
// Load friends list.
friendStore := friends.NewFriendStore(friends.DefaultPath())
if err := friendStore.Load(); err != nil {
	debug.Log("[friends] load error: %v", err)
}
```

Add import: `"github.com/rw3iss/slackers/internal/friends"`

Update `NewModel` call to pass `friendStore`:

```go
model := tui.NewModel(slackSvc, socketSvc, cfg, version, friendStore)
```

- [ ] **Step 2: Handle friends-only mode**

When validation fails but friends exist, create nil services:

```go
if err := cfg.Validate(); err != nil {
	if friendStore.Count() > 0 {
		fmt.Println("Running in friends-only mode (no workspace configured)")
		// Create nil-safe services.
		slackSvc = nil
		socketSvc = nil
	} else {
		fmt.Println("Configuration is incomplete. Starting setup...")
		if setupErr := runSetupFlow(cfg); setupErr != nil {
			return setupErr
		}
	}
}
```

Note: The TUI model and Slack services must handle nil gracefully. `SlackService` calls should be guarded with nil checks — most already are via the `tryWithFallback` pattern, but `loadChannelsCmd` and similar should return empty results when svc is nil.

- [ ] **Step 3: Build to verify**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go build ./...`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add cmd/slackers/main.go
git commit -m "feat: create FriendStore in main, support friends-only mode"
```

---

## Task 10: Config command update and final polish

**Files:**
- Modify: `cmd/slackers/main.go` (config command)
- Modify: `README.md`

- [ ] **Step 1: Add friends count to config output**

In the config command, add to the State section:

```go
fmt.Printf("    friends:            %d\n", friendStore.Count())
```

- [ ] **Step 2: Update README features list**

Add to Features section:

```
- **Friends list** -- private P2P chat with befriended Slackers users, works without a Slack workspace
```

Add to shortcuts table:

```
| `Ctrl-B` | Send friend request to current DM user |
```

- [ ] **Step 3: Run all tests**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && go test ./... && go vet ./...`
Expected: All tests pass, no vet warnings

- [ ] **Step 4: Build all platforms**

Run: `cd /home/rw3iss/Sites/others/tools/slackers && make build-all`
Expected: All three binaries built successfully

- [ ] **Step 5: Final commit**

```bash
git add .
git commit -m "feat: Friends list — complete P2P friend system with chat, pings, and friends-only mode"
```

---

## Summary

| Task | Description | Est. |
|------|-------------|------|
| 1 | Friend data model + persistence + tests | 5 min |
| 2 | P2P protocol extensions (friend_request, ping) | 3 min |
| 3 | IsFriend channel type + sidebar section | 4 min |
| 4 | Friend request overlay UI | 4 min |
| 5 | Befriend shortcut + keymap | 3 min |
| 6 | Wire friends into TUI model (largest) | 15 min |
| 7 | P2P message routing for friend channels | 4 min |
| 8 | Online detection and friend pinging | 4 min |
| 9 | Main.go FriendStore + friends-only mode | 4 min |
| 10 | Config update + README + final polish | 3 min |
