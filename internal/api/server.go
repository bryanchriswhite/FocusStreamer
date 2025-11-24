package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
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

	// Overlay management
	api.HandleFunc("/overlay/types", s.handleGetWidgetTypes).Methods("GET")
	api.HandleFunc("/overlay/instances", s.handleGetWidgetInstances).Methods("GET")
	api.HandleFunc("/overlay/instances", s.handleCreateWidget).Methods("POST")
	api.HandleFunc("/overlay/instances/{id}", s.handleUpdateWidget).Methods("PUT")
	api.HandleFunc("/overlay/instances/{id}", s.handleDeleteWidget).Methods("DELETE")
	api.HandleFunc("/overlay/enabled", s.handleSetOverlayEnabled).Methods("PUT")

	// Health check
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// MJPEG stream endpoints (if MJPEG output is enabled)
	if s.mjpegOut != nil {
		s.router.HandleFunc("/view", s.mjpegOut.GetViewerHandler())   // Responsive HTML viewer
		s.router.HandleFunc("/stream", s.mjpegOut.GetHTTPHandler())    // Raw MJPEG feed
		s.router.HandleFunc("/stats", s.mjpegOut.GetStatsHandler())
	}

	// Serve static files (React app from web/dist)
	s.router.PathPrefix("/").Handler(s.createStaticHandler())
}

// createStaticHandler creates a handler for serving static files
func (s *Server) createStaticHandler() http.Handler {
	// Get the web/dist directory path
	webDistPath := filepath.Join("web", "dist")

	// Get absolute path for better debugging
	absPath, _ := filepath.Abs(webDistPath)
	log.Printf("Looking for web UI at: %s", absPath)

	// Check if the directory exists
	if _, err := os.Stat(webDistPath); os.IsNotExist(err) {
		log.Printf("Warning: web/dist directory not found at %s", absPath)
		log.Printf("Serving fallback HTML. To see the React UI, run from project root: cd /path/to/FocusStreamer && ./build/focusstreamer serve")
		return http.HandlerFunc(s.handleFallbackIndex)
	}

	log.Printf("✅ Found web UI build at: %s", absPath)

	// Create file server for the dist directory
	fileServer := http.FileServer(http.Dir(webDistPath))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve static files for API routes or MJPEG stream routes
		if strings.HasPrefix(r.URL.Path, "/api") ||
			strings.HasPrefix(r.URL.Path, "/view") ||
			strings.HasPrefix(r.URL.Path, "/stream") ||
			strings.HasPrefix(r.URL.Path, "/stats") {
			http.NotFound(w, r)
			return
		}

		// For root or any non-asset path, serve index.html (for client-side routing)
		path := filepath.Join(webDistPath, r.URL.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) && !strings.HasPrefix(r.URL.Path, "/assets") {
			// Serve index.html for client-side routing
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
	log.Printf("Starting server on http://localhost%s\n", addr)
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
		log.Printf("Error decoding add allowlist request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("API: Adding '%s' to allowlist", req.AppClass)

	if err := s.configMgr.AddAllowlistedApp(req.AppClass); err != nil {
		log.Printf("Error adding to allowlist: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("API: Successfully added '%s' to allowlist", req.AppClass)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleRemoveFromAllowlist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appClass := vars["id"]

	log.Printf("API: Removing '%s' from allowlist", appClass)

	if err := s.configMgr.RemoveAllowlistedApp(appClass); err != nil {
		log.Printf("Error removing from allowlist: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("API: Successfully removed '%s' from allowlist", appClass)

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
		log.Printf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()

	// Subscribe to window changes
	updates := s.windowMgr.Subscribe()
	defer s.windowMgr.Unsubscribe(updates)

	// Send initial window
	if current := s.windowMgr.GetCurrentWindow(); current != nil {
		if err := conn.WriteJSON(current); err != nil {
			log.Printf("WebSocket write error: %v\n", err)
			return
		}
	}

	// Stream updates
	for window := range updates {
		if err := conn.WriteJSON(window); err != nil {
			log.Printf("WebSocket write error: %v\n", err)
			return
		}
	}
}

func (s *Server) handleGetWindowScreenshot(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	windowClass := vars["id"]

	log.Printf("Screenshot requested for window class: %s", windowClass)

	// Find window by class
	window, err := s.windowMgr.FindWindowByClass(windowClass)
	if err != nil {
		log.Printf("Window not found: %v", err)
		http.Error(w, "Window not found", http.StatusNotFound)
		return
	}

	// Capture screenshot using window manager
	pngData, err := s.windowMgr.CaptureWindowScreenshot(window.ID)
	if err != nil {
		log.Printf("Failed to capture screenshot: %v", err)
		http.Error(w, fmt.Sprintf("Failed to capture screenshot: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully captured screenshot for %s (%d bytes)", windowClass, len(pngData))

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
		log.Printf("Error decoding create widget request: %v", err)
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

	log.Printf("API: Creating widget: %s (type: %s)", widgetID, widgetType)

	// Create widget
	widget, err := s.overlayMgr.CreateWidget(widgetType, widgetID, req)
	if err != nil {
		log.Printf("Error creating widget: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Add to manager
	if err := s.overlayMgr.AddWidget(widget); err != nil {
		log.Printf("Error adding widget: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		log.Printf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already added
	}

	log.Printf("API: Successfully created widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(widget.GetConfig())
}

func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	widgetID := vars["id"]

	var config map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		log.Printf("Error decoding update widget request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("API: Updating widget: %s", widgetID)

	if err := s.overlayMgr.UpdateWidget(widgetID, config); err != nil {
		log.Printf("Error updating widget: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		log.Printf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already updated
	}

	// Get updated widget config
	widget, exists := s.overlayMgr.GetWidget(widgetID)
	if !exists {
		http.Error(w, "widget not found after update", http.StatusInternalServerError)
		return
	}

	log.Printf("API: Successfully updated widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(widget.GetConfig())
}

func (s *Server) handleDeleteWidget(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	widgetID := vars["id"]

	log.Printf("API: Deleting widget: %s", widgetID)

	if err := s.overlayMgr.RemoveWidget(widgetID); err != nil {
		log.Printf("Error removing widget: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Update config to persist
	if err := s.saveOverlayConfig(); err != nil {
		log.Printf("Error saving overlay config: %v", err)
		// Don't fail the request, widget is already removed
	}

	log.Printf("API: Successfully deleted widget: %s", widgetID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleSetOverlayEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding set overlay enabled request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("API: Setting overlay enabled: %v", req.Enabled)

	s.overlayMgr.SetEnabled(req.Enabled)

	// Update config to persist
	cfg := s.configMgr.Get()
	cfg.Overlay.Enabled = req.Enabled
	if err := s.configMgr.Update(cfg); err != nil {
		log.Printf("Error saving config: %v", err)
		// Don't fail the request, overlay state is already updated
	}

	log.Printf("API: Successfully set overlay enabled: %v", req.Enabled)

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
