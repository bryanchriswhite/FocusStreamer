package window

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/composite"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/capture"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/bryanchriswhite/FocusStreamer/internal/output"
	"github.com/bryanchriswhite/FocusStreamer/internal/overlay"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Manager handles window detection and monitoring
type Manager struct {
	// Backend for window discovery (X11 or KWin)
	backend Backend

	// Capture router for X11/PipeWire capture
	captureRouter *capture.Router

	// X11 connection for screenshot capture (needed regardless of backend)
	conn             *xgb.Conn
	root             xproto.Window
	screen           *xproto.ScreenInfo
	compositeEnabled bool

	configMgr     *config.Manager
	currentWindow *config.WindowInfo
	mu            sync.RWMutex
	listeners     []chan *config.WindowInfo
	stopChan      chan struct{}

	// Output for streaming frames
	output            output.Output
	overlayMgr        *overlay.Manager
	streamStopChan    chan struct{}
	streamRunning     bool
	streamMu          sync.Mutex
	lastAllowedWindow *config.WindowInfo // Last allowlisted window to stream
}

// NewManager creates a new window manager with auto-detected backend
func NewManager(configMgr *config.Manager) (*Manager, error) {
	log := logger.WithComponent("window-manager")

	// Auto-detect backend
	backend, err := detectBackend()
	if err != nil {
		return nil, fmt.Errorf("failed to detect window backend: %w", err)
	}
	log.Info().Str("backend", backend.Name()).Msg("Using window backend")

	// Always need X11 connection for screenshot capture
	conn, err := xgb.NewConn()
	if err != nil {
		backend.Close()
		return nil, fmt.Errorf("failed to connect to X server for screenshots: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	root := screen.Root

	// Initialize composite extension
	compositeEnabled := false
	if err := composite.Init(conn); err != nil {
		log.Warn().
			Err(err).
			Msg("Composite extension not available - window screenshots may fail for obscured or off-screen windows")
	} else {
		compositeEnabled = true
		log.Info().Msg("Composite extension initialized successfully")
	}

	// Initialize capture router
	captureRouter, err := capture.NewRouter()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create capture router")
	} else {
		if err := captureRouter.Start(); err != nil {
			log.Warn().Err(err).Msg("Failed to start capture router")
			captureRouter = nil
		} else {
			log.Info().
				Bool("has_x11", captureRouter.HasX11()).
				Bool("has_pipewire", captureRouter.HasPipeWire()).
				Msg("Capture router initialized")
		}
	}

	m := &Manager{
		backend:          backend,
		captureRouter:    captureRouter,
		conn:             conn,
		root:             root,
		screen:           screen,
		configMgr:        configMgr,
		listeners:        make([]chan *config.WindowInfo, 0),
		stopChan:         make(chan struct{}),
		compositeEnabled: compositeEnabled,
	}

	return m, nil
}

// detectBackend auto-detects the appropriate window backend
func detectBackend() (Backend, error) {
	log := logger.WithComponent("window-manager")

	// Check if running on Wayland
	sessionType := os.Getenv("XDG_SESSION_TYPE")
	log.Debug().Str("XDG_SESSION_TYPE", sessionType).Msg("Detecting session type")

	if sessionType == "wayland" {
		// Try KWin backend first
		log.Info().Msg("Wayland session detected, trying KWin backend")
		kwin, err := NewKWinBackend()
		if err == nil {
			return kwin, nil
		}
		log.Warn().Err(err).Msg("KWin backend not available, falling back to X11")
	}

	// Fall back to X11
	log.Info().Msg("Using X11 backend")
	return NewX11Backend()
}

// Start begins monitoring window focus changes
func (m *Manager) Start() error {
	// Use backend for focus monitoring
	err := m.backend.WatchFocus(func(info *config.WindowInfo) {
		m.mu.Lock()
		m.currentWindow = info
		m.mu.Unlock()
		m.notifyListeners(info)
	})
	if err != nil {
		return fmt.Errorf("failed to start focus monitoring: %w", err)
	}

	// Get initial focused window
	if info, err := m.backend.GetFocusedWindow(); err == nil {
		m.mu.Lock()
		m.currentWindow = info
		m.mu.Unlock()
	} else {
		logger.WithComponent("window").Warn().
			Err(err).
			Msg("Failed to get initial window")
	}

	return nil
}

// Stop stops the window manager
func (m *Manager) Stop() {
	close(m.stopChan)
	m.backend.StopWatching()
	m.backend.Close()
	if m.captureRouter != nil {
		m.captureRouter.Stop()
	}
	m.conn.Close()
}

// GetCurrentWindow returns the currently focused window
func (m *Manager) GetCurrentWindow() *config.WindowInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentWindow
}

