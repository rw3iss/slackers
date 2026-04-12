package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/auth"
	"github.com/rw3iss/slackers/internal/backup"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/friends"
	"github.com/rw3iss/slackers/internal/setup"
	"github.com/rw3iss/slackers/internal/slack"
	themepkg "github.com/rw3iss/slackers/internal/theme"
	"github.com/rw3iss/slackers/internal/tui"
	"github.com/rw3iss/slackers/internal/types"
	"github.com/rw3iss/slackers/internal/workspace"
	"github.com/spf13/cobra"
)

// resetTerminal forces the terminal back to a sane state.
func resetTerminal() {
	// Exit alternate screen buffer.
	fmt.Fprint(os.Stdout, "\033[?1049l")
	// Show cursor.
	fmt.Fprint(os.Stdout, "\033[?25h")
	// Reset all attributes (colors, bold, etc).
	fmt.Fprint(os.Stdout, "\033[0m")
	// Reset title.
	fmt.Fprint(os.Stdout, "\033]0;\a")
	// Disable mouse tracking (in case it was enabled).
	fmt.Fprint(os.Stdout, "\033[?1000l\033[?1002l\033[?1003l\033[?1006l")
	// Re-enable line wrapping.
	fmt.Fprint(os.Stdout, "\033[?7h")
	// Reset scrolling region to full screen.
	fmt.Fprint(os.Stdout, "\033[r")
	// Move cursor to bottom-left and clear line.
	fmt.Fprint(os.Stdout, "\033[999;1H\r\n")
	// Use stty to restore cooked mode, echo, etc.
	if cmd := exec.Command("stty", "sane"); cmd != nil {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		_ = cmd.Run()
	}
}

var version = "0.21.0"

