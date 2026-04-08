// Package shortcuts manages keyboard shortcut mappings with user overrides.
package shortcuts

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed defaults.json
var defaultsJSON []byte

// ShortcutMap maps action names to their key bindings.
type ShortcutMap map[string][]string

// ActionDescription maps action names to human-readable descriptions.
var ActionDescriptions = map[string]string{
	"quit":               "Quit the application",
	"tab":                "Cycle focus to next panel",
	"shift_tab":          "Cycle focus to previous panel",
	"up":                 "Navigate up / scroll up",
	"down":               "Navigate down / scroll down",
	"enter":              "Select channel / send message",
	"escape":             "Toggle sidebar/input focus",
	"focus_input":        "Focus the message input",
	"refresh":            "Refresh channel list",
	"page_up":            "Scroll up by page",
	"page_down":          "Scroll down by page",
	"half_page_up":       "Half-page scroll up",
	"half_page_down":     "Half-page scroll down",
	"home":               "Jump to top",
	"end":                "Jump to bottom",
	"help":               "Toggle help page",
	"settings":           "Open settings",
	"search_channels":    "Search and jump to a channel",
	"hide_channel":       "Hide selected channel",
	"show_hidden":        "View and unhide hidden channels",
	"rename_group":       "Rename/alias selected channel",
	"toggle_hidden":      "Toggle hidden channels visible",
	"next_unread":        "Jump to next unread channel",
	"search_messages":    "Search messages",
	"attach_file":        "Attach file to send",
	"toggle_full_mode":   "Toggle full screen chat mode",
	"files_list":         "Browse all files",
	"cancel_download":    "Cancel file download",
	"focus_input_global": "Exit file select, focus input",
	"enter_file_select":  "Enter file select mode",
	"toggle_file_select": "Toggle file select in messages",
	"sidebar_collapse":   "Collapse/expand channel group",
	"toggle_input_mode":  "Toggle input mode (normal/edit)",
	"toggle_theme":       "Toggle between primary and alternate themes",
	"friend_details":     "Open friend details for current friend chat",
	"notifications":      "Open the notifications view",
	"share_my_info":      "Insert [FRIEND:me] into the chat input — expands to your full contact card on send",
	"befriend":           "Send friend request to the current DM user",
	"emoji_picker":       "Open emoji picker (insert at cursor)",
	"select_message":     "Enter message select mode (react / reply / edit / delete)",
	"shortcuts_editor":   "Open the keyboard shortcuts editor",
}

// ActionOrder defines the display order for the shortcuts editor.
var ActionOrder = []string{
	"quit", "tab", "shift_tab", "escape", "enter",
	"up", "down", "page_up", "page_down",
	"half_page_up", "half_page_down", "home", "end",
	"focus_input", "search_channels", "search_messages",
	"next_unread", "refresh",
	"attach_file", "files_list", "toggle_file_select",
	"enter_file_select", "focus_input_global", "cancel_download",
	"hide_channel", "show_hidden", "toggle_hidden",
	"rename_group", "sidebar_collapse",
	"toggle_input_mode", "toggle_full_mode", "toggle_theme",
	"befriend", "emoji_picker", "select_message",
	"friend_details", "notifications", "share_my_info",
	"help", "settings", "shortcuts_editor",
}

// DefaultShortcuts returns the built-in default shortcut mappings.
func DefaultShortcuts() ShortcutMap {
	var m ShortcutMap
	if err := json.Unmarshal(defaultsJSON, &m); err != nil {
		panic(fmt.Sprintf("invalid defaults.json: %v", err))
	}
	return m
}

// UserConfigPath returns the path to the user's shortcuts override file.
func UserConfigPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "shortcuts.json")
}

// Load reads the user's shortcut overrides. Returns empty map if file doesn't exist.
func Load(path string) (ShortcutMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ShortcutMap{}, nil
		}
		return nil, fmt.Errorf("reading shortcuts: %w", err)
	}

	var m ShortcutMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing shortcuts: %w", err)
	}
	return m, nil
}

// Save writes shortcut overrides to disk (only the diff from defaults).
func Save(path string, overrides ShortcutMap) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating shortcuts dir: %w", err)
	}

	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling shortcuts: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// Merge overlays user overrides on top of defaults.
// Only actions present in overrides replace the defaults.
func Merge(defaults, overrides ShortcutMap) ShortcutMap {
	merged := make(ShortcutMap, len(defaults))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

// KeysForAction returns the key bindings for an action from a merged map.
func KeysForAction(m ShortcutMap, action string) []string {
	if keys, ok := m[action]; ok {
		return keys
	}
	return nil
}
