package commands

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bryanchriswhite/FocusStreamer/internal/api"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/bryanchriswhite/FocusStreamer/internal/output"
	"github.com/bryanchriswhite/FocusStreamer/internal/overlay"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the FocusStreamer server",
	Long: `Start the FocusStreamer HTTP server with X11 window monitoring.

The server provides a REST API and web UI for managing application allowlists
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

	// Initialize configuration manager (with basic logging first)
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

	// Initialize structured logger with config
	logger.Init(cfg.LogLevel, true)
	logger.Info("FocusStreamer starting")
	logger.WithComponent("config").Info().
		Str("path", configMgr.GetConfigPath()).
		Str("log_level", cfg.LogLevel).
		Msg("Configuration loaded")

	// Clean up any broken placeholder image paths on startup
	if removed, err := configMgr.CleanupBrokenPlaceholderPaths(); err != nil {
		logger.WithComponent("config").Warn().Err(err).Msg("Failed to clean up broken placeholder paths")
	} else if removed > 0 {
		logger.WithComponent("config").Info().Int("count", removed).Msg("Cleaned up broken placeholder image paths")
	}

	// Initialize window manager
	logger.WithComponent("init").Info().Msg("Connecting to X11 server")
	windowMgr, err := window.NewManager(configMgr)
	if err != nil {
		return fmt.Errorf("failed to initialize window manager: %w", err)
	}
	defer windowMgr.Stop()

	// Start window monitoring
	logger.WithComponent("serve").Info().Msg("Starting window focus monitoring...")
	if err := windowMgr.Start(); err != nil {
		return fmt.Errorf("failed to start window manager: %w", err)
	}

	// Initialize overlay manager
	logger.WithComponent("serve").Info().Msg("Initializing overlay system...")
	overlayMgr := overlay.NewManager()
	overlayMgr.SetEnabled(cfg.Overlay.Enabled)

	// Load overlay widgets from config
	if len(cfg.Overlay.Widgets) > 0 {
		logger.WithComponent("serve").Info().Msgf("Loading %d overlay widgets from config...", len(cfg.Overlay.Widgets))
		if err := overlayMgr.LoadFromConfig(cfg.Overlay.Widgets); err != nil {
			logger.WithComponent("serve").Info().Msgf("Warning: failed to load overlay widgets: %v", err)
		}
	}
	defer overlayMgr.Clear()

	logger.WithComponent("serve").Info().Msgf("Overlay system initialized (enabled: %v, widgets: %d)",
		overlayMgr.IsEnabled(), len(overlayMgr.GetAllWidgets()))

	// Initialize MJPEG stream output
	logger.WithComponent("serve").Info().Msg("Initializing MJPEG stream output...")
	mjpegOut := output.NewMJPEGOutput(output.Config{
		Width:  cfg.VirtualDisplay.Width,
		Height: cfg.VirtualDisplay.Height,
		FPS:    cfg.VirtualDisplay.FPS,
	})
	if err := mjpegOut.Start(); err != nil {
		return fmt.Errorf("failed to start MJPEG output: %w", err)
	}
	defer mjpegOut.Stop()

	// Set MJPEG output and overlay manager on window manager
	windowMgr.SetOutput(mjpegOut)
	windowMgr.SetOverlayManager(overlayMgr)

	// Start streaming
	if err := windowMgr.StartStreaming(cfg.VirtualDisplay.FPS); err != nil {
		return fmt.Errorf("failed to start streaming: %w", err)
	}
	defer windowMgr.StopStreaming()

	logger.WithComponent("serve").Info().Msgf("MJPEG stream initialized (%dx%d @ %d FPS)",
		cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height, cfg.VirtualDisplay.FPS)

	// Initialize API server
	logger.WithComponent("serve").Info().Msg("Initializing HTTP server...")
	server := api.NewServer(windowMgr, configMgr, nil, mjpegOut, overlayMgr)

	// Set up profile change callback to notify window manager
	server.SetOnProfileChange(func(profileID string) {
		windowMgr.OnProfileChanged(profileID)
	})

	// Start server in a goroutine
	go func() {
		logger.WithComponent("serve").Info().Msgf("Server starting on http://localhost:%d", cfg.ServerPort)
		logger.WithComponent("serve").Info().Msgf("Open http://localhost:%d in your browser to configure", cfg.ServerPort)
		if err := server.Start(cfg.ServerPort); err != nil {
			logger.WithComponent("serve").Fatal().Msgf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println()
	logger.WithComponent("serve").Info().Msg("✅ FocusStreamer is running!")
	logger.WithComponent("serve").Info().Msgf("   - Web UI: http://localhost:%d", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msgf("   - API: http://localhost:%d/api", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msgf("   - Stream Viewer: http://localhost:%d/view (open this in browser and share the tab in Discord!)", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msgf("   - Raw MJPEG Feed: http://localhost:%d/stream", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msgf("   - Stream Stats: http://localhost:%d/stats", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msgf("   - Overlay API: http://localhost:%d/api/overlay/types", cfg.ServerPort)
	logger.WithComponent("serve").Info().Msg("   - Press Ctrl+C to stop")
	fmt.Println()

	<-sigChan

	fmt.Println()
	logger.WithComponent("serve").Info().Msg("Shutting down gracefully...")
	return nil
}
