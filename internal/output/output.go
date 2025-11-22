package output

import (
	"image"
)

// Output defines the interface for frame output mechanisms.
// This allows us to swap between different output methods:
// - MJPEG HTTP stream
// - X11 window display
// - V4L2 virtual camera
// - etc.
type Output interface {
	// Start initializes the output mechanism
	Start() error

	// Stop cleanly shuts down the output
	Stop() error

	// WriteFrame sends a frame to the output
	// The image is expected to be in RGBA format
	WriteFrame(frame *image.RGBA) error

	// Name returns a human-readable name for this output type
	Name() string

	// IsRunning returns true if the output is currently active
	IsRunning() bool
}

// Config holds common configuration for all output types
type Config struct {
	Width  int
	Height int
	FPS    int
}
