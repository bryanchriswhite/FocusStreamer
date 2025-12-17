package capture

import (
	"fmt"
	"image"
	"image/color"
	"sync"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/composite"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// X11Capturer captures windows using X11/XWayland
type X11Capturer struct {
	conn             *xgb.Conn
	root             xproto.Window
	screen           *xproto.ScreenInfo
	compositeEnabled bool
	mu               sync.Mutex
}

// NewX11Capturer creates a new X11 capturer
func NewX11Capturer() (*X11Capturer, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	root := screen.Root

	c := &X11Capturer{
		conn:   conn,
		root:   root,
		screen: screen,
	}

	return c, nil
}

// Start initializes the X11 capturer
func (c *X11Capturer) Start() error {
	log := logger.WithComponent("x11-capturer")

	// Initialize composite extension
	if err := composite.Init(c.conn); err != nil {
		log.Warn().
			Err(err).
			Msg("Composite extension not available - window screenshots may fail for obscured windows")
		c.compositeEnabled = false
	} else {
		c.compositeEnabled = true
		log.Info().Msg("Composite extension initialized")
	}

	return nil
}

// Stop closes the X11 connection
func (c *X11Capturer) Stop() error {
	c.conn.Close()
	return nil
}

// Name returns the capturer name
func (c *X11Capturer) Name() string {
	return "X11"
}

// IsAvailable checks if X11 capture is available
func (c *X11Capturer) IsAvailable() bool {
	return c.conn != nil
}

// CanCapture checks if this capturer can capture the given window
func (c *X11Capturer) CanCapture(window *config.WindowInfo) bool {
	// Cannot capture native Wayland windows
	if window.IsNativeWayland {
		return false
	}
	// Must have a valid X11 window ID
	return window.ID != 0 && window.ID != 0x200000 // Filter out placeholder IDs
}

// CaptureWindow captures a window by its info
func (c *X11Capturer) CaptureWindow(window *config.WindowInfo) (*image.RGBA, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.CanCapture(window) {
		return nil, fmt.Errorf("cannot capture window: native Wayland or invalid ID")
	}

	win := xproto.Window(window.ID)

	// Check window attributes first
	attrs, err := xproto.GetWindowAttributes(c.conn, win).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get window attributes: %w", err)
	}

	log := logger.WithComponent("x11-capturer")
	log.Debug().
		Uint32("window_id", window.ID).
		Uint16("class", attrs.Class).
		Uint8("map_state", attrs.MapState).
		Msg("Window attributes")

	// If window is not suitable for capture, try to find a suitable child window
	if attrs.Class != xproto.WindowClassInputOutput || attrs.MapState != xproto.MapStateViewable {
		log.Debug().
			Uint32("window_id", window.ID).
			Msg("Window not directly capturable, searching for child windows")

		childWin, err := c.findCapturableChild(win)
		if err != nil {
			return nil, fmt.Errorf("no capturable window found: %w", err)
		}

		log.Debug().
			Uint32("child_window_id", uint32(childWin)).
			Msg("Found capturable child window")
		win = childWin
	}

	// Get window geometry
	geom, err := xproto.GetGeometry(c.conn, xproto.Drawable(win)).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get window geometry: %w", err)
	}

	log.Debug().
		Uint32("window_id", uint32(win)).
		Uint16("width", geom.Width).
		Uint16("height", geom.Height).
		Msg("Capturing window")

	return c.captureWindowDrawable(win, geom)
}

// CaptureRegion captures a region of the root window
func (c *X11Capturer) CaptureRegion(x, y, width, height int) (*image.RGBA, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get image data from root window
	reply, err := xproto.GetImage(
		c.conn,
		xproto.ImageFormatZPixmap,
		xproto.Drawable(c.root),
		int16(x), int16(y),
		uint16(width), uint16(height),
		0xffffffff,
	).Reply()

	if err != nil {
		return nil, fmt.Errorf("failed to get image: %w", err)
	}

	return c.convertImageData(reply.Data, width, height), nil
}

