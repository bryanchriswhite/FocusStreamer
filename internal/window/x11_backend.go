package window

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// X11Backend implements the Backend interface using X11
type X11Backend struct {
	conn          *xgb.Conn
	root          xproto.Window
	screen        *xproto.ScreenInfo
	mu            sync.RWMutex
	currentWindow *config.WindowInfo
	stopChan      chan struct{}
	watching      bool
}

// NewX11Backend creates a new X11 backend
func NewX11Backend() (*X11Backend, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	root := screen.Root

	return &X11Backend{
		conn:     conn,
		root:     root,
		screen:   screen,
		stopChan: make(chan struct{}),
	}, nil
}

// Connect establishes connection to X11 (already done in NewX11Backend)
func (b *X11Backend) Connect() error {
	// Connection already established in NewX11Backend
	return nil
}

// Close closes the X11 connection
func (b *X11Backend) Close() error {
	b.StopWatching()
	b.conn.Close()
	return nil
}

// Name returns the backend name
func (b *X11Backend) Name() string {
	return "x11"
}

// GetConn returns the X11 connection (needed by Manager for screenshots)
func (b *X11Backend) GetConn() *xgb.Conn {
	return b.conn
}

// GetRoot returns the root window
func (b *X11Backend) GetRoot() xproto.Window {
	return b.root
}

// GetScreen returns the screen info
func (b *X11Backend) GetScreen() *xproto.ScreenInfo {
	return b.screen
}

// ListWindows returns all visible windows using EWMH _NET_CLIENT_LIST with QueryTree fallback
func (b *X11Backend) ListWindows() ([]*config.WindowInfo, error) {
	log := logger.WithComponent("x11-backend")

	// Try EWMH _NET_CLIENT_LIST first (preferred method)
	windows, err := b.listWindowsEWMH()
	if err == nil && len(windows) > 0 {
		log.Debug().Int("count", len(windows)).Msg("ListWindows: using EWMH _NET_CLIENT_LIST")
		return windows, nil
	}
	if err != nil {
		log.Debug().Err(err).Msg("ListWindows: EWMH failed, falling back to QueryTree")
	} else {
		log.Debug().Msg("ListWindows: EWMH returned empty, falling back to QueryTree")
	}

	// Fallback to QueryTree if EWMH fails or returns empty
	windows, err = b.listWindowsQueryTree()
	if err != nil {
		log.Error().Err(err).Msg("ListWindows: QueryTree fallback failed")
		return nil, err
	}
	log.Debug().Int("count", len(windows)).Msg("ListWindows: using QueryTree fallback")
	return windows, nil
}

// listWindowsEWMH gets windows from _NET_CLIENT_LIST (EWMH standard)
func (b *X11Backend) listWindowsEWMH() ([]*config.WindowInfo, error) {
	log := logger.WithComponent("x11-backend")

	clientListAtom, err := b.getAtom("_NET_CLIENT_LIST")
	if err != nil {
		return nil, fmt.Errorf("failed to get _NET_CLIENT_LIST atom: %w", err)
	}

	reply, err := xproto.GetProperty(
		b.conn,
		false,
		b.root,
		clientListAtom,
		xproto.GetPropertyTypeAny,
		0,
		(1<<32)-1,
	).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get _NET_CLIENT_LIST property: %w", err)
	}

	log.Debug().
		Uint32("valueLen", reply.ValueLen).
		Int("valueBytes", len(reply.Value)).
		Msg("listWindowsEWMH: got _NET_CLIENT_LIST property")

	if reply.ValueLen == 0 {
		return nil, fmt.Errorf("_NET_CLIENT_LIST is empty")
	}

	// Parse window IDs from the property (array of 32-bit window IDs)
	windows := make([]*config.WindowInfo, 0)
	windowCount := len(reply.Value) / 4
	log.Debug().Int("windowCount", windowCount).Msg("listWindowsEWMH: parsing window IDs")

	for i := 0; i+4 <= len(reply.Value); i += 4 {
		winID := xproto.Window(uint32(reply.Value[i]) |
			uint32(reply.Value[i+1])<<8 |
			uint32(reply.Value[i+2])<<16 |
			uint32(reply.Value[i+3])<<24)

		info, err := b.getWindowInfo(winID)
		if err != nil {
			log.Debug().Uint32("winID", uint32(winID)).Err(err).Msg("listWindowsEWMH: failed to get window info")
			continue
		}

		log.Debug().
			Uint32("winID", uint32(winID)).
			Str("title", info.Title).
			Str("class", info.Class).
			Int("pid", info.PID).
			Msg("listWindowsEWMH: got window info")

		// Skip windows without titles or class (usually not user windows)
		if info.Title == "" && info.Class == "" {
			log.Debug().Uint32("winID", uint32(winID)).Msg("listWindowsEWMH: skipping window without title or class")
			continue
		}

		info.Focused = false
		windows = append(windows, info)
	}

	return windows, nil
}

