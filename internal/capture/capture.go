package capture

import (
	"image"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
)

// Capturer defines the interface for window/screen capture backends
type Capturer interface {
	// Start initializes the capturer and any required resources
	Start() error

	// Stop releases resources and stops any background processes
	Stop() error

	// CaptureWindow captures the contents of a specific window
	// Returns an RGBA image of the window content
	CaptureWindow(window *config.WindowInfo) (*image.RGBA, error)

	// CaptureRegion captures a specific region of the screen
	// Useful for cropping a full-screen capture to a window's geometry
	CaptureRegion(x, y, width, height int) (*image.RGBA, error)

	// Name returns a human-readable name for this capturer
	Name() string

	// IsAvailable checks if this capturer can be used in the current environment
	IsAvailable() bool

	// CanCapture checks if this capturer can capture the given window
	// X11 capturer returns false for native Wayland windows
	// PipeWire capturer typically returns true for all windows
	CanCapture(window *config.WindowInfo) bool
}
