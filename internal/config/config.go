package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds all application configuration values.
type Config struct {
	BotToken                 string            `json:"bot_token"`
	AppToken                 string            `json:"app_token"`
	UserToken                string            `json:"user_token,omitempty"`
	ClientID                 string            `json:"client_id,omitempty"`
	ClientSecret             string            `json:"client_secret,omitempty"`
	SidebarWidth             int               `json:"sidebar_width,omitempty"`
	TimestampFormat          string            `json:"timestamp_format,omitempty"`
	HiddenChannels           []string          `json:"hidden_channels,omitempty"`
	ChannelAliases           map[string]string `json:"channel_aliases,omitempty"`
	ChannelSortBy            string            `json:"channel_sort_by,omitempty"`
	ChannelSortAsc           *bool             `json:"channel_sort_asc,omitempty"`
	LastChannelID            string            `json:"last_channel_id,omitempty"`
	DownloadPath             string            `json:"download_path,omitempty"`
	SecureMode               bool              `json:"secure_mode,omitempty"`
	P2PPort                  int               `json:"p2p_port,omitempty"`
	P2PAddress               string            `json:"p2p_address,omitempty"`
	SecureWhitelist          []string          `json:"secure_whitelist,omitempty"`
	SecureKeyPath            string            `json:"secure_key_path,omitempty"`
	CollapsedGroups          []string          `json:"collapsed_groups,omitempty"`
	AutoUpdate               *bool             `json:"auto_update,omitempty"`
	MouseEnabled             bool              `json:"mouse_enabled,omitempty"`
	AwayTimeout              int               `json:"away_timeout,omitempty"`
	LastSeenTS               map[string]string `json:"last_seen_ts,omitempty"`
	InputHistory             []string          `json:"input_history,omitempty"`
	InputHistoryMax          int               `json:"input_history_max,omitempty"`
	PollInterval             int               `json:"poll_interval,omitempty"`
	PollIntervalBg           int               `json:"poll_interval_bg,omitempty"`
	PollPriority             int               `json:"poll_priority,omitempty"`
	Notifications            bool              `json:"notifications,omitempty"`
	EmojiFavorites           []string          `json:"emoji_favorites,omitempty"`
	FavoriteFolders          []string          `json:"favorite_folders,omitempty"`
	NotificationTimeout      int               `json:"notification_timeout,omitempty"` // seconds; 0 → 3
	AutoAcceptFriendRequests bool              `json:"auto_accept_friend_requests,omitempty"`
	// SetupSkipped is set when the user explicitly chose the
	// friends-only setup path. It lets Validate() pass with no
	// Slack tokens — adding tokens later (via the manual setup
	// flow or by editing the file) automatically reactivates Slack
	// features on the next launch without re-running setup.
	SetupSkipped         bool   `json:"setup_skipped,omitempty"`
	ReplyFormat          string `json:"reply_format,omitempty"`        // "inline" or "inside"
	FriendHistoryDays    int    `json:"friend_history_days,omitempty"` // 0 = keep all
	FriendHistoryEncrypt bool   `json:"friend_history_encrypt,omitempty"`
	Theme                string `json:"theme,omitempty"`
	AltTheme             string `json:"alt_theme,omitempty"`
	SidebarItemSpacing   int    `json:"sidebar_item_spacing,omitempty"`
	MessageItemSpacing   int    `json:"message_item_spacing,omitempty"`
	SlackerID            string `json:"slacker_id,omitempty"`
	MyName               string `json:"my_name,omitempty"`
	MyEmail              string `json:"my_email,omitempty"`
	// ShareMyInfoFormat controls how [FRIEND:me] expansions and any
	// other "share my contact card" automated insertions are encoded
	// when sent over chat. "" or "hash" → SLF2 compact hash (smaller
	// and obfuscated). "json" → raw single-line JSON (full profile,
	// readable in plain text on the wire).
	ShareMyInfoFormat string `json:"share_my_info_format,omitempty"`
	// FriendPingSeconds controls how often the app polls friend
	// connection state, propagates online/offline transitions in
	// the sidebar, fires the profile-sync / request-pending pings
	// on reconnect, and retries any messages still flagged
	// Pending. Minimum enforced at 2s; 0 or missing falls back to
	// the 5s default.
	FriendPingSeconds int    `json:"friend_ping_seconds,omitempty"`
	ConfigPath        string `json:"-"`
}

