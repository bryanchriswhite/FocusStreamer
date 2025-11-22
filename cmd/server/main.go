package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bryanchriswhite/FocusStreamer/internal/api"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/output"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
)

func main() {
	fmt.Println("🎯 FocusStreamer - Virtual Display for Discord Screen Sharing")
	fmt.Println("=============================================================")

	// Initialize configuration manager
	log.Println("Loading configuration...")
	configMgr, err := config.NewManager("")
	if err != nil {
		log.Fatalf("Failed to initialize config manager: %v", err)
	}
	cfg := configMgr.Get()
	log.Printf("Configuration loaded from: %s", configMgr.GetConfigPath())

	// Initialize window manager
	log.Println("Connecting to X11 server...")
	windowMgr, err := window.NewManager(configMgr)
	if err != nil {
		log.Fatalf("Failed to initialize window manager: %v", err)
	}
	defer windowMgr.Stop()

	// Start window monitoring
	log.Println("Starting window focus monitoring...")
	if err := windowMgr.Start(); err != nil {
		log.Fatalf("Failed to start window manager: %v", err)
	}

	// Initialize MJPEG stream output
	log.Println("Initializing MJPEG stream output...")
	mjpegOut := output.NewMJPEGOutput(output.Config{
		Width:  cfg.VirtualDisplay.Width,
		Height: cfg.VirtualDisplay.Height,
		FPS:    cfg.VirtualDisplay.FPS,
	})
	if err := mjpegOut.Start(); err != nil {
		log.Fatalf("Failed to start MJPEG output: %v", err)
	}
	defer mjpegOut.Stop()

	// Set MJPEG output on window manager and start streaming
	windowMgr.SetOutput(mjpegOut)
	if err := windowMgr.StartStreaming(cfg.VirtualDisplay.FPS); err != nil {
		log.Fatalf("Failed to start streaming: %v", err)
	}
	defer windowMgr.StopStreaming()

	// Initialize API server
	log.Println("Initializing HTTP server...")
	server := api.NewServer(windowMgr, configMgr, nil, mjpegOut)

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
}