// listWindowsQueryTree gets windows by querying root window children
func (b *X11Backend) listWindowsQueryTree() ([]*config.WindowInfo, error) {
	log := logger.WithComponent("x11-backend")

	tree, err := xproto.QueryTree(b.conn, b.root).Reply()
	if err != nil {
		return nil, err
	}

	log.Debug().Int("childCount", len(tree.Children)).Msg("listWindowsQueryTree: got children from root")

	windows := make([]*config.WindowInfo, 0)
	skippedNoInfo := 0
	skippedNoTitleClass := 0

	for _, child := range tree.Children {
		info, err := b.getWindowInfo(child)
		if err != nil {
			skippedNoInfo++
			continue
		}

		// Skip windows without titles or class (usually not user windows)
		if info.Title == "" && info.Class == "" {
			skippedNoTitleClass++
			continue
		}

		log.Debug().
			Uint32("winID", uint32(child)).
			Str("title", info.Title).
			Str("class", info.Class).
			Int("pid", info.PID).
			Msg("listWindowsQueryTree: found window")

		info.Focused = false
		windows = append(windows, info)
	}

	log.Debug().
		Int("found", len(windows)).
		Int("skippedNoInfo", skippedNoInfo).
		Int("skippedNoTitleClass", skippedNoTitleClass).
		Msg("listWindowsQueryTree: summary")

	return windows, nil
}

// GetFocusedWindow returns the currently focused window
func (b *X11Backend) GetFocusedWindow() (*config.WindowInfo, error) {
	focusReply, err := xproto.GetInputFocus(b.conn).Reply()
	if err != nil {
		return nil, err
	}

	return b.getWindowInfo(focusReply.Focus)
}

// WatchFocus starts watching for focus changes
func (b *X11Backend) WatchFocus(callback func(*config.WindowInfo)) error {
	b.mu.Lock()
	if b.watching {
		b.mu.Unlock()
		return fmt.Errorf("already watching")
	}
	b.watching = true
	b.stopChan = make(chan struct{})
	b.mu.Unlock()

	// Subscribe to window focus events on root
	const eventMask = xproto.EventMaskPropertyChange | xproto.EventMaskFocusChange
	if err := xproto.ChangeWindowAttributesChecked(
		b.conn,
		b.root,
		xproto.CwEventMask,
		[]uint32{eventMask},
	).Check(); err != nil {
		return fmt.Errorf("failed to set event mask: %w", err)
	}

	go b.watchFocusLoop(callback)
	return nil
}

