package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rw3iss/slackers/internal/api"
	"github.com/rw3iss/slackers/internal/debug"
)

// Manager handles plugin lifecycle: registration, enable/disable,
// initialization, and config directory management.
type Manager struct {
	mu        sync.RWMutex
	plugins   map[string]*pluginEntry
	appAPI    api.API
	configDir string // ~/.config/slackers/plugins/
}

type pluginEntry struct {
	plugin   Plugin
	state    PluginState
	manifest Manifest
}

// NewManager creates a plugin manager. configDir is the root
// plugins directory (typically ~/.config/slackers/plugins/).
func NewManager(configDir string) *Manager {
	return &Manager{
		plugins:   make(map[string]*pluginEntry),
		configDir: configDir,
	}
}

// ConfigDir returns the plugin configuration directory path.
func (m *Manager) ConfigDir() string {
	return m.configDir
}

// Register adds a plugin to the manager. Called at app startup
// by compiled-in plugins. Does NOT enable or initialize — the
// manager checks the plugin index to decide that.
func (m *Manager) Register(p Plugin) {
	manifest := p.Manifest()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plugins[manifest.Name] = &pluginEntry{
		plugin:   p,
		state:    StateDisabled,
		manifest: manifest,
	}
	debug.Log("[plugins] registered: %s v%s", manifest.Name, manifest.Version)
}

// InitAll initializes all registered plugins that are enabled
// in the plugin index. Called once at app startup after the API
// host is ready.
func (m *Manager) InitAll(appAPI api.API) error {
	m.appAPI = appAPI
	idx := LoadIndex(m.configDir)

	m.mu.Lock()
	defer m.mu.Unlock()

	for name, entry := range m.plugins {
		idxEntry, exists := idx.Plugins[name]
		if !exists {
			// New plugin — auto-enable and add to index.
			idx.Plugins[name] = PluginIndexEntry{
				Enabled:     true,
				InstalledAt: time.Now().Format("2006-01-02"),
			}
			idxEntry = idx.Plugins[name]
		}
		if !idxEntry.Enabled {
			debug.Log("[plugins] %s is disabled, skipping init", name)
			continue
		}
		if err := entry.plugin.Init(appAPI); err != nil {
			debug.Log("[plugins] %s init failed: %v", name, err)
			continue
		}
		entry.state = StateEnabled
		debug.Log("[plugins] %s initialized (enabled)", name)
	}

	// Persist any index changes (new plugins auto-added).
	_ = SaveIndex(m.configDir, idx)
	return nil
}

// Enable activates a disabled plugin.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if entry.state != StateDisabled {
		return nil // already enabled
	}
	if m.appAPI != nil {
		if err := entry.plugin.Init(m.appAPI); err != nil {
			return fmt.Errorf("init failed: %w", err)
		}
	}
	entry.state = StateEnabled
	m.updateIndex(name, true)
	debug.Log("[plugins] %s enabled", name)
	return nil
}

// Disable deactivates an enabled plugin.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if entry.state == StateDisabled {
		return nil
	}
	if entry.state == StateRunning {
		_ = entry.plugin.Stop()
	}
	entry.state = StateDisabled
	m.updateIndex(name, false)
	debug.Log("[plugins] %s disabled", name)
	return nil
}

// Uninstall removes a plugin entirely — stops it, calls Destroy,
// removes its config directory, and removes it from the index.
func (m *Manager) Uninstall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if entry.state == StateRunning {
		_ = entry.plugin.Stop()
	}
	_ = entry.plugin.Destroy()
	delete(m.plugins, name)

	// Remove plugin config directory.
	pluginDir := filepath.Join(m.configDir, name)
	_ = os.RemoveAll(pluginDir)

	// Remove from index.
	idx := LoadIndex(m.configDir)
	delete(idx.Plugins, name)
	_ = SaveIndex(m.configDir, idx)

	debug.Log("[plugins] %s uninstalled", name)
	return nil
}

// Start activates a plugin's main process (lazy load).
func (m *Manager) Start(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if entry.state != StateEnabled {
		return fmt.Errorf("plugin %q is not enabled", name)
	}
	if err := entry.plugin.Start(); err != nil {
		return err
	}
	entry.state = StateRunning
	debug.Log("[plugins] %s started", name)
	return nil
}

// Stop deactivates a plugin's main process.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if entry.state != StateRunning {
		return nil
	}
	if err := entry.plugin.Stop(); err != nil {
		return err
	}
	entry.state = StateEnabled
	debug.Log("[plugins] %s stopped", name)
	return nil
}

// List returns info about all registered plugins.
func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	idx := LoadIndex(m.configDir)
	out := make([]PluginInfo, 0, len(m.plugins))
	for name, entry := range m.plugins {
		installedAt := ""
		if ie, ok := idx.Plugins[name]; ok {
			installedAt = ie.InstalledAt
		}
		out = append(out, PluginInfo{
			Name:        entry.manifest.Name,
			Version:     entry.manifest.Version,
			Author:      entry.manifest.Author,
			Description: entry.manifest.Description,
			State:       entry.state,
			InstalledAt: installedAt,
		})
	}
	return out
}

// Get returns info about a specific plugin, or nil if not found.
func (m *Manager) Get(name string) *PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.plugins[name]
	if !ok {
		return nil
	}
	idx := LoadIndex(m.configDir)
	installedAt := ""
	if ie, ok := idx.Plugins[name]; ok {
		installedAt = ie.InstalledAt
	}
	return &PluginInfo{
		Name:        entry.manifest.Name,
		Version:     entry.manifest.Version,
		Author:      entry.manifest.Author,
		Description: entry.manifest.Description,
		State:       entry.state,
		InstalledAt: installedAt,
	}
}

// GetPlugin returns the live Plugin instance, or nil if not found.
func (m *Manager) GetPlugin(name string) Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.plugins[name]
	if !ok {
		return nil
	}
	return entry.plugin
}

// Names returns the names of all registered plugins (for command completion).
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// EnabledPlugins returns all enabled plugins.
func (m *Manager) EnabledPlugins() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Plugin
	for _, entry := range m.plugins {
		if entry.state >= StateEnabled {
			out = append(out, entry.plugin)
		}
	}
	return out
}

// RouteMessage delivers a P2P plugin message to the named plugin.
// Returns true if a plugin handled it, false if the plugin wasn't
// found or didn't consume the message.
func (m *Manager) RouteMessage(pluginName, senderID, data string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.plugins[pluginName]
	if !ok || entry.state < StateEnabled {
		return false
	}
	return entry.plugin.MessageFilter(senderID, data)
}

// updateIndex persists the enable/disable state change.
func (m *Manager) updateIndex(name string, enabled bool) {
	idx := LoadIndex(m.configDir)
	entry := idx.Plugins[name]
	entry.Enabled = enabled
	if entry.InstalledAt == "" {
		entry.InstalledAt = time.Now().Format("2006-01-02")
	}
	idx.Plugins[name] = entry
	_ = SaveIndex(m.configDir, idx)
}
