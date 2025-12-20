package window

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/godbus/dbus/v5"
)

// KWinBackend implements the Backend interface using KWin's D-Bus interface
// and kdotool for window enumeration
type KWinBackend struct {
	conn          *dbus.Conn
	mu            sync.RWMutex
	currentWindow *config.WindowInfo
	stopChan      chan struct{}
	watching      bool
	useKdotool    bool
	// Map from hashed ID to original UUID string (for KWin 6)
	windowUUIDs   map[uint32]string
	uuidMu        sync.RWMutex
	// X11 connection for active window detection (XWayland fallback)
	x11Conn       *xgb.Conn
	x11Root       xproto.Window
	activeAtom    xproto.Atom
	// Cache for KWin script-based active window detection
	cachedActiveUUID     string
	cachedActiveUUIDTime time.Time
	// Channel for desktop change events to trigger immediate focus check
	desktopChangeChan chan struct{}
}

// KWin D-Bus constants
const (
	kwinService                    = "org.kde.KWin"
	kwinPath                       = "/KWin"
	kwinInterface                  = "org.kde.KWin"
	windowsRunnerPath              = "/WindowsRunner"
	krunnerInterface               = "org.kde.krunner1"
	virtualDesktopManagerPath      = "/VirtualDesktopManager"
	virtualDesktopManagerInterface = "org.kde.KWin.VirtualDesktopManager"
)

// NewKWinBackend creates a new KWin D-Bus backend
func NewKWinBackend() (*KWinBackend, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus: %w", err)
	}

	// Check if KWin service is available
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to list D-Bus names: %w", err)
	}

	kwinFound := false
	for _, name := range names {
		if name == kwinService {
			kwinFound = true
			break
		}
	}

	if !kwinFound {
		conn.Close()
		return nil, fmt.Errorf("KWin service not found on D-Bus")
	}

	// Check if kdotool is available (preferred method for window enumeration)
	useKdotool := false
	if _, err := exec.LookPath("kdotool"); err == nil {
		useKdotool = true
		logger.WithComponent("kwin-backend").Info().Msg("Using kdotool for window enumeration")
	} else {
		logger.WithComponent("kwin-backend").Info().Msg("kdotool not found, using qdbus fallback")
	}

	logger.WithComponent("kwin-backend").Info().Msg("Connected to KWin D-Bus service")

	// Initialize X11 connection for active window detection (via XWayland)
	var x11Conn *xgb.Conn
	var x11Root xproto.Window
	var activeAtom xproto.Atom

	x11Conn, err = xgb.NewConn()
	if err != nil {
		logger.WithComponent("kwin-backend").Warn().Err(err).Msg("Failed to connect to X11 for active window detection")
	} else {
		setup := xproto.Setup(x11Conn)
		x11Root = setup.DefaultScreen(x11Conn).Root

		// Get _NET_ACTIVE_WINDOW atom
		atomReply, err := xproto.InternAtom(x11Conn, false, uint16(len("_NET_ACTIVE_WINDOW")), "_NET_ACTIVE_WINDOW").Reply()
		if err == nil {
			activeAtom = atomReply.Atom
			logger.WithComponent("kwin-backend").Debug().Msg("X11 active window detection initialized")
		}
	}

	return &KWinBackend{
		conn:        conn,
		stopChan:    make(chan struct{}),
		useKdotool:  useKdotool,
		windowUUIDs: make(map[uint32]string),
		x11Conn:     x11Conn,
		x11Root:     x11Root,
		activeAtom:  activeAtom,
	}, nil
}

// Connect establishes connection (already done in NewKWinBackend)
func (b *KWinBackend) Connect() error {
	return nil
}

// Close closes the D-Bus and X11 connections
func (b *KWinBackend) Close() error {
	b.StopWatching()
	if b.x11Conn != nil {
		b.x11Conn.Close()
	}
	return b.conn.Close()
}

// Name returns the backend name
func (b *KWinBackend) Name() string {
	return "kwin"
}

// ListWindows returns all visible windows
func (b *KWinBackend) ListWindows() ([]*config.WindowInfo, error) {
	if b.useKdotool {
		return b.listWindowsKdotool()
	}
	return b.listWindowsQdbus()
}

// listWindowsKdotool uses kdotool to enumerate windows
func (b *KWinBackend) listWindowsKdotool() ([]*config.WindowInfo, error) {
	log := logger.WithComponent("kwin-backend")

	// Get list of window IDs
	cmd := exec.Command("kdotool", "search", "--name", ".")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kdotool search failed: %w", err)
	}

	windows := make([]*config.WindowInfo, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// kdotool returns window IDs as strings (UUIDs)
		windowID := line

		// Get window info
		info, err := b.getWindowInfoKdotool(windowID)
		if err != nil {
			log.Debug().Str("windowID", windowID).Err(err).Msg("Failed to get window info")
			continue
		}

		if info.Title == "" && info.Class == "" {
			continue
		}

		windows = append(windows, info)
	}
	return windows, nil
}