// watchFocusLoop polls for focus changes
func (b *X11Backend) watchFocusLoop(callback func(*config.WindowInfo)) {
	log := logger.WithComponent("x11-backend")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Get initial focus
	if info, err := b.GetFocusedWindow(); err == nil {
		b.mu.Lock()
		b.currentWindow = info
		b.mu.Unlock()
		callback(info)
	}

	for {
		select {
		case <-b.stopChan:
			return
		case <-ticker.C:
			// Poll focus
			info, err := b.GetFocusedWindow()
			if err != nil {
				log.Debug().Err(err).Msg("Failed to get focused window")
				continue
			}

			b.mu.Lock()
			// Detect changes in window ID, title, or geometry
			changed := b.currentWindow == nil ||
				b.currentWindow.ID != info.ID ||
				b.currentWindow.Title != info.Title ||
				b.currentWindow.Geometry != info.Geometry
			if changed {
				b.currentWindow = info
			}
			b.mu.Unlock()

			if changed {
				callback(info)
			}
		}
	}
}

// StopWatching stops the focus watching loop
func (b *X11Backend) StopWatching() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.watching {
		close(b.stopChan)
		b.watching = false
	}
}

// getWindowInfo retrieves information about a window
func (b *X11Backend) getWindowInfo(win xproto.Window) (*config.WindowInfo, error) {
	info := &config.WindowInfo{
		ID:      uint32(win),
		Focused: true,
	}

	// Get window geometry
	geom, err := xproto.GetGeometry(b.conn, xproto.Drawable(win)).Reply()
	if err == nil {
		info.Geometry = config.Geometry{
			X:      int(geom.X),
			Y:      int(geom.Y),
			Width:  int(geom.Width),
			Height: int(geom.Height),
		}
	}

	// Get window title
	titleAtom, err := b.getAtom("_NET_WM_NAME")
	if err == nil {
		if title, err := b.getProperty(win, titleAtom); err == nil {
			info.Title = title
		}
	}

	// Try alternative title property
	if info.Title == "" {
		titleAtom, err = b.getAtom("WM_NAME")
		if err == nil {
			if title, err := b.getProperty(win, titleAtom); err == nil {
				info.Title = title
			}
		}
	}

	// Get window class
	// WM_CLASS format is: instance\0class\0 (two null-terminated strings)
	classAtom, err := b.getAtom("WM_CLASS")
	if err == nil {
		if classRaw, err := b.getProperty(win, classAtom); err == nil {
			// Parse WM_CLASS: skip first string (instance), get second string (class)
			parts := strings.Split(classRaw, "\x00")
			if len(parts) >= 2 && parts[1] != "" {
				info.Class = parts[1] // Use the class name (second part)
			} else if len(parts) >= 1 && parts[0] != "" {
				info.Class = parts[0] // Fallback to instance if class is empty
			}
		}
	}

	// Get PID
	pidAtom, err := b.getAtom("_NET_WM_PID")
	if err == nil {
		pidReply, err := xproto.GetProperty(
			b.conn,
			false,
			win,
			pidAtom,
			xproto.AtomCardinal,
			0,
			1,
		).Reply()
		if err == nil && len(pidReply.Value) >= 4 {
			info.PID = int(uint32(pidReply.Value[0]) |
				uint32(pidReply.Value[1])<<8 |
				uint32(pidReply.Value[2])<<16 |
				uint32(pidReply.Value[3])<<24)
		}
	}

	return info, nil
}

// GetWindowInfo is the public version for use by Manager
func (b *X11Backend) GetWindowInfo(windowID uint32) (*config.WindowInfo, error) {
	return b.getWindowInfo(xproto.Window(windowID))
}

// getAtom gets an atom ID by name
func (b *X11Backend) getAtom(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(b.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// getProperty gets a property value as a string
func (b *X11Backend) getProperty(win xproto.Window, atom xproto.Atom) (string, error) {
	reply, err := xproto.GetProperty(
		b.conn,
		false,
		win,
		atom,
		xproto.GetPropertyTypeAny,
		0,
		(1<<32)-1,
	).Reply()
	if err != nil {
		return "", err
	}

	if reply.ValueLen == 0 {
		return "", fmt.Errorf("empty property")
	}

	return string(reply.Value), nil
}