// ListWindows returns all visible windows via the backend
func (m *Manager) ListWindows() ([]*config.WindowInfo, error) {
	return m.backend.ListWindows()
}

// IsWindowAllowlisted checks if a window is allowlisted
func (m *Manager) IsWindowAllowlisted(window *config.WindowInfo) bool {
	cfg := m.configMgr.Get()

	// Normalize class to lowercase for comparison
	normalizedClass := strings.ToLower(window.Class)

	// Check exact match in allowlisted apps
	for _, app := range cfg.AllowlistedApps {
		if app == normalizedClass {
			return true
		}
	}

	// Check pattern matching (matches against both class and title)
	for _, pattern := range cfg.AllowlistPatterns {
		if matched, err := regexp.MatchString(pattern, window.Class); err == nil && matched {
			return true
		}
		if matched, err := regexp.MatchString(pattern, window.Title); err == nil && matched {
			return true
		}
	}

	return false
}

// Subscribe adds a listener for window changes
func (m *Manager) Subscribe() chan *config.WindowInfo {
	ch := make(chan *config.WindowInfo, 10)
	m.mu.Lock()
	m.listeners = append(m.listeners, ch)
	m.mu.Unlock()
	return ch
}

// Unsubscribe removes a listener
func (m *Manager) Unsubscribe(ch chan *config.WindowInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, listener := range m.listeners {
		if listener == ch {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

// notifyListeners notifies all listeners of window changes
func (m *Manager) notifyListeners(window *config.WindowInfo) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, listener := range m.listeners {
		select {
		case listener <- window:
		default:
			// Skip if channel is full
		}
	}
}

// FindWindowByClass finds the first window with the given class
func (m *Manager) FindWindowByClass(windowClass string) (*config.WindowInfo, error) {
	windows, err := m.ListWindows()
	if err != nil {
		return nil, err
	}

	for _, win := range windows {
		if win.Class == windowClass {
			return win, nil
		}
	}

	return nil, fmt.Errorf("window not found: %s", windowClass)
}

// CaptureWindowScreenshot captures a screenshot of a window by ID and returns PNG data
func (m *Manager) CaptureWindowScreenshot(windowID uint32) ([]byte, error) {
	win := xproto.Window(windowID)

	// Check window attributes first
	attrs, err := xproto.GetWindowAttributes(m.conn, win).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get window attributes: %w", err)
	}

	logger.WithComponent("window").Debug().
		Uint32("window_id", uint32(windowID)).
		Uint16("class", attrs.Class).
		Uint8("map_state", attrs.MapState).
		Msg("Window attributes")

	// If window is not suitable for capture, try to find a suitable child window
	if attrs.Class != xproto.WindowClassInputOutput || attrs.MapState != xproto.MapStateViewable {
		logger.WithComponent("window").Debug().
			Uint32("window_id", uint32(windowID)).
			Msg("Window not directly capturable, searching for child windows")

		// Try to find a child window that can be captured
		childWin, err := m.findCapturableChild(win)
		if err != nil {
			return nil, fmt.Errorf("no capturable window found: %w", err)
		}

		logger.WithComponent("window").Debug().
			Uint32("child_window_id", uint32(childWin)).
			Msg("Found capturable child window")
		win = childWin

		// Get attributes of child window
		attrs, err = xproto.GetWindowAttributes(m.conn, win).Reply()
		if err != nil {
			return nil, fmt.Errorf("failed to get child window attributes: %w", err)
		}
		logger.WithComponent("window").Debug().
			Uint32("window_id", uint32(win)).
			Uint16("class", attrs.Class).
			Uint8("map_state", attrs.MapState).
			Msg("Child window attributes")
	}

	// Get window geometry
	geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(win)).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get window geometry: %w", err)
	}

	logger.WithComponent("window").Debug().
		Uint32("window_id", uint32(win)).
		Uint16("width", geom.Width).
		Uint16("height", geom.Height).
		Int16("x", geom.X).
		Int16("y", geom.Y).
		Msg("Window geometry")

	// Capture window image
	img, err := m.captureWindow(win, geom)
	if err != nil {
		return nil, fmt.Errorf("failed to capture window: %w", err)
	}

	// Encode as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// findCapturableChild recursively searches for a capturable child window
