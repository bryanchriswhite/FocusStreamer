package display

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// WindowCapturer interface for capturing window screenshots
type WindowCapturer interface {
	CaptureWindowScreenshot(windowID uint32) ([]byte, error)
}

// Manager handles the virtual display window and rendering
type Manager struct {
	conn           *xgb.Conn
	screen         *xproto.ScreenInfo
	displayWindow  xproto.Window
	gc             xproto.Gcontext // Persistent graphics context
	width          int
	height         int
	fps            int
	running        bool
	mu             sync.RWMutex
	stopChan       chan struct{}
	currentImage   *image.RGBA
	windowCapturer WindowCapturer
}

// NewManager creates a new display manager
func NewManager(cfg *config.DisplayConfig) (*Manager, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	// Default FPS if not set or invalid
	fps := cfg.FPS
	if fps <= 0 {
		fps = 10
	}

	m := &Manager{
		conn:     conn,
		screen:   screen,
		width:    cfg.Width,
		height:   cfg.Height,
		fps:      fps,
		stopChan: make(chan struct{}),
	}

	return m, nil
}

// SetWindowCapturer sets the window capturer to use for screenshots
func (m *Manager) SetWindowCapturer(capturer WindowCapturer) {
	m.windowCapturer = capturer
}

// Start creates and shows the virtual display window
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("display already running")
	}

	// Create the display window
	windowID, err := xproto.NewWindowId(m.conn)
	if err != nil {
		return fmt.Errorf("failed to create window ID: %w", err)
	}

	m.displayWindow = windowID

	// Create window with black background
	mask := uint32(xproto.CwBackPixel | xproto.CwEventMask)
	values := []uint32{
		0x000000, // Black background
		xproto.EventMaskExposure | xproto.EventMaskStructureNotify,
	}

	err = xproto.CreateWindowChecked(
		m.conn,
		m.screen.RootDepth,
		m.displayWindow,
		m.screen.Root,
		0, 0, // x, y
		uint16(m.width), uint16(m.height),
		0, // border width
		xproto.WindowClassInputOutput,
		m.screen.RootVisual,
		mask,
		values,
	).Check()

	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Set window title
	title := "FocusStreamer - Virtual Display"
	if err := m.setWindowTitle(title); err != nil {
		logger.WithComponent("display").Warn().
			Err(err).
			Msg("Failed to set window title")
	}

	// Set window class for identification
	if err := m.setWindowClass("focusstreamer", "FocusStreamer"); err != nil {
		logger.WithComponent("display").Warn().
			Err(err).
			Msg("Failed to set window class")
	}

	// Map (show) the window
	if err := xproto.MapWindowChecked(m.conn, m.displayWindow).Check(); err != nil {
		return fmt.Errorf("failed to map window: %w", err)
	}

	// Flush to ensure window is created
	m.conn.Sync()

	// Create persistent graphics context
	gc, err := xproto.NewGcontextId(m.conn)
	if err != nil {
		return fmt.Errorf("failed to create graphics context: %w", err)
	}
	m.gc = gc
	logger.WithComponent("display").Debug().
		Uint32("gc_id", uint32(m.gc)).
		Uint32("window_id", uint32(m.displayWindow)).
		Msg("Created graphics context ID")

	err = xproto.CreateGCChecked(
		m.conn,
		m.gc,
		xproto.Drawable(m.displayWindow),
		0,
		nil,
	).Check()
	if err != nil {
		return fmt.Errorf("failed to create GC: %w", err)
	}

	// Sync to ensure GC is fully created on server
	m.conn.Sync()
	logger.WithComponent("display").Debug().Msg("Graphics context created successfully")

	m.running = true
	logger.WithComponent("display").Info().
		Int("width", m.width).
		Int("height", m.height).
		Uint32("window_id", uint32(m.displayWindow)).
		Msg("Virtual display window created")

	return nil
}

// Stop closes the virtual display window
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopChan)

	// Free graphics context
	if m.gc != 0 {
		xproto.FreeGC(m.conn, m.gc)
	}

	if m.displayWindow != 0 {
		xproto.DestroyWindow(m.conn, m.displayWindow)
		m.conn.Sync()
	}

	m.running = false
	logger.WithComponent("display").Info().Msg("Virtual display window closed")
}