var rootCmd = &cobra.Command{
	Use:   "slackers",
	Short: "A terminal-based Slack client",
	Long:  "Slackers is a TUI Slack client built with Go, Bubbletea, and the Slack API.",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		botToken, _ := cmd.Flags().GetString("bot-token")
		appToken, _ := cmd.Flags().GetString("app-token")
		sidebarWidth, _ := cmd.Flags().GetInt("sidebar-width")
		debugMode, _ := cmd.Flags().GetBool("debug")

		if debugMode {
			if err := debug.Init(""); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not init debug log: %v\n", err)
			} else {
				defer debug.Close()
				// Startup banner with a unique marker so we can
				// verify which build is actually running. If you
				// don't see "BUILD-MARKER-FRIENDPILL" in the log
				// after launching, you're running a stale binary.
				debug.Log("=== slackers starting (BUILD-MARKER-FRIENDPILL v1) ===")
			}
		}

		if configPath == "" {
			configPath = config.DefaultConfigPath()
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if botToken != "" {
			cfg.BotToken = botToken
		}
		if appToken != "" {
			cfg.AppToken = appToken
		}
		if sidebarWidth > 0 {
			cfg.SidebarWidth = sidebarWidth
		}

		if err := cfg.Validate(); err != nil {
			fmt.Println("Configuration is incomplete. Starting setup...")
			fmt.Println()
			if setupErr := runSetupFlow(cfg); setupErr != nil {
				return setupErr
			}
		}

		// Friends-only safety net: if the user previously chose
		// "Skip" but never had SecureMode flipped on, do it now —
		// otherwise the P2P node won't start and Test Connection
		// reports "P2P not available". Also seed P2PAddress + name
		// best-effort.
		if cfg.SetupSkipped && cfg.BotToken == "" {
			dirty := false
			if !cfg.SecureMode {
				cfg.SecureMode = true
				dirty = true
			}
			if cfg.MyName == "" {
				if h, hErr := os.Hostname(); hErr == nil && h != "" {
					cfg.MyName = h
					dirty = true
				}
			}
			if cfg.P2PAddress == "" {
				// Don't block startup — detect IP in background.
				go func() {
					if ip, ipErr := detectPublicIPCLI(); ipErr == nil && ip != "" {
						cfg.P2PAddress = ip
						_ = config.Save(cfg)
					}
				}()
			}
			if dirty {
				_ = config.Save(cfg)
			}
		}

		// Ensure terminal is restored no matter how we exit.
		defer resetTerminal()

		// Catch signals to reset terminal before exit.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		go func() {
			<-sigCh
			resetTerminal()
			os.Exit(0)
		}()

		// Auto-update check (if enabled).
		autoUpdate := cfg.AutoUpdate == nil || *cfg.AutoUpdate // default true
		if autoUpdate {
			latest, downloadURL, err := checkLatestRelease()
			if err == nil && latest != "" {
				latestClean := strings.TrimPrefix(latest, "v")
				if latestClean != version && latestClean > version && downloadURL != "" {
					fmt.Printf("Updating to %s...\n", latest)
					exePath, _ := os.Executable()
					exePath, _ = filepath.EvalSymlinks(exePath)
					tmpPath := exePath + ".new"
					if err := downloadBinary(downloadURL, tmpPath); err == nil {
						os.Chmod(tmpPath, 0755)
						backupPath := exePath + ".bak"
						os.Remove(backupPath)
						if os.Rename(exePath, backupPath) == nil {
							if os.Rename(tmpPath, exePath) == nil {
								os.Remove(backupPath)
								fmt.Printf("Updated to %s. Restarting...\n", latest)
								// Re-exec the new binary with the same args.
								syscall.Exec(exePath, os.Args, os.Environ())
							} else {
								os.Rename(backupPath, exePath)
							}
						}
						os.Remove(tmpPath)
					}
				}
			}
		}

		// Load friends list.
		friendStore := friends.NewFriendStore(friends.DefaultPath())
		if err := friendStore.Load(); err != nil {
			debug.Log("[friends] load error: %v", err)
		}

		// Create friend chat history store.
		friendHistory := friends.NewChatHistoryStore(
			friends.DefaultHistoryDir(),
			cfg.FriendHistoryEncrypt,
		)
		// Prune old history on startup.
		if cfg.FriendHistoryDays > 0 {
			friendHistory.Prune(cfg.FriendHistoryDays)
		}

		// Load workspaces from disk.
		wsList, wsErr := workspace.LoadAll(cfg.ConfigDir())
		if wsErr != nil {
			debug.Log("workspace load error: %v", wsErr)
			wsList = []*workspace.Workspace{}
		}

		// Migrate from old single-workspace config if needed.
		if cfg.NeedsMigration() && len(wsList) == 0 {
			// Try AuthTest to get teamID for migration.
			migrated := false
			tmpSvc := slack.NewSlackClient(cfg.BotToken, cfg.UserToken)
			teamName, teamID, authErr := tmpSvc.AuthTest()
			if authErr == nil && teamID != "" {
				migSrc := workspace.MigrationSource{
					BotToken:       cfg.BotToken,
					AppToken:       cfg.AppToken,
					UserToken:      cfg.UserToken,
					ClientID:       cfg.ClientID,
					ClientSecret:   cfg.ClientSecret,
					ChannelAliases: cfg.ChannelAliases,
					HiddenChannels: cfg.HiddenChannels,
					LastChannelID:  cfg.LastChannelID,
				}
				if migErr := workspace.MigrateFromConfig(cfg.ConfigDir(), teamID, teamName, migSrc); migErr != nil {
					debug.Log("workspace migration error: %v", migErr)
				} else {
					cfg.LastActiveWorkspace = teamID
					cfg.ClearWorkspaceFields()
					_ = config.Save(cfg)
					// Reload workspaces after migration.
					wsList, _ = workspace.LoadAll(cfg.ConfigDir())
					migrated = true
					debug.Log("migrated single-workspace config to workspace %s (%s)", teamID, teamName)
				}
			} else {
				debug.Log("workspace migration skipped (offline or auth failed): %v", authErr)
			}

			// If migration failed (offline), create an in-memory workspace
			// from the old config tokens so the app still works.
			if !migrated && cfg.BotToken != "" {
				inMemWs := &workspace.Workspace{
					Config: workspace.WorkspaceConfig{
						TeamID:       "legacy",
						Name:         "Workspace",
						BotToken:     cfg.BotToken,
						AppToken:     cfg.AppToken,
						UserToken:    cfg.UserToken,
						ClientID:     cfg.ClientID,
						ClientSecret: cfg.ClientSecret,
						AutoSignIn:   true,
						LastChannel:  cfg.LastChannelID,
					},
					Users:       make(map[string]types.User),
					ChannelMeta: make(map[string]workspace.ChannelMeta),
					LastSeen:    make(map[string]string),
				}
				wsList = []*workspace.Workspace{inMemWs}
				if cfg.LastActiveWorkspace == "" {
					cfg.LastActiveWorkspace = "legacy"
				}
			}
		}

		model := tui.NewModel(wsList, cfg, version, friendStore, friendHistory)
		opts := []tea.ProgramOption{tea.WithAltScreen()}
		if cfg.MouseEnabled {
			opts = append(opts, tea.WithMouseCellMotion())
		}
		p := tea.NewProgram(model, opts...)
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}

		return nil
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup [json|hash|--client-id X --client-secret Y --app-token Z]",
	Short: "Run interactive setup wizard, or import/share workspace credentials",
	Long: `Set up Slackers with your Slack tokens.

With no arguments, runs the interactive wizard (manual token entry or OAuth).

With a single positional argument, parses and imports credentials in any of
the supported formats:

  slackers setup '{"client_id":"...","client_secret":"...","app_token":"..."}'
  slackers setup <compact-hash>

With --client-id / --client-secret / --app-token / --user-token flags, writes
those fields directly into the config:

  slackers setup --client-id 123.456 --client-secret deadbeef --app-token xapp-...

To print the current workspace config in a shareable format:

  slackers setup share          # default: compact hash
  slackers setup share hash     # explicit compact hash
  slackers setup share json     # full JSON

The shared output includes the import commands teammates can run to
bootstrap their own clients. The user OAuth token (xoxp-) is intentionally
omitted from 'setup share' output — only client id / client secret / app
token are exported.`,
	DisableFlagParsing: true, // we parse flags ourselves via setup.ParseFlags
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			cfg, _ = config.Load(config.DefaultConfigPath())
		}

		// `slackers setup` with no args → interactive wizard.
		if len(args) == 0 {
			return runSetupFlow(cfg)
		}

		// `slackers setup share [hash|json]`
		if args[0] == "share" {
			return runSetupShare(cfg, args[1:])
		}

		// Otherwise this is an import. Try the three format
		// paths via internal/setup.ParseAny (JSON, flags, hash).
		// Joining args back with spaces lets `setup --a 1 --b 2`
		// and `setup '<hash with no spaces>'` both flow through
		// the same parser.
		joined := strings.Join(args, " ")
		parsed, err := setup.ParseAny(joined)
		if err != nil {
			return fmt.Errorf("setup: %w", err)
		}
		return runSetupImportCLI(cfg, parsed)
	},
}