// getWindowInfoKdotool gets info for a single window via kdotool
func (b *KWinBackend) getWindowInfoKdotool(windowID string) (*config.WindowInfo, error) {
	// Get window name
	nameCmd := exec.Command("kdotool", "getwindowname", windowID)
	nameOutput, _ := nameCmd.Output()
	name := strings.TrimSpace(string(nameOutput))

	// Get window class (kdotool getwindowclassname)
	classCmd := exec.Command("kdotool", "getwindowclassname", windowID)
	classOutput, _ := classCmd.Output()
	class := strings.TrimSpace(string(classOutput))

	// Get window PID
	pidCmd := exec.Command("kdotool", "getwindowpid", windowID)
	pidOutput, _ := pidCmd.Output()
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidOutput)))

	// Get window geometry via kdotool
	geomCmd := exec.Command("kdotool", "getwindowgeometry", windowID)
	geomOutput, _ := geomCmd.Output()
	geometry := b.parseKdotoolGeometry(string(geomOutput))

	// Generate numeric ID from string ID
	id := hashStringToUint32(windowID)

	// Try to get X11 window ID for XWayland detection
	isNativeWayland := true
	windowPath := "/org/kde/KWin/Window/" + windowID
	if xid, err := b.getWindowXID(windowPath); err == nil && xid > 0 && xid != 0x200000 {
		id = xid // Use actual X11 ID for capture
		isNativeWayland = false
	}

	// Get window desktop
	desktop := b.getWindowDesktopFromDBus(windowPath)

	return &config.WindowInfo{
		ID:              id,
		Title:           name,
		Class:           class,
		PID:             pid,
		Focused:         false,
		Geometry:        geometry,
		IsNativeWayland: isNativeWayland,
		Desktop:         desktop,
	}, nil
}

// parseKdotoolGeometry parses geometry output from kdotool
func (b *KWinBackend) parseKdotoolGeometry(output string) config.Geometry {
	geometry := config.Geometry{}

	// kdotool output format: "Window <id>\n  Position: X,Y\n  Geometry: WxH"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Position:") {
			posStr := strings.TrimPrefix(line, "Position:")
			posStr = strings.TrimSpace(posStr)
			parts := strings.Split(posStr, ",")
			if len(parts) >= 2 {
				geometry.X, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
				geometry.Y, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		} else if strings.HasPrefix(line, "Geometry:") {
			geomStr := strings.TrimPrefix(line, "Geometry:")
			geomStr = strings.TrimSpace(geomStr)
			parts := strings.Split(geomStr, "x")
			if len(parts) >= 2 {
				geometry.Width, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
				geometry.Height, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		}
	}

	return geometry
}

// listWindowsQdbus uses D-Bus to enumerate windows (fallback)
func (b *KWinBackend) listWindowsQdbus() ([]*config.WindowInfo, error) {
	// Try WindowsRunner API first (most reliable on KDE6)
	windows, err := b.listWindowsViaRunner()
	if err == nil && len(windows) > 0 {
		return windows, nil
	}

	// Try D-Bus introspection
	windows, err = b.listWindowsDBusIntrospect()
	if err == nil && len(windows) > 0 {
		return windows, nil
	}

	// Try qdbus6/qdbus command as fallback
	qdbusCmd := "qdbus6"
	if _, err := exec.LookPath("qdbus6"); err != nil {
		qdbusCmd = "qdbus"
		if _, err := exec.LookPath("qdbus"); err != nil {
			return b.listWindowsWmctrl()
		}
	}

	// List all objects under org.kde.KWin
	cmd := exec.Command(qdbusCmd, "org.kde.KWin")
	output, err := cmd.Output()
	if err != nil {
		return b.listWindowsWmctrl()
	}

	// Parse output - look for window/client paths
	windowPaths := []string{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// KWin6 uses /org/kde/KWin/Window/<uuid> paths
		if strings.Contains(line, "/Window/") || strings.Contains(line, "/Client/") {
			windowPaths = append(windowPaths, line)
		}
	}

	windows = make([]*config.WindowInfo, 0)
	for _, path := range windowPaths {
		info, err := b.getWindowInfoFromDBus(path)
		if err != nil {
			continue
		}
		if info.Title == "" && info.Class == "" {
			continue
		}
		info.Focused = false
		windows = append(windows, info)
	}

	return windows, nil
}

// KRunnerMatch represents a match returned by the KRunner interface
// D-Bus signature: (sssida{sv}) - id, text, iconName, type, relevance, properties
type KRunnerMatch struct {
	ID         string
	Text       string
	IconName   string
	Type       int32
	Relevance  float64
	Properties map[string]dbus.Variant
}

