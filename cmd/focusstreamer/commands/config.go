package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage FocusStreamer configuration",
	Long:  `View and manage FocusStreamer configuration settings.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  `Display the current FocusStreamer configuration.`,
	Example: `  # Show configuration as YAML (default)
  focusstreamer config show

  # Show configuration as JSON
  focusstreamer config show --format json`,
	RunE: runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a configuration value",
	Long:  `Set a specific configuration value.`,
	Example: `  # Set server port
  focusstreamer config set server_port 9090

  # Set log level
  focusstreamer config set log_level debug`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get KEY",
	Short: "Get a configuration value",
	Long:  `Get a specific configuration value.`,
	Example: `  # Get server port
  focusstreamer config get server_port

  # Get log level
  focusstreamer config get log_level`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	Long:  `Display the path to the configuration file.`,
	RunE:  runConfigPath,
}

var formatFlag string

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configPathCmd)

	configShowCmd.Flags().StringVarP(&formatFlag, "format", "f", "yaml", "output format (yaml or json)")
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := configMgr.Get()

	switch formatFlag {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	case "yaml":
		encoder := yaml.NewEncoder(os.Stdout)
		encoder.SetIndent(2)
		return encoder.Encode(cfg)
	default:
		return fmt.Errorf("unsupported format: %s (use 'yaml' or 'json')", formatFlag)
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	v := configMgr.GetViper()

	// Handle different types
	switch key {
	case "server_port":
		var port int
		if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
			return fmt.Errorf("invalid port number: %s", value)
		}
		v.Set(key, port)
	case "log_level":
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[value] {
			return fmt.Errorf("invalid log level: %s (use: debug, info, warn, error)", value)
		}
		v.Set(key, value)
	case "virtual_display.width", "virtual_display.height", "virtual_display.refresh_hz":
		var num int
		if _, err := fmt.Sscanf(value, "%d", &num); err != nil {
			return fmt.Errorf("invalid number: %s", value)
		}
		v.Set(key, num)
	case "virtual_display.enabled":
		var enabled bool
		if _, err := fmt.Sscanf(value, "%t", &enabled); err != nil {
			return fmt.Errorf("invalid boolean: %s (use: true or false)", value)
		}
		v.Set(key, enabled)
	default:
		// Default to string
		v.Set(key, value)
	}

	if err := configMgr.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Configuration updated: %s = %s\n", key, value)
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	v := configMgr.GetViper()
	if !v.IsSet(key) {
		return fmt.Errorf("configuration key not found: %s", key)
	}

	fmt.Println(v.Get(key))
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println(configMgr.GetConfigPath())
	return nil
}
