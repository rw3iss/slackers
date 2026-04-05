package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/rw3iss/slackers/internal/shortcuts"
)

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	Quit             key.Binding
	Tab              key.Binding
	ShiftTab         key.Binding
	Up               key.Binding
	Down             key.Binding
	Enter            key.Binding
	Escape           key.Binding
	FocusInput       key.Binding
	Refresh          key.Binding
	PageUp           key.Binding
	PageDown         key.Binding
	HalfPageUp       key.Binding
	HalfPageDown     key.Binding
	Home             key.Binding
	End              key.Binding
	Help             key.Binding
	Settings         key.Binding
	Search           key.Binding
	HideChannel      key.Binding
	ShowHidden       key.Binding
	RenameGroup      key.Binding
	ToggleHidden     key.Binding
	NextUnread       key.Binding
	SearchMessages   key.Binding
	AttachFile       key.Binding
	ToggleFullMode   key.Binding
	FilesList        key.Binding
	CancelDownload   key.Binding
	FocusInputGlobal key.Binding
	EnterFileSelect  key.Binding
	ToggleFileSelect key.Binding
	SidebarCollapse  key.Binding
}

// binding creates a key.Binding from a ShortcutMap action name.
func binding(sm shortcuts.ShortcutMap, action, helpKey, helpDesc string) key.Binding {
	keys := shortcuts.KeysForAction(sm, action)
	if len(keys) == 0 {
		return key.NewBinding(key.WithHelp(helpKey, helpDesc))
	}
	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, helpDesc),
	)
}

// BuildKeyMap creates a KeyMap from a merged ShortcutMap.
func BuildKeyMap(sm shortcuts.ShortcutMap) KeyMap {
	return KeyMap{
		Quit:             binding(sm, "quit", "ctrl+q", "quit"),
		Tab:              binding(sm, "tab", "tab", "next pane"),
		ShiftTab:         binding(sm, "shift_tab", "shift+tab", "prev pane"),
		Up:               binding(sm, "up", "↑/k", "up"),
		Down:             binding(sm, "down", "↓/j", "down"),
		Enter:            binding(sm, "enter", "enter", "select/send"),
		Escape:           binding(sm, "escape", "esc", "toggle focus"),
		FocusInput:       binding(sm, "focus_input", "i", "focus input"),
		Refresh:          binding(sm, "refresh", "ctrl+r", "refresh"),
		PageUp:           binding(sm, "page_up", "pgup", "page up"),
		PageDown:         binding(sm, "page_down", "pgdown", "page down"),
		HalfPageUp:       binding(sm, "half_page_up", "ctrl+u", "half page up"),
		HalfPageDown:     binding(sm, "half_page_down", "ctrl+d", "half page down"),
		Home:             binding(sm, "home", "home", "scroll to top"),
		End:              binding(sm, "end", "end", "scroll to bottom"),
		Help:             binding(sm, "help", "ctrl+h", "help"),
		Settings:         binding(sm, "settings", "ctrl+s", "settings"),
		Search:           binding(sm, "search_channels", "ctrl+k", "search channels"),
		HideChannel:      binding(sm, "hide_channel", "ctrl+x", "hide channel"),
		ShowHidden:       binding(sm, "show_hidden", "ctrl+g", "show hidden"),
		RenameGroup:      binding(sm, "rename_group", "ctrl+a", "rename group"),
		ToggleHidden:     binding(sm, "toggle_hidden", "ctrl+o", "toggle hidden"),
		NextUnread:       binding(sm, "next_unread", "ctrl+n", "next unread"),
		SearchMessages:   binding(sm, "search_messages", "ctrl+f", "search messages"),
		AttachFile:       binding(sm, "attach_file", "ctrl+u", "attach file"),
		ToggleFullMode:   binding(sm, "toggle_full_mode", "ctrl+w", "full screen"),
		FilesList:        binding(sm, "files_list", "ctrl+l", "files list"),
		CancelDownload:   binding(sm, "cancel_download", "ctrl+d", "cancel download"),
		FocusInputGlobal: binding(sm, "focus_input_global", "ctrl+down", "focus input"),
		EnterFileSelect:  binding(sm, "enter_file_select", "ctrl+up", "file select"),
		ToggleFileSelect: binding(sm, "toggle_file_select", "f", "file select"),
		SidebarCollapse:  binding(sm, "sidebar_collapse", "space", "collapse group"),
	}
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return BuildKeyMap(shortcuts.DefaultShortcuts())
}