// runSetupImportCLI applies a parsed setup.Config to the on-disk
// config file and prints a summary to stdout. Used from
// `slackers setup <arg>`. When the CLI detects existing
// credentials we still overwrite — there's no interactive TUI
// here to prompt y/Enter on, and the user deliberately invoked
// the import by running the command.
func runSetupImportCLI(cfg *config.Config, payload setup.Config) error {
	if payload.IsEmpty() {
		return fmt.Errorf("setup: payload has no fields")
	}
	changed := []string{}
	if payload.ClientID != "" && cfg.ClientID != payload.ClientID {
		cfg.ClientID = payload.ClientID
		changed = append(changed, "client id")
	}
	if payload.ClientSecret != "" && cfg.ClientSecret != payload.ClientSecret {
		cfg.ClientSecret = payload.ClientSecret
		changed = append(changed, "client secret")
	}
	if payload.AppToken != "" && cfg.AppToken != payload.AppToken {
		cfg.AppToken = payload.AppToken
		changed = append(changed, "app token")
	}
	if payload.UserToken != "" && cfg.UserToken != payload.UserToken {
		cfg.UserToken = payload.UserToken
		changed = append(changed, "user token")
	}
	if len(changed) == 0 {
		fmt.Println("setup: every field already matches the current config — nothing to do")
		return nil
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("setup: saved in memory but failed to persist: %w", err)
	}
	fmt.Printf("setup: updated %s\n", strings.Join(changed, ", "))
	fmt.Println("restart slackers for Slack services to pick up the new tokens")
	return nil
}

// runSetupShare prints the current config in either hash (default)
// or JSON form with the matching CLI + in-app import commands
// prefixed so teammates can copy-paste straight in.
//
// The user OAuth token (xoxp-) is intentionally excluded from the
// shared payload — even a leaked hash can only bootstrap a fresh
// OAuth login, not impersonate the sharer.
func runSetupShare(cfg *config.Config, args []string) error {
	format := "hash"
	if len(args) > 0 {
		format = strings.ToLower(args[0])
	}
	if format != "hash" && format != "json" {
		return fmt.Errorf("setup share: format must be 'hash' or 'json', got %q", format)
	}
	share := setup.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AppToken:     cfg.AppToken,
		// UserToken intentionally omitted.
	}
	if share.IsEmpty() {
		return fmt.Errorf("setup share: no workspace credentials configured yet — run 'slackers setup' first")
	}

	hash, err := setup.Encode(share)
	if err != nil {
		return fmt.Errorf("setup share: encode hash: %w", err)
	}
	js, err := share.ToJSON()
	if err != nil {
		return fmt.Errorf("setup share: encode json: %w", err)
	}

	fmt.Println("Run this command to set up slackers with your current workspace.")
	fmt.Println("Either format will work — the hash is shorter for sharing.")
	fmt.Println()
	fmt.Println("  slackers setup '" + js + "'")
	fmt.Println("  slackers setup " + hash)
	fmt.Println()
	fmt.Println("Or from inside a running slackers instance:")
	fmt.Println()
	fmt.Println("  /setup " + js)
	fmt.Println("  /setup " + hash)
	fmt.Println()
	if format == "hash" {
		fmt.Println("Default output (hash):")
		fmt.Println(hash)
	} else {
		fmt.Println("JSON output:")
		fmt.Println(js)
	}
	return nil
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authorize with Slack via browser (OAuth)",
	Long: `Opens your browser to authorize Slackers with your Slack workspace.
This automatically obtains your bot and user tokens.

You still need an App-Level Token (xapp-...) for real-time messaging.
If not already configured, you will be prompted for it.

Requires client_id and client_secret to be set in your config, or provided
via flags. The Slack app admin can share these with teammates.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			cfg, _ = config.Load(config.DefaultConfigPath())
		}

		clientID, _ := cmd.Flags().GetString("client-id")
		clientSecret, _ := cmd.Flags().GetString("client-secret")
		appToken, _ := cmd.Flags().GetString("app-token")

		if clientID != "" {
			cfg.ClientID = clientID
		}
		if clientSecret != "" {
			cfg.ClientSecret = clientSecret
		}
		if appToken != "" {
			cfg.AppToken = appToken
		}

		if err := runOAuthFlow(cfg); err != nil {
			return err
		}

		return nil
	},
}

var joinCmd = &cobra.Command{
	Use:   "join <team-config-url>",
	Short: "Join a workspace using a team config URL",
	Long: `One-command onboarding for teammates. The workspace admin hosts a small
JSON config file containing the Client ID, Client Secret, and App-Level Token.
This command fetches that config, then opens the browser for OAuth authorization.

Example:
  slackers join https://example.com/slackers-team.json
  slackers join https://gist.githubusercontent.com/user/abc123/raw/team.json