func (m *Manager) findCapturableChild(parent xproto.Window) (xproto.Window, error) {
	// Query child windows
	tree, err := xproto.QueryTree(m.conn, parent).Reply()
	if err != nil {
		return 0, fmt.Errorf("failed to query tree: %w", err)
	}

	logger.WithComponent("window").Debug().
		Uint32("parent_window_id", uint32(parent)).
		Int("child_count", len(tree.Children)).
		Msg("Searching child windows")

	// Search through children for a capturable window
	for _, child := range tree.Children {
		attrs, err := xproto.GetWindowAttributes(m.conn, child).Reply()
		if err != nil {
			logger.WithComponent("window").Debug().
				Uint32("child_id", uint32(child)).
				Err(err).
				Msg("Failed to get child attributes")
			continue
		}

		geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(child)).Reply()
		if err != nil {
			logger.WithComponent("window").Debug().
				Uint32("child_id", uint32(child)).
				Err(err).
				Msg("Failed to get child geometry")
			continue
		}

		logger.WithComponent("window").Debug().
			Uint32("child_id", uint32(child)).
			Uint16("class", attrs.Class).
			Uint8("map_state", attrs.MapState).
			Uint16("width", geom.Width).
			Uint16("height", geom.Height).
			Msg("Evaluating child window")

		// Check if this child is capturable
		if attrs.Class == xproto.WindowClassInputOutput && attrs.MapState == xproto.MapStateViewable {
			// Must have reasonable dimensions
			if geom.Width > 10 && geom.Height > 10 {
				logger.WithComponent("window").Debug().
					Uint32("child_id", uint32(child)).
					Msg("Found capturable child")
				return child, nil
			} else {
				logger.WithComponent("window").Debug().
					Uint32("child_id", uint32(child)).
					Msg("Child too small (need >10x10)")
			}
		} else {
			reasons := []string{}
			if attrs.Class != xproto.WindowClassInputOutput {
				reasons = append(reasons, fmt.Sprintf("class=%d (need %d)", attrs.Class, xproto.WindowClassInputOutput))
			}
			if attrs.MapState != xproto.MapStateViewable {
				reasons = append(reasons, fmt.Sprintf("mapState=%d (need %d)", attrs.MapState, xproto.MapStateViewable))
			}
			logger.WithComponent("window").Debug().
				Uint32("child_id", uint32(child)).
				Strs("reasons", reasons).
				Msg("Child not capturable")
		}

		// Recursively search this child's children
		if grandchild, err := m.findCapturableChild(child); err == nil {
			return grandchild, nil
		}
	}

	return 0, fmt.Errorf("no capturable child found")
}

// captureWindow captures a window's content as an image
func (m *Manager) captureWindow(win xproto.Window, geom *xproto.GetGeometryReply) (*image.RGBA, error) {
	var drawable xproto.Drawable

	// Use Composite extension if available for more reliable capture
	if m.compositeEnabled {
		// Redirect window to off-screen buffer for compositing
		// Use CompositeRedirectAutomatic (0) for temporary redirection
		err := composite.RedirectWindowChecked(m.conn, win, composite.RedirectAutomatic).Check()
		if err != nil {
			logger.WithComponent("window").Warn().
				Err(err).
				Uint32("window_id", uint32(win)).
				Msg("Failed to redirect window via Composite, falling back to direct capture")
			drawable = xproto.Drawable(win)
		} else {
			// Ensure we unredirect when done
			defer composite.UnredirectWindow(m.conn, win, composite.RedirectAutomatic)

			// Create a pixmap ID and associate it with the window's off-screen buffer
			pixmap, err := xproto.NewPixmapId(m.conn)
			if err != nil {
				logger.WithComponent("window").Warn().
					Err(err).
					Uint32("window_id", uint32(win)).
					Msg("Failed to generate pixmap ID, falling back to direct capture")
				drawable = xproto.Drawable(win)
			} else {
				// Associate the pixmap with the window's off-screen buffer
				err = composite.NameWindowPixmapChecked(m.conn, win, pixmap).Check()
				if err != nil {
					logger.WithComponent("window").Warn().
						Err(err).
						Uint32("window_id", uint32(win)).
						Msg("Failed to name window pixmap, falling back to direct capture")
					drawable = xproto.Drawable(win)
				} else {
					drawable = xproto.Drawable(pixmap)
					logger.WithComponent("window").Debug().
						Uint32("window_id", uint32(win)).
						Msg("Using Composite pixmap for window capture")
					// Free pixmap when done
					defer xproto.FreePixmap(m.conn, pixmap)
				}
			}
		}
	} else {
		drawable = xproto.Drawable(win)
	}

	// Get window image data
	reply, err := xproto.GetImage(
		m.conn,
		xproto.ImageFormatZPixmap,
		drawable,
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
	}

	return img, nil
}

