package capture

import (
	"fmt"
	"image"
	"sync"

	"github.com/bryanchriswhite/FocusStreamer/internal/capture/pipewire"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// Router routes capture requests to the appropriate capturer
type Router struct {
	x11Capturer      *X11Capturer
	pipewireCapturer *pipewire.Capturer
	mu               sync.RWMutex
	started          bool
}

// NewRouter creates a new capture router
func NewRouter() (*Router, error) {
	return &Router{}, nil
}

// Start initializes the available capturers
func (r *Router) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	log := logger.WithComponent("capture-router")

	// Try to initialize X11 capturer
	x11, err := NewX11Capturer()
	if err != nil {
		log.Warn().Err(err).Msg("X11 capturer not available")
	} else {
		if err := x11.Start(); err != nil {
			log.Warn().Err(err).Msg("Failed to start X11 capturer")
			x11 = nil
		} else {
			r.x11Capturer = x11
			log.Info().Msg("X11 capturer initialized")
		}
	}

	// TODO: PipeWire capture is disabled due to go-gst library instability
	// The library causes CGO crashes and cursor issues with the portal dialog.
	// Need to implement PipeWire capture using a more robust approach:
	// - Use gst-launch subprocess with file/shm output
	// - Use wlr-screencopy protocol directly
	// - Use a different GStreamer binding
	log.Info().Msg("PipeWire capturer disabled (go-gst library unstable)")

	if r.x11Capturer == nil && r.pipewireCapturer == nil {
		return fmt.Errorf("no capture backends available")
	}

	r.started = true
	return nil
}

// Stop stops all capturers
func (r *Router) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.x11Capturer != nil {
		r.x11Capturer.Stop()
		r.x11Capturer = nil
	}

	if r.pipewireCapturer != nil {
		r.pipewireCapturer.Stop()
		r.pipewireCapturer = nil
	}

	r.started = false
	return nil
}

// CaptureWindow captures a window using the most appropriate capturer
func (r *Router) CaptureWindow(window *config.WindowInfo) (*image.RGBA, error) {
	r.mu.RLock()
	x11 := r.x11Capturer
	pw := r.pipewireCapturer
	r.mu.RUnlock()

	log := logger.WithComponent("capture-router")

	// Route based on window type
	// 1. For XWayland windows (not native Wayland), prefer X11 if available
	// 2. For native Wayland windows, use PipeWire
	// 3. Fall back to whatever is available

	if !window.IsNativeWayland && x11 != nil && x11.CanCapture(window) {
		log.Debug().
			Uint32("id", window.ID).
			Str("class", window.Class).
			Msg("Using X11 capturer for XWayland window")
		return x11.CaptureWindow(window)
	}

	if pw != nil && pw.CanCapture(window) {
		log.Debug().
			Uint32("id", window.ID).
			Str("class", window.Class).
			Bool("native_wayland", window.IsNativeWayland).
			Msg("Using PipeWire capturer")
		return pw.CaptureWindow(window)
	}

	// Try X11 as last resort
	if x11 != nil {
		log.Debug().
			Uint32("id", window.ID).
			Str("class", window.Class).
			Msg("Falling back to X11 capturer")
		return x11.CaptureWindow(window)
	}

	return nil, fmt.Errorf("no capturer available for window %s (native_wayland=%v, id=%d)",
		window.Class, window.IsNativeWayland, window.ID)
}

// CaptureRegion captures a region of the screen
func (r *Router) CaptureRegion(x, y, width, height int) (*image.RGBA, error) {
	r.mu.RLock()
	x11 := r.x11Capturer
	pw := r.pipewireCapturer
	r.mu.RUnlock()

	// Prefer PipeWire for region capture (more reliable on Wayland)
	if pw != nil {
		return pw.CaptureRegion(x, y, width, height)
	}

	if x11 != nil {
		return x11.CaptureRegion(x, y, width, height)
	}

	return nil, fmt.Errorf("no capturer available for region capture")
}

// GetX11Capturer returns the X11 capturer (for backward compatibility)
func (r *Router) GetX11Capturer() *X11Capturer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.x11Capturer
}

// GetPipeWireCapturer returns the PipeWire capturer
func (r *Router) GetPipeWireCapturer() *pipewire.Capturer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pipewireCapturer
}

// HasPipeWire returns true if PipeWire capture is available
func (r *Router) HasPipeWire() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pipewireCapturer != nil
}

// HasX11 returns true if X11 capture is available
func (r *Router) HasX11() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.x11Capturer != nil
}

// CanCapture checks if any capturer can handle the window
func (r *Router) CanCapture(window *config.WindowInfo) bool {
	r.mu.RLock()
	x11 := r.x11Capturer
	pw := r.pipewireCapturer
	r.mu.RUnlock()

	if x11 != nil && x11.CanCapture(window) {
		return true
	}
	if pw != nil && pw.CanCapture(window) {
		return true
	}
	return false
}