The team config JSON format:
  {
    "client_id": "1234567890.1234567890",
    "client_secret": "abc123...",
    "app_token": "xapp-..."
  }`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		teamURL := args[0]

		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			cfg, _ = config.Load(config.DefaultConfigPath())
		}

		fmt.Printf("Fetching team config from %s...\n", teamURL)

		teamCfg, err := fetchTeamConfig(teamURL)
		if err != nil {
			return fmt.Errorf("failed to fetch team config: %w", err)
		}

		if teamCfg.ClientID != "" {
			cfg.ClientID = teamCfg.ClientID
		}
		if teamCfg.ClientSecret != "" {
			cfg.ClientSecret = teamCfg.ClientSecret
		}
		if teamCfg.AppToken != "" {
			cfg.AppToken = teamCfg.AppToken
		}

		fmt.Println("Team config loaded.")

		if err := runOAuthFlowWithTeamID(cfg, teamCfg.TeamID); err != nil {
			return err
		}

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("slackers v%s\n", version)
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := config.DefaultConfigPath()
		fmt.Printf("Config path: %s\n\n", configPath)

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		boolStr := func(b bool) string {
			if b {
				return "on"
			}
			return "off"
		}
		boolPtrStr := func(b *bool, def bool) string {
			if b == nil {
				return boolStr(def) + " (default)"
			}
			return boolStr(*b)
		}
		intDefault := func(v, def int) string {
			if v == 0 {
				return fmt.Sprintf("%d (default)", def)
			}
			return fmt.Sprintf("%d", v)
		}
		strDefault := func(v, def string) string {
			if v == "" {
				return def + " (default)"
			}
			return v
		}

		fmt.Println("Current configuration:")
		fmt.Println()
		fmt.Println("  Credentials:")
		fmt.Printf("    bot_token:          %s\n", maskToken(cfg.BotToken))
		fmt.Printf("    app_token:          %s\n", maskToken(cfg.AppToken))
		fmt.Printf("    user_token:         %s\n", maskToken(cfg.UserToken))
		fmt.Printf("    client_id:          %s\n", maskToken(cfg.ClientID))
		fmt.Printf("    client_secret:      %s\n", maskToken(cfg.ClientSecret))
		fmt.Println()
		fmt.Println("  Display:")
		fmt.Printf("    sidebar_width:      %s\n", intDefault(cfg.SidebarWidth, 30))
		fmt.Printf("    timestamp_format:   %s\n", strDefault(cfg.TimestampFormat, "15:04"))
		fmt.Printf("    mouse_enabled:      %s\n", boolStr(cfg.MouseEnabled))
		fmt.Printf("    notifications:      %s\n", boolStr(cfg.Notifications))
		fmt.Printf("    channel_sort_by:    %s\n", strDefault(cfg.ChannelSortBy, "type"))
		sortAsc := "asc"
		if cfg.ChannelSortAsc != nil && !*cfg.ChannelSortAsc {
			sortAsc = "desc"
		}
		fmt.Printf("    channel_sort_asc:   %s\n", sortAsc)
		fmt.Printf("    download_path:      %s\n", strDefault(cfg.DownloadPath, "~/Downloads"))
		fmt.Println()
		fmt.Println("  Polling:")
		fmt.Printf("    poll_interval:      %ss\n", intDefault(cfg.PollInterval, 10))
		fmt.Printf("    poll_interval_bg:   %ss\n", intDefault(cfg.PollIntervalBg, 30))
		fmt.Printf("    poll_priority:      %s\n", intDefault(cfg.PollPriority, 3))
		fmt.Println()
		fmt.Println("  Behavior:")
		fmt.Printf("    auto_update:        %s\n", boolPtrStr(cfg.AutoUpdate, true))
		if cfg.AwayTimeout > 0 {
			fmt.Printf("    away_timeout:       %ds\n", cfg.AwayTimeout)
		} else {
			fmt.Printf("    away_timeout:       disabled\n")
		}
		fmt.Printf("    input_history_max:  %s\n", intDefault(cfg.InputHistoryMax, 20))
		fmt.Println()
		fmt.Println("  Secure Messaging:")
		fmt.Printf("    secure_mode:        %s\n", boolStr(cfg.SecureMode))
		fmt.Printf("    p2p_port:           %s\n", intDefault(cfg.P2PPort, 9900))
		fmt.Printf("    p2p_address:        %s\n", strDefault(cfg.P2PAddress, "(auto)"))
		fmt.Printf("    secure_key_path:    %s\n", strDefault(cfg.SecureKeyPath, "(default)"))
		fmt.Printf("    secure_whitelist:   %d users\n", len(cfg.SecureWhitelist))
		fmt.Println()
		fmt.Println("  State:")
		fmt.Printf("    last_channel_id:    %s\n", strDefault(cfg.LastChannelID, "(none)"))
		fmt.Printf("    hidden_channels:    %d\n", len(cfg.HiddenChannels))
		fmt.Printf("    channel_aliases:    %d\n", len(cfg.ChannelAliases))
		fmt.Printf("    collapsed_groups:   %d\n", len(cfg.CollapsedGroups))
		fmt.Printf("    input_history:      %d entries\n", len(cfg.InputHistory))
		fmt.Printf("    last_seen_ts:       %d channels\n", len(cfg.LastSeenTS))
		fStore := friends.NewFriendStore(friends.DefaultPath())
		_ = fStore.Load()
		fmt.Printf("    friends:            %d\n", fStore.Count())

		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Current version: v%s\n", version)
		fmt.Println("Checking for updates...")

		latest, downloadURL, err := checkLatestRelease()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if latest == version || latest == "v"+version {
			fmt.Println("You're already on the latest version.")
			return nil
		}

		fmt.Printf("New version available: %s\n\n", latest)
		fmt.Print("Update now? [y/N]: ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Update cancelled.")
			return nil
		}

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot find current binary: %w", err)
		}
		exePath, err = filepath.EvalSymlinks(exePath)
		if err != nil {
			return fmt.Errorf("cannot resolve binary path: %w", err)
		}

		fmt.Printf("Downloading %s...\n", downloadURL)
		tmpPath := exePath + ".new"
		if err := downloadBinary(downloadURL, tmpPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("download failed: %w", err)
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("chmod failed: %w", err)
		}

		// Replace the binary atomically.
		backupPath := exePath + ".bak"
		os.Remove(backupPath)
		if err := os.Rename(exePath, backupPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("backup failed: %w", err)
		}
		if err := os.Rename(tmpPath, exePath); err != nil {
			// Try to restore backup.
			os.Rename(backupPath, exePath)
			return fmt.Errorf("replace failed: %w", err)
		}
		os.Remove(backupPath)

		fmt.Printf("Updated to %s successfully.\n", latest)
		return nil
	},
}

// checkLatestRelease queries GitHub for the latest release tag and asset URL.
func checkLatestRelease() (string, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/rw3iss/slackers/releases/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", "", err
	}

	// Find the right asset for this platform.
	assetName := platformAssetName()
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return release.TagName, asset.BrowserDownloadURL, nil
		}
	}

	return release.TagName, "", fmt.Errorf("no binary found for %s in release %s", assetName, release.TagName)
}

// platformAssetName returns the expected release asset name for the current OS/arch.
func platformAssetName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	name := fmt.Sprintf("slackers-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// downloadBinary downloads a URL to a local file path.
func downloadBinary(url, destPath string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

var scriptsCmd = &cobra.Command{
	Use:   "scripts",
	Short: "Run project scripts (install, uninstall, cleanup)",
}

var scriptsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Run the install script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScript("install.sh")
	},
}

var scriptsUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Run the uninstall script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScript("uninstall.sh")
	},
}

var scriptsCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Run the cleanup script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScript("cleanup.sh")
	},
}

var friendsHelpCmd = &cobra.Command{
	Use:   "friends",
	Short: "How to set up private P2P friend connections",
	Run: func(cmd *cobra.Command, args []string) {
		goos := runtime.GOOS

		fmt.Println(`
