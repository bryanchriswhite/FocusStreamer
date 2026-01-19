package window

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	"image/png"
	"net/url"
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

// ZoomState represents the current zoom and pan state for the stream
type ZoomState struct {
	Scale   float64 `json:"scale"`   // 1.0 = no zoom, 2.0 = 2x zoom, max 4.0
	OffsetX float64 `json:"offsetX"` // Pan offset X as percentage (0.0 = left edge, 1.0 = right edge)
	OffsetY float64 `json:"offsetY"` // Pan offset Y as percentage (0.0 = top edge, 1.0 = bottom edge)
}

// BrowserContext represents the current browser tab context
// for a given window class.
type BrowserContext struct {
	WindowClass string    `json:"window_class"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	UpdatedAt   time.Time `json:"updated_at"`
}

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

	// Manual standby control
	forceStandby bool

	// Allowlist bypass mode - when enabled, all windows are shown regardless of allowlist
	allowlistBypass bool

	// Browser URL contexts keyed by window class
	browserContexts   map[string]BrowserContext
	browserContextMu  sync.RWMutex
	browserContextTTL time.Duration

	// Zoom and pan control
	zoomState ZoomState
	zoomMu    sync.RWMutex

	// Last unzoomed frame for minimap thumbnail
	lastUnzoomedFrame *image.RGBA
	unzoomedFrameMu   sync.RWMutex

	// Cached placeholder frame
	cachedPlaceholder     *image.RGBA
	cachedPlaceholderPath string // Path used to generate cached placeholder
	cachedPlaceholderSize image.Point

	// Placeholder rotation state
	wasInStandby          bool // True if previous frame was showing placeholder
	currentPlaceholderIdx int  // Index of currently selected placeholder (-1 = default)

	// Health monitoring
	lastFrameTime        time.Time
	lastFrameIntervalWarn time.Time
	consecutiveFailures  int
	healthMu             sync.RWMutex
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
		backend:           backend,
		captureRouter:     captureRouter,
		conn:              conn,
		root:              root,
		screen:            screen,
		configMgr:         configMgr,
		listeners:         make([]chan *config.WindowInfo, 0),
		stopChan:          make(chan struct{}),
		compositeEnabled:  compositeEnabled,
		browserContexts:   make(map[string]BrowserContext),
		browserContextTTL: 5 * time.Second,
		zoomState:         ZoomState{Scale: 1.0, OffsetX: 0.5, OffsetY: 0.5},
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
	return m.GetWindowAllowlistSource(window) != config.AllowlistSourceNone
}

// GetWindowAllowlistSource returns why a window is allowlisted (explicit, pattern, or none)
func (m *Manager) GetWindowAllowlistSource(window *config.WindowInfo) config.AllowlistSource {
	if window == nil {
		return config.AllowlistSourceNone
	}

	if m.isBrowserWindow(window.Class) {
		return m.getBrowserAllowlistSource(window.Class)
	}

	cfg := m.configMgr.Get()

	// Normalize class to lowercase for comparison
	normalizedClass := strings.ToLower(window.Class)

	// Check exact match in allowlisted apps first (explicit takes priority)
	for _, app := range cfg.AllowlistedApps {
		if app == normalizedClass {
			return config.AllowlistSourceExplicit
		}
	}

	// Check pattern matching (matches against both class and title)
	for _, pattern := range cfg.AllowlistPatterns {
		if matched, err := regexp.MatchString(pattern, window.Class); err == nil && matched {
			return config.AllowlistSourcePattern
		}
		if matched, err := regexp.MatchString(pattern, window.Title); err == nil && matched {
			return config.AllowlistSourcePattern
		}
	}

	// Check title-only patterns (matches against title only)
	for _, pattern := range cfg.AllowlistTitlePatterns {
		if matched, err := regexp.MatchString(pattern, window.Title); err == nil && matched {
			return config.AllowlistSourcePattern
		}
	}

	return config.AllowlistSourceNone
}

// UpdateBrowserContext updates the active browser URL context.
func (m *Manager) UpdateBrowserContext(windowClass, urlValue, title string) {
	normalized := strings.ToLower(windowClass)
	if normalized == "" {
		return
	}

	if err := m.configMgr.AddBrowserWindowClass(normalized); err != nil {
		logger.WithComponent("window").Warn().Err(err).Msg("Failed to store browser window class")
	}

	ctx := BrowserContext{
		WindowClass: normalized,
		URL:         urlValue,
		Title:       title,
		UpdatedAt:   time.Now(),
	}

	m.browserContextMu.Lock()
	m.browserContexts[normalized] = ctx
	m.browserContextMu.Unlock()
}

// GetBrowserContext returns the current browser context for a window class.
func (m *Manager) GetBrowserContext(windowClass string) (BrowserContext, bool) {
	normalized := strings.ToLower(windowClass)
	m.browserContextMu.RLock()
	ctx, ok := m.browserContexts[normalized]
	m.browserContextMu.RUnlock()
	return ctx, ok
}

// GetBrowserContextStatus returns the current browser context and freshness.
func (m *Manager) GetBrowserContextStatus(windowClass string) (BrowserContext, bool, bool) {
	ctx, ok := m.GetBrowserContext(windowClass)
	if !ok {
		return BrowserContext{}, false, false
	}
	return ctx, true, m.isBrowserContextFresh(ctx)
}

func (m *Manager) GetBrowserContextTTL() time.Duration {
	return m.browserContextTTL
}

func (m *Manager) isBrowserWindow(windowClass string) bool {
	return m.configMgr.IsBrowserWindowClass(windowClass) || m.hasBrowserContext(windowClass)
}

func (m *Manager) hasBrowserContext(windowClass string) bool {
	normalized := strings.ToLower(windowClass)
	m.browserContextMu.RLock()
	_, ok := m.browserContexts[normalized]
	m.browserContextMu.RUnlock()
	return ok
}

func (m *Manager) getBrowserAllowlistSource(windowClass string) config.AllowlistSource {
	if m.configMgr.IsBrowserBlocked(windowClass) {
		return config.AllowlistSourceNone
	}

	ctx, ok := m.GetBrowserContext(windowClass)
	if !ok || !m.isBrowserContextFresh(ctx) {
		return config.AllowlistSourceNone
	}

	if m.isURLAllowlisted(ctx.URL) {
		return config.AllowlistSourceURL
	}

	return config.AllowlistSourceNone
}

func (m *Manager) isBrowserContextFresh(ctx BrowserContext) bool {
	if ctx.UpdatedAt.IsZero() {
		return false
	}
	return time.Since(ctx.UpdatedAt) <= m.browserContextTTL
}

func (m *Manager) isURLAllowlisted(urlValue string) bool {
	cfg := m.configMgr.Get()
	for _, rule := range cfg.AllowlistURLRules {
		if matchURLRule(urlValue, rule) {
			return true
		}
	}
	return false
}

func matchURLRule(urlValue string, rule config.UrlRule) bool {
	normalizedPattern := strings.TrimSpace(strings.ToLower(rule.Pattern))
	if normalizedPattern == "" {
		return false
	}

	parsedURL, err := url.Parse(urlValue)
	if err != nil || parsedURL.Host == "" {
		return false
	}

	host := strings.ToLower(parsedURL.Hostname())
	if host == "" {
		return false
	}

	switch rule.Type {
	case config.UrlRuleTypePage:
		parsedURL.Fragment = ""
		normalizedURL, err := normalizeURL(parsedURL.String())
		if err != nil {
			return false
		}
		patternURL, err := normalizeURL(rule.Pattern)
		if err != nil {
			return false
		}
		return normalizedURL == patternURL
	case config.UrlRuleTypeDomain:
		patternHost := strings.TrimPrefix(normalizedPattern, ".")
		if host == patternHost {
			return true
		}
		return strings.HasSuffix(host, "."+patternHost)
	case config.UrlRuleTypeSubdomain:
		patternHost := strings.TrimPrefix(normalizedPattern, ".")
		return host == patternHost
	default:
		return false
	}
}

func normalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("url missing scheme or host")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
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

// WindowState represents the state of a window for capture decisions
type WindowState int

const (
	WindowStateInvalid     WindowState = iota // Window doesn't exist or has bad geometry
	WindowStateValid                          // Window exists but not capturable (obscured/minimized)
	WindowStateCapturable                     // Window exists and can be captured
)

// checkWindowState checks both validity and capturability in a single set of X11 calls
// Returns: state (invalid/valid/capturable)
func (m *Manager) checkWindowState(window *config.WindowInfo) WindowState {
	if window == nil {
		return WindowStateInvalid
	}

	log := logger.WithComponent("window-state")

	// Check window attributes via X11 - single call for both existence and map state
	attrs, err := xproto.GetWindowAttributes(m.conn, xproto.Window(window.ID)).Reply()
	if err != nil {
		// On Wayland, X11 window attributes may fail even for valid windows
		// This is handled via class-based recovery in the streaming loop
		return WindowStateInvalid
	}

	// Check geometry to ensure window has reasonable size
	geom, err := xproto.GetGeometry(m.conn, xproto.Drawable(window.ID)).Reply()
	if err != nil {
		// On Wayland, X11 geometry queries may fail even for valid windows
		return WindowStateInvalid
	}

	// Window should have reasonable dimensions (at least 10x10)
	if geom.Width < 10 || geom.Height < 10 {
		log.Debug().
			Uint32("window_id", window.ID).
			Str("window_class", window.Class).
			Uint16("width", geom.Width).
			Uint16("height", geom.Height).
			Msg("Window has invalid geometry")
		return WindowStateInvalid
	}

	// Window exists with valid geometry - check if capturable
	if attrs.MapState == xproto.MapStateViewable {
		return WindowStateCapturable
	}

	// Window exists but not viewable (obscured/minimized)
	log.Debug().
		Uint32("window_id", window.ID).
		Str("window_class", window.Class).
		Uint8("map_state", attrs.MapState).
		Msg("Window not viewable")
	return WindowStateValid
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
			allowlistSource := m.GetWindowAllowlistSource(win)
			appMap[win.Class] = &config.Application{
				ID:              win.Class,
				Name:            win.Class, // Will be updated below
				WindowClass:     win.Class,
				PID:             win.PID,
				Allowlisted:     allowlistSource != config.AllowlistSourceNone,
				AllowlistSource: allowlistSource,
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

// captureState holds a consistent snapshot of state needed for frame capture
type captureState struct {
	forceStandby      bool
	wasInStandby      bool
	allowlistBypass   bool
	lastAllowedWindow *config.WindowInfo
	currentWindow     *config.WindowInfo
}

// getCaptureState returns a consistent snapshot of capture-related state
func (m *Manager) getCaptureState() captureState {
	m.streamMu.Lock()
	state := captureState{
		forceStandby:      m.forceStandby,
		wasInStandby:      m.wasInStandby,
		allowlistBypass:   m.allowlistBypass,
		lastAllowedWindow: m.lastAllowedWindow,
	}
	m.streamMu.Unlock()

	m.mu.RLock()
	state.currentWindow = m.currentWindow
	m.mu.RUnlock()

	return state
}

// updateCaptureState updates the capture state after processing
func (m *Manager) updateCaptureState(wasInStandby bool, lastAllowed *config.WindowInfo) {
	m.streamMu.Lock()
	m.wasInStandby = wasInStandby
	if lastAllowed != nil {
		m.lastAllowedWindow = lastAllowed
	}
	m.streamMu.Unlock()
}

// clearLastAllowedWindow clears the last allowed window
func (m *Manager) clearLastAllowedWindow() {
	m.streamMu.Lock()
	m.lastAllowedWindow = nil
	m.streamMu.Unlock()
}

// captureAndStream captures the current focused window and sends it to the output
func (m *Manager) captureAndStream() {
	log := logger.WithComponent("stream")

	// Track frame timing for health monitoring
	frameStart := time.Now()
	m.healthMu.Lock()
	lastFrame := m.lastFrameTime
	m.lastFrameTime = frameStart
	m.healthMu.Unlock()

	// Warn if frame interval is too long (>3x expected interval)
	// Rate-limit to once per 10 seconds to avoid log spam
	if !lastFrame.IsZero() {
		interval := frameStart.Sub(lastFrame)
		// Calculate threshold based on actual FPS setting
		cfg := m.configMgr.Get()
		fps := cfg.VirtualDisplay.FPS
		if fps <= 0 {
			fps = 10 // default
		}
		expectedInterval := time.Second / time.Duration(fps)
		threshold := expectedInterval * 3 // 3x expected = real stall

		if interval > threshold {
			m.healthMu.Lock()
			lastWarn := m.lastFrameIntervalWarn
			if frameStart.Sub(lastWarn) > 10*time.Second {
				m.lastFrameIntervalWarn = frameStart
				m.healthMu.Unlock()
				log.Warn().
					Dur("interval", interval).
					Dur("threshold", threshold).
					Int("fps", fps).
					Msg("Frame interval exceeds threshold - possible stall")
			} else {
				m.healthMu.Unlock()
			}
		}
	}

	// Track whether this frame shows standby/placeholder
	showingStandby := false

	// Check if force standby is enabled
	m.streamMu.Lock()
	forceStandby := m.forceStandby
	wasInStandby := m.wasInStandby
	m.streamMu.Unlock()

	if forceStandby {
		showingStandby = true
		// Detect transition TO standby for rotation
		if !wasInStandby {
			m.rotatePlaceholder()
		}
		if m.output != nil {
			cfg := m.configMgr.Get()
			placeholder := m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
			m.output.WriteFrame(placeholder)
		}
		// Update wasInStandby before returning
		m.streamMu.Lock()
		m.wasInStandby = showingStandby
		m.streamMu.Unlock()
		return
	}

	// Get current window
	m.mu.RLock()
	currentWin := m.currentWindow
	m.mu.RUnlock()

	// Get current desktop once for all checks
	currentDesktop := m.backend.GetCurrentDesktop()

	// Check if window is on current desktop
	// Desktop -1 means window is on all desktops (sticky)
	if currentWin != nil {
		if currentWin.Desktop != -1 && currentWin.Desktop != currentDesktop {
			log.Debug().
				Int("window_desktop", currentWin.Desktop).
				Int("current_desktop", currentDesktop).
				Str("window_class", currentWin.Class).
				Msg("Window not on current desktop, treating as unfocused")
			currentWin = nil
		}
	}

	var windowToCapture *config.WindowInfo
	var usePlaceholder bool

	// Check allowlist bypass mode
	m.streamMu.Lock()
	bypassEnabled := m.allowlistBypass
	lastAllowed := m.lastAllowedWindow
	m.streamMu.Unlock()

	if currentWin == nil {
		// No window focused (or not on current desktop) - try to use last allowed window
		if lastAllowed != nil {
			// Check window state in a single X11 call
			state := m.checkWindowState(lastAllowed)

			if state == WindowStateInvalid {
				// Window ID might be stale - try to find window by class before giving up
				refreshedWin, err := m.FindWindowByClass(lastAllowed.Class)
				refreshedOnCurrentDesktop := refreshedWin != nil && (refreshedWin.Desktop == -1 || refreshedWin.Desktop == currentDesktop)
				if err == nil && refreshedOnCurrentDesktop && (bypassEnabled || m.IsWindowAllowlisted(refreshedWin)) {
					// Found the window by class on current desktop - try to capture it
					// On Wayland, X11 state checks may fail but capture can still work via PipeWire
					// Only log when window ID actually changes to avoid spam
					if refreshedWin.ID != lastAllowed.ID {
						log.Debug().
							Uint32("old_window_id", lastAllowed.ID).
							Uint32("new_window_id", refreshedWin.ID).
							Str("window_class", lastAllowed.Class).
							Msg("Recovered window by class with new ID")
					}
					m.streamMu.Lock()
					m.lastAllowedWindow = refreshedWin
					m.streamMu.Unlock()
					windowToCapture = refreshedWin
				} else {
					if err == nil && !refreshedOnCurrentDesktop {
						log.Debug().
							Uint32("window_id", refreshedWin.ID).
							Str("window_class", refreshedWin.Class).
							Int("window_desktop", refreshedWin.Desktop).
							Int("current_desktop", currentDesktop).
							Msg("Recovered window by class but not on current desktop")
					} else {
						log.Debug().
							Uint32("window_id", lastAllowed.ID).
							Str("window_class", lastAllowed.Class).
							Msg("Last allowed window no longer valid (closed)")
					}
					m.streamMu.Lock()
					m.lastAllowedWindow = nil
					m.streamMu.Unlock()
					usePlaceholder = true
				}
			} else {
				lastAllowedOnCurrentDesktop := lastAllowed.Desktop == -1 || lastAllowed.Desktop == currentDesktop

				if lastAllowedOnCurrentDesktop && (bypassEnabled || m.IsWindowAllowlisted(lastAllowed)) {
					if state == WindowStateCapturable {
						windowToCapture = lastAllowed
					} else {
						// Window exists but not capturable (obscured/minimized) - show placeholder
						// but keep lastAllowedWindow for when it becomes capturable again
						usePlaceholder = true
					}
				} else {
					// Last allowed window no longer valid for capture (wrong desktop or not allowlisted)
					if !lastAllowedOnCurrentDesktop {
						log.Debug().
							Int("window_desktop", lastAllowed.Desktop).
							Int("current_desktop", currentDesktop).
							Str("window_class", lastAllowed.Class).
							Msg("Last allowed window not on current desktop")
					}
					m.streamMu.Lock()
					m.lastAllowedWindow = nil
					m.streamMu.Unlock()
					usePlaceholder = true
				}
			}
		} else {
			usePlaceholder = true
		}
	} else {
		// Check if current window is allowlisted (or bypass is enabled)
		isAllowlisted := bypassEnabled || m.IsWindowAllowlisted(currentWin)
		if isAllowlisted {
			// Current window is allowlisted - use it and save as last allowed
			windowToCapture = currentWin
			m.streamMu.Lock()
			m.lastAllowedWindow = currentWin
			m.streamMu.Unlock()
		} else {
			// Current window is not allowlisted - use last allowed window if available
			if lastAllowed != nil {
				// Check if same window (e.g., browser tab changed to non-matching title)
				if lastAllowed.ID == currentWin.ID {
					log.Debug().
						Uint32("current_id", currentWin.ID).
						Str("current_class", currentWin.Class).
						Msg("Current window same as lastAllowed but no longer allowlisted")
					m.streamMu.Lock()
					m.lastAllowedWindow = nil
					m.streamMu.Unlock()
					usePlaceholder = true
				} else {
					// Check window state in a single X11 call
					state := m.checkWindowState(lastAllowed)

					if state == WindowStateInvalid {
						// Window ID might be stale - try to find window by class before giving up
						refreshedWin, err := m.FindWindowByClass(lastAllowed.Class)
						refreshedOnCurrentDesktop := refreshedWin != nil && (refreshedWin.Desktop == -1 || refreshedWin.Desktop == currentDesktop)
						if err == nil && refreshedOnCurrentDesktop && (bypassEnabled || m.IsWindowAllowlisted(refreshedWin)) {
							// Found the window by class on current desktop - try to capture it
							// On Wayland, X11 state checks may fail but capture can still work via PipeWire
							// Only log when window ID actually changes to avoid spam
							if refreshedWin.ID != lastAllowed.ID {
								log.Debug().
									Uint32("old_window_id", lastAllowed.ID).
									Uint32("new_window_id", refreshedWin.ID).
									Str("window_class", lastAllowed.Class).
									Msg("Recovered window by class with new ID")
							}
							m.streamMu.Lock()
							m.lastAllowedWindow = refreshedWin
							m.streamMu.Unlock()
							windowToCapture = refreshedWin
						} else {
							if err == nil && !refreshedOnCurrentDesktop {
								log.Debug().
									Uint32("window_id", refreshedWin.ID).
									Str("window_class", refreshedWin.Class).
									Int("window_desktop", refreshedWin.Desktop).
									Int("current_desktop", currentDesktop).
									Msg("Recovered window by class but not on current desktop")
							} else {
								log.Debug().
									Uint32("window_id", lastAllowed.ID).
									Str("window_class", lastAllowed.Class).
									Msg("Last allowed window no longer valid (closed)")
							}
							m.streamMu.Lock()
							m.lastAllowedWindow = nil
							m.streamMu.Unlock()
							usePlaceholder = true
						}
					} else {
						lastAllowedOnCurrentDesktop := lastAllowed.Desktop == -1 || lastAllowed.Desktop == currentDesktop
						lastAllowedStillAllowlisted := bypassEnabled || m.IsWindowAllowlisted(lastAllowed)

						if lastAllowedOnCurrentDesktop && lastAllowedStillAllowlisted {
							if state == WindowStateCapturable {
								windowToCapture = lastAllowed
							} else {
								// Window exists but not capturable - show placeholder but keep reference
								log.Debug().
									Uint32("window_id", lastAllowed.ID).
									Str("window_class", lastAllowed.Class).
									Int("state", int(state)).
									Msg("Last allowed window not capturable (obscured/minimized)")
								usePlaceholder = true
							}
						} else {
							log.Debug().
								Uint32("window_id", lastAllowed.ID).
								Str("window_class", lastAllowed.Class).
								Bool("on_current_desktop", lastAllowedOnCurrentDesktop).
								Bool("still_allowlisted", lastAllowedStillAllowlisted).
								Int("window_desktop", lastAllowed.Desktop).
								Int("current_desktop", currentDesktop).
								Msg("Last allowed window no longer valid for fallback")
							m.streamMu.Lock()
							m.lastAllowedWindow = nil
							m.streamMu.Unlock()
							usePlaceholder = true
						}
					}
				}
			} else {
				// No allowlisted window yet - show placeholder
				usePlaceholder = true
			}
		}
	}

	var img *image.RGBA

	if usePlaceholder {
		showingStandby = true
		// Detect transition TO standby for rotation
		if !wasInStandby {
			m.rotatePlaceholder()
		}
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
			// Track consecutive failures for health monitoring
			m.healthMu.Lock()
			m.consecutiveFailures++
			failures := m.consecutiveFailures
			m.healthMu.Unlock()

			// Log warning at thresholds
			if failures == 5 || failures%50 == 0 {
				log.Warn().
					Int("consecutive_failures", failures).
					Str("window_class", windowToCapture.Class).
					Uint32("window_id", windowToCapture.ID).
					Msg("Consecutive capture failures - window may be closed or inaccessible")
			}

			showingStandby = true
			// Detect transition TO standby for rotation
			if !wasInStandby {
				m.rotatePlaceholder()
			}
			m.streamMu.Lock()
			m.lastAllowedWindow = nil
			m.streamMu.Unlock()

			cfg := m.configMgr.Get()
			img = m.createPlaceholderFrame(cfg.VirtualDisplay.Width, cfg.VirtualDisplay.Height)
		} else {
			// Reset consecutive failures on successful capture
			m.healthMu.Lock()
			m.consecutiveFailures = 0
			m.healthMu.Unlock()
		}
	}

	// Store unzoomed frame for minimap thumbnail
	m.unzoomedFrameMu.Lock()
	m.lastUnzoomedFrame = img
	m.unzoomedFrameMu.Unlock()

	// Apply zoom/pan transformation if active
	img = m.applyZoom(img)

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

	// Update wasInStandby for next frame's transition detection
	m.streamMu.Lock()
	m.wasInStandby = showingStandby
	m.streamMu.Unlock()
}

// createPlaceholderFrame creates a placeholder frame with a large centered target symbol
// when no allowlisted window has been focused yet
func (m *Manager) createPlaceholderFrame(width, height int) *image.RGBA {
	// Get the current placeholder path based on index
	paths := m.configMgr.GetPlaceholderImagePaths()
	m.streamMu.Lock()
	idx := m.currentPlaceholderIdx
	m.streamMu.Unlock()

	var currentPath string
	if idx >= 0 && idx < len(paths) {
		currentPath = paths[idx]
	}

	// Check if we can use cached placeholder
	m.streamMu.Lock()
	if m.cachedPlaceholder != nil &&
		m.cachedPlaceholderPath == currentPath &&
		m.cachedPlaceholderSize.X == width &&
		m.cachedPlaceholderSize.Y == height {
		cached := m.cachedPlaceholder
		m.streamMu.Unlock()
		return cached
	}
	m.streamMu.Unlock()

	log := logger.WithComponent("placeholder")
	log.Debug().Msg("Generating new placeholder frame")

	// Try to load custom placeholder image if configured
	if currentPath != "" {
		if customImg, err := m.loadAndResizeImage(currentPath, width, height); err == nil {
			log.Debug().Str("path", currentPath).Int("index", idx).Msg("Using custom placeholder image")
			// Cache it
			m.streamMu.Lock()
			m.cachedPlaceholder = customImg
			m.cachedPlaceholderPath = currentPath
			m.cachedPlaceholderSize = image.Point{X: width, Y: height}
			m.streamMu.Unlock()
			return customImg
		} else {
			log.Warn().Err(err).Str("path", currentPath).Msg("Failed to load custom placeholder, using default")
		}
	}

	// Default placeholder: Create blank canvas with dark background
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

	// Cache the default placeholder
	m.streamMu.Lock()
	m.cachedPlaceholder = img
	m.cachedPlaceholderPath = "" // Empty path means default placeholder
	m.cachedPlaceholderSize = image.Point{X: width, Y: height}
	m.streamMu.Unlock()

	return img
}

// loadAndResizeImage loads an image from disk and resizes it to fit the given dimensions
// while maintaining aspect ratio, centering it on a dark background
func (m *Manager) loadAndResizeImage(path string, width, height int) (*image.RGBA, error) {
	// Open file
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	// Decode image (supports PNG, JPEG, GIF via registered decoders)
	srcImg, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Create destination canvas with black background
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bgColor := color.RGBA{0, 0, 0, 255} // Pure black background
	draw.Draw(dst, dst.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// Calculate scaling to fit while maintaining aspect ratio
	srcBounds := srcImg.Bounds()
	srcW := float64(srcBounds.Dx())
	srcH := float64(srcBounds.Dy())
	dstW := float64(width)
	dstH := float64(height)

	// Calculate scale factor (fit within bounds)
	scaleX := dstW / srcW
	scaleY := dstH / srcH
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	// Calculate new dimensions
	newW := int(srcW * scale)
	newH := int(srcH * scale)

	// Calculate offset to center the image
	offsetX := (width - newW) / 2
	offsetY := (height - newH) / 2

	// Scale and draw the image centered on the canvas
	dstRect := image.Rect(offsetX, offsetY, offsetX+newW, offsetY+newH)
	xdraw.CatmullRom.Scale(dst, dstRect, srcImg, srcBounds, xdraw.Over, nil)

	return dst, nil
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

// SetForceStandby sets the force standby mode
func (m *Manager) SetForceStandby(enabled bool) {
	m.streamMu.Lock()
	m.forceStandby = enabled
	m.streamMu.Unlock()
	logger.WithComponent("stream").Info().Bool("enabled", enabled).Msg("Force standby mode changed")
}

// GetForceStandby returns the current force standby state
func (m *Manager) GetForceStandby() bool {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	return m.forceStandby
}

// ToggleForceStandby toggles the force standby mode and returns the new state
func (m *Manager) ToggleForceStandby() bool {
	m.streamMu.Lock()
	wasInStandby := m.wasInStandby
	m.forceStandby = !m.forceStandby
	newState := m.forceStandby
	m.streamMu.Unlock()

	// If turning ON standby and we weren't already showing placeholder, rotate
	if newState && !wasInStandby {
		m.rotatePlaceholder()
	}

	logger.WithComponent("stream").Info().Bool("enabled", newState).Msg("Force standby mode toggled")
	return newState
}

// rotatePlaceholder cycles to the next placeholder image
// Called only on transition TO standby mode
func (m *Manager) rotatePlaceholder() {
	m.CyclePlaceholder(1)
}

// SetAllowlistBypass sets the allowlist bypass mode
func (m *Manager) SetAllowlistBypass(enabled bool) {
	m.streamMu.Lock()
	m.allowlistBypass = enabled
	m.streamMu.Unlock()
	logger.WithComponent("stream").Info().Bool("enabled", enabled).Msg("Allowlist bypass mode changed")
}

// GetAllowlistBypass returns the current allowlist bypass state
func (m *Manager) GetAllowlistBypass() bool {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	return m.allowlistBypass
}

// ToggleAllowlistBypass toggles the allowlist bypass mode and returns the new state
func (m *Manager) ToggleAllowlistBypass() bool {
	m.streamMu.Lock()
	m.allowlistBypass = !m.allowlistBypass
	newState := m.allowlistBypass
	m.streamMu.Unlock()

	logger.WithComponent("stream").Info().Bool("enabled", newState).Msg("Allowlist bypass mode toggled")
	return newState
}

// CyclePlaceholder cycles the placeholder by the given direction (+1 for next, -1 for prev)
func (m *Manager) CyclePlaceholder(direction int) {
	paths := m.configMgr.GetPlaceholderImagePaths()
	log := logger.WithComponent("placeholder")

	if len(paths) == 0 {
		m.streamMu.Lock()
		m.currentPlaceholderIdx = -1
		m.cachedPlaceholder = nil // Invalidate cache
		m.streamMu.Unlock()
		log.Debug().Msg("No placeholder images configured, using default")
		return
	}

	if len(paths) == 1 {
		m.streamMu.Lock()
		if m.currentPlaceholderIdx != 0 {
			m.currentPlaceholderIdx = 0
			m.cachedPlaceholder = nil // Invalidate cache
		}
		m.streamMu.Unlock()
		log.Debug().Str("path", paths[0]).Msg("Single placeholder image, no cycling needed")
		return
	}

	// Cycle in the given direction
	m.streamMu.Lock()
	currentIdx := m.currentPlaceholderIdx
	newIdx := (currentIdx + direction + len(paths)) % len(paths)
	m.currentPlaceholderIdx = newIdx
	m.cachedPlaceholder = nil // Invalidate cache to force reload
	m.streamMu.Unlock()

	log.Debug().
		Int("new_index", newIdx).
		Int("direction", direction).
		Str("path", paths[newIdx]).
		Msg("Cycled placeholder image")
}

// GetZoomState returns the current zoom state
func (m *Manager) GetZoomState() ZoomState {
	m.zoomMu.RLock()
	defer m.zoomMu.RUnlock()
	return m.zoomState
}

// SetZoomState sets the zoom state with validation
func (m *Manager) SetZoomState(state ZoomState) ZoomState {
	m.zoomMu.Lock()
	defer m.zoomMu.Unlock()

	// Clamp scale between 1.0 and 4.0
	if state.Scale < 1.0 {
		state.Scale = 1.0
	} else if state.Scale > 4.0 {
		state.Scale = 4.0
	}

	// Clamp offsets to valid range based on scale
	// At scale 2.0, viewport is 50% of image, so offset can be 0.25 to 0.75
	// At scale 4.0, viewport is 25% of image, so offset can be 0.125 to 0.875
	if state.Scale > 1.0 {
		viewportSize := 1.0 / state.Scale
		minOffset := viewportSize / 2
		maxOffset := 1.0 - viewportSize/2

		if state.OffsetX < minOffset {
			state.OffsetX = minOffset
		} else if state.OffsetX > maxOffset {
			state.OffsetX = maxOffset
		}

		if state.OffsetY < minOffset {
			state.OffsetY = minOffset
		} else if state.OffsetY > maxOffset {
			state.OffsetY = maxOffset
		}
	} else {
		// At scale 1.0, offset should be centered
		state.OffsetX = 0.5
		state.OffsetY = 0.5
	}

	m.zoomState = state
	return m.zoomState
}

// ResetZoom resets the zoom to default (no zoom)
func (m *Manager) ResetZoom() ZoomState {
	return m.SetZoomState(ZoomState{Scale: 1.0, OffsetX: 0.5, OffsetY: 0.5})
}

// GetThumbnail returns a scaled-down unzoomed thumbnail of the current stream frame
func (m *Manager) GetThumbnail(maxWidth int) *image.RGBA {
	m.unzoomedFrameMu.RLock()
	src := m.lastUnzoomedFrame
	m.unzoomedFrameMu.RUnlock()

	if src == nil {
		return nil
	}

	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	// Calculate thumbnail dimensions maintaining aspect ratio
	scale := float64(maxWidth) / float64(srcWidth)
	dstWidth := maxWidth
	dstHeight := int(float64(srcHeight) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	return dst
}

// applyZoom applies the current zoom/pan state to an image
func (m *Manager) applyZoom(img *image.RGBA) *image.RGBA {
	m.zoomMu.RLock()
	state := m.zoomState
	m.zoomMu.RUnlock()

	// No zoom needed if scale is 1.0
	if state.Scale <= 1.0 {
		return img
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Calculate the size of the viewport in source pixels
	viewportWidth := float64(width) / state.Scale
	viewportHeight := float64(height) / state.Scale

	// Calculate the top-left corner of the crop region
	// Offset is the center position as a percentage
	cropX := state.OffsetX*float64(width) - viewportWidth/2
	cropY := state.OffsetY*float64(height) - viewportHeight/2

	// Clamp to ensure we don't go out of bounds
	if cropX < 0 {
		cropX = 0
	}
	if cropY < 0 {
		cropY = 0
	}
	if cropX+viewportWidth > float64(width) {
		cropX = float64(width) - viewportWidth
	}
	if cropY+viewportHeight > float64(height) {
		cropY = float64(height) - viewportHeight
	}

	// Create the crop rectangle
	cropRect := image.Rect(
		int(cropX),
		int(cropY),
		int(cropX+viewportWidth),
		int(cropY+viewportHeight),
	)

	// Create destination image at virtual display size (fills output canvas when zoomed)
	cfg := m.configMgr.Get()
	dstWidth := cfg.VirtualDisplay.Width
	dstHeight := cfg.VirtualDisplay.Height
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))

	// Calculate scaling to maintain aspect ratio (letterbox if needed)
	cropWidth := cropRect.Dx()
	cropHeight := cropRect.Dy()
	cropAspect := float64(cropWidth) / float64(cropHeight)
	dstAspect := float64(dstWidth) / float64(dstHeight)

	var scaledWidth, scaledHeight int
	if cropAspect > dstAspect {
		// Source is wider - fit to width, letterbox top/bottom
		scaledWidth = dstWidth
		scaledHeight = int(float64(dstWidth) / cropAspect)
	} else {
		// Source is taller - fit to height, letterbox left/right
		scaledHeight = dstHeight
		scaledWidth = int(float64(dstHeight) * cropAspect)
	}

	// Center the scaled image
	offsetX := (dstWidth - scaledWidth) / 2
	offsetY := (dstHeight - scaledHeight) / 2
	scaledRect := image.Rect(offsetX, offsetY, offsetX+scaledWidth, offsetY+scaledHeight)

	// Scale the cropped region to the centered rectangle (maintains aspect ratio)
	xdraw.CatmullRom.Scale(dst, scaledRect, img, cropRect, xdraw.Over, nil)

	return dst
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

// HealthStatus contains streaming health information
type HealthStatus struct {
	LastFrameTime       time.Time `json:"last_frame_time"`
	FrameAge            string    `json:"frame_age"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	IsHealthy           bool      `json:"is_healthy"`
	StreamRunning       bool      `json:"stream_running"`
}