// IsRunning returns whether the display is currently running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// RenderWindow captures and renders a window to the display
func (m *Manager) RenderWindow(windowID uint32) error {
	if !m.running {
		return fmt.Errorf("display not running")
	}

	// Use window capturer if available (uses XComposite for reliable capture)
	if m.windowCapturer != nil {
		pngData, err := m.windowCapturer.CaptureWindowScreenshot(windowID)
		if err != nil {
			return fmt.Errorf("failed to capture window via capturer: %w", err)
		}

		// Decode PNG
		img, err := png.Decode(bytes.NewReader(pngData))
		if err != nil {
			return fmt.Errorf("failed to decode PNG: %w", err)
		}

		// Convert to RGBA if needed
		var rgbaImg *image.RGBA
		if rgba, ok := img.(*image.RGBA); ok {
			rgbaImg = rgba
		} else {
			bounds := img.Bounds()
			rgbaImg = image.NewRGBA(bounds)
			draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)
		}

		// Render to display window
		if err := m.renderImage(rgbaImg); err != nil {
			return fmt.Errorf("failed to render image: %w", err)
		}

		return nil
	}

	// Fallback to local capture (less reliable, kept for compatibility)
	geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(windowID)).Reply()
	if err != nil {
		return fmt.Errorf("failed to get window geometry: %w", err)
	}

	img, err := m.captureWindow(xproto.Window(windowID), geom)
	if err != nil {
		return fmt.Errorf("failed to capture window: %w", err)
	}

	if err := m.renderImage(img); err != nil {
		return fmt.Errorf("failed to render image: %w", err)
	}

	return nil
}

// ClearDisplay clears the display window (shows black screen)
func (m *Manager) ClearDisplay() error {
	if !m.running {
		return fmt.Errorf("display not running")
	}

	// Create black image
	img := image.NewRGBA(image.Rect(0, 0, m.width, m.height))
	draw.Draw(img, img.Bounds(), &image.Uniform{image.Black}, image.Point{}, draw.Src)

	return m.renderImage(img)
}

