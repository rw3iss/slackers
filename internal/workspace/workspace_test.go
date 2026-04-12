// internal/workspace/workspace_test.go
package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// ── TestCompoundID ──────────────────────────────────────────────────────────

func TestCompoundID(t *testing.T) {
	got := CompoundID("T123", "C456")
	want := "T123:C456"
	if got != want {
		t.Fatalf("CompoundID: got %q, want %q", got, want)
	}
}

func TestSplitCompoundID_roundTrip(t *testing.T) {
	teamID, channelID := SplitCompoundID("T123:C456")
	if teamID != "T123" {
		t.Errorf("teamID: got %q, want %q", teamID, "T123")
	}
	if channelID != "C456" {
		t.Errorf("channelID: got %q, want %q", channelID, "C456")
	}
}

func TestSplitCompoundID_friendChannel(t *testing.T) {
	// Friend channels have no colon — teamID should be empty.
	teamID, channelID := SplitCompoundID("FRIEND123")
	if teamID != "" {
		t.Errorf("teamID: got %q, want empty string", teamID)
	}
	if channelID != "FRIEND123" {
		t.Errorf("channelID: got %q, want %q", channelID, "FRIEND123")
	}
}

// ── TestDisplayName ─────────────────────────────────────────────────────────

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		cfg      WorkspaceConfig
		teamName string
		want     string
	}{
		{
			name:     "user-set name wins",
			cfg:      WorkspaceConfig{TeamID: "T1", Name: "My Company"},
			teamName: "Slack Team Name",
			want:     "My Company",
		},
		{
			name:     "falls back to TeamName when Name is empty",
			cfg:      WorkspaceConfig{TeamID: "T1", Name: ""},
			teamName: "Slack Team Name",
			want:     "Slack Team Name",
		},
		{
			name:     "falls back to TeamID when both Name and TeamName are empty",
			cfg:      WorkspaceConfig{TeamID: "T1", Name: ""},
			teamName: "",
			want:     "T1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := &Workspace{Config: tc.cfg, TeamName: tc.teamName}
			if got := ws.DisplayName(); got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slackers-ws-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// ── TestCreateLoadDelete ─────────────────────────────────────────────────────

func TestCreateLoadDelete(t *testing.T) {
	configDir := tmpDir(t)

	cfg := WorkspaceConfig{
		TeamID:   "T999",
		Name:     "Test Workspace",
		BotToken: "xoxb-test",
	}

	// Create.
	ws, err := Create(configDir, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws == nil {
		t.Fatal("Create returned nil workspace")
	}

	// Files should exist on disk.
	wsDir := WorkspaceDir(configDir, "T999")
	for _, fname := range []string{"workspace.json", "channels.json"} {
		path := filepath.Join(wsDir, fname)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", path, err)
		}
	}

	// Load should restore the same fields.
	loaded, err := Load(configDir, "T999")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Config.TeamID != "T999" {
		t.Errorf("TeamID: got %q, want %q", loaded.Config.TeamID, "T999")
	}
	if loaded.Config.Name != "Test Workspace" {
		t.Errorf("Name: got %q, want %q", loaded.Config.Name, "Test Workspace")
	}
	if loaded.Config.BotToken != "xoxb-test" {
		t.Errorf("BotToken: got %q, want %q", loaded.Config.BotToken, "xoxb-test")
	}

	// LoadAll should return exactly one workspace.
	all, err := LoadAll(configDir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("LoadAll: got %d workspaces, want 1", len(all))
	}
	if all[0].Config.TeamID != "T999" {
		t.Errorf("LoadAll[0].TeamID: got %q, want %q", all[0].Config.TeamID, "T999")
	}

	// Delete should remove the workspace dir.
	if err := Delete(configDir, "T999"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Errorf("workspace dir should be gone after Delete, stat err: %v", err)
	}

	// LoadAll should now return empty slice.
	all2, err := LoadAll(configDir)
	if err != nil {
		t.Fatalf("LoadAll after delete: %v", err)
	}
	if len(all2) != 0 {
		t.Errorf("LoadAll after delete: got %d workspaces, want 0", len(all2))
	}
}

// ── TestChannelMetaPersistence ───────────────────────────────────────────────

func TestChannelMetaPersistence(t *testing.T) {
	configDir := tmpDir(t)

	ws, err := Create(configDir, WorkspaceConfig{TeamID: "T777", Name: "Meta Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ws.ChannelMeta["C001"] = ChannelMeta{Alias: "general-alias", Hidden: false}
	ws.ChannelMeta["C002"] = ChannelMeta{Alias: "", Hidden: true, SortKey: 5}

	if err := Save(configDir, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(configDir, "T777")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	c1, ok := loaded.ChannelMeta["C001"]
	if !ok {
		t.Fatal("ChannelMeta[C001] missing after load")
	}
	if c1.Alias != "general-alias" {
		t.Errorf("C001.Alias: got %q, want %q", c1.Alias, "general-alias")
	}
	if c1.Hidden {
		t.Error("C001.Hidden: expected false")
	}

	c2, ok := loaded.ChannelMeta["C002"]
	if !ok {
		t.Fatal("ChannelMeta[C002] missing after load")
	}
	if !c2.Hidden {
		t.Error("C002.Hidden: expected true")
	}
	if c2.SortKey != 5 {
		t.Errorf("C002.SortKey: got %d, want 5", c2.SortKey)
	}
}

// ── TestMigrateFromConfig ────────────────────────────────────────────────────

func TestMigrateFromConfig(t *testing.T) {
	dir := t.TempDir()
	src := MigrationSource{
		BotToken:       "xoxb-old",
		AppToken:       "xapp-old",
		UserToken:      "xoxp-old",
		ChannelAliases: map[string]string{"C001": "dev", "C002": "ops"},
		HiddenChannels: []string{"C003"},
		LastChannelID:  "C001",
	}
	err := MigrateFromConfig(dir, "T_OLD", "Old Corp", src)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ws, err := Load(dir, "T_OLD")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ws.Config.BotToken != "xoxb-old" {
		t.Fatalf("token: %q", ws.Config.BotToken)
	}
	if ws.Config.Name != "Old Corp" {
		t.Fatalf("name: %q", ws.Config.Name)
	}
	if ws.ChannelMeta["C001"].Alias != "dev" {
		t.Fatalf("alias: %q", ws.ChannelMeta["C001"].Alias)
	}
	if !ws.ChannelMeta["C003"].Hidden {
		t.Fatal("C003 should be hidden")
	}
	if ws.Config.LastChannel != "C001" {
		t.Fatalf("last channel: %q", ws.Config.LastChannel)
	}
	// Running again should be a no-op (already exists).
	err = MigrateFromConfig(dir, "T_OLD", "Old Corp", src)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// ── TestLoadAllEmpty ─────────────────────────────────────────────────────────

func TestLoadAllEmpty(t *testing.T) {
	configDir := tmpDir(t)

	// No workspaces directory created at all — should return empty non-nil slice.
	all, err := LoadAll(configDir)
	if err != nil {
		t.Fatalf("LoadAll on empty dir: %v", err)
	}
	if all == nil {
		t.Fatal("LoadAll returned nil, want non-nil empty slice")
	}
	if len(all) != 0 {
		t.Errorf("LoadAll: got %d items, want 0", len(all))
	}
}
