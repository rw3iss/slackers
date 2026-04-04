package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/tui"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

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

		// Merge CLI flag overrides
		if botToken != "" {
			cfg.BotToken = botToken
		}
		if appToken != "" {
			cfg.AppToken = appToken
		}
		if sidebarWidth > 0 {
			cfg.SidebarWidth = sidebarWidth
		}

		// Validate config; if invalid, prompt interactively
		if err := cfg.Validate(); err != nil {
			fmt.Println("Configuration is incomplete. Starting interactive setup...")
			prompted, promptErr := config.PromptTokens()
			if promptErr != nil {
				return fmt.Errorf("setup failed: %w", promptErr)
			}
			cfg.Merge(prompted)
			if saveErr := config.Save(cfg); saveErr != nil {
				return fmt.Errorf("failed to save config: %w", saveErr)
			}
		}

		slackSvc := slack.NewSlackClient(cfg.BotToken, cfg.UserToken)
		socketSvc := slack.NewSocketClient(cfg.BotToken, cfg.AppToken)

		model := tui.NewModel(slackSvc, socketSvc)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			cfg, _ = config.Load(config.DefaultConfigPath())
		}

		prompted, err := config.PromptTokens()
		if err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		cfg.Merge(prompted)

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println("Setup complete! Run 'slackers' to start the TUI.")
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

	scriptsCmd.AddCommand(scriptsInstallCmd)
	scriptsCmd.AddCommand(scriptsUninstallCmd)
	scriptsCmd.AddCommand(scriptsCleanupCmd)

	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(scriptsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// maskToken shows the first 10 characters of a token followed by "..."
// Returns "(not set)" if the token is empty.
func maskToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) <= 10 {
		return token + "..."
	}
	return token[:10] + "..."
}

// runScript locates and executes a shell script from the scripts/ directory.
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

// findScript searches for the script relative to the executable, then falls
// back to known install paths.
func findScript(name string) (string, error) {
	// Try relative to executable
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

	// Try working directory
	if cwd, err := os.Getwd(); err == nil {
		p := filepath.Join(cwd, "scripts", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Fall back to known install paths
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
