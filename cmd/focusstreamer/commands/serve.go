package commands

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bryanchriswhite/FocusStreamer/internal/api"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/display"
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

var noDisplay bool

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().BoolVar(&noDisplay, "no-display", false, "disable virtual display window")
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

	// Initialize overlay manager
	log.Println("Initializing overlay system...")
	overlayMgr := overlay.NewManager()
	overlayMgr.SetEnabled(cfg.Overlay.Enabled)

	// Load overlay widgets from config
	if len(cfg.Overlay.Widgets) > 0 {
		log.Printf("Loading %d overlay widgets from config...", len(cfg.Overlay.Widgets))
		if err := overlayMgr.LoadFromConfig(cfg.Overlay.Widgets); err != nil {
			log.Printf("Warning: failed to load overlay widgets: %v", err)
		}
	}
	defer overlayMgr.Clear()

	log.Printf("Overlay system initialized (enabled: %v, widgets: %d)",
		overlayMgr.IsEnabled(), len(overlayMgr.GetAllWidgets()))

	// Initialize MJPEG stream output
	log.Println("Initializing MJPEG stream output...")
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

	log.Printf("MJPEG stream initialized (%dx%d @ %d FPS)",
		cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height, cfg.VirtualDisplay.FPS)

	// Initialize display manager (if enabled)
	var displayMgr *display.Manager
	if cfg.VirtualDisplay.Enabled && !noDisplay {
		log.Println("Initializing virtual display...")
		displayMgr, err = display.NewManager(&cfg.VirtualDisplay)
		if err != nil {
			return fmt.Errorf("failed to initialize display manager: %w", err)
		}
		defer displayMgr.Stop()

		// Set window manager as the capturer (uses XComposite for reliable capture)
		displayMgr.SetWindowCapturer(windowMgr)

		// Start the display window
		if err := displayMgr.Start(); err != nil {
			return fmt.Errorf("failed to start display: %w", err)
		}

		// Start display update loop
		go displayMgr.UpdateLoop(
			windowMgr.GetCurrentWindow,
			windowMgr.IsWindowAllowlisted,
		)

		log.Printf("Virtual display created (Window ID: %d)", displayMgr.GetWindowID())
	} else {
		log.Println("Virtual display disabled")
	}

	// Initialize API server
	log.Println("Initializing HTTP server...")
	server := api.NewServer(windowMgr, configMgr, displayMgr, mjpegOut, overlayMgr)

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
	log.Printf("   - Stream Viewer: http://localhost:%d/view (open this in browser and share the tab in Discord!)", cfg.ServerPort)
	log.Printf("   - Raw MJPEG Feed: http://localhost:%d/stream", cfg.ServerPort)
	log.Printf("   - Stream Stats: http://localhost:%d/stats", cfg.ServerPort)
	log.Printf("   - Overlay API: http://localhost:%d/api/overlay/types", cfg.ServerPort)
	if displayMgr != nil && displayMgr.IsRunning() {
		log.Printf("   - Virtual Display: Window ID %d", displayMgr.GetWindowID())
	}
	log.Println("   - Press Ctrl+C to stop")
	fmt.Println()

	<-sigChan

	fmt.Println()
	log.Println("Shutting down gracefully...")
	return nil
}
