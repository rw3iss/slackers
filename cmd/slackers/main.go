package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/auth"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/tui"
	"github.com/spf13/cobra"
)

var version = "0.2.0"

var rootCmd = &cobra.Command{
	Use:   "slackers",
	Short: "A terminal-based Slack client",
	Long:  "Slackers is a TUI Slack client built with Go, Bubbletea, and the Slack API.",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		botToken, _ := cmd.Flags().GetString("bot-token")
		appToken, _ := cmd.Flags().GetString("app-token")
		sidebarWidth, _ := cmd.Flags().GetInt("sidebar-width")

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

		slackSvc := slack.NewSlackClient(cfg.BotToken, cfg.UserToken)
		socketSvc := slack.NewSocketClient(cfg.BotToken, cfg.AppToken)

		model := tui.NewModel(slackSvc, socketSvc, cfg)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}

		return nil
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run interactive setup wizard",
	Long: `Set up Slackers with your Slack tokens.

Two methods are available:
  1. Manual  - paste tokens directly (you get them from your Slack app settings)
  2. OAuth   - authorize via browser (automatically obtains bot + user tokens)

The OAuth method still requires an App-Level Token (xapp-...) which must be
created manually in your Slack app settings under Socket Mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			cfg, _ = config.Load(config.DefaultConfigPath())
		}

		if err := runSetupFlow(cfg); err != nil {
			return err
		}

		return nil
	},
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

		if clientID != "" {
			cfg.ClientID = clientID
		}
		if clientSecret != "" {
			cfg.ClientSecret = clientSecret
		}

		if err := runOAuthFlow(cfg); err != nil {
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

		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Current configuration:")
		fmt.Printf("  bot_token:        %s\n", maskToken(cfg.BotToken))
		fmt.Printf("  app_token:        %s\n", maskToken(cfg.AppToken))
		fmt.Printf("  user_token:       %s\n", maskToken(cfg.UserToken))
		fmt.Printf("  client_id:        %s\n", maskToken(cfg.ClientID))
		fmt.Printf("  client_secret:    %s\n", maskToken(cfg.ClientSecret))
		fmt.Printf("  sidebar_width:    %d\n", cfg.SidebarWidth)
		fmt.Printf("  timestamp_format: %s\n", cfg.TimestampFormat)

		return nil
	},
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

func init() {
	rootCmd.SilenceUsage = true

	rootCmd.Flags().String("config", "", "Path to custom config file")
	rootCmd.Flags().String("bot-token", "", "Override Slack bot token")
	rootCmd.Flags().String("app-token", "", "Override Slack app token")
	rootCmd.Flags().Int("sidebar-width", 0, "Override sidebar width")

	loginCmd.Flags().String("client-id", "", "Slack app Client ID")
	loginCmd.Flags().String("client-secret", "", "Slack app Client Secret")

	scriptsCmd.AddCommand(scriptsInstallCmd)
	scriptsCmd.AddCommand(scriptsUninstallCmd)
	scriptsCmd.AddCommand(scriptsCleanupCmd)

	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(scriptsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runSetupFlow presents the user with a choice between manual and OAuth setup.
func runSetupFlow(cfg *config.Config) error {
	fmt.Println("How would you like to set up Slackers?")
	fmt.Println()
	fmt.Println("  1) Manual  - paste tokens directly")
	fmt.Println("  2) OAuth   - authorize via browser (recommended)")
	fmt.Println()
	fmt.Print("Choose [1/2]: ")

	var choice string
	fmt.Scanln(&choice)
	fmt.Println()

	switch strings.TrimSpace(choice) {
	case "2":
		return runOAuthFlow(cfg)
	default:
		return runManualFlow(cfg)
	}
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
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	})
	if err != nil {
		return fmt.Errorf("OAuth failed: %w", err)
	}

	cfg.BotToken = result.BotToken
	cfg.UserToken = result.UserToken

	fmt.Println()
	fmt.Printf("Authorized with workspace: %s\n", result.TeamName)
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