// findCapturableChild recursively searches for a capturable child window
func (c *X11Capturer) findCapturableChild(parent xproto.Window) (xproto.Window, error) {
	log := logger.WithComponent("x11-capturer")

	tree, err := xproto.QueryTree(c.conn, parent).Reply()
	if err != nil {
		return 0, fmt.Errorf("failed to query tree: %w", err)
	}

	log.Debug().
		Uint32("parent_window_id", uint32(parent)).
		Int("child_count", len(tree.Children)).
		Msg("Searching child windows")

	for _, child := range tree.Children {
		attrs, err := xproto.GetWindowAttributes(c.conn, child).Reply()
		if err != nil {
			continue
		}

		geom, err := xproto.GetGeometry(c.conn, xproto.Drawable(child)).Reply()
		if err != nil {
			continue
		}

		// Check if this child is capturable
		if attrs.Class == xproto.WindowClassInputOutput && attrs.MapState == xproto.MapStateViewable {
			if geom.Width > 10 && geom.Height > 10 {
				return child, nil
			}
		}

		// Recursively search this child's children
		if grandchild, err := c.findCapturableChild(child); err == nil {
			return grandchild, nil
		}
	}

	return 0, fmt.Errorf("no capturable child found")
}

// captureWindowDrawable captures a window's content using Composite extension if available
func (c *X11Capturer) captureWindowDrawable(win xproto.Window, geom *xproto.GetGeometryReply) (*image.RGBA, error) {
	var drawable xproto.Drawable
	log := logger.WithComponent("x11-capturer")

	// Use Composite extension if available for more reliable capture
	if c.compositeEnabled {
		err := composite.RedirectWindowChecked(c.conn, win, composite.RedirectAutomatic).Check()
		if err != nil {
			log.Warn().
				Err(err).
				Uint32("window_id", uint32(win)).
				Msg("Failed to redirect window via Composite, falling back to direct capture")
			drawable = xproto.Drawable(win)
		} else {
			defer composite.UnredirectWindow(c.conn, win, composite.RedirectAutomatic)

			pixmap, err := xproto.NewPixmapId(c.conn)
			if err != nil {
				drawable = xproto.Drawable(win)
			} else {
				err = composite.NameWindowPixmapChecked(c.conn, win, pixmap).Check()
				if err != nil {
					drawable = xproto.Drawable(win)
				} else {
					drawable = xproto.Drawable(pixmap)
					log.Debug().
						Uint32("window_id", uint32(win)).
						Msg("Using Composite pixmap for window capture")
					defer xproto.FreePixmap(c.conn, pixmap)
				}
			}
		}
	} else {
		drawable = xproto.Drawable(win)
	}

	// Get window image data
	reply, err := xproto.GetImage(
		c.conn,
		xproto.ImageFormatZPixmap,
		drawable,
		0, 0,
		geom.Width, geom.Height,
		0xffffffff,
	).Reply()

	if err != nil {
		return nil, fmt.Errorf("failed to get image: %w", err)
	}

	return c.convertImageData(reply.Data, int(geom.Width), int(geom.Height)), nil
}

// convertImageData converts X11 image data to RGBA
func (c *X11Capturer) convertImageData(data []byte, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	depth := int(c.screen.RootDepth)

	if depth == 24 || depth == 32 {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				i := (y*width + x) * 4
				if i+3 < len(data) {
					// BGRA to RGBA
					img.Set(x, y, color.RGBA{
						R: data[i+2],
						G: data[i+1],
						B: data[i],
						A: 255,
					})
				}
			}
		}
	}

	return img
}

// GetConnection returns the X11 connection (for sharing with manager)
func (c *X11Capturer) GetConnection() *xgb.Conn {
	return c.conn
}

// GetRoot returns the root window
func (c *X11Capturer) GetRoot() xproto.Window {
	return c.root
}

// GetScreen returns the screen info
func (c *X11Capturer) GetScreen() *xproto.ScreenInfo {
	return c.screen
}
