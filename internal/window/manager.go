package window

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/composite"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/output"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Manager handles X11 window detection and monitoring
type Manager struct {
	conn             *xgb.Conn
	root             xproto.Window
	screen           *xproto.ScreenInfo
	configMgr        *config.Manager
	currentWindow    *config.WindowInfo
	mu               sync.RWMutex
	listeners        []chan *config.WindowInfo
	stopChan         chan struct{}
	compositeEnabled bool

	// Output for streaming frames
	output              output.Output
	streamStopChan      chan struct{}
	streamRunning       bool
	streamMu            sync.Mutex
	lastAllowedWindow   *config.WindowInfo // Last allowlisted window to stream
}

// NewManager creates a new window manager
func NewManager(configMgr *config.Manager) (*Manager, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	root := screen.Root

	// Initialize composite extension
	compositeEnabled := false
	if err := composite.Init(conn); err != nil {
		log.Printf("Warning: Composite extension not available: %v", err)
		log.Printf("Window screenshots may fail for obscured or off-screen windows")
	} else {
		compositeEnabled = true
		log.Printf("Composite extension initialized successfully")
	}

	m := &Manager{
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

// Start begins monitoring window focus changes
func (m *Manager) Start() error {
	// Subscribe to window focus events
	const eventMask = xproto.EventMaskPropertyChange | xproto.EventMaskFocusChange

	if err := xproto.ChangeWindowAttributesChecked(
		m.conn,
		m.root,
		xproto.CwEventMask,
		[]uint32{eventMask},
	).Check(); err != nil {
		return fmt.Errorf("failed to set event mask: %w", err)
	}

	// Start monitoring in a goroutine
	go m.monitorFocus()

	// Get initial focused window
	if err := m.updateCurrentWindow(); err != nil {
		fmt.Printf("Warning: failed to get initial window: %v\n", err)
	}

	return nil
}

// Stop stops the window manager
func (m *Manager) Stop() {
	close(m.stopChan)
	m.conn.Close()
}

// monitorFocus monitors window focus changes
func (m *Manager) monitorFocus() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if err := m.updateCurrentWindow(); err != nil {
				fmt.Printf("Error updating window: %v\n", err)
			}
		}
	}
}

// updateCurrentWindow updates the currently focused window
func (m *Manager) updateCurrentWindow() error {
	focusReply, err := xproto.GetInputFocus(m.conn).Reply()
	if err != nil {
		return err
	}

	focusWindow := focusReply.Focus

	// Get window information
	windowInfo, err := m.getWindowInfo(focusWindow)
	if err != nil {
		return err
	}

	// Check if window changed
	m.mu.Lock()
	changed := m.currentWindow == nil || m.currentWindow.ID != windowInfo.ID
	m.currentWindow = windowInfo
	m.mu.Unlock()

	// Notify listeners if window changed
	if changed {
		m.notifyListeners(windowInfo)
	}

	return nil
}

