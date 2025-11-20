package commands

import (
	"fmt"
	"regexp"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/spf13/cobra"
)

var patternCmd = &cobra.Command{
	Use:   "pattern",
	Short: "Manage whitelist patterns",
	Long: `Add or remove regex patterns for auto-whitelisting applications.

Patterns are matched against both window class and window title.`,
}

var patternAddCmd = &cobra.Command{
	Use:   "add PATTERN",
	Short: "Add a whitelist pattern",
	Long:  `Add a regex pattern for auto-whitelisting applications.`,
	Example: `  # Match all terminal applications
  focusstreamer pattern add ".*[Tt]erminal.*"

  # Match all applications with "Code" in the name
  focusstreamer pattern add ".*Code.*"

  # Match Firefox specifically
  focusstreamer pattern add "^firefox$"`,
	Args: cobra.ExactArgs(1),
	RunE: runPatternAdd,
}

var patternRemoveCmd = &cobra.Command{
	Use:   "remove PATTERN",
	Short: "Remove a whitelist pattern",
	Long:  `Remove a regex pattern from auto-whitelisting.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runPatternRemove,
}

var patternListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelist patterns",
	Long:  `Display all configured whitelist patterns.`,
	RunE:  runPatternList,
}

func init() {
	rootCmd.AddCommand(patternCmd)
	patternCmd.AddCommand(patternAddCmd)
	patternCmd.AddCommand(patternRemoveCmd)
	patternCmd.AddCommand(patternListCmd)
}

func runPatternAdd(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	// Validate regex
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.AddPattern(pattern); err != nil {
		return fmt.Errorf("failed to add pattern: %w", err)
	}

	fmt.Printf("✅ Added pattern: %s\n", pattern)
	return nil
}

func runPatternRemove(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.RemovePattern(pattern); err != nil {
		return fmt.Errorf("failed to remove pattern: %w", err)
	}

	fmt.Printf("✅ Removed pattern: %s\n", pattern)
	return nil
}

func runPatternList(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := configMgr.Get()

	fmt.Println("Whitelist Patterns:")
	if len(cfg.WhitelistPatterns) == 0 {
		fmt.Println("  (none)")
	} else {
		for i, pattern := range cfg.WhitelistPatterns {
			fmt.Printf("  %d. %s\n", i+1, pattern)
		}
	}

	return nil
}
