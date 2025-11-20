package commands

import (
	"fmt"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/spf13/cobra"
)

var whitelistCmd = &cobra.Command{
	Use:   "whitelist",
	Short: "Manage application whitelist",
	Long:  `Add or remove applications from the whitelist.`,
}

var whitelistAddCmd = &cobra.Command{
	Use:   "add CLASS",
	Short: "Add an application to the whitelist",
	Long:  `Add an application to the whitelist by its window class.`,
	Example: `  # Add Firefox to whitelist
  focusstreamer whitelist add firefox

  # Add terminal to whitelist
  focusstreamer whitelist add gnome-terminal-server`,
	Args: cobra.ExactArgs(1),
	RunE: runWhitelistAdd,
}

var whitelistRemoveCmd = &cobra.Command{
	Use:   "remove CLASS",
	Short: "Remove an application from the whitelist",
	Long:  `Remove an application from the whitelist by its window class.`,
	Example: `  # Remove Firefox from whitelist
  focusstreamer whitelist remove firefox

  # Remove terminal from whitelist
  focusstreamer whitelist remove gnome-terminal-server`,
	Args: cobra.ExactArgs(1),
	RunE: runWhitelistRemove,
}

var whitelistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted applications",
	Long:  `Display all whitelisted applications and patterns.`,
	RunE:  runWhitelistList,
}

func init() {
	rootCmd.AddCommand(whitelistCmd)
	whitelistCmd.AddCommand(whitelistAddCmd)
	whitelistCmd.AddCommand(whitelistRemoveCmd)
	whitelistCmd.AddCommand(whitelistListCmd)
}

func runWhitelistAdd(cmd *cobra.Command, args []string) error {
	appClass := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.AddWhitelistedApp(appClass); err != nil {
		return fmt.Errorf("failed to add to whitelist: %w", err)
	}

	fmt.Printf("✅ Added '%s' to whitelist\n", appClass)
	return nil
}

func runWhitelistRemove(cmd *cobra.Command, args []string) error {
	appClass := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.RemoveWhitelistedApp(appClass); err != nil {
		return fmt.Errorf("failed to remove from whitelist: %w", err)
	}

	fmt.Printf("✅ Removed '%s' from whitelist\n", appClass)
	return nil
}

func runWhitelistList(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := configMgr.Get()

	fmt.Println("Whitelisted Applications:")
	if len(cfg.WhitelistedApps) == 0 {
		fmt.Println("  (none)")
	} else {
		for app := range cfg.WhitelistedApps {
			fmt.Printf("  • %s\n", app)
		}
	}

	fmt.Println("\nWhitelist Patterns:")
	if len(cfg.WhitelistPatterns) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, pattern := range cfg.WhitelistPatterns {
			fmt.Printf("  • %s\n", pattern)
		}
	}

	return nil
}
