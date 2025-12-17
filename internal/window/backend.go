package window

import (
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
)

// Backend defines the interface for window discovery backends (X11, KWin, etc.)
type Backend interface {
	// Connect establishes connection to the display server
	Connect() error

	// Close closes the connection to the display server
	Close() error

	// ListWindows returns all visible application windows
	ListWindows() ([]*config.WindowInfo, error)

	// GetFocusedWindow returns the currently focused window
	GetFocusedWindow() (*config.WindowInfo, error)

	// WatchFocus starts watching for focus changes and calls the callback
	// when the focused window changes. This should be called in a goroutine.
	WatchFocus(callback func(*config.WindowInfo)) error

	// StopWatching stops the focus watching loop
	StopWatching()

	// Name returns the backend name (e.g., "x11", "kwin")
	Name() string
}