// listWindowsViaRunner uses the KRunner WindowsRunner plugin to enumerate windows
func (b *KWinBackend) listWindowsViaRunner() ([]*config.WindowInfo, error) {
	// Call the KRunner Match method on WindowsRunner
	// Returns a(sssida{sv}) - array of structs
	obj := b.conn.Object(kwinService, windowsRunnerPath)

	// The Match method takes a query string - empty string returns all windows
	var rawMatches [][]interface{}
	err := obj.Call(krunnerInterface+".Match", 0, "").Store(&rawMatches)
	if err != nil {
		return nil, fmt.Errorf("failed to call Match: %w", err)
	}

	windows := make([]*config.WindowInfo, 0)

	// Clear and rebuild UUID map
	b.uuidMu.Lock()
	b.windowUUIDs = make(map[uint32]string)

	for _, rawMatch := range rawMatches {
		// Parse the struct: (sssida{sv})
		// 0: id (string)
		// 1: text (string)
		// 2: iconName (string)
		// 3: type (int32)
		// 4: relevance (float64)
		// 5: properties (map[string]dbus.Variant)
		if len(rawMatch) < 6 {
			continue
		}

		rawID, ok := rawMatch[0].(string)
		if !ok {
			continue
		}

		text, _ := rawMatch[1].(string)
		iconName, _ := rawMatch[2].(string)

		// Parse properties map (we don't currently use subtext but keep parsing for future use)
		_ = rawMatch[5] // properties map available if needed

		info := &config.WindowInfo{
			Focused: false,
			Title:   text,
		}

		// Use iconName as class
		info.Class = iconName

		// If no iconName, try to extract class from title
		// Common pattern: "Page Title - Application Name" or "Page Title — Application Name"
		if info.Class == "" && text != "" {
			// Try both " - " and " — " (em dash) as separators
			for _, sep := range []string{" — ", " - "} {
				if idx := strings.LastIndex(text, sep); idx > 0 {
					potentialClass := strings.TrimSpace(text[idx+len(sep):])
					// Validate it looks like an app name (not too long, no special chars at start)
					if len(potentialClass) <= 30 && len(potentialClass) > 0 {
						info.Class = strings.ToLower(potentialClass)
						break
					}
				}
			}
		}

		// Generate ID from the raw ID string
		info.ID = hashStringToUint32(rawID)
		b.windowUUIDs[info.ID] = rawID

		// Skip windows without useful info
		if info.Title == "" && info.Class == "" {
			continue
		}

		// Extract the actual window UUID from the rawID for XWayland lookup
		// Format is like "0_{dc80ff04-3245-4d9b-b9a8-1582640d39e1}"
		info.IsNativeWayland = true // Default to native Wayland
		if strings.Contains(rawID, "{") && strings.Contains(rawID, "}") {
			start := strings.Index(rawID, "{")
			end := strings.Index(rawID, "}")
			if start >= 0 && end > start {
				uuid := rawID[start+1 : end]
				// Try to find the X11 window ID from KWin's window path
				windowPath := "/org/kde/KWin/Window/" + uuid
				if xidVar, err := b.getWindowXID(windowPath); err == nil && xidVar > 0 && xidVar != 0x200000 {
					info.ID = xidVar
					info.IsNativeWayland = false
				}
				// Get geometry from D-Bus
				info.Geometry = b.getWindowGeometryFromDBus(windowPath)
			}
		}

		windows = append(windows, info)
	}

	b.uuidMu.Unlock()

	return windows, nil
}

// getWindowXID tries to get the X11 window ID for a KWin window path
func (b *KWinBackend) getWindowXID(windowPath string) (uint32, error) {
	obj := b.conn.Object(kwinService, dbus.ObjectPath(windowPath))

	// Try to get internalId or xwayland window ID
	// KWin 6 exposes these as properties
	interfaces := []string{
		"org.kde.KWin.Window",
		"org.kde.KWin.Client",
	}

	for _, iface := range interfaces {
		// Try internalId (X11 window ID for XWayland windows)
		if xid, err := obj.GetProperty(iface + ".internalId"); err == nil {
			switch v := xid.Value().(type) {
			case uint32:
				return v, nil
			case int32:
				return uint32(v), nil
			case uint64:
				return uint32(v), nil
			case int64:
				return uint32(v), nil
			}
		}

		// Try windowId
		if xid, err := obj.GetProperty(iface + ".windowId"); err == nil {
			switch v := xid.Value().(type) {
			case uint32:
				return v, nil
			case int32:
				return uint32(v), nil
			case uint64:
				return uint32(v), nil
			case int64:
				return uint32(v), nil
			}
		}
	}

	return 0, fmt.Errorf("no XID found")
}

// getWindowGeometryFromDBus gets window geometry from KWin D-Bus using getWindowInfo method
func (b *KWinBackend) getWindowGeometryFromDBus(windowPath string) config.Geometry {
	geometry := config.Geometry{}

	// Extract UUID from windowPath (format: /org/kde/KWin/Window/{uuid})
	uuid := ""
	if strings.Contains(windowPath, "/Window/") {
		parts := strings.Split(windowPath, "/Window/")
		if len(parts) > 1 {
			uuid = parts[1]
		}
	}

	if uuid == "" {
		return geometry
	}

	// Call getWindowInfo method on /KWin interface
	obj := b.conn.Object(kwinService, kwinPath)
	var result map[string]dbus.Variant
	if err := obj.Call(kwinInterface+".getWindowInfo", 0, uuid).Store(&result); err != nil {
		return geometry
	}

	// Extract geometry from result
	if xVar, ok := result["x"]; ok {
		switch v := xVar.Value().(type) {
		case float64:
			geometry.X = int(v)
		case int32:
			geometry.X = int(v)
		case int64:
			geometry.X = int(v)
		}
	}

	if yVar, ok := result["y"]; ok {
		switch v := yVar.Value().(type) {
		case float64:
			geometry.Y = int(v)
		case int32:
			geometry.Y = int(v)
		case int64:
			geometry.Y = int(v)
		}
	}

	if wVar, ok := result["width"]; ok {
		switch v := wVar.Value().(type) {
		case float64:
			geometry.Width = int(v)
		case int32:
			geometry.Width = int(v)
		case int64:
			geometry.Width = int(v)
		}
	}

	if hVar, ok := result["height"]; ok {
		switch v := hVar.Value().(type) {
		case float64:
			geometry.Height = int(v)
		case int32:
			geometry.Height = int(v)
		case int64:
			geometry.Height = int(v)
		}
	}

	return geometry
}

