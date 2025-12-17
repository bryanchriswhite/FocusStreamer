package commands

import (
	"fmt"
	"regexp"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/spf13/cobra"
)

var titlePatternCmd = &cobra.Command{
	Use:   "title-pattern",
	Short: "Manage title-only allowlist patterns",
	Long: `Add or remove regex patterns for allowlisting windows by title only.

Unlike regular patterns which match both class and title, title patterns
ONLY match against the window title. This is useful for allowing specific
browser tabs or document titles without matching other windows.`,
}

var titlePatternAddCmd = &cobra.Command{
	Use:   "add PATTERN",
	Short: "Add a title-only allowlist pattern",
	Long:  `Add a regex pattern that matches window titles only.`,
	Example: `  # Allow any window with "GitHub" in the title
  focusstreamer title-pattern add ".*GitHub.*"

  # Allow specific Brave browser tabs
  focusstreamer title-pattern add ".*GitHub.*- Brave.*"
  focusstreamer title-pattern add ".*Claude.*- Brave.*"

  # Allow windows with specific document names
  focusstreamer title-pattern add ".*README\\.md.*"`,
	Args: cobra.ExactArgs(1),
	RunE: runTitlePatternAdd,
}

var titlePatternRemoveCmd = &cobra.Command{
	Use:   "remove PATTERN",
	Short: "Remove a title-only allowlist pattern",
	Long:  `Remove a regex pattern from title-only allowlisting.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTitlePatternRemove,
}

var titlePatternListCmd = &cobra.Command{
	Use:   "list",
	Short: "List title-only allowlist patterns",
	Long:  `Display all configured title-only allowlist patterns.`,
	RunE:  runTitlePatternList,
}

func init() {
	rootCmd.AddCommand(titlePatternCmd)
	titlePatternCmd.AddCommand(titlePatternAddCmd)
	titlePatternCmd.AddCommand(titlePatternRemoveCmd)
	titlePatternCmd.AddCommand(titlePatternListCmd)
}

func runTitlePatternAdd(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	// Validate regex
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.AddTitlePattern(pattern); err != nil {
		return fmt.Errorf("failed to add title pattern: %w", err)
	}

	fmt.Printf("Added title pattern: %s\n", pattern)
	return nil
}

func runTitlePatternRemove(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := configMgr.RemoveTitlePattern(pattern); err != nil {
		return fmt.Errorf("failed to remove title pattern: %w", err)
	}

	fmt.Printf("Removed title pattern: %s\n", pattern)
	return nil
}

func runTitlePatternList(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := configMgr.Get()

	fmt.Println("Title-Only Allowlist Patterns:")
	if len(cfg.AllowlistTitlePatterns) == 0 {
		fmt.Println("  (none)")
	} else {
		for i, pattern := range cfg.AllowlistTitlePatterns {
			fmt.Printf("  %d. %s\n", i+1, pattern)
		}
	}

	return nil
}
