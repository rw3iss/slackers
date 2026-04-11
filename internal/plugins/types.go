package plugins

import (
	"github.com/rw3iss/slackers/internal/api"
	"github.com/rw3iss/slackers/internal/commands"
)

// Plugin is the interface every plugin must implement.
type Plugin interface {
	// Manifest returns the plugin's metadata.
	Manifest() Manifest

	// Init is called when the plugin is enabled at app startup.
	// The API gives the plugin access to all app subsystems.
	Init(appAPI api.API) error

	// Start is called when the plugin is activated (lazy load).
	// Heavy initialization (network calls, UI setup) goes here.
	Start() error

	// Stop is called when the plugin is deactivated.
	Stop() error

	// Destroy is called when the plugin is uninstalled.
	// Clean up any persistent state.
	Destroy() error

	// Commands returns slash commands the plugin wants to register.
	// Called once during Init. Return nil if the plugin has no commands.
	Commands() []*commands.Command

	// Shortcuts returns custom keyboard shortcuts the plugin wants.
	// Map of action name → key bindings. Return nil if unused.
	Shortcuts() map[string][]string

	// MessageFilter handles an incoming P2P plugin message.
	// Called when a friend sends a message addressed to this plugin.
	// Return true if the message was handled. senderID is the friend's
	// user ID, data is the JSON payload.
	MessageFilter(senderID, data string) bool

	// ConfigFields returns user-editable config fields for the plugin
	// config screen. Each field has a key, label, current value, and
	// description. Return nil if the plugin has no user config.
	ConfigFields() []ConfigField

	// SetConfig is called when the user saves a config field.
	SetConfig(key, value string)
}

// ConfigField describes one editable setting in a plugin's config.
type ConfigField struct {
	Key         string // internal key (e.g. "city")
	Label       string // display label (e.g. "City / Zipcode")
	Value       string // current value
	Description string // help text shown when selected
}

// PluginState tracks a plugin's lifecycle state.
type PluginState int

const (
	StateDisabled PluginState = iota // installed but not active
	StateEnabled                     // active, commands registered
	StateRunning                     // main process executing
)

func (s PluginState) String() string {
	switch s {
	case StateDisabled:
		return "disabled"
	case StateEnabled:
		return "enabled"
	case StateRunning:
		return "running"
	}
	return "unknown"
}

// PluginInfo is a read-only snapshot of a plugin's state,
// returned by Manager.List() and Manager.Get().
type PluginInfo struct {
	Name        string
	Version     string
	Author      string
	Description string
	State       PluginState
	InstalledAt string // ISO 8601 date
}
