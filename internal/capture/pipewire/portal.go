package pipewire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/godbus/dbus/v5"
)

// Portal handles xdg-desktop-portal screen sharing via D-Bus
type Portal struct {
	conn          *dbus.Conn
	sessionHandle dbus.ObjectPath
	nodeID        uint32
	mu            sync.Mutex
	restoreToken  string
	tokenPath     string
}

// Portal D-Bus constants
const (
	portalService   = "org.freedesktop.portal.Desktop"
	portalPath      = "/org/freedesktop/portal/desktop"
	screenCastIface = "org.freedesktop.portal.ScreenCast"
	requestIface    = "org.freedesktop.portal.Request"
)

// Source types for SelectSources
const (
	SourceTypeMonitor = 1 << 0
	SourceTypeWindow  = 1 << 1
	SourceTypeVirtual = 1 << 2
)

// Cursor modes for SelectSources
const (
	CursorModeHidden   = 1 << 0
	CursorModeEmbedded = 1 << 1
	CursorModeMetadata = 1 << 2
)

// Persist modes for SelectSources
const (
	PersistModeNone        = 0
	PersistModeApplication = 1
	PersistModeSession     = 2
)

// NewPortal creates a new portal client
func NewPortal() (*Portal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus: %w", err)
	}

	// Set up token storage path
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.Getenv("HOME")
	}
	tokenPath := filepath.Join(configDir, "focusstreamer", "portal_token")

	p := &Portal{
		conn:      conn,
		tokenPath: tokenPath,
	}

	// Try to load existing restore token
	p.loadRestoreToken()

	return p, nil
}

// Close closes the portal connection
func (p *Portal) Close() error {
	if p.sessionHandle != "" {
		// Close the session
		p.conn.Object(portalService, p.sessionHandle).Call(
			"org.freedesktop.portal.Session.Close", 0,
		)
	}
	return p.conn.Close()
}

// GetNodeID returns the PipeWire node ID for screen capture
func (p *Portal) GetNodeID() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nodeID
}

// StartScreenShare initiates the screen sharing session
func (p *Portal) StartScreenShare() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log := logger.WithComponent("portal")

	// Create session
	sessionHandle, err := p.createSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	p.sessionHandle = sessionHandle
	log.Debug().Str("session", string(sessionHandle)).Msg("Created portal session")

	// Select sources
	err = p.selectSources(sessionHandle)
	if err != nil {
		return fmt.Errorf("failed to select sources: %w", err)
	}
	log.Debug().Msg("Selected sources")

	// Start the session
	nodeID, err := p.start(sessionHandle)
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	p.nodeID = nodeID
	log.Info().Uint32("node_id", nodeID).Msg("Screen sharing started")

	return nil
}

// createSession creates a new portal session
func (p *Portal) createSession() (dbus.ObjectPath, error) {
	log := logger.WithComponent("portal")
	obj := p.conn.Object(portalService, portalPath)

	// Generate a unique token for this request
	token := fmt.Sprintf("focusstreamer%d", os.Getpid())

	options := map[string]dbus.Variant{
		"handle_token":         dbus.MakeVariant(token),
		"session_handle_token": dbus.MakeVariant(fmt.Sprintf("session%d", os.Getpid())),
	}

	// Set up response channel BEFORE making the call
	responseChan := make(chan *dbus.Signal, 10)

	// Add match rule for Response signals
	matchRule := fmt.Sprintf("type='signal',interface='%s',member='Response'", requestIface)
	if err := p.conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		log.Warn().Err(err).Msg("Failed to add match rule")
	}

	p.conn.Signal(responseChan)
	defer p.conn.RemoveSignal(responseChan)

	var requestPath dbus.ObjectPath
	err := obj.Call(screenCastIface+".CreateSession", 0, options).Store(&requestPath)
	if err != nil {
		return "", fmt.Errorf("CreateSession call failed: %w", err)
	}

	log.Info().Str("request_path", string(requestPath)).Msg("Waiting for CreateSession response (portal dialog may appear)")

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			return "", fmt.Errorf("timeout waiting for CreateSession response")
		case sig := <-responseChan:
			log.Debug().
				Str("signal_path", string(sig.Path)).
				Str("signal_name", sig.Name).
				Msg("Received signal")

			if sig.Path == requestPath && sig.Name == requestIface+".Response" {
				if len(sig.Body) < 2 {
					return "", fmt.Errorf("invalid response")
				}

				response := sig.Body[0].(uint32)
				results := sig.Body[1].(map[string]dbus.Variant)

				if response != 0 {
					return "", fmt.Errorf("portal request denied (code %d)", response)
				}

				if sessionHandle, ok := results["session_handle"]; ok {
					// Handle both string and ObjectPath types
					switch v := sessionHandle.Value().(type) {
					case dbus.ObjectPath:
						return v, nil
					case string:
						return dbus.ObjectPath(v), nil
					default:
						return "", fmt.Errorf("unexpected session_handle type: %T", v)
					}
				}
				return "", fmt.Errorf("no session handle in response")
			}
		}
	}
}