// getWindowInfo retrieves information about a window
func (m *Manager) getWindowInfo(win xproto.Window) (*config.WindowInfo, error) {
	info := &config.WindowInfo{
		ID:      uint32(win),
		Focused: true,
	}

	// Get window geometry
	geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(win)).Reply()
	if err == nil {
		info.Geometry = config.Geometry{
			X:      int(geom.X),
			Y:      int(geom.Y),
			Width:  int(geom.Width),
			Height: int(geom.Height),
		}
	}

	// Get window title
	titleAtom, err := m.getAtom("_NET_WM_NAME")
	if err == nil {
		if title, err := m.getProperty(win, titleAtom); err == nil {
			info.Title = title
		}
	}

	// Try alternative title property
	if info.Title == "" {
		titleAtom, err = m.getAtom("WM_NAME")
		if err == nil {
			if title, err := m.getProperty(win, titleAtom); err == nil {
				info.Title = title
			}
		}
	}

	// Get window class
	// WM_CLASS format is: instance\0class\0 (two null-terminated strings)
	classAtom, err := m.getAtom("WM_CLASS")
	if err == nil {
		if classRaw, err := m.getProperty(win, classAtom); err == nil {
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
	pidAtom, err := m.getAtom("_NET_WM_PID")
	if err == nil {
		pidReply, err := xproto.GetProperty(
			m.conn,
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

// getAtom gets an atom ID by name
func (m *Manager) getAtom(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(m.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// getProperty gets a property value as a string
func (m *Manager) getProperty(win xproto.Window, atom xproto.Atom) (string, error) {
	reply, err := xproto.GetProperty(
		m.conn,
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

// GetCurrentWindow returns the currently focused window
func (m *Manager) GetCurrentWindow() *config.WindowInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentWindow
}

// ListWindows returns all visible windows
func (m *Manager) ListWindows() ([]*config.WindowInfo, error) {
	tree, err := xproto.QueryTree(m.conn, m.root).Reply()
	if err != nil {
		return nil, err
	}

	windows := make([]*config.WindowInfo, 0)
	for _, child := range tree.Children {
		info, err := m.getWindowInfo(child)
		if err != nil {
			continue
		}

		// Skip windows without titles (usually not user windows)
		if info.Title == "" {
			continue
		}

		info.Focused = false
		windows = append(windows, info)
	}

	return windows, nil
}

// IsWindowAllowlisted checks if a window is allowlisted
func (m *Manager) IsWindowAllowlisted(window *config.WindowInfo) bool {
	cfg := m.configMgr.Get()

	// Normalize class to lowercase for comparison (config keys are lowercased by viper)
	normalizedClass := strings.ToLower(window.Class)

	// Check exact match in allowlisted apps
	if cfg.AllowlistedApps[normalizedClass] {
		return true
	}

	// Check pattern matching
	for _, pattern := range cfg.AllowlistPatterns {
		if matched, _ := regexp.MatchString(pattern, window.Class); matched {
			return true
		}
		if matched, _ := regexp.MatchString(pattern, window.Title); matched {
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

	log.Printf("Window %d attributes: class=%d, mapState=%d", windowID, attrs.Class, attrs.MapState)

	// If window is not suitable for capture, try to find a suitable child window
	if attrs.Class != xproto.WindowClassInputOutput || attrs.MapState != xproto.MapStateViewable {
		log.Printf("Window %d not directly capturable, searching for child windows...", windowID)

		// Try to find a child window that can be captured
		childWin, err := m.findCapturableChild(win)
		if err != nil {
			return nil, fmt.Errorf("no capturable window found: %w", err)
		}

		log.Printf("Found capturable child window: %d", childWin)
		win = childWin

		// Get attributes of child window
		attrs, err = xproto.GetWindowAttributes(m.conn, win).Reply()
		if err != nil {
			return nil, fmt.Errorf("failed to get child window attributes: %w", err)
		}
		log.Printf("Child window %d attributes: class=%d, mapState=%d", win, attrs.Class, attrs.MapState)
	}

	// Get window geometry
	geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(win)).Reply()
	if err != nil {
		return nil, fmt.Errorf("failed to get window geometry: %w", err)
	}

	log.Printf("Window %d geometry: %dx%d at (%d,%d)", win, geom.Width, geom.Height, geom.X, geom.Y)

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

	log.Printf("Window %d has %d children", parent, len(tree.Children))

	// Search through children for a capturable window
	for _, child := range tree.Children {
		attrs, err := xproto.GetWindowAttributes(m.conn, child).Reply()
		if err != nil {
			log.Printf("  Child %d: failed to get attributes: %v", child, err)
			continue
		}

		geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(child)).Reply()
		if err != nil {
			log.Printf("  Child %d: failed to get geometry: %v", child, err)
			continue
		}

		log.Printf("  Child %d: class=%d, mapState=%d, size=%dx%d",
			child, attrs.Class, attrs.MapState, geom.Width, geom.Height)

		// Check if this child is capturable
		if attrs.Class == xproto.WindowClassInputOutput && attrs.MapState == xproto.MapStateViewable {
			// Must have reasonable dimensions
			if geom.Width > 10 && geom.Height > 10 {
				log.Printf("  -> Found capturable child: %d", child)
				return child, nil
			} else {
				log.Printf("  -> Too small (need >10x10)")
			}
		} else {
			reasons := []string{}
			if attrs.Class != xproto.WindowClassInputOutput {
				reasons = append(reasons, fmt.Sprintf("class=%d (need %d)", attrs.Class, xproto.WindowClassInputOutput))
			}
			if attrs.MapState != xproto.MapStateViewable {
				reasons = append(reasons, fmt.Sprintf("mapState=%d (need %d)", attrs.MapState, xproto.MapStateViewable))
			}
			log.Printf("  -> Not capturable: %v", reasons)
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
			log.Printf("Warning: Failed to redirect window via Composite: %v", err)
			log.Printf("Falling back to direct window capture")
			drawable = xproto.Drawable(win)
		} else {
			// Ensure we unredirect when done
			defer composite.UnredirectWindow(m.conn, win, composite.RedirectAutomatic)

			// Create a pixmap ID and associate it with the window's off-screen buffer
			pixmap, err := xproto.NewPixmapId(m.conn)
			if err != nil {
				log.Printf("Warning: Failed to generate pixmap ID: %v", err)
				log.Printf("Falling back to direct window capture")
				drawable = xproto.Drawable(win)
			} else {
				// Associate the pixmap with the window's off-screen buffer
				err = composite.NameWindowPixmapChecked(m.conn, win, pixmap).Check()
				if err != nil {
					log.Printf("Warning: Failed to name window pixmap via Composite: %v", err)
					log.Printf("Falling back to direct window capture")
					drawable = xproto.Drawable(win)
				} else {
					drawable = xproto.Drawable(pixmap)
					log.Printf("Using Composite pixmap for window capture")
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

	// Group windows by class
	appMap := make(map[string]*config.Application)
	for _, win := range windows {
		if win.Class == "" {
			continue
		}

		if _, exists := appMap[win.Class]; !exists {
			appMap[win.Class] = &config.Application{
				ID:          win.Class,
				Name:        win.Class,
				WindowClass: win.Class,
				PID:         win.PID,
				Allowlisted: m.configMgr.IsAllowlisted(win.Class),
			}
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

	log.Printf("Started streaming at %d FPS", fps)
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
	log.Printf("Stopped streaming")
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
		log.Printf("[MJPEG STREAM] Capturing allowlisted window: '%s' (class=%s, id=%d)", currentWin.Title, currentWin.Class, currentWin.ID)
	} else {
		// Current window is not allowlisted - use last allowed window if available
		m.streamMu.Lock()
		lastAllowed := m.lastAllowedWindow
		m.streamMu.Unlock()

		if lastAllowed != nil {
			// We have a previous allowlisted window - keep streaming it (sticky behavior)
			windowToCapture = lastAllowed
			log.Printf("[MJPEG STREAM] Current window '%s' not allowlisted, using sticky window: '%s' (class=%s, id=%d)", currentWin.Title, lastAllowed.Title, lastAllowed.Class, lastAllowed.ID)
		} else {
			// No allowlisted window yet - show placeholder
			usePlaceholder = true
			log.Printf("[MJPEG STREAM] No allowlisted window available, showing placeholder")
		}
	}

	var img *image.RGBA

	if usePlaceholder {
		// Create and send placeholder frame
		cfg := m.configMgr.Get()
		img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
	} else {
		// Capture window
		geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(windowToCapture.ID)).Reply()
		if err != nil {
			// Window might have been closed - clear lastAllowedWindow and send placeholder
			m.streamMu.Lock()
			m.lastAllowedWindow = nil
			m.streamMu.Unlock()

			// Send placeholder instead of returning without a frame
			cfg := m.configMgr.Get()
			img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
		} else {
			img, err = m.captureWindow(xproto.Window(windowToCapture.ID), geom)
			if err != nil {
				// Failed to capture - clear lastAllowedWindow and send placeholder
				m.streamMu.Lock()
				m.lastAllowedWindow = nil
				m.streamMu.Unlock()

				// Send placeholder instead of returning without a frame
				cfg := m.configMgr.Get()
				img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
			}
		}
	}

	// Send to output at native resolution - browser will scale to fit viewport
	if err := m.output.WriteFrame(img); err != nil {
		log.Printf("Failed to write frame to output: %v", err)
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