// DefaultConfigDir returns the default configuration directory (~/.config/slackers/).
func DefaultConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers")
}

// DefaultConfigPath returns the default path to the config.json file.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.json")
}

func boolPtr(b bool) *bool { return &b }

func defaults() *Config {
	home, _ := os.UserHomeDir()
	downloadPath := filepath.Join(home, "Downloads")

	return &Config{
		SidebarWidth:    30,
		TimestampFormat: "15:04",
		AutoUpdate:      boolPtr(true),
		MouseEnabled:    true,
		Notifications:   true,
		PollInterval:    10,
		PollIntervalBg:  30,
		PollPriority:    3,
		InputHistoryMax: 20,
		DownloadPath:    downloadPath,
		P2PPort:         9900,
		// New users default to JSON sharing so [FRIEND:me] expansions
		// carry the full Name/Email profile out of the box. Existing
		// configs that omit the field continue to honour their saved
		// value (an empty string falls through to the same default).
		ShareMyInfoFormat: "json",
		ConfigPath:        DefaultConfigPath(),
	}
}

// Load reads configuration from the given JSON file path.
// If the file does not exist, default values are returned.
func Load(path string) (*Config, error) {
	cfg := defaults()
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if cfg.SidebarWidth == 0 {
		cfg.SidebarWidth = 25
	}
	if cfg.TimestampFormat == "" {
		cfg.TimestampFormat = "15:04"
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10
	}
	cfg.ConfigPath = path

	// Generate a unique SlackerID on first run.
	if cfg.SlackerID == "" {
		cfg.SlackerID = generateSlackerID()
		_ = Save(cfg)
	}

	return cfg, nil
}

// generateSlackerID creates a random 16-byte hex identifier.
func generateSlackerID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Save writes the configuration to its ConfigPath as formatted JSON,
// creating parent directories if they do not exist.
func Save(cfg *Config) error {
	dir := filepath.Dir(cfg.ConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(cfg.ConfigPath, data, 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// NotificationTTL returns the user's notification timeout as a
// time.Duration, falling back to 3 seconds when unset.
func (c *Config) NotificationTTL() time.Duration {
	if c == nil || c.NotificationTimeout <= 0 {
		return 3 * time.Second
	}
	return time.Duration(c.NotificationTimeout) * time.Second
}

// Merge applies non-zero override values into c.
func (c *Config) Merge(overrides *Config) {
	if overrides.BotToken != "" {
		c.BotToken = overrides.BotToken
	}
	if overrides.AppToken != "" {
		c.AppToken = overrides.AppToken
	}
	if overrides.SidebarWidth != 0 {
		c.SidebarWidth = overrides.SidebarWidth
	}
	if overrides.TimestampFormat != "" {
		c.TimestampFormat = overrides.TimestampFormat
	}
	if overrides.UserToken != "" {
		c.UserToken = overrides.UserToken
	}
	if overrides.ClientID != "" {
		c.ClientID = overrides.ClientID
	}
	if overrides.ClientSecret != "" {
		c.ClientSecret = overrides.ClientSecret
	}
	if overrides.ConfigPath != "" {
		c.ConfigPath = overrides.ConfigPath
	}
}

// Validate checks that required configuration fields are present.
// Friends-only mode (SetupSkipped == true) bypasses the token check
// so the app can launch without a Slack workspace.
func (c *Config) Validate() error {
	if c.SetupSkipped {
		return nil
	}
	if c.BotToken == "" {
		return errors.New("bot_token is required")
	}
	if c.AppToken == "" {
		return errors.New("app_token is required")
	}
	return nil
}

// PromptTokens interactively prompts the user for missing tokens via stdin.
// This should be called before the TUI starts.
func PromptTokens() (*Config, error) {
	cfg := &Config{}

	fmt.Print("Enter Bot Token (xoxb-...): ")
	if _, err := fmt.Scanln(&cfg.BotToken); err != nil {
		return nil, fmt.Errorf("reading bot token: %w", err)
	}

	fmt.Print("Enter App Token (xapp-...): ")
	if _, err := fmt.Scanln(&cfg.AppToken); err != nil {
		return nil, fmt.Errorf("reading app token: %w", err)
	}

	fmt.Print("Enter User Token (xoxp-..., optional, press Enter to skip): ")
	var userToken string
	fmt.Scanln(&userToken)
	if userToken != "" {
		cfg.UserToken = userToken
	}

	return cfg, nil
}