// selectSources selects what to share (full screen)
func (p *Portal) selectSources(sessionHandle dbus.ObjectPath) error {
	log := logger.WithComponent("portal")
	obj := p.conn.Object(portalService, portalPath)

	token := fmt.Sprintf("select%d", os.Getpid())

	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"types":        dbus.MakeVariant(uint32(SourceTypeMonitor)), // Capture monitors
		"multiple":     dbus.MakeVariant(false),                     // Single source
		"cursor_mode":  dbus.MakeVariant(uint32(CursorModeEmbedded)), // Embed cursor
		"persist_mode": dbus.MakeVariant(uint32(PersistModeSession)), // Persist permission
	}

	// Add restore token if we have one
	if p.restoreToken != "" {
		options["restore_token"] = dbus.MakeVariant(p.restoreToken)
		log.Debug().Msg("Using saved restore token")
	}

	// Set up response channel BEFORE making the call
	responseChan := make(chan *dbus.Signal, 10)

	matchRule := fmt.Sprintf("type='signal',interface='%s',member='Response'", requestIface)
	if err := p.conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		log.Warn().Err(err).Msg("Failed to add match rule")
	}

	p.conn.Signal(responseChan)
	defer p.conn.RemoveSignal(responseChan)

	var requestPath dbus.ObjectPath
	err := obj.Call(screenCastIface+".SelectSources", 0, sessionHandle, options).Store(&requestPath)
	if err != nil {
		return fmt.Errorf("SelectSources call failed: %w", err)
	}

	log.Info().Str("request_path", string(requestPath)).Msg("Waiting for SelectSources response (select screen in dialog)")

	// Wait for response with timeout (user needs to select screen)
	timeout := time.After(60 * time.Second)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for SelectSources response (user did not select screen)")
		case sig := <-responseChan:
			log.Debug().
				Str("signal_path", string(sig.Path)).
				Str("signal_name", sig.Name).
				Msg("Received signal")

			if sig.Path == requestPath && sig.Name == requestIface+".Response" {
				if len(sig.Body) < 1 {
					return fmt.Errorf("invalid response")
				}

				response := sig.Body[0].(uint32)
				if response != 0 {
					return fmt.Errorf("source selection denied (code %d)", response)
				}

				return nil
			}
		}
	}
}

// start starts the screen capture session
func (p *Portal) start(sessionHandle dbus.ObjectPath) (uint32, error) {
	log := logger.WithComponent("portal")
	obj := p.conn.Object(portalService, portalPath)

	token := fmt.Sprintf("start%d", os.Getpid())

	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
	}

	// Set up response channel BEFORE making the call
	responseChan := make(chan *dbus.Signal, 10)

	matchRule := fmt.Sprintf("type='signal',interface='%s',member='Response'", requestIface)
	if err := p.conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		log.Warn().Err(err).Msg("Failed to add match rule")
	}

	p.conn.Signal(responseChan)
	defer p.conn.RemoveSignal(responseChan)

	// Start with empty parent window
	var requestPath dbus.ObjectPath
	err := obj.Call(screenCastIface+".Start", 0, sessionHandle, "", options).Store(&requestPath)
	if err != nil {
		return 0, fmt.Errorf("Start call failed: %w", err)
	}

	log.Info().Str("request_path", string(requestPath)).Msg("Waiting for Start response")

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			return 0, fmt.Errorf("timeout waiting for Start response")
		case sig := <-responseChan:
			log.Debug().
				Str("signal_path", string(sig.Path)).
				Str("signal_name", sig.Name).
				Msg("Received signal")

			if sig.Path == requestPath && sig.Name == requestIface+".Response" {
				if len(sig.Body) < 2 {
					return 0, fmt.Errorf("invalid response")
				}

				response := sig.Body[0].(uint32)
				results := sig.Body[1].(map[string]dbus.Variant)

				if response != 0 {
					return 0, fmt.Errorf("start denied (code %d)", response)
				}

				// Save restore token for future sessions
				if restoreToken, ok := results["restore_token"]; ok {
					if token, ok := restoreToken.Value().(string); ok {
						p.restoreToken = token
						p.saveRestoreToken()
						log.Debug().Msg("Saved restore token for future sessions")
					}
				}

				// Extract streams
				if streams, ok := results["streams"]; ok {
					// streams is a(ua{sv}) - array of (node_id, properties)
					log.Debug().Interface("streams_type", fmt.Sprintf("%T", streams.Value())).Msg("Parsing streams")

					switch v := streams.Value().(type) {
					case [][]interface{}:
						if len(v) > 0 && len(v[0]) > 0 {
							if nodeID, ok := v[0][0].(uint32); ok {
								return nodeID, nil
							}
						}
					case []interface{}:
						// Sometimes it comes as []interface{} containing structs
						if len(v) > 0 {
							if stream, ok := v[0].([]interface{}); ok && len(stream) > 0 {
								if nodeID, ok := stream[0].(uint32); ok {
									return nodeID, nil
								}
							}
						}
					default:
						log.Warn().Str("type", fmt.Sprintf("%T", v)).Msg("Unknown streams format")
					}
				}

				return 0, fmt.Errorf("no streams in response")
			}
		}
	}
}

// loadRestoreToken loads the restore token from disk
func (p *Portal) loadRestoreToken() {
	data, err := os.ReadFile(p.tokenPath)
	if err != nil {
		return
	}

	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &token); err != nil {
		return
	}
	p.restoreToken = token.Token
}

// saveRestoreToken saves the restore token to disk
func (p *Portal) saveRestoreToken() {
	if p.restoreToken == "" {
		return
	}

	// Create directory if needed
	dir := filepath.Dir(p.tokenPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	token := struct {
		Token string `json:"token"`
	}{Token: p.restoreToken}

	data, err := json.Marshal(token)
	if err != nil {
		return
	}

	os.WriteFile(p.tokenPath, data, 0600)
}