// listWindowsDBusIntrospect uses D-Bus introspection to find windows
func (b *KWinBackend) listWindowsDBusIntrospect() ([]*config.WindowInfo, error) {
	log := logger.WithComponent("kwin-backend")

	// Introspect /org/kde/KWin to find child nodes
	obj := b.conn.Object(kwinService, "/org/kde/KWin")

	var introspectXML string
	if err := obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&introspectXML); err != nil {
		return nil, fmt.Errorf("introspection failed: %w", err)
	}

	// Parse XML to find window nodes
	// Look for <node name="Window"/> or <node name="Client"/>
	windows := make([]*config.WindowInfo, 0)

	// Simple parsing - look for window/client node patterns
	lines := strings.Split(introspectXML, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<node name=\"") {
			// Extract node name
			start := strings.Index(line, "\"") + 1
			end := strings.LastIndex(line, "\"")
			if start > 0 && end > start {
				nodeName := line[start:end]
				if nodeName == "Window" || nodeName == "Client" {
					// Found window container, introspect it
					containerPath := "/org/kde/KWin/" + nodeName
					windowPaths, err := b.introspectContainer(containerPath)
					if err != nil {
						log.Debug().Str("container", containerPath).Err(err).Msg("Failed to introspect container")
						continue
					}

					for _, path := range windowPaths {
						info, err := b.getWindowInfoFromDBus(path)
						if err != nil {
							continue
						}
						if info.Title == "" && info.Class == "" {
							continue
						}
						info.Focused = false
						windows = append(windows, info)
					}
				}
			}
		}
	}

	return windows, nil
}

// introspectContainer introspects a container path to find child windows
func (b *KWinBackend) introspectContainer(containerPath string) ([]string, error) {
	obj := b.conn.Object(kwinService, dbus.ObjectPath(containerPath))

	var introspectXML string
	if err := obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&introspectXML); err != nil {
		return nil, err
	}

	paths := []string{}
	lines := strings.Split(introspectXML, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<node name=\"") {
			start := strings.Index(line, "\"") + 1
			end := strings.LastIndex(line, "\"")
			if start > 0 && end > start {
				nodeName := line[start:end]
				paths = append(paths, containerPath+"/"+nodeName)
			}
		}
	}

	return paths, nil
}

// listWindowsWmctrl uses wmctrl as a last resort fallback
func (b *KWinBackend) listWindowsWmctrl() ([]*config.WindowInfo, error) {
	cmd := exec.Command("wmctrl", "-l", "-p")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("wmctrl failed: %w", err)
	}

	windows := make([]*config.WindowInfo, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// wmctrl format: WindowID Desktop PID Hostname Title...
		windowIDStr := fields[0]
		pid, _ := strconv.Atoi(fields[2])
		title := strings.Join(fields[4:], " ")

		// Convert hex window ID to uint32
		windowID, _ := strconv.ParseUint(strings.TrimPrefix(windowIDStr, "0x"), 16, 32)

		info := &config.WindowInfo{
			ID:      uint32(windowID),
			Title:   title,
			PID:     pid,
			Focused: false,
		}

		// Try to get class via xprop (wmctrl doesn't provide it)
		classCmd := exec.Command("xprop", "-id", windowIDStr, "WM_CLASS")
		if classOutput, err := classCmd.Output(); err == nil {
			// Parse: WM_CLASS(STRING) = "instance", "class"
			if parts := strings.Split(string(classOutput), "\""); len(parts) >= 4 {
				info.Class = parts[3]
			}
		}

		if info.Title == "" && info.Class == "" {
			continue
		}

		windows = append(windows, info)
	}

	return windows, nil
}

// GetFocusedWindow returns the currently focused window
func (b *KWinBackend) GetFocusedWindow() (*config.WindowInfo, error) {
	if b.useKdotool {
		return b.getFocusedWindowKdotool()
	}
	return b.getFocusedWindowQdbus()
}

// getFocusedWindowKdotool gets the active window via kdotool
func (b *KWinBackend) getFocusedWindowKdotool() (*config.WindowInfo, error) {
	cmd := exec.Command("kdotool", "getactivewindow")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kdotool getactivewindow failed: %w", err)
	}

	windowID := strings.TrimSpace(string(output))
	if windowID == "" {
		return nil, fmt.Errorf("no active window")
	}

	info, err := b.getWindowInfoKdotool(windowID)
	if err != nil {
		return nil, err
	}

	info.Focused = true
	return info, nil
}

