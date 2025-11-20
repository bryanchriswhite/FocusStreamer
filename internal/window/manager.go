package window

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
)

// Manager handles X11 window detection and monitoring
type Manager struct {
	conn          *xgb.Conn
	root          xproto.Window
	configMgr     *config.Manager
	currentWindow *config.WindowInfo
	mu            sync.RWMutex
	listeners     []chan *config.WindowInfo
	stopChan      chan struct{}
}

// NewManager creates a new window manager
func NewManager(configMgr *config.Manager) (*Manager, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	setup := xproto.Setup(conn)
	root := setup.DefaultScreen(conn).Root

	m := &Manager{
		conn:      conn,
		root:      root,
		configMgr: configMgr,
		listeners: make([]chan *config.WindowInfo, 0),
		stopChan:  make(chan struct{}),
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
	classAtom, err := m.getAtom("WM_CLASS")
	if err == nil {
		if class, err := m.getProperty(win, classAtom); err == nil {
			info.Class = class
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

	// Check exact match in allowlisted apps
	if cfg.AllowlistedApps[window.Class] {
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