// captureWindow captures a window's content as an image
func (m *Manager) captureWindow(win xproto.Window, geom *xproto.GetGeometryReply) (*image.RGBA, error) {
	logger.WithComponent("display").Debug().
		Uint32("window_id", uint32(win)).
		Uint16("width", geom.Width).
		Uint16("height", geom.Height).
		Msg("Display capture")

	// Get window image data
	reply, err := xproto.GetImage(
		m.conn,
		xproto.ImageFormatZPixmap,
		xproto.Drawable(win),
		0, 0,
		geom.Width, geom.Height,
		0xffffffff, // plane mask
	).Reply()

	if err != nil {
		return nil, fmt.Errorf("failed to get image: %w", err)
	}

	// Convert to RGBA image
	img := image.NewRGBA(image.Rect(0, 0, int(geom.Width), int(geom.Height)))

	// Parse image data (assuming 32-bit BGRA format)
	data := reply.Data
	depth := int(m.screen.RootDepth)

	logger.WithComponent("display").Debug().
		Int("bytes", len(data)).
		Int("depth", depth).
		Int("expected", int(geom.Width)*int(geom.Height)*4).
		Msg("Display capture data received")

	if len(data) == 0 {
		logger.WithComponent("display").Warn().
			Uint32("window_id", uint32(win)).
			Msg("Display capture returned empty data")
		return img, nil
	}

	if depth == 24 || depth == 32 {
		for y := 0; y < int(geom.Height); y++ {
			for x := 0; x < int(geom.Width); x++ {
				i := (y*int(geom.Width) + x) * 4
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
	} else {
		logger.WithComponent("display").Warn().
			Int("depth", depth).
			Uint32("window_id", uint32(win)).
			Msg("Unsupported color depth")
	}

	return img, nil
}

// renderImage renders an image to the display window
func (m *Manager) renderImage(img *image.RGBA) error {
	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	// Calculate scaling to fit display while maintaining aspect ratio
	scaleX := float64(m.width) / float64(srcWidth)
	scaleY := float64(m.height) / float64(srcHeight)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	// Calculate destination size
	dstWidth := int(float64(srcWidth) * scale)
	dstHeight := int(float64(srcHeight) * scale)

	// Center the image
	offsetX := (m.width - dstWidth) / 2
	offsetY := (m.height - dstHeight) / 2

	// Create output image
	output := image.NewRGBA(image.Rect(0, 0, m.width, m.height))

	// Fill with black background
	draw.Draw(output, output.Bounds(), &image.Uniform{image.Black}, image.Point{}, draw.Src)

	// Scale and draw the image (simple nearest-neighbor scaling)
	dstRect := image.Rect(offsetX, offsetY, offsetX+dstWidth, offsetY+dstHeight)
	m.scaleImage(output, dstRect, img, bounds)

	// Convert to X11 format and put image
	return m.putImage(output)
}

// scaleImage performs simple nearest-neighbor scaling
func (m *Manager) scaleImage(dst *image.RGBA, dstRect image.Rectangle, src *image.RGBA, srcRect image.Rectangle) {
	dstWidth := dstRect.Dx()
	dstHeight := dstRect.Dy()
	srcWidth := srcRect.Dx()
	srcHeight := srcRect.Dy()

	for dy := 0; dy < dstHeight; dy++ {
		for dx := 0; dx < dstWidth; dx++ {
			// Map destination pixel to source pixel
			sx := (dx * srcWidth) / dstWidth
			sy := (dy * srcHeight) / dstHeight

			srcX := srcRect.Min.X + sx
			srcY := srcRect.Min.Y + sy
			dstX := dstRect.Min.X + dx
			dstY := dstRect.Min.Y + dy

			dst.Set(dstX, dstY, src.At(srcX, srcY))
		}
	}
}

// putImage sends an image to the X server to be displayed
func (m *Manager) putImage(img *image.RGBA) error {
	// Validate image dimensions
	bounds := img.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	logger.WithComponent("display").Debug().Msgf("putImage: img size=%dx%d, display size=%dx%d, pix len=%d",
		imgWidth, imgHeight, m.width, m.height, len(img.Pix))

	if imgWidth != m.width || imgHeight != m.height {
		return fmt.Errorf("image size mismatch: got %dx%d, expected %dx%d",
			imgWidth, imgHeight, m.width, m.height)
	}

	// Get format information for the depth
	depth := m.screen.RootDepth
	setup := xproto.Setup(m.conn)

	// Find the format that matches our depth
	var bitsPerPixel uint8
	var scanlinePad uint8
	for _, format := range setup.PixmapFormats {
		if format.Depth == depth {
			bitsPerPixel = format.BitsPerPixel
			scanlinePad = format.ScanlinePad
			logger.WithComponent("display").Debug().Msgf("putImage: found format for depth %d: bitsPerPixel=%d, scanlinePad=%d",
				depth, bitsPerPixel, scanlinePad)
			break
		}
	}

	if bitsPerPixel == 0 {
		return fmt.Errorf("no format found for depth %d", depth)
	}

	// Calculate bytes per pixel and scanline stride
	bytesPerPixel := int(bitsPerPixel) / 8
	unpadded := imgWidth * bytesPerPixel

	// Calculate stride with scanline padding
	// Scanlines must be padded to scanlinePad bits (usually 32 bits = 4 bytes)
	padBytes := int(scanlinePad) / 8
	stride := ((unpadded + padBytes - 1) / padBytes) * padBytes

	logger.WithComponent("display").Debug().Msgf("putImage: bytesPerPixel=%d, unpadded=%d, stride=%d, total=%d",
		bytesPerPixel, unpadded, stride, stride*imgHeight)

	// Allocate buffer with proper stride
	data := make([]byte, stride*imgHeight)

	// Convert RGBA to X11 format with proper padding
	for y := 0; y < imgHeight; y++ {
		dstRowStart := y * stride
		for x := 0; x < imgWidth; x++ {
			srcIdx := (y*imgWidth + x) * 4
			dstIdx := dstRowStart + x*bytesPerPixel

			if bytesPerPixel == 4 {
				// 32-bit or 24-bit depth with 32bpp: BGRx format
				// Byte order matches X11 visual masks: 0xff (B), 0xff00 (G), 0xff0000 (R)
				data[dstIdx] = img.Pix[srcIdx+2]     // B (byte 0)
				data[dstIdx+1] = img.Pix[srcIdx+1]   // G (byte 1)
				data[dstIdx+2] = img.Pix[srcIdx]     // R (byte 2)
				if depth == 32 {
					data[dstIdx+3] = img.Pix[srcIdx+3] // A (byte 3) - only for depth 32
				} else {
					data[dstIdx+3] = 0 // Padding byte for depth 24
				}
			} else if bytesPerPixel == 3 {
				// 24-bit: BGR
				data[dstIdx] = img.Pix[srcIdx+2]     // B
				data[dstIdx+1] = img.Pix[srcIdx+1]   // G
				data[dstIdx+2] = img.Pix[srcIdx]     // R
			} else {
				return fmt.Errorf("unsupported bytes per pixel: %d", bytesPerPixel)
			}
		}
		// Padding bytes are already zero-initialized
	}

	// Try creating a fresh GC just for this putImage call
	testGc, err := xproto.NewGcontextId(m.conn)
	if err != nil {
		return fmt.Errorf("failed to create test GC ID: %w", err)
	}

	err = xproto.CreateGCChecked(
		m.conn,
		testGc,
		xproto.Drawable(m.displayWindow),
		xproto.GcForeground|xproto.GcBackground,
		[]uint32{
			0xffffffff, // foreground: white
			0x00000000, // background: black
		},
	).Check()
	if err != nil {
		return fmt.Errorf("failed to create test GC: %w", err)
	}
	defer xproto.FreeGC(m.conn, testGc)

	logger.WithComponent("display").Debug().Msgf("putImage: using test GC %d instead of persistent GC %d", testGc, m.gc)

	// Put image to window using test GC
	err = xproto.PutImageChecked(
		m.conn,
		xproto.ImageFormatZPixmap,
		xproto.Drawable(m.displayWindow),
		testGc,
		uint16(m.width),
		uint16(m.height),
		0, 0, // dst x, y
		0,    // left pad
		depth,
		data,
	).Check()

	if err != nil {
		return fmt.Errorf("failed to put image: %w", err)
	}

	// Flush to display
	m.conn.Sync()
	return nil
}

// setWindowTitle sets the window title
func (m *Manager) setWindowTitle(title string) error {
	titleAtom, err := m.getAtom("_NET_WM_NAME")
	if err != nil {
		return err
	}

	utf8Atom, err := m.getAtom("UTF8_STRING")
	if err != nil {
		return err
	}

	return xproto.ChangePropertyChecked(
		m.conn,
		xproto.PropModeReplace,
		m.displayWindow,
		titleAtom,
		utf8Atom,
		8,
		uint32(len(title)),
		[]byte(title),
	).Check()
}

// setWindowClass sets the window class
func (m *Manager) setWindowClass(instance, class string) error {
	classAtom, err := m.getAtom("WM_CLASS")
	if err != nil {
		return err
	}

	// WM_CLASS format: instance\0class\0
	classStr := instance + "\x00" + class + "\x00"

	return xproto.ChangePropertyChecked(
		m.conn,
		xproto.PropModeReplace,
		m.displayWindow,
		classAtom,
		xproto.AtomString,
		8,
		uint32(len(classStr)),
		[]byte(classStr),
	).Check()
}

// getAtom gets an atom ID by name
func (m *Manager) getAtom(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(m.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// GetWindowID returns the display window ID
func (m *Manager) GetWindowID() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return uint32(m.displayWindow)
}

// UpdateLoop continuously updates the display based on focused window
func (m *Manager) UpdateLoop(getCurrentWindow func() *config.WindowInfo, isAllowlisted func(*config.WindowInfo) bool) {
	// Calculate update interval from FPS (e.g., 10 FPS = 100ms, 30 FPS = 33ms)
	updateInterval := time.Duration(1000/m.fps) * time.Millisecond
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	logger.WithComponent("display").Info().
		Int("fps", m.fps).
		Dur("interval", updateInterval).
		Msg("Display update loop started")

	var lastWindowID uint32

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if !m.running {
				continue
			}

			window := getCurrentWindow()

			// If no window or not allowlisted, clear display
			if window == nil {
				if lastWindowID != 0 {
					logger.WithComponent("display").Debug().Msg("No focused window, clearing display")
					m.ClearDisplay()
					lastWindowID = 0
				}
				continue
			}

			if !isAllowlisted(window) {
				if lastWindowID != 0 {
					logger.WithComponent("display").Debug().
						Str("title", window.Title).
						Str("class", window.Class).
						Msg("Window not allowlisted, clearing display")
					m.ClearDisplay()
					lastWindowID = 0
				}
				continue
			}

			// If window changed, render it
			if window.ID != lastWindowID {
				logger.WithComponent("display").Debug().Msgf("UpdateLoop: rendering allowlisted window '%s' (class=%s, id=%d)", window.Title, window.Class, window.ID)
				if err := m.RenderWindow(window.ID); err != nil {
					logger.WithComponent("display").Debug().Msgf("Failed to render window %d: %v", window.ID, err)
					m.ClearDisplay()
					lastWindowID = 0
				} else {
					lastWindowID = window.ID
				}
			} else {
				// Periodically refresh the same window
				if err := m.RenderWindow(window.ID); err != nil {
					logger.WithComponent("display").Error().
						Err(err).
						Uint32("window_id", window.ID).
						Msg("Failed to refresh window")
				}
			}
		}
	}
}