// getActiveWindowUUIDViaQdbus tries to get the active window UUID via qdbus command
func (b *KWinBackend) getActiveWindowUUIDViaQdbus() string {
	// Try qdbus6 first, then qdbus
	qdbusCmd := "qdbus6"
	if _, err := exec.LookPath("qdbus6"); err != nil {
		qdbusCmd = "qdbus"
		if _, err := exec.LookPath("qdbus"); err != nil {
			return ""
		}
	}

	// KDE6: Use KWin scripting to get active window (activeClient method doesn't exist)
	uuid := b.getActiveWindowViaKWinScript(qdbusCmd)
	if uuid != "" {
		return uuid
	}

	// Fallback: Try legacy KDE5 approach with activeClient property
	cmd := exec.Command(qdbusCmd, "org.kde.KWin", "/KWin", "org.kde.KWin.activeClient")
	output, err := cmd.Output()
	if err == nil {
		result := strings.TrimSpace(string(output))
		if strings.Contains(result, "/Window/") {
			parts := strings.Split(result, "/Window/")
			if len(parts) > 1 {
				return parts[1]
			}
		}
		if len(result) > 0 && result != "/" {
			return result
		}
	}

	return ""
}

// getActiveWindowViaKWinScript uses KWin scripting to get the active window UUID
// This is needed for KDE6 which doesn't expose activeClient via D-Bus properties
// Results are cached for 200ms to avoid excessive overhead
func (b *KWinBackend) getActiveWindowViaKWinScript(qdbusCmd string) string {
	log := logger.WithComponent("kwin-backend")

	// Check cache first (valid for 200ms)
	b.uuidMu.RLock()
	if time.Since(b.cachedActiveUUIDTime) < 200*time.Millisecond && b.cachedActiveUUID != "" {
		uuid := b.cachedActiveUUID
		b.uuidMu.RUnlock()
		return uuid
	}
	b.uuidMu.RUnlock()

	// Create a temporary script
	scriptContent := `var win = workspace.activeWindow || workspace.activeClient;
if (win) {
    print("FOCUSSTREAMER_ACTIVE:" + win.internalId);
} else {
    print("FOCUSSTREAMER_ACTIVE:none");
}`

	// Write script to temp file
	scriptPath := "/tmp/focusstreamer_active_probe.js"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
		log.Debug().Err(err).Msg("Failed to write KWin script")
		return ""
	}
	defer os.Remove(scriptPath)

	scriptName := fmt.Sprintf("focusstreamer_probe_%d", time.Now().UnixNano())

	// Load the script
	loadCmd := exec.Command(qdbusCmd, "org.kde.KWin", "/Scripting", "org.kde.kwin.Scripting.loadScript", scriptPath, scriptName)
	if _, err := loadCmd.Output(); err != nil {
		log.Debug().Err(err).Msg("Failed to load KWin script")
		return ""
	}

	// Start the scripting engine
	startCmd := exec.Command(qdbusCmd, "org.kde.KWin", "/Scripting", "org.kde.kwin.Scripting.start")
	startCmd.Run()

	// Give script time to execute (reduced from 100ms)
	time.Sleep(50 * time.Millisecond)

	// Unload the script
	unloadCmd := exec.Command(qdbusCmd, "org.kde.KWin", "/Scripting", "org.kde.kwin.Scripting.unloadScript", scriptName)
	unloadCmd.Run()

	// Read from journal to get the script output
	journalCmd := exec.Command("journalctl", "--user", "-u", "plasma-kwin_wayland.service", "-n", "10", "--no-pager", "-o", "cat")
	journalOutput, err := journalCmd.Output()
	if err != nil {
		// Try alternative: journalctl for kwin_x11 or generic
		journalCmd = exec.Command("journalctl", "--user", "-n", "10", "--no-pager", "-o", "cat")
		journalOutput, _ = journalCmd.Output()
	}

	// Parse the output for our marker
	lines := strings.Split(string(journalOutput), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.Contains(line, "FOCUSSTREAMER_ACTIVE:") {
			parts := strings.Split(line, "FOCUSSTREAMER_ACTIVE:")
			if len(parts) > 1 {
				uuid := strings.TrimSpace(parts[1])
				// Remove braces if present
				uuid = strings.Trim(uuid, "{}")
				if uuid != "" && uuid != "none" {
					// Cache the result
					b.uuidMu.Lock()
					b.cachedActiveUUID = uuid
					b.cachedActiveUUIDTime = time.Now()
					b.uuidMu.Unlock()
					return uuid
				}
			}
		}
	}

	return ""
}

// getX11ActiveWindow returns the X11 window ID of the currently active window
func (b *KWinBackend) getX11ActiveWindow() (uint32, error) {
	if b.x11Conn == nil || b.activeAtom == 0 {
		return 0, fmt.Errorf("X11 not initialized")
	}

	// Get _NET_ACTIVE_WINDOW property from root window
	reply, err := xproto.GetProperty(b.x11Conn, false, b.x11Root, b.activeAtom, xproto.AtomWindow, 0, 1).Reply()
	if err != nil {
		return 0, fmt.Errorf("failed to get _NET_ACTIVE_WINDOW: %w", err)
	}

	if reply.ValueLen == 0 {
		return 0, fmt.Errorf("no active window")
	}

	// Parse the window ID (32-bit value)
	windowID := uint32(reply.Value[0]) | uint32(reply.Value[1])<<8 | uint32(reply.Value[2])<<16 | uint32(reply.Value[3])<<24
	return windowID, nil
}

