package commands

import (
	"fmt"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/spf13/cobra"
)

var allowlistCmd = &cobra.Command{
	Use:   "allowlist",
	Short: "Manage application allowlist",
	Long:  `Add or remove applications from the allowlist.`,
}

var allowlistAddCmd = &cobra.Command{
	Use:   "add CLASS",
	Short: "Add an application to the allowlist",
	Long:  `Add an application to the allowlist by its window class.`,
	Example: `  # Add Firefox to allowlist
  focusstreamer allowlist add firefox

  # Add terminal to allowlist
  focusstreamer allowlist add gnome-terminal-server`,
	Args: cobra.ExactArgs(1),
	RunE: runAllowlistAdd,
}

var allowlistRemoveCmd = &cobra.Command{
	Use:   "remove CLASS",
	Short: "Remove an application from the allowlist",
	Long:  `Remove an application from the allowlist by its window class.`,
	Example: `  # Remove Firefox from allowlist
  focusstreamer allowlist remove firefox

  # Remove terminal from allowlist
  focusstreamer allowlist remove gnome-terminal-server`,
	Args: cobra.ExactArgs(1),
	RunE: runAllowlistRemove,
}

var allowlistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List allowlisted applications",
	Long:  `Display all allowlisted applications and patterns.`,
	RunE:  runAllowlistList,
}

func init() {
	rootCmd.AddCommand(allowlistCmd)
	allowlistCmd.AddCommand(allowlistAddCmd)
	allowlistCmd.AddCommand(allowlistRemoveCmd)
	allowlistCmd.AddCommand(allowlistListCmd)
}

func runAllowlistAdd(cmd *cobra.Command, args []string) error {
	appClass := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.AddAllowlistedApp(appClass); err != nil {
		return fmt.Errorf("failed to add to allowlist: %w", err)
	}

	fmt.Printf("✅ Added '%s' to allowlist\n", appClass)
	return nil
}

func runAllowlistRemove(cmd *cobra.Command, args []string) error {
	appClass := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.RemoveAllowlistedApp(appClass); err != nil {
		return fmt.Errorf("failed to remove from allowlist: %w", err)
	}

	fmt.Printf("✅ Removed '%s' from allowlist\n", appClass)
	return nil
}

func runAllowlistList(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := configMgr.Get()

	fmt.Println("Allowlisted Applications:")
	if len(cfg.AllowlistedApps) == 0 {
		fmt.Println("  (none)")
	} else {
		for app := range cfg.AllowlistedApps {
			fmt.Printf("  • %s\n", app)
		}
	}

	fmt.Println("\nAllowlist Patterns:")
	if len(cfg.AllowlistPatterns) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, pattern := range cfg.AllowlistPatterns {
			fmt.Printf("  • %s\n", pattern)
		}
	}

	return nil
}
