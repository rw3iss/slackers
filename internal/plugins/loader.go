package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/shortcuts"
)

// MergeShortcuts collects shortcuts.json from each enabled plugin's
// .config/ directory and merges them into the base shortcut map.
// Plugin shortcuts are applied BEFORE user overrides, so user
// settings always win.
func (m *Manager) MergeShortcuts(base shortcuts.ShortcutMap) shortcuts.ShortcutMap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	merged := base
	for name, entry := range m.plugins {
		if entry.state < StateEnabled {
			continue
		}
		path := filepath.Join(m.configDir, name, ".config", "shortcuts.json")
		pluginShortcuts, err := shortcuts.Load(path)
		if err != nil {
			continue // no shortcuts.json or parse error — skip
		}
		debug.Log("[plugins] merging shortcuts from %s", name)
		merged = shortcuts.Merge(merged, pluginShortcuts)
	}
	return merged
}

// PluginSettings reads a plugin's .config/settings.json as a
// generic map. Returns nil if the file doesn't exist.
func (m *Manager) PluginSettings(name string) map[string]any {
	path := filepath.Join(m.configDir, name, ".config", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}
	return settings
}

// SavePluginSettings writes a plugin's settings to its
// .config/settings.json file.
func (m *Manager) SavePluginSettings(name string, settings map[string]any) error {
	dir := filepath.Join(m.configDir, name, ".config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o644)
}

// PluginThemeDirs returns paths to all enabled plugins' themes/
// directories that exist on disk. The theme loader can scan these
// alongside the user's themes/ directory.
func (m *Manager) PluginThemeDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var dirs []string
	for name, entry := range m.plugins {
		if entry.state < StateEnabled {
			continue
		}
		dir := filepath.Join(m.configDir, name, "themes")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
