package pipewire

import (
	"fmt"
	"image"
	"sync"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// Capturer implements the capture.Capturer interface using PipeWire
type Capturer struct {
	portal   *Portal
	pipeline *GStreamerPipeline
	mu       sync.Mutex
	started  bool
}

// NewCapturer creates a new PipeWire capturer
func NewCapturer() (*Capturer, error) {
	return &Capturer{}, nil
}

// Start initializes the PipeWire capture session
func (c *Capturer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("capturer already started")
	}

	log := logger.WithComponent("pipewire-capturer")

	// Initialize portal
	portal, err := NewPortal()
	if err != nil {
		return fmt.Errorf("failed to create portal: %w", err)
	}
	c.portal = portal

	// Start screen sharing session
	if err := portal.StartScreenShare(); err != nil {
		portal.Close()
		return fmt.Errorf("failed to start screen share: %w", err)
	}

	nodeID := portal.GetNodeID()
	log.Info().Uint32("node_id", nodeID).Msg("Got PipeWire node ID")

	// Create and start GStreamer pipeline
	pipeline, err := NewGStreamerPipeline(nodeID)
	if err != nil {
		portal.Close()
		return fmt.Errorf("failed to create pipeline: %w", err)
	}
	c.pipeline = pipeline

	if err := pipeline.Start(); err != nil {
		portal.Close()
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	c.started = true
	log.Info().Msg("PipeWire capturer started")

	return nil
}

// Stop stops the PipeWire capture session
func (c *Capturer) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	log := logger.WithComponent("pipewire-capturer")

	if c.pipeline != nil {
		c.pipeline.Stop()
		c.pipeline = nil
	}

	if c.portal != nil {
		c.portal.Close()
		c.portal = nil
	}

	c.started = false
	log.Info().Msg("PipeWire capturer stopped")

	return nil
}

// CaptureWindow captures a window by cropping the screen capture to window geometry
func (c *Capturer) CaptureWindow(window *config.WindowInfo) (*image.RGBA, error) {
	c.mu.Lock()
	pipeline := c.pipeline
	c.mu.Unlock()

	if pipeline == nil || !pipeline.IsRunning() {
		return nil, fmt.Errorf("pipeline not running")
	}

	// Get the window geometry
	geom := window.Geometry
	if geom.Width <= 0 || geom.Height <= 0 {
		// If no geometry, return full frame
		return pipeline.GetLatestFrame(), nil
	}

	// Crop the screen capture to the window's position
	return c.CaptureRegion(geom.X, geom.Y, geom.Width, geom.Height)
}

// CaptureRegion captures a specific region of the screen
func (c *Capturer) CaptureRegion(x, y, width, height int) (*image.RGBA, error) {
	c.mu.Lock()
	pipeline := c.pipeline
	c.mu.Unlock()

	if pipeline == nil || !pipeline.IsRunning() {
		return nil, fmt.Errorf("pipeline not running")
	}

	cropped := pipeline.CropFrame(x, y, width, height)
	if cropped == nil {
		return nil, fmt.Errorf("no frame available")
	}

	return cropped, nil
}

// Name returns the capturer name
func (c *Capturer) Name() string {
	return "PipeWire"
}

// IsAvailable checks if PipeWire capture is available
func (c *Capturer) IsAvailable() bool {
	// Check if we can create a portal connection
	conn, err := NewPortal()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// CanCapture returns true for all windows (PipeWire can capture any window via screen capture)
func (c *Capturer) CanCapture(window *config.WindowInfo) bool {
	// PipeWire screen capture can handle any window, including native Wayland
	// We need geometry info to crop the screen capture
	return window.Geometry.Width > 0 && window.Geometry.Height > 0
}

// GetFullScreen returns the full screen capture without cropping
func (c *Capturer) GetFullScreen() (*image.RGBA, error) {
	c.mu.Lock()
	pipeline := c.pipeline
	c.mu.Unlock()

	if pipeline == nil || !pipeline.IsRunning() {
		return nil, fmt.Errorf("pipeline not running")
	}

	frame := pipeline.GetLatestFrame()
	if frame == nil {
		return nil, fmt.Errorf("no frame available")
	}

	return frame, nil
}