SLACKERS FRIENDS — Private P2P Chat Guide
==========================================

The Friends feature lets you chat directly with other Slackers users
over end-to-end encrypted libp2p peer-to-peer connections. Messages
never pass through Slack's servers.

QUICK START — over Slack
-------------------------
1. Both users install Slackers and turn ON Secure Mode in
   Friends Config > Edit My Info.
2. User A opens a Slack DM with User B and presses Ctrl+B (Befriend)
   to send a friend request over P2P.
3. User B sees a popup (or auto-accepts, see below) and confirms.
4. Both users now have each other in the "Friends" sidebar section.
5. Click a friend's name to open a private P2P chat.

QUICK START — manual share (works across workspaces / strangers)
----------------------------------------------------------------
1. Open Friends Config (Ctrl+F or via Settings) > Edit My Info.
   - Set your Name (optional Email).
   - Press 'r' on the P2P Endpoint row to auto-detect your public IP.
   - Make sure Secure Mode is ON.
2. Friends Config > Share My Info shows four ways to share:
   * The full JSON contact card (Enter to copy)
   * [ Export to file ] — writes the JSON to ~/Downloads
   * One-line CLI import command (Enter to copy):
       slackers import-friend SLF2.<short hash>
   * Hash only — the bare SLF2.<...> token (Enter to copy)
3. Send any one of these to your friend by any channel — chat,
   email, paper. The hash is the smallest (~109 chars) and the
   easiest to paste.
4. Your friend either:
   a) Runs the one-line CLI command in their terminal, OR
   b) Opens Friends Config > Add a Friend and presses Ctrl-J to
      paste either the JSON or the SLF2.<hash> from clipboard,
      then Ctrl-S to save.
5. Saving in Add a Friend automatically dials the new peer and
   sends a friend-request handshake. If they're online, the
   handshake completes and the connection is live immediately.
6. Repeat in the other direction so both sides know each other.

AUTO-ACCEPT
-----------
Friends Config > Edit My Info > Auto-accept = ON
   Incoming friend requests are accepted silently in the background;
   the new friend appears in your sidebar and a status message
   confirms it. With Auto-accept OFF, requests show a notification
   and a confirmation modal you can accept or reject.`)

		fmt.Println(`
NETWORK SETUP
-------------
For P2P connections to work, each user needs port 9900/tcp (or their
configured P2P port) reachable from the internet.`)

		switch goos {
		case "linux":
			fmt.Println(`
  Linux (ufw):
    sudo ufw allow 9900/tcp

  Linux (iptables):
    sudo iptables -A INPUT -p tcp --dport 9900 -j ACCEPT

  Linux (firewalld):
    sudo firewall-cmd --add-port=9900/tcp --permanent
    sudo firewall-cmd --reload`)
		case "darwin":
			fmt.Println(`
  macOS:
    System Settings > Network > Firewall > Options
    Add Slackers to allowed incoming connections

  Or via command line:
    sudo /usr/libexec/ApplicationFirewall/socketfilterfw --add $(which slackers)`)
		case "windows":
			fmt.Println(`
  Windows (PowerShell as Administrator):
    New-NetFirewallRule -DisplayName "Slackers P2P" -Direction Inbound -Protocol TCP -LocalPort 9900 -Action Allow

  Or via command line:
    netsh advfirewall firewall add rule name="Slackers P2P" dir=in action=allow protocol=TCP localport=9900`)
		}

		fmt.Println(`
ROUTER / PORT FORWARDING
-------------------------
If behind a NAT router, forward port 9900 TCP to your local IP:
  1. Find your local IP (ip addr / ifconfig / ipconfig)
  2. Log into your router admin panel
  3. Add a port forwarding rule: external 9900 → internal IP:9900

Slackers uses libp2p with UPnP and hole punching, so port forwarding
may not always be required — but it increases reliability.

CONFIGURATION
-------------
  Friends Config > Edit My Info:
    Name              Display name in friends' sidebars
    Email             Optional, for uniqueness checks
    P2P Endpoint      Your public IP/hostname (press 'r' to auto-detect)
    P2P Port          Default 9900
    Secure Mode       Must be ON for any P2P feature
    Auto-accept       Auto-accept incoming friend requests

  Keyboard shortcuts (rebindable in Settings > Shortcuts):
    Ctrl+B            Befriend the current Slack DM user
    Alt+I             Open friend details for the current friend chat
    Alt+N             Notifications view (unread / reactions / requests)
    Ctrl+F            Friends Config

IMPORT / EXPORT
---------------
  Single friend (CLI):
    slackers import-friend <file>          (JSON file path)
    slackers import-friend SLF2.<hash>     (hash from Share My Info)
    slackers import-friend SLF1.<hash>     (legacy gzip+JSON hash, still supported)
    slackers import-friend ... --yes       (overwrite existing match)

  Single friend (UI):
    Friends Config > Add a Friend > Ctrl-J
      Pastes JSON or SLF2/SLF1 hash from clipboard.
    Friends Config > Edit Friend > Ctrl-J
      Pastes JSON or hash and merges any non-empty fields into
      blank slots on the existing friend (existing values are
      never overwritten).

  Bulk friends list:
    Friends Config > Export Friends List   → all friends as JSON
    Friends Config > Import Friends List   → with conflict overwrite

