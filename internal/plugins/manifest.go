package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Manifest describes a plugin's metadata. Stored as manifest.json
// in each plugin's directory.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Homepage    string `json:"homepage,omitempty"`
	MinVersion  string `json:"min_version,omitempty"`
}

// LoadManifest reads a manifest.json from a plugin directory.
func LoadManifest(pluginDir string) (Manifest, error) {
	path := filepath.Join(pluginDir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// SaveManifest writes a manifest.json to a plugin directory.
func SaveManifest(pluginDir string, m Manifest) error {
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o644)
}

// PluginIndex tracks which plugins are installed and their
// enable/disable state. Stored as plugins.json in the plugins
// config directory.
type PluginIndex struct {
	Plugins map[string]PluginIndexEntry `json:"plugins"`
}

// PluginIndexEntry is one entry in the plugin index.
type PluginIndexEntry struct {
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at,omitempty"`
}

// LoadIndex reads the plugin index from disk.
func LoadIndex(configDir string) PluginIndex {
	path := filepath.Join(configDir, "plugins.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return PluginIndex{Plugins: make(map[string]PluginIndexEntry)}
	}
	var idx PluginIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return PluginIndex{Plugins: make(map[string]PluginIndexEntry)}
	}
	if idx.Plugins == nil {
		idx.Plugins = make(map[string]PluginIndexEntry)
	}
	return idx
}

// SaveIndex writes the plugin index to disk.
func SaveIndex(configDir string, idx PluginIndex) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "plugins.json"), data, 0o644)
}
