package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "focusstreamer",
		Short: "FocusStreamer - Virtual display for Discord screen sharing",
		Long: `FocusStreamer creates a virtual display for Discord screen sharing that
dynamically shows only the currently focused application window based on
configurable filters.

Features:
  • Detect running applications via X11
  • Track focused window in real-time
  • Allowlist specific applications
  • Use regex patterns for auto-allowlisting
  • Persistent configuration
  • REST API for integration
  • Modern web UI for configuration`,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/focusstreamer/config.yaml)")
	rootCmd.PersistentFlags().Int("port", 0, "server port (default is 8080)")
	rootCmd.PersistentFlags().String("log-level", "", "log level (debug, info, warn, error)")

	// Bind flags to viper
	viper.BindPFlag("server_port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// GetConfigFile returns the config file path
func GetConfigFile() string {
	return cfgFile
}