// getFocusedWindowQdbus gets the active window via qdbus/D-Bus
func (b *KWinBackend) getFocusedWindowQdbus() (*config.WindowInfo, error) {
	// First, try X11 active window detection (works for XWayland apps)
	x11ActiveID, err := b.getX11ActiveWindow()
	if err == nil && x11ActiveID > 0 && x11ActiveID != 0x200000 {
		// x11ActiveID 0x200000 is a placeholder on Wayland, ignore it

		// Get all windows and find one matching this X11 ID
		windows, err := b.listWindowsViaRunner()
		if err == nil {
			for _, win := range windows {
				// Check if this window's ID matches the X11 active window
				if win.ID == x11ActiveID {
					win.Focused = true
					return win, nil
				}
			}
		}

		// Try to get window info directly via X11
		info, err := b.getWindowInfoFromX11(x11ActiveID)
		if err == nil {
			info.Focused = true
			return info, nil
		}
	}

	// Try getting activeWindow path directly from D-Bus (legacy KWin 5)
	obj := b.conn.Object(kwinService, kwinPath)

	propertyNames := []string{
		"activeWindow",
		"activeClient",
	}

	for _, propName := range propertyNames {
		variant, err := obj.GetProperty(kwinInterface + "." + propName)
		if err == nil {
			if path, ok := variant.Value().(dbus.ObjectPath); ok && path != "/" {
				return b.getWindowInfoFromDBus(string(path))
			}
			if s, ok := variant.Value().(string); ok && s != "" && s != "/" {
				return b.getWindowInfoFromDBus(s)
			}
		}
	}

	// Try to find active window from the full window list
	windows, err := b.listWindowsViaRunner()
	if err != nil {
		return nil, fmt.Errorf("failed to list windows: %w", err)
	}

	// Try qdbus to get active window UUID directly (KDE6)
	activeUUID := b.getActiveWindowUUIDViaQdbus()
	if activeUUID != "" {
		for _, win := range windows {
			b.uuidMu.RLock()
			rawID := b.windowUUIDs[win.ID]
			b.uuidMu.RUnlock()

			// Check if this window's UUID matches
			if strings.Contains(rawID, activeUUID) {
				win.Focused = true
				return win, nil
			}
		}
	}

	// Find window marked as active by querying each window's active property
	for _, win := range windows {
		b.uuidMu.RLock()
		rawID := b.windowUUIDs[win.ID]
		b.uuidMu.RUnlock()

		if rawID == "" {
			continue
		}

		// Extract UUID and check if window is active
		if strings.Contains(rawID, "{") && strings.Contains(rawID, "}") {
			start := strings.Index(rawID, "{")
			end := strings.Index(rawID, "}")
			if start >= 0 && end > start {
				uuid := rawID[start+1 : end]
				windowPath := "/org/kde/KWin/Window/" + uuid
				windowObj := b.conn.Object(kwinService, dbus.ObjectPath(windowPath))

				for _, iface := range []string{"org.kde.KWin.Window", "org.kde.KWin.Client"} {
					activeVar, err := windowObj.GetProperty(iface + ".active")
					if err == nil {
						if active, ok := activeVar.Value().(bool); ok && active {
							win.Focused = true
							return win, nil
						}
					}
				}
			}
		}
	}

	// Fallback: return first window (not ideal but better than nothing)
	if len(windows) > 0 {
		windows[0].Focused = true
		return windows[0], nil
	}

	return nil, fmt.Errorf("no active window found")
}

// getWindowInfoFromX11 gets window info directly from X11
func (b *KWinBackend) getWindowInfoFromX11(windowID uint32) (*config.WindowInfo, error) {
	if b.x11Conn == nil {
		return nil, fmt.Errorf("X11 not initialized")
	}

	info := &config.WindowInfo{
		ID:      windowID,
		Focused: false,
	}

	win := xproto.Window(windowID)

	// Get WM_NAME
	wmNameAtom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("WM_NAME")), "WM_NAME").Reply()
	if wmNameAtom != nil {
		nameReply, err := xproto.GetProperty(b.x11Conn, false, win, wmNameAtom.Atom, xproto.AtomString, 0, 256).Reply()
		if err == nil && nameReply.ValueLen > 0 {
			info.Title = string(nameReply.Value)
		}
	}

	// Try _NET_WM_NAME for UTF-8 names
	if info.Title == "" {
		netWmNameAtom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("_NET_WM_NAME")), "_NET_WM_NAME").Reply()
		utf8Atom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("UTF8_STRING")), "UTF8_STRING").Reply()
		if netWmNameAtom != nil && utf8Atom != nil {
			nameReply, err := xproto.GetProperty(b.x11Conn, false, win, netWmNameAtom.Atom, utf8Atom.Atom, 0, 256).Reply()
			if err == nil && nameReply.ValueLen > 0 {
				info.Title = string(nameReply.Value)
			}
		}
	}

	// Get WM_CLASS
	wmClassAtom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("WM_CLASS")), "WM_CLASS").Reply()
	if wmClassAtom != nil {
		classReply, err := xproto.GetProperty(b.x11Conn, false, win, wmClassAtom.Atom, xproto.AtomString, 0, 256).Reply()
		if err == nil && classReply.ValueLen > 0 {
			// WM_CLASS is two null-terminated strings: instance and class
			classData := string(classReply.Value)
			parts := strings.Split(classData, "\x00")
			if len(parts) >= 2 && parts[1] != "" {
				info.Class = strings.ToLower(parts[1])
			} else if len(parts) >= 1 && parts[0] != "" {
				info.Class = strings.ToLower(parts[0])
			}
		}
	}

	// Get _NET_WM_PID
	pidAtom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("_NET_WM_PID")), "_NET_WM_PID").Reply()
	cardinalAtom, _ := xproto.InternAtom(b.x11Conn, false, uint16(len("CARDINAL")), "CARDINAL").Reply()
	if pidAtom != nil && cardinalAtom != nil {
		pidReply, err := xproto.GetProperty(b.x11Conn, false, win, pidAtom.Atom, cardinalAtom.Atom, 0, 1).Reply()
		if err == nil && pidReply.ValueLen > 0 {
			info.PID = int(pidReply.Value[0]) | int(pidReply.Value[1])<<8 | int(pidReply.Value[2])<<16 | int(pidReply.Value[3])<<24
		}
	}

	return info, nil
}