SECURITY
--------
  - libp2p TCP transport with NAT hole punching enabled
  - X25519 key exchange + ChaCha20-Poly1305 message encryption
  - Each friendship derives its own per-pair key (rotatable from
    Edit Friend > Public Key > Enter)
  - Friend records and per-pair keys live in ~/.config/slackers/friends.json
  - Chat history is encrypted on disk per-friend
  - Messages are never relayed through Slack or any third party

CONTACT CARD FORMATS
--------------------
  SLF2 (current, ~109 chars)
    Compact binary format. Includes only what's needed to dial:
    public key, peer ID, IPv4, port. Display name is filled in
    automatically as "Friend <last 8 chars of peer ID>" on
    import — you can rename later.

  SLF1 (legacy)
    gzip+base64url(JSON). Larger (~280 chars) but still accepted
    by every import path for backward compatibility.

  JSON
    Full contact card with name/email/etc. Verbose but
    human-readable. Can be saved to a file or pasted directly.`)
	},
}

func init() {
	rootCmd.SilenceUsage = true

	rootCmd.Flags().String("config", "", "Path to custom config file")
	rootCmd.Flags().String("bot-token", "", "Override Slack bot token")
	rootCmd.Flags().String("app-token", "", "Override Slack app token")
	rootCmd.Flags().Int("sidebar-width", 0, "Override sidebar width")
	rootCmd.Flags().Bool("debug", false, "Enable debug logging to ~/.config/slackers/debug.log")

	exportCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	importCmd.Flags().String("mode", "", "Import mode: replace or merge")
	importCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")

	loginCmd.Flags().String("client-id", "", "Slack app Client ID")
	loginCmd.Flags().String("client-secret", "", "Slack app Client Secret")
	loginCmd.Flags().String("app-token", "", "Slack app-level token (xapp-...)")

	scriptsCmd.AddCommand(scriptsInstallCmd)
	scriptsCmd.AddCommand(scriptsUninstallCmd)
	scriptsCmd.AddCommand(scriptsCleanupCmd)

	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(joinCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(scriptsCmd)
	rootCmd.AddCommand(friendsHelpCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(importThemeCmd)
	rootCmd.AddCommand(importFriendCmd)
}

var importFriendCmd = &cobra.Command{
	Use:   "import-friend <file or hash>",
	Short: "Add a friend from a contact-card JSON file or a SLF1.<hash> string",
	Long: `Imports a single friend's contact card into your local friends list.

The argument can be either:
  • A path to a JSON contact card file (relative or absolute), or
  • An SLF1.<...> hash string copied from another user's
    Friends → Share My Info screen.

If a friend with the same Slacker ID, email, or endpoint already exists,
the command prompts to skip, abort, or overwrite the existing entry.
Use --yes to overwrite without prompting.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		input := args[0]
		yes, _ := cmd.Flags().GetBool("yes")

		var card friends.ContactCard
		// 1) Try as a file path first.
		if info, err := os.Stat(input); err == nil && !info.IsDir() {
			data, rerr := os.ReadFile(input)
			if rerr != nil {
				return fmt.Errorf("reading %s: %w", input, rerr)
			}
			parsed, perr := friends.ParseAnyContactCard(string(data))
			if perr != nil {
				return fmt.Errorf("parsing %s: %w", input, perr)
			}
			card = parsed
		} else {
			// 2) Otherwise treat the argument as a literal contact
			//    string (hash or inline JSON).
			parsed, perr := friends.ParseAnyContactCard(input)
			if perr != nil {
				return fmt.Errorf("input is neither a readable file nor a valid contact card or hash: %w", perr)
			}
			card = parsed
		}

		if card.Name == "" && card.SlackerID == "" {
			return fmt.Errorf("contact card has no name or slacker_id")
		}

		// Open the local friend store and check for conflicts.
		store := friends.NewFriendStore(friends.DefaultPath())
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading friends: %w", err)
		}
		incoming := friends.FriendFromCard(card)
		conflict := store.FindConflict(incoming)
		if conflict != "" {
			if !yes {
				fmt.Printf("A friend already exists matching this contact (id=%s).\n", conflict)
				fmt.Print("Overwrite? [y/N]: ")
				var resp string
				fmt.Scanln(&resp)
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "y") {
					fmt.Println("Aborted.")
					return nil
				}
			}
			store.Remove(conflict)
		}
		if err := store.Add(incoming); err != nil {
			return fmt.Errorf("adding friend: %w", err)
		}
		if err := store.Save(); err != nil {
			return fmt.Errorf("saving friends: %w", err)
		}
		fmt.Printf("✓ Imported friend %q (slacker_id=%s)\n", card.Name, card.SlackerID)
		return nil
	},
}

func init() {
	importFriendCmd.Flags().Bool("yes", false, "Overwrite existing matching friend without prompting")
}