// GetApplications returns a list of unique applications
func (m *Manager) GetApplications() ([]config.Application, error) {
	windows, err := m.ListWindows()
	if err != nil {
		return nil, err
	}

	// Group windows by class, track display names from titles
	appMap := make(map[string]*config.Application)
	appNames := make(map[string]string) // class -> best display name

	for _, win := range windows {
		if win.Class == "" {
			continue
		}

		// Try to extract a better display name from the window title
		// Many apps format titles as "Document — AppName" or "Document - AppName"
		if win.Title != "" {
			for _, sep := range []string{" — ", " - "} {
				if idx := strings.LastIndex(win.Title, sep); idx > 0 {
					potentialName := strings.TrimSpace(win.Title[idx+len(sep):])
					// Use it if it's reasonable length and not already set
					if len(potentialName) > 0 && len(potentialName) <= 30 {
						if _, exists := appNames[win.Class]; !exists {
							appNames[win.Class] = potentialName
						}
						break
					}
				}
			}
		}

		if _, exists := appMap[win.Class]; !exists {
			appMap[win.Class] = &config.Application{
				ID:          win.Class,
				Name:        win.Class, // Will be updated below
				WindowClass: win.Class,
				PID:         win.PID,
				Allowlisted: m.IsWindowAllowlisted(win),
			}
		}
	}

	// Update display names with extracted names
	for class, name := range appNames {
		if app, exists := appMap[class]; exists {
			app.Name = name
		}
	}

	// Convert map to slice
	apps := make([]config.Application, 0, len(appMap))
	for _, app := range appMap {
		apps = append(apps, *app)
	}

	return apps, nil
}

// SetOutput sets the output destination for captured frames
func (m *Manager) SetOutput(out output.Output) {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	m.output = out
}

// SetOverlayManager sets the overlay manager for rendering overlays
func (m *Manager) SetOverlayManager(overlayMgr *overlay.Manager) {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	m.overlayMgr = overlayMgr
}

// StartStreaming begins continuous capture and streaming of the focused window
func (m *Manager) StartStreaming(fps int) error {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()

	if m.streamRunning {
		return fmt.Errorf("streaming already running")
	}

	if m.output == nil {
		return fmt.Errorf("no output configured")
	}

	m.streamStopChan = make(chan struct{})
	m.streamRunning = true

	go m.streamLoop(fps)

	logger.WithComponent("window").Info().
		Int("fps", fps).
		Msg("Started streaming")
	return nil
}

// StopStreaming stops the continuous capture and streaming
func (m *Manager) StopStreaming() {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()

	if !m.streamRunning {
		return
	}

	close(m.streamStopChan)
	m.streamRunning = false
	logger.WithComponent("window").Info().Msg("Stopped streaming")
}

// streamLoop continuously captures and streams the focused window
func (m *Manager) streamLoop(fps int) {
	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	for {
		select {
		case <-m.streamStopChan:
			return
		case <-ticker.C:
			m.captureAndStream()
		}
	}
}

// captureAndStream captures the current focused window and sends it to the output
func (m *Manager) captureAndStream() {
	// Get current window
	m.mu.RLock()
	currentWin := m.currentWindow
	m.mu.RUnlock()

	if currentWin == nil {
		// No window focused - send placeholder
		if m.output != nil {
			cfg := m.configMgr.Get()
			placeholder := m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
			m.output.WriteFrame(placeholder)
		}
		return
	}

	var windowToCapture *config.WindowInfo
	var usePlaceholder bool

	// Check if current window is allowlisted
	if m.IsWindowAllowlisted(currentWin) {
		// Current window is allowlisted - use it and save as last allowed
		windowToCapture = currentWin
		m.streamMu.Lock()
		m.lastAllowedWindow = currentWin
		m.streamMu.Unlock()
	} else {
		// Current window is not allowlisted - use last allowed window if available
		m.streamMu.Lock()
		lastAllowed := m.lastAllowedWindow
		m.streamMu.Unlock()

		if lastAllowed != nil {
			// We have a previous allowlisted window - keep streaming it (sticky behavior)
			windowToCapture = lastAllowed
		} else {
			// No allowlisted window yet - show placeholder
			usePlaceholder = true
		}
	}

	var img *image.RGBA
	log := logger.WithComponent("stream")

	if usePlaceholder {
		// Create and send placeholder frame
		cfg := m.configMgr.Get()
		img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
	} else {
		var err error

		// Try capture router first (supports both X11 and PipeWire)
		if m.captureRouter != nil && m.captureRouter.CanCapture(windowToCapture) {
			img, err = m.captureRouter.CaptureWindow(windowToCapture)
			if err != nil {
				log.Debug().
					Uint32("id", windowToCapture.ID).
					Str("class", windowToCapture.Class).
					Bool("native_wayland", windowToCapture.IsNativeWayland).
					Err(err).
					Msg("Capture router failed, trying fallback")
			}
		}

		// Fallback to direct X11 capture if router failed or unavailable
		if img == nil && !windowToCapture.IsNativeWayland {
			geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(windowToCapture.ID)).Reply()
			if err != nil {
				log.Debug().
					Uint32("id", windowToCapture.ID).
					Str("class", windowToCapture.Class).
					Err(err).
					Msg("Failed to get window geometry")
			} else {
				img, err = m.captureWindow(xproto.Window(windowToCapture.ID), geom)
				if err != nil {
					log.Debug().
						Uint32("id", windowToCapture.ID).
						Str("class", windowToCapture.Class).
						Err(err).
						Msg("Direct X11 capture failed")
				}
			}
		}

		// If capture failed, clear lastAllowedWindow and send placeholder
		if img == nil {
			m.streamMu.Lock()
			m.lastAllowedWindow = nil
			m.streamMu.Unlock()

			cfg := m.configMgr.Get()
			img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
		}
	}

	// Apply overlay rendering if overlay manager is set
	if m.overlayMgr != nil {
		if err := m.overlayMgr.Render(img); err != nil {
			logger.WithComponent("stream").Error().
				Err(err).
				Msg("Failed to render overlay")
		}
	}

	// Send to output at native resolution - browser will scale to fit viewport
	if err := m.output.WriteFrame(img); err != nil {
		logger.WithComponent("stream").Error().
			Err(err).
			Msg("Failed to write frame to output")
	}
}

