package api

import (
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/bryanchriswhite/FocusStreamer/internal/output"
	"github.com/bryanchriswhite/FocusStreamer/internal/overlay"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Server represents the HTTP API server
type Server struct {
	router     *mux.Router
	windowMgr  *window.Manager
	configMgr  *config.Manager
	mjpegOut   *output.MJPEGOutput
	overlayMgr *overlay.Manager
	upgrader   websocket.Upgrader
}

// NewServer creates a new API server
func NewServer(windowMgr *window.Manager, configMgr *config.Manager, displayMgr interface{}, mjpegOut *output.MJPEGOutput, overlayMgr *overlay.Manager) *Server {
	s := &Server{
		router:     mux.NewRouter(),
		windowMgr:  windowMgr,
		configMgr:  configMgr,
		mjpegOut:   mjpegOut,
		overlayMgr: overlayMgr,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// API routes
	api := s.router.PathPrefix("/api").Subrouter()

	// Application management
	api.HandleFunc("/applications", s.handleGetApplications).Methods("GET")
	api.HandleFunc("/applications/allowlisted", s.handleGetAllowlisted).Methods("GET")
	api.HandleFunc("/applications/allowlist", s.handleAddToAllowlist).Methods("POST")
	api.HandleFunc("/applications/allowlist/{id}", s.handleRemoveFromAllowlist).Methods("DELETE")

	// Window state
	api.HandleFunc("/window/current", s.handleGetCurrentWindow).Methods("GET")
	api.HandleFunc("/window/stream", s.handleWindowStream)
	api.HandleFunc("/window/{id}/screenshot", s.handleGetWindowScreenshot).Methods("GET")

	// Configuration
	api.HandleFunc("/config", s.handleGetConfig).Methods("GET")
	api.HandleFunc("/config", s.handleUpdateConfig).Methods("PUT")
	api.HandleFunc("/config/patterns", s.handleAddPattern).Methods("POST")
	api.HandleFunc("/config/patterns", s.handleRemovePattern).Methods("DELETE")
	api.HandleFunc("/config/placeholder-image", s.handleGetPlaceholder).Methods("GET")
	api.HandleFunc("/config/placeholder-image", s.handleUploadPlaceholder).Methods("POST")
	api.HandleFunc("/config/placeholder-image", s.handleDeletePlaceholder).Methods("DELETE")

	// Overlay management
	api.HandleFunc("/overlay/types", s.handleGetWidgetTypes).Methods("GET")
	api.HandleFunc("/overlay/instances", s.handleGetWidgetInstances).Methods("GET")
	api.HandleFunc("/overlay/instances", s.handleCreateWidget).Methods("POST")
	api.HandleFunc("/overlay/instances/{id}", s.handleUpdateWidget).Methods("PUT")
	api.HandleFunc("/overlay/instances/{id}", s.handleDeleteWidget).Methods("DELETE")
	api.HandleFunc("/overlay/enabled", s.handleSetOverlayEnabled).Methods("PUT")

	// Stream control
	api.HandleFunc("/stream/standby", s.handleGetStandby).Methods("GET")
	api.HandleFunc("/stream/standby", s.handleToggleStandby).Methods("POST")
	api.HandleFunc("/stream/zoom", s.handleGetZoom).Methods("GET")
	api.HandleFunc("/stream/zoom", s.handleSetZoom).Methods("POST")
	api.HandleFunc("/stream/zoom/reset", s.handleResetZoom).Methods("POST")
	api.HandleFunc("/stream/thumbnail", s.handleThumbnail).Methods("GET")

	// Health check
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// MJPEG stream endpoints (if MJPEG output is enabled)
	if s.mjpegOut != nil {
		s.router.HandleFunc("/", s.mjpegOut.GetViewerHandler())          // Clean HTML viewer (root)
		s.router.HandleFunc("/control", s.mjpegOut.GetControlHandler())  // HTML viewer with controls
		s.router.HandleFunc("/stream", s.mjpegOut.GetHTTPHandler())      // Raw MJPEG feed
		s.router.HandleFunc("/stats", s.mjpegOut.GetStatsHandler())
	}

	// Serve static files (React app from web/dist) at /settings
	s.router.PathPrefix("/settings").Handler(s.createSettingsHandler())
}

// createSettingsHandler creates a handler for serving the React settings app at /settings
func (s *Server) createSettingsHandler() http.Handler {
	// Get the web/dist directory path
	webDistPath := filepath.Join("web", "dist")

	// Get absolute path for better debugging
	absPath, _ := filepath.Abs(webDistPath)
	logger.WithComponent("overlay").Info().Msgf("Looking for web UI at: %s", absPath)

	// Check if the directory exists
	if _, err := os.Stat(webDistPath); os.IsNotExist(err) {
		logger.WithComponent("overlay").Info().Msgf("Warning: web/dist directory not found at %s", absPath)
		logger.WithComponent("overlay").Info().Msgf("Serving fallback HTML. To see the React UI, run from project root: cd /path/to/FocusStreamer && ./build/focusstreamer serve")
		return http.HandlerFunc(s.handleFallbackIndex)
	}

	logger.WithComponent("overlay").Info().Msgf("✅ Found web UI build at: %s", absPath)

	// Create file server with /settings prefix stripped
	fileServer := http.StripPrefix("/settings", http.FileServer(http.Dir(webDistPath)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /settings prefix to get the actual file path
		filePath := strings.TrimPrefix(r.URL.Path, "/settings")
		if filePath == "" {
			filePath = "/"
		}

		// Check if the file exists
		path := filepath.Join(webDistPath, filePath)
		if _, err := os.Stat(path); os.IsNotExist(err) && !strings.HasPrefix(filePath, "/assets") {
			// Serve index.html for SPA routing
			http.ServeFile(w, r, filepath.Join(webDistPath, "index.html"))
			return
		}

		// Serve the requested file
		fileServer.ServeHTTP(w, r)
	})
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)
	logger.WithComponent("overlay").Info().Msgf("Starting server on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, s.enableCORS(s.router))
}

// enableCORS adds CORS headers
func (s *Server) enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HTTP Handlers

func (s *Server) handleGetApplications(w http.ResponseWriter, r *http.Request) {
	apps, err := s.windowMgr.GetApplications()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apps)
}

func (s *Server) handleGetAllowlisted(w http.ResponseWriter, r *http.Request) {
	apps, err := s.windowMgr.GetApplications()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter allowlisted apps
	allowlisted := make([]config.Application, 0)
	for _, app := range apps {
		if app.Allowlisted {
			allowlisted = append(allowlisted, app)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allowlisted)
}

func (s *Server) handleAddToAllowlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AppClass string `json:"app_class"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error decoding add allowlist request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Adding '%s' to allowlist", req.AppClass)

	if err := s.configMgr.AddAllowlistedApp(req.AppClass); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error adding to allowlist: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully added '%s' to allowlist", req.AppClass)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleRemoveFromAllowlist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appClass := vars["id"]

	logger.WithComponent("overlay").Info().Msgf("API: Removing '%s' from allowlist", appClass)

	if err := s.configMgr.RemoveAllowlistedApp(appClass); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error removing from allowlist: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully removed '%s' from allowlist", appClass)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleGetCurrentWindow(w http.ResponseWriter, r *http.Request) {
	currentWindow := s.windowMgr.GetCurrentWindow()
	if currentWindow == nil {
		http.Error(w, "No window focused", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentWindow)
}

func (s *Server) handleWindowStream(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.WithComponent("overlay").Info().Msgf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()

	// Subscribe to window changes
	updates := s.windowMgr.Subscribe()
	defer s.windowMgr.Unsubscribe(updates)

	// Send initial window
	if current := s.windowMgr.GetCurrentWindow(); current != nil {
		if err := conn.WriteJSON(current); err != nil {
			logger.WithComponent("overlay").Info().Msgf("WebSocket write error: %v\n", err)
			return
		}
	}

	// Stream updates
	for window := range updates {
		if err := conn.WriteJSON(window); err != nil {
			logger.WithComponent("overlay").Info().Msgf("WebSocket write error: %v\n", err)
			return
		}
	}
}

func (s *Server) handleGetWindowScreenshot(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	windowClass := vars["id"]

	logger.WithComponent("overlay").Info().Msgf("Screenshot requested for window class: %s", windowClass)

	// Find window by class
	window, err := s.windowMgr.FindWindowByClass(windowClass)
	if err != nil {
		logger.WithComponent("overlay").Info().Msgf("Window not found: %v", err)
		http.Error(w, "Window not found", http.StatusNotFound)
		return
	}

	// Capture screenshot using window manager
	pngData, err := s.windowMgr.CaptureWindowScreenshot(window.ID)
	if err != nil {
		logger.WithComponent("overlay").Info().Msgf("Failed to capture screenshot: %v", err)
		http.Error(w, fmt.Sprintf("Failed to capture screenshot: %v", err), http.StatusInternalServerError)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("Successfully captured screenshot for %s (%d bytes)", windowClass, len(pngData))

	// Return PNG image
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(pngData)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.configMgr.Get()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.configMgr.Update(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleAddPattern(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pattern string `json:"pattern"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.configMgr.AddPattern(req.Pattern); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleRemovePattern(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pattern string `json:"pattern"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.configMgr.RemovePattern(req.Pattern); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleGetPlaceholder(w http.ResponseWriter, r *http.Request) {
	cfg := s.configMgr.Get()

	if cfg.PlaceholderImagePath == "" {
		http.Error(w, "No custom placeholder image set", http.StatusNotFound)
		return
	}

	// Check if file exists
	if _, err := os.Stat(cfg.PlaceholderImagePath); os.IsNotExist(err) {
		http.Error(w, "Placeholder image file not found", http.StatusNotFound)
		return
	}

	// Determine content type based on extension
	ext := strings.ToLower(filepath.Ext(cfg.PlaceholderImagePath))
	var contentType string
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	default:
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, cfg.PlaceholderImagePath)
}

func (s *Server) handleUploadPlaceholder(w http.ResponseWriter, r *http.Request) {
	log := logger.WithComponent("api")

	// Parse multipart form (10MB max)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Error().Err(err).Msg("Failed to parse multipart form")
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get the file from form
	file, header, err := r.FormFile("image")
	if err != nil {
		log.Error().Err(err).Msg("Failed to get image from form")
		http.Error(w, "Failed to get image: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		http.Error(w, "Invalid image format. Supported: PNG, JPEG, GIF", http.StatusBadRequest)
		return
	}

	// Determine destination path
	configDir := s.configMgr.GetConfigDir()
	destPath := filepath.Join(configDir, "placeholder"+ext)

	// Remove old placeholder if exists with different extension
	oldPath := s.configMgr.GetPlaceholderImagePath()
	if oldPath != "" && oldPath != destPath {
		os.Remove(oldPath)
	}

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		log.Error().Err(err).Str("path", destPath).Msg("Failed to create placeholder file")
		http.Error(w, "Failed to save image: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer destFile.Close()

	// Copy file content
	if _, err := io.Copy(destFile, file); err != nil {
		log.Error().Err(err).Msg("Failed to copy image data")
		http.Error(w, "Failed to save image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update config with new path
	if err := s.configMgr.SetPlaceholderImage(destPath); err != nil {
		log.Error().Err(err).Msg("Failed to update config")
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Info().Str("path", destPath).Msg("Placeholder image uploaded")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"path":   destPath,
	})
}

func (s *Server) handleDeletePlaceholder(w http.ResponseWriter, r *http.Request) {
	log := logger.WithComponent("api")

	cfg := s.configMgr.Get()

	// Remove file if it exists
	if cfg.PlaceholderImagePath != "" {
		if err := os.Remove(cfg.PlaceholderImagePath); err != nil && !os.IsNotExist(err) {
			log.Error().Err(err).Str("path", cfg.PlaceholderImagePath).Msg("Failed to delete placeholder file")
			http.Error(w, "Failed to delete image: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Clear config
	if err := s.configMgr.ClearPlaceholderImage(); err != nil {
		log.Error().Err(err).Msg("Failed to update config")
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Info().Msg("Placeholder image deleted")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleGetStandby(w http.ResponseWriter, r *http.Request) {
	enabled := s.windowMgr.GetForceStandby()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": enabled,
	})
}

func (s *Server) handleToggleStandby(w http.ResponseWriter, r *http.Request) {
	newState := s.windowMgr.ToggleForceStandby()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": newState,
		"status":  "success",
	})
}

func (s *Server) handleGetZoom(w http.ResponseWriter, r *http.Request) {
	state := s.windowMgr.GetZoomState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handleSetZoom(w http.ResponseWriter, r *http.Request) {
	var req window.ZoomState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	newState := s.windowMgr.SetZoomState(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newState)
}

func (s *Server) handleResetZoom(w http.ResponseWriter, r *http.Request) {
	newState := s.windowMgr.ResetZoom()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newState)
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	thumb := s.windowMgr.GetThumbnail(200) // 200px wide thumbnail
	if thumb == nil {
		http.Error(w, "No frame available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	jpeg.Encode(w, thumb, &jpeg.Options{Quality: 70})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"version": "0.1.0",
	})
}

func (s *Server) handleFallbackIndex(w http.ResponseWriter, r *http.Request) {
	// Fallback HTML page when React build is not available
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FocusStreamer</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            max-width: 800px;
            margin: 50px auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .container {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            margin-top: 0;
        }
        .status {
            padding: 10px;
            background: #e8f5e9;
            border-left: 4px solid #4caf50;
            margin: 20px 0;
        }
        .info {
            color: #666;
            line-height: 1.6;
        }
        a {
            color: #1976d2;
            text-decoration: none;
        }
        a:hover {
            text-decoration: underline;
        }
        code {
            background: #f5f5f5;
            padding: 2px 6px;
            border-radius: 3px;
            font-family: 'Courier New', monospace;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🎯 FocusStreamer</h1>
        <div class="status">
            ✅ Server is running
        </div>
        <div class="info">
            <p>FocusStreamer is a tool for creating virtual displays with focus-aware window streaming.</p>
            <h3>API Endpoints:</h3>
            <ul>
                <li><a href="/api/health">/api/health</a> - Server health check</li>
                <li><a href="/api/applications">/api/applications</a> - List all applications</li>
                <li><a href="/api/config">/api/config</a> - View configuration</li>
                <li><a href="/api/window/current">/api/window/current</a> - Current focused window</li>
            </ul>
            <h3>Coming Soon:</h3>
            <p>React-based web UI for managing allowlisted applications and configuration.</p>
            <p>In the meantime, you can use the API endpoints directly or with tools like <code>curl</code>.</p>
        </div>
    </div>
</body>
</html>`

	// Only serve HTML for root path
	if r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
		return
	}

	// For other paths, return 404
	if !strings.HasPrefix(r.URL.Path, "/api") {
		http.NotFound(w, r)
	}
}

// Overlay API handlers

func (s *Server) handleGetWidgetTypes(w http.ResponseWriter, r *http.Request) {
	types := s.overlayMgr.GetAvailableWidgetTypes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

func (s *Server) handleGetWidgetInstances(w http.ResponseWriter, r *http.Request) {
	widgets := s.overlayMgr.GetAllWidgets()
	instances := make([]map[string]interface{}, 0, len(widgets))
	for _, widget := range widgets {
		instances = append(instances, widget.GetConfig())
	}

	response := map[string]interface{}{
		"enabled":   s.overlayMgr.IsEnabled(),
		"instances": instances,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleCreateWidget(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error decoding create widget request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	widgetType, ok := req["type"].(string)
	if !ok {
		http.Error(w, "missing or invalid 'type' field", http.StatusBadRequest)
		return
	}

	widgetID, ok := req["id"].(string)
	if !ok {
		http.Error(w, "missing or invalid 'id' field", http.StatusBadRequest)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Creating widget: %s (type: %s)", widgetID, widgetType)

	// Create widget
	widget, err := s.overlayMgr.CreateWidget(widgetType, widgetID, req)
	if err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error creating widget: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Add to manager
	if err := s.overlayMgr.AddWidget(widget); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error adding widget: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already added
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully created widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(widget.GetConfig())
}

func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	widgetID := vars["id"]

	var config map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error decoding update widget request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Updating widget: %s", widgetID)

	if err := s.overlayMgr.UpdateWidget(widgetID, config); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error updating widget: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already updated
	}

	// Get updated widget config
	widget, exists := s.overlayMgr.GetWidget(widgetID)
	if !exists {
		http.Error(w, "widget not found after update", http.StatusInternalServerError)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully updated widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(widget.GetConfig())
}

func (s *Server) handleDeleteWidget(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	widgetID := vars["id"]

	logger.WithComponent("overlay").Info().Msgf("API: Deleting widget: %s", widgetID)

	if err := s.overlayMgr.RemoveWidget(widgetID); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error removing widget: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already removed
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully deleted widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleSetOverlayEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error decoding set overlay enabled request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger.WithComponent("overlay").Info().Msgf("API: Setting overlay enabled: %v", req.Enabled)

	s.overlayMgr.SetEnabled(req.Enabled)

	// Update config to persist
	cfg := s.configMgr.Get()
	cfg.Overlay.Enabled = req.Enabled
	if err := s.configMgr.Update(cfg); err != nil {
		logger.WithComponent("overlay").Info().Msgf("Error saving config: %v", err)
		// Don't fail the request, overlay state is already updated
	}

	logger.WithComponent("overlay").Info().Msgf("API: Successfully set overlay enabled: %v", req.Enabled)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": req.Enabled,
		"status":  "success",
	})
}

// saveOverlayConfig saves the current overlay configuration to disk
func (s *Server) saveOverlayConfig() error {
	cfg := s.configMgr.Get()
	cfg.Overlay.Widgets = s.overlayMgr.ExportConfig()
	return s.configMgr.Update(cfg)
}