// Note: KWin scripting API (getFocusedWindowViaScript) was removed as it doesn't work on KDE6
// The runScript method no longer exists in org.kde.kwin.Scripting interface

// getWindowInfoFromDBus gets window info from a KWin client D-Bus path
func (b *KWinBackend) getWindowInfoFromDBus(clientPath string) (*config.WindowInfo, error) {
	// clientPath looks like "/org/kde/KWin/Window/<uuid>" or "/org/kde/KWin/Client/<id>"
	obj := b.conn.Object(kwinService, dbus.ObjectPath(clientPath))

	info := &config.WindowInfo{
		ID:      hashStringToUint32(clientPath),
		Focused: false,
	}

	// Try different interface names (KWin5 vs KWin6)
	interfaces := []string{
		"org.kde.KWin.Window",   // KWin6
		"org.kde.KWin.Client",   // KWin5
		"org.kde.kwin.Toplevel", // Older KWin
	}

	for _, iface := range interfaces {
		// Try caption/title
		if info.Title == "" {
			if caption, err := obj.GetProperty(iface + ".caption"); err == nil {
				if s, ok := caption.Value().(string); ok {
					info.Title = s
				}
			}
		}

		// Try resourceClass
		if info.Class == "" {
			if resourceClass, err := obj.GetProperty(iface + ".resourceClass"); err == nil {
				if s, ok := resourceClass.Value().(string); ok {
					info.Class = s
				}
			}
			// Also try resourceName as fallback
			if info.Class == "" {
				if resourceName, err := obj.GetProperty(iface + ".resourceName"); err == nil {
					if s, ok := resourceName.Value().(string); ok {
						info.Class = s
					}
				}
			}
		}

		// Try pid
		if info.PID == 0 {
			if pid, err := obj.GetProperty(iface + ".pid"); err == nil {
				switch p := pid.Value().(type) {
				case int32:
					info.PID = int(p)
				case uint32:
					info.PID = int(p)
				case int64:
					info.PID = int(p)
				}
			}
		}

		// If we got useful info, break
		if info.Title != "" || info.Class != "" {
			break
		}
	}

	// Get window desktop
	info.Desktop = b.getWindowDesktopFromDBus(clientPath)

	return info, nil
}

// WatchFocus starts watching for focus changes
func (b *KWinBackend) WatchFocus(callback func(*config.WindowInfo)) error {
	log := logger.WithComponent("kwin-backend")

	b.mu.Lock()
	if b.watching {
		b.mu.Unlock()
		return fmt.Errorf("already watching")
	}
	b.watching = true
	b.stopChan = make(chan struct{})
	b.desktopChangeChan = make(chan struct{}, 1) // Buffered to avoid blocking signal handler
	b.mu.Unlock()

	// Set up D-Bus signal matching for desktop changes
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface(virtualDesktopManagerInterface),
		dbus.WithMatchMember("currentChanged"),
	); err != nil {
		log.Warn().Err(err).Msg("Failed to add match for VirtualDesktopManager.currentChanged signal")
	} else {
		log.Debug().Msg("Subscribed to VirtualDesktopManager.currentChanged signal")
	}

	// Also match showingDesktopChanged for "Show Desktop" mode
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface(kwinInterface),
		dbus.WithMatchMember("showingDesktopChanged"),
	); err != nil {
		log.Warn().Err(err).Msg("Failed to add match for KWin.showingDesktopChanged signal")
	} else {
		log.Debug().Msg("Subscribed to KWin.showingDesktopChanged signal")
	}

	// Start goroutine to listen for D-Bus signals
	go b.watchDesktopSignals()

	go b.watchFocusLoop(callback)
	return nil
}

// watchDesktopSignals listens for D-Bus signals related to desktop changes
func (b *KWinBackend) watchDesktopSignals() {
	log := logger.WithComponent("kwin-backend")
	signalChan := make(chan *dbus.Signal, 10)
	b.conn.Signal(signalChan)

	for {
		select {
		case <-b.stopChan:
			b.conn.RemoveSignal(signalChan)
			return
		case sig := <-signalChan:
			if sig == nil {
				continue
			}

			// Check if this is a desktop-related signal
			switch sig.Name {
			case virtualDesktopManagerInterface + ".currentChanged":
				log.Debug().Msg("Desktop switched, triggering focus re-evaluation")
				b.triggerDesktopChange()
			case kwinInterface + ".showingDesktopChanged":
				log.Debug().Msg("Show desktop state changed, triggering focus re-evaluation")
				b.triggerDesktopChange()
			}
		}
	}
}