// GetHealthStatus returns the current health status of the stream
func (m *Manager) GetHealthStatus() HealthStatus {
	m.healthMu.RLock()
	lastFrame := m.lastFrameTime
	failures := m.consecutiveFailures
	m.healthMu.RUnlock()

	m.streamMu.Lock()
	running := m.streamRunning
	m.streamMu.Unlock()

	var frameAge string
	if lastFrame.IsZero() {
		frameAge = "never"
	} else {
		frameAge = time.Since(lastFrame).Round(time.Millisecond).String()
	}

	// Consider unhealthy if: not running, >5 consecutive failures, or frame age > 1s
	isHealthy := running && failures < 5 && (lastFrame.IsZero() || time.Since(lastFrame) < time.Second)

	return HealthStatus{
		LastFrameTime:       lastFrame,
		FrameAge:            frameAge,
		ConsecutiveFailures: failures,
		IsHealthy:           isHealthy,
		StreamRunning:       running,
	}
}

// OnProfileChanged should be called when the active profile changes.
// It invalidates cached state that depends on profile settings.
func (m *Manager) OnProfileChanged(profileID string) {
	logger.WithComponent("window-manager").Info().
		Str("profile_id", profileID).
		Msg("Profile changed, invalidating caches")

	// Clear cached placeholder image
	m.streamMu.Lock()
	m.cachedPlaceholder = nil
	m.cachedPlaceholderPath = ""
	m.currentPlaceholderIdx = 0
	m.streamMu.Unlock()

	// Clear the last allowed window since allowlist may have changed
	m.streamMu.Lock()
	m.lastAllowedWindow = nil
	m.streamMu.Unlock()
}
