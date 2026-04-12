// internal/workspace/store.go
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rw3iss/slackers/internal/types"
)

// Dir returns the root workspaces directory.
func Dir(configDir string) string {
	return filepath.Join(configDir, "workspaces")
}

// WorkspaceDir returns the directory for a specific workspace.
func WorkspaceDir(configDir, teamID string) string {
	return filepath.Join(configDir, "workspaces", teamID)
}

// LoadAll reads all workspace configs from subdirectories of the workspaces dir.
// Returns an empty (non-nil) slice if no workspaces exist. Skips corrupt entries.
// Result is sorted alphabetically by display name.
func LoadAll(configDir string) ([]*Workspace, error) {
	root := Dir(configDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Workspace{}, nil
		}
		return nil, fmt.Errorf("workspace: read dir %s: %w", root, err)
	}

	var workspaces []*Workspace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ws, err := Load(configDir, e.Name())
		if err != nil {
			// Skip corrupt workspace dirs but don't abort.
			continue
		}
		workspaces = append(workspaces, ws)
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].DisplayName() < workspaces[j].DisplayName()
	})

	if workspaces == nil {
		workspaces = []*Workspace{}
	}
	return workspaces, nil
}

// Load reads a single workspace's workspace.json and channels.json.
func Load(configDir, teamID string) (*Workspace, error) {
	dir := WorkspaceDir(configDir, teamID)

	// Load workspace config.
	cfgPath := filepath.Join(dir, "workspace.json")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: read %s: %w", cfgPath, err)
	}
	var cfg WorkspaceConfig
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: parse %s: %w", cfgPath, err)
	}
	// Ensure TeamID matches the directory name.
	cfg.TeamID = teamID

	// Load channel metadata (optional — file may not exist yet).
	channels := make(map[string]ChannelMeta)
	chPath := filepath.Join(dir, "channels.json")
	chData, err := os.ReadFile(chPath)
	if err == nil {
		if jsonErr := json.Unmarshal(chData, &channels); jsonErr != nil {
			// Corrupt channels.json is non-fatal; start fresh.
			channels = make(map[string]ChannelMeta)
		}
	}

	ws := &Workspace{
		Config:      cfg,
		Users:       make(map[string]types.User),
		ChannelMeta: channels,
		LastSeen:    make(map[string]string),
	}
	return ws, nil
}

// Save writes workspace.json (0600) and channels.json to the workspace directory.
// Creates the directory (0700) if it does not exist.
func Save(configDir string, ws *Workspace) error {
	dir := WorkspaceDir(configDir, ws.Config.TeamID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", dir, err)
	}

	// Write workspace.json.
	cfgData, err := json.MarshalIndent(ws.Config, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal config: %w", err)
	}
	cfgPath := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(cfgPath, cfgData, 0600); err != nil {
		return fmt.Errorf("workspace: write %s: %w", cfgPath, err)
	}

	// Write channels.json.
	meta := ws.ChannelMeta
	if meta == nil {
		meta = make(map[string]ChannelMeta)
	}
	chData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal channels: %w", err)
	}
	chPath := filepath.Join(dir, "channels.json")
	if err := os.WriteFile(chPath, chData, 0600); err != nil {
		return fmt.Errorf("workspace: write %s: %w", chPath, err)
	}

	return nil
}

// Create makes a new Workspace from cfg and saves it to disk.
// Returns an error if cfg.TeamID is empty.
func Create(configDir string, cfg WorkspaceConfig) (*Workspace, error) {
	if cfg.TeamID == "" {
		return nil, errors.New("workspace: TeamID must not be empty")
	}
	ws := &Workspace{
		Config:      cfg,
		Users:       make(map[string]types.User),
		ChannelMeta: make(map[string]ChannelMeta),
		LastSeen:    make(map[string]string),
	}
	if err := Save(configDir, ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// Delete removes the workspace directory and all its contents.
func Delete(configDir, teamID string) error {
	dir := WorkspaceDir(configDir, teamID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("workspace: delete %s: %w", dir, err)
	}
	return nil
}

// Exists reports whether the workspace directory exists.
func Exists(configDir, teamID string) bool {
	dir := WorkspaceDir(configDir, teamID)
	_, err := os.Stat(dir)
	return err == nil
}
