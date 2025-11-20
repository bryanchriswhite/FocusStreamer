package commands

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bryanchriswhite/FocusStreamer/internal/api"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the FocusStreamer server",
	Long: `Start the FocusStreamer HTTP server with X11 window monitoring.

The server provides a REST API and web UI for managing application whitelists
and viewing the currently focused window.`,
	Example: `  # Start server on default port (8080)
  focusstreamer serve

  # Start server on custom port
  focusstreamer serve --port 9090

  # Start with specific config file
  focusstreamer serve --config /path/to/config.yaml

  # Start with debug logging
  focusstreamer serve --log-level debug`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	fmt.Println("🎯 FocusStreamer - Virtual Display for Discord Screen Sharing")
	fmt.Println("=============================================================")

	// Initialize configuration manager
	log.Println("Loading configuration...")
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	// Override port from flag if provided
	if viper.IsSet("server_port") {
		port := viper.GetInt("server_port")
		if port > 0 {
			configMgr.SetPort(port)
		}
	}

	// Override log level from flag if provided
	if viper.IsSet("log_level") {
		logLevel := viper.GetString("log_level")
		if logLevel != "" {
			configMgr.SetLogLevel(logLevel)
		}
	}

	cfg := configMgr.Get()
	log.Printf("Configuration loaded from: %s", configMgr.GetConfigPath())
	log.Printf("Log level: %s", cfg.LogLevel)

	// Initialize window manager
	log.Println("Connecting to X11 server...")
	windowMgr, err := window.NewManager(configMgr)
	if err != nil {
		return fmt.Errorf("failed to initialize window manager: %w", err)
	}
	defer windowMgr.Stop()

	// Start window monitoring
	log.Println("Starting window focus monitoring...")
	if err := windowMgr.Start(); err != nil {
		return fmt.Errorf("failed to start window manager: %w", err)
	}

	// Initialize API server
	log.Println("Initializing HTTP server...")
	server := api.NewServer(windowMgr, configMgr)

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on http://localhost:%d", cfg.ServerPort)
		log.Printf("Open http://localhost:%d in your browser to configure", cfg.ServerPort)
		if err := server.Start(cfg.ServerPort); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println()
	log.Println("✅ FocusStreamer is running!")
	log.Printf("   - Web UI: http://localhost:%d", cfg.ServerPort)
	log.Printf("   - API: http://localhost:%d/api", cfg.ServerPort)
	log.Println("   - Press Ctrl+C to stop")
	fmt.Println()

	<-sigChan

	fmt.Println()
	log.Println("Shutting down gracefully...")
	return nil
}