// createPlaceholderFrame creates a placeholder frame with a large centered target symbol
// when no allowlisted window has been focused yet
func (m *Manager) createPlaceholderFrame(width, height int) *image.RGBA {
	// Create blank canvas with dark background
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bgColor := color.RGBA{20, 20, 30, 255} // Dark blue-gray background
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// Draw a large target/circle symbol at ~1/4 size
	centerX := width / 2
	centerY := height / 2
	targetSize := width / 4 // Target takes up 1/4 of width

	// Draw concentric circles to create a target symbol
	circleColor1 := color.RGBA{70, 130, 180, 255}  // Steel blue
	circleColor2 := color.RGBA{100, 149, 237, 255} // Cornflower blue

	// Draw outer circle
	drawCircle(img, centerX, centerY, targetSize/2, circleColor1)
	// Draw middle circle
	drawCircle(img, centerX, centerY, targetSize/3, circleColor2)
	// Draw inner circle
	drawCircle(img, centerX, centerY, targetSize/6, circleColor1)
	// Draw center dot
	drawCircle(img, centerX, centerY, targetSize/12, color.RGBA{255, 255, 255, 255})

	// Add text below the target
	text := "Waiting for allowlisted window..."
	textColor := color.RGBA{150, 150, 160, 255}

	// Calculate text position (centered below target)
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(textColor),
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{},
	}

	textWidth := d.MeasureString(text)
	textX := (fixed.I(width) - textWidth) / 2
	textY := fixed.I(centerY + targetSize/2 + 40)

	d.Dot = fixed.Point26_6{X: textX, Y: textY}
	d.DrawString(text)

	return img
}

// drawCircle draws a filled circle at the given position
func drawCircle(img *image.RGBA, cx, cy, radius int, col color.Color) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				img.Set(cx+x, cy+y, col)
			}
		}
	}
}

// scaleAndLetterbox scales an image to fill the max dimensions while maintaining aspect ratio
// Always scales to maximize the viewable area without letterboxing
func (m *Manager) scaleAndLetterbox(src *image.RGBA, out output.Output) *image.RGBA {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	// Target dimensions - scale to fill these while maintaining aspect ratio
	targetWidth := 1920
	targetHeight := 1080

	// If source is already the target size, return as-is
	if srcWidth == targetWidth && srcHeight == targetHeight {
		return src
	}

	// Calculate scaling factor to fit within target dimensions while maintaining aspect ratio
	scaleX := float64(targetWidth) / float64(srcWidth)
	scaleY := float64(targetHeight) / float64(srcHeight)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	// Calculate scaled dimensions (maintain aspect ratio)
	scaledWidth := int(float64(srcWidth) * scale)
	scaledHeight := int(float64(srcHeight) * scale)

	// Create destination image at scaled size (no black bars)
	dst := image.NewRGBA(image.Rect(0, 0, scaledWidth, scaledHeight))

	// Scale the source image to fit
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Src, nil)

	return dst
}