var importThemeCmd = &cobra.Command{
	Use:   "import-theme <file>",
	Short: "Install a single theme JSON file into your slackers themes directory",
	Long: `Validates the given theme file and copies it into your local
slackers themes directory so it shows up in Settings → Theme.

If a theme with the same (sanitized) filename already exists, it is
overwritten.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src := args[0]
		t, err := themepkg.Load(src)
		if err != nil {
			return fmt.Errorf("invalid theme file: %w", err)
		}
		if t.Name == "" {
			return fmt.Errorf("theme file is missing a name")
		}
		// Validate the colors map exists.
		if t.Colors == nil {
			return fmt.Errorf("theme file has no colors map")
		}
		path, err := themepkg.Save(t)
		if err != nil {
			return fmt.Errorf("saving theme: %w", err)
		}
		fmt.Printf("✓ Imported theme %q to %s\n", t.Name, path)
		return nil
	},
}

var exportCmd = &cobra.Command{
	Use:   "export [path]",
	Short: "Export your entire slackers config (settings, themes, friends, history) to a zip archive",
	Long: `Exports the entire $XDG_CONFIG_HOME/slackers directory to a single zip
archive. By default the file is written to ~/Downloads with a timestamped
filename. Pass an explicit path to override the destination.

The archive contains your tokens and chat history — treat it like a
sensitive credential file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		var dest string
		if len(args) > 0 {
			dest = args[0]
			if info, err := os.Stat(dest); err == nil && info.IsDir() {
				dest = filepath.Join(dest, backup.DefaultExportName())
			}
		} else {
			dest = filepath.Join(backup.DefaultExportDir(), backup.DefaultExportName())
		}
		if !yes {
			fmt.Printf("This will export your entire slackers config to:\n  %s\n", dest)
			fmt.Print("Continue? [y/N]: ")
			var resp string
			fmt.Scanln(&resp)
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "y") {
				fmt.Println("Cancelled.")
				return nil
			}
		}
		path, err := backup.Export(dest)
		if err != nil {
			return fmt.Errorf("export failed: %w", err)
		}
		fmt.Printf("✓ Exported to %s\n", path)
		return nil
	},
}

var importCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Import a slackers config archive (replace or merge)",
	Long: `Imports a slackers config archive previously created by 'slackers export'.

By default this prompts to choose between replacing your existing
config wholesale or merging the imported data on top (friends are
unioned, chat histories merged by message ID, emoji favorites
unioned, themes added, and the main config overlaid).

Use --mode to skip the prompt:
  --mode replace   wipe the existing config and write the archive
  --mode merge     keep existing data, add/update from the archive`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src := args[0]
		modeFlag, _ := cmd.Flags().GetString("mode")
		yes, _ := cmd.Flags().GetBool("yes")

		var mode backup.MergeMode
		switch strings.ToLower(modeFlag) {
		case "replace":
			mode = backup.MergeReplace
		case "merge":
			mode = backup.MergeUnion
		case "":
			fmt.Printf("Importing %s\n", src)
			fmt.Println("Choose mode:")
			fmt.Println("  [1] Replace — wipe existing config and unpack archive")
			fmt.Println("  [2] Merge   — keep existing data, add/update from archive")
			fmt.Print("Select [1/2]: ")
			var resp string
			fmt.Scanln(&resp)
			switch strings.TrimSpace(resp) {
			case "1":
				mode = backup.MergeReplace
			case "2":
				mode = backup.MergeUnion
			default:
				fmt.Println("Cancelled.")
				return nil
			}
		default:
			return fmt.Errorf("unknown --mode %q (expected replace or merge)", modeFlag)
		}

		if !yes {
			label := "merge"
			if mode == backup.MergeReplace {
				label = "REPLACE"
			}
			fmt.Printf("About to %s your slackers config from:\n  %s\n", label, src)
			fmt.Print("Continue? [y/N]: ")
			var resp string
			fmt.Scanln(&resp)
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "y") {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		if err := backup.Import(src, mode); err != nil {
			return fmt.Errorf("import failed: %w", err)
		}
		fmt.Println("✓ Import complete.")
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runSetupFlow presents the user with a choice between manual and OAuth setup.
func runSetupFlow(cfg *config.Config) error {
	// Ensure Ctrl-C exits during setup prompts.
	setupSig := make(chan os.Signal, 1)
	signal.Notify(setupSig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-setupSig
		fmt.Println("\nSetup cancelled.")
		os.Exit(0)
	}()
	defer signal.Stop(setupSig)

	fmt.Println(tui.BannerText())
	fmt.Println("How would you like to set up Slackers?")
	fmt.Println()
	fmt.Println("  1) Manual  - paste tokens directly")
	fmt.Println("  2) OAuth   - authorize via browser (recommended)")
	fmt.Println("  3) Skip    - no Slack workspace; use friends + P2P only")
	fmt.Println()
	fmt.Println("     (You can add Slack tokens later via 'slackers setup' or by")
	fmt.Println("      editing ~/.config/slackers/config.json — Slack features turn")
	fmt.Println("      on automatically once a valid bot+app token pair is detected.)")
	fmt.Println()
	fmt.Print("Choose [1/2/3]: ")

	var choice string
	fmt.Scanln(&choice)
	fmt.Println()

	switch strings.TrimSpace(choice) {
	case "2":
		return runOAuthFlow(cfg)
	case "3":
		return runSkipFlow(cfg)
	default:
		return runManualFlow(cfg)
	}
}

// detectPublicIPCLI does a best-effort HTTP lookup to api.ipify.org
// to grab the user's public IPv4. Used by the friends-only setup
// path to seed cfg.P2PAddress on a fresh install.
func detectPublicIPCLI() (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// runSkipFlow saves an empty config (no tokens) so the user can launch
// the TUI in friends-only mode and add Slack tokens later. This makes
// the app usable for pure P2P friend chat without requiring a Slack
// workspace at all.
func runSkipFlow(cfg *config.Config) error {
	cfg.SetupSkipped = true
	// Friends-only mode is useless without P2P, so flip Secure Mode
	// on by default. Users can still toggle it later from
	// Friends Config → Edit My Info.
	cfg.SecureMode = true
	// Default the display name to the OS user/hostname so the user
	// doesn't end up with an empty card on the very first launch.
	if cfg.MyName == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			cfg.MyName = h
		} else if u := os.Getenv("USER"); u != "" {
			cfg.MyName = u
		}
	}
	// Best-effort public IP detection so the multiaddr in the
	// initial contact card is non-empty.
	if cfg.P2PAddress == "" {
		if ip, err := detectPublicIPCLI(); err == nil && ip != "" {
			cfg.P2PAddress = ip
		}
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("Setup complete — Slack features are disabled.")
	fmt.Println()
	fmt.Println("You can:")
	fmt.Println("  • Run 'slackers' to launch the TUI (Friends-only mode).")
	fmt.Println("  • Open Friends Config from Settings to set your name and add friends.")
	fmt.Println("  • Add Slack tokens later by re-running 'slackers setup' or editing")
	fmt.Println("    ~/.config/slackers/config.json — Slack features will activate")
	fmt.Println("    automatically on the next launch once a valid token pair is found.")
	return nil
}

// runManualFlow prompts for tokens via stdin.
func runManualFlow(cfg *config.Config) error {
	prompted, err := config.PromptTokens()
	if err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	cfg.Merge(prompted)

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("Setup complete! Run 'slackers' to start the TUI.")
	return nil
}

// runOAuthFlow handles the browser-based OAuth setup.
func runOAuthFlow(cfg *config.Config) error {
	return runOAuthFlowWithTeamID(cfg, "")
}

// runOAuthFlowWithTeamID handles the browser-based OAuth setup.
// If expectedTeamID is non-empty, the workspace returned by Slack must match
// or the flow is aborted — this prevents a malicious team config from routing
// tokens through an attacker-controlled Slack app.
func runOAuthFlowWithTeamID(cfg *config.Config, expectedTeamID string) error {
	// Ensure we have client credentials.
	if cfg.ClientID == "" {
		fmt.Print("Enter Client ID (from your Slack app's Basic Information page): ")
		fmt.Scanln(&cfg.ClientID)
	}
	if cfg.ClientSecret == "" {
		fmt.Print("Enter Client Secret (from your Slack app's Basic Information page): ")
		fmt.Scanln(&cfg.ClientSecret)
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return fmt.Errorf("client ID and secret are required for OAuth")
	}

	// Run the OAuth flow.
	result, err := auth.RunOAuthFlow(auth.OAuthConfig{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		ExpectedTeamID: expectedTeamID,
	})
	if err != nil {
		return fmt.Errorf("OAuth failed: %w", err)
	}

	cfg.BotToken = result.BotToken
	cfg.UserToken = result.UserToken

	fmt.Println()
	fmt.Printf("Authorized with workspace: %s (team ID: %s, app ID: %s)\n",
		result.TeamName, result.TeamID, result.AppID)
	fmt.Printf("  Bot token: %s\n", maskToken(cfg.BotToken))
	fmt.Printf("  User token: %s\n", maskToken(cfg.UserToken))

	// App-level token is still needed for Socket Mode.
	if cfg.AppToken == "" {
		fmt.Println()
		fmt.Println("One more thing: Slackers needs an App-Level Token for real-time messaging.")
		fmt.Println("Generate one at: https://api.slack.com/apps → your app → Basic Information → App-Level Tokens")
		fmt.Println("Create a token with the 'connections:write' scope.")
		fmt.Println()
		fmt.Print("Enter App-Level Token (xapp-...): ")
		fmt.Scanln(&cfg.AppToken)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("Setup complete! Run 'slackers' to start the TUI.")
	return nil
}

// teamConfig is the JSON format for team config URLs.
type teamConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AppToken     string `json:"app_token"`
	TeamID       string `json:"team_id"`
}

// fetchTeamConfig downloads and parses a team config JSON file from a URL.
// Warns on insecure HTTP and missing team_id but lets the user proceed.
func fetchTeamConfig(teamURL string) (*teamConfig, error) {
	if !strings.HasPrefix(teamURL, "https://") {
		fmt.Println()
		fmt.Println("⚠ Warning: team config URL does not use HTTPS.")
		fmt.Println("  Credentials (client_secret, app_token) will be transmitted in cleartext.")
		fmt.Print("  Continue anyway? [y/N]: ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			return nil, fmt.Errorf("aborted: use an HTTPS URL for secure credential transfer")
		}
	}

	resp, err := http.Get(teamURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, teamURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var tc teamConfig
	if err := json.Unmarshal(body, &tc); err != nil {
		return nil, fmt.Errorf("invalid team config JSON: %w", err)
	}

	if tc.ClientID == "" || tc.ClientSecret == "" {
		return nil, fmt.Errorf("team config must include client_id and client_secret")
	}

	if tc.TeamID == "" {
		fmt.Println()
		fmt.Println("⚠ Warning: team config does not include team_id.")
		fmt.Println("  Workspace identity cannot be verified after OAuth.")
		fmt.Println("  This means you are trusting that this config belongs to the workspace you expect.")
		fmt.Print("  Continue anyway? [y/N]: ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			return nil, fmt.Errorf("aborted: ask the workspace admin to add team_id to the config")
		}
	}

	return &tc, nil
}

func maskToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) <= 10 {
		return token + "..."
	}
	return token[:10] + "..."
}

func runScript(name string) error {
	scriptPath, err := findScript(name)
	if err != nil {
		return err
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findScript(name string) (string, error) {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "scripts", name),
			filepath.Join(exeDir, "..", "scripts", name),
			filepath.Join(exeDir, "..", "share", "slackers", "scripts", name),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		p := filepath.Join(cwd, "scripts", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	home, _ := os.UserHomeDir()
	fallbacks := []string{
		filepath.Join(home, ".local", "share", "slackers", "scripts", name),
		filepath.Join("/usr", "local", "share", "slackers", "scripts", name),
	}
	for _, p := range fallbacks {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("script %q not found; searched near executable and in %s",
		name, strings.Join([]string{"./scripts/", "~/.local/share/slackers/scripts/"}, ", "))
}