// triggerDesktopChange notifies the focus loop to re-check immediately
func (b *KWinBackend) triggerDesktopChange() {
	select {
	case b.desktopChangeChan <- struct{}{}:
	default:
		// Channel already has a pending notification, skip
	}
}

// watchFocusLoop watches for focus changes via polling and desktop change events
func (b *KWinBackend) watchFocusLoop(callback func(*config.WindowInfo)) {
	log := logger.WithComponent("kwin-backend")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Get initial focus
	if info, err := b.GetFocusedWindow(); err == nil {
		b.mu.Lock()
		b.currentWindow = info
		b.mu.Unlock()
		callback(info)
	}

	checkFocus := func() {
		info, err := b.GetFocusedWindow()
		if err != nil {
			log.Debug().Err(err).Msg("Failed to get focused window")
			return
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

	for {
		select {
		case <-b.stopChan:
			return
		case <-b.desktopChangeChan:
			// Desktop switched - immediate focus re-evaluation
			log.Debug().Msg("Processing desktop change event")
			checkFocus()
		case <-ticker.C:
			// Regular polling
			checkFocus()
		}
	}
}

// StopWatching stops the focus watching loop
func (b *KWinBackend) StopWatching() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.watching {
		close(b.stopChan)
		b.watching = false
	}
}

// GetCurrentDesktop returns the current virtual desktop number
func (b *KWinBackend) GetCurrentDesktop() int {
	// Get current desktop UUID from VirtualDesktopManager
	obj := b.conn.Object(kwinService, virtualDesktopManagerPath)
	currentProp, err := obj.GetProperty(virtualDesktopManagerInterface + ".current")
	if err != nil {
		return 0
	}

	currentUUID, ok := currentProp.Value().(string)
	if !ok || currentUUID == "" {
		return 0
	}

	// Get desktops list to find the index
	return b.desktopUUIDToIndex(currentUUID)
}

// desktopUUIDToIndex converts a desktop UUID to its index number
func (b *KWinBackend) desktopUUIDToIndex(uuid string) int {
	obj := b.conn.Object(kwinService, virtualDesktopManagerPath)
	desktopsProp, err := obj.GetProperty(virtualDesktopManagerInterface + ".desktops")
	if err != nil {
		return 0
	}

	// desktops is a list of (uint32 index, string uuid, string name)
	desktops, ok := desktopsProp.Value().([][]interface{})
	if !ok {
		// Try alternative format: []struct
		if v := desktopsProp.Value(); v != nil {
			// Use reflection to iterate
			switch dv := v.(type) {
			case []interface{}:
				for _, d := range dv {
					if tuple, ok := d.([]interface{}); ok && len(tuple) >= 2 {
						if idx, ok := tuple[0].(uint32); ok {
							if desktopUUID, ok := tuple[1].(string); ok {
								if desktopUUID == uuid {
									return int(idx)
								}
							}
						}
					}
				}
			}
		}
		return 0
	}

	for _, d := range desktops {
		if len(d) >= 2 {
			if idx, ok := d[0].(uint32); ok {
				if desktopUUID, ok := d[1].(string); ok {
					if desktopUUID == uuid {
						return int(idx)
					}
				}
			}
		}
	}

	return 0
}

// getWindowDesktopFromDBus gets the desktop number for a window via D-Bus
func (b *KWinBackend) getWindowDesktopFromDBus(windowPath string) int {
	obj := b.conn.Object(kwinService, dbus.ObjectPath(windowPath))

	// Try different interface names
	interfaces := []string{
		"org.kde.KWin.Window", // KWin6
		"org.kde.KWin.Client", // KWin5
	}

	for _, iface := range interfaces {
		desktopsProp, err := obj.GetProperty(iface + ".desktops")
		if err != nil {
			continue
		}

		// desktops is a list of desktop UUIDs
		switch v := desktopsProp.Value().(type) {
		case []string:
			if len(v) > 0 {
				return b.desktopUUIDToIndex(v[0])
			}
		case []interface{}:
			if len(v) > 0 {
				if uuid, ok := v[0].(string); ok {
					return b.desktopUUIDToIndex(uuid)
				}
			}
		}
	}

	// -1 means on all desktops (sticky) if we can't determine
	return 0
}

// getWindowDesktopFromQueryInfo gets desktop from queryWindowInfo for active window
func (b *KWinBackend) getWindowDesktopFromQueryInfo() int {
	obj := b.conn.Object(kwinService, kwinPath)

	var result map[string]dbus.Variant
	err := obj.Call(kwinInterface+".queryWindowInfo", 0).Store(&result)
	if err != nil {
		return 0
	}

	if desktopsVar, ok := result["desktops"]; ok {
		switch v := desktopsVar.Value().(type) {
		case []string:
			if len(v) > 0 {
				return b.desktopUUIDToIndex(v[0])
			}
		case []interface{}:
			if len(v) > 0 {
				if uuid, ok := v[0].(string); ok {
					return b.desktopUUIDToIndex(uuid)
				}
			}
		}
	}

	return 0
}

// hashStringToUint32 creates a simple hash of a string to uint32
// Used to convert KWin's UUID-style window IDs to numeric IDs
func hashStringToUint32(s string) uint32 {
	var hash uint32 = 5381
	for i := 0; i < len(s); i++ {
		hash = ((hash << 5) + hash) + uint32(s[i])
	}
	return hash
}
