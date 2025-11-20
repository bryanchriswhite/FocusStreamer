package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/display"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Server represents the HTTP API server
type Server struct {
	router     *mux.Router
	windowMgr  *window.Manager
	configMgr  *config.Manager
	displayMgr *display.Manager
	upgrader   websocket.Upgrader
}

// NewServer creates a new API server
func NewServer(windowMgr *window.Manager, configMgr *config.Manager, displayMgr *display.Manager) *Server {
	s := &Server{
		router:     mux.NewRouter(),
		windowMgr:  windowMgr,
		configMgr:  configMgr,
		displayMgr: displayMgr,
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

	// Configuration
	api.HandleFunc("/config", s.handleGetConfig).Methods("GET")
	api.HandleFunc("/config", s.handleUpdateConfig).Methods("PUT")
	api.HandleFunc("/config/patterns", s.handleAddPattern).Methods("POST")
	api.HandleFunc("/config/patterns", s.handleRemovePattern).Methods("DELETE")

	// Display management
	api.HandleFunc("/display/status", s.handleDisplayStatus).Methods("GET")

	// Health check
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Serve static files (will be the React app)
	// For now, serve a simple index page
	s.router.PathPrefix("/").HandlerFunc(s.handleIndex)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.configMgr.AddAllowlistedApp(req.AppClass); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleRemoveFromAllowlist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appClass := vars["id"]

	if err := s.configMgr.RemoveAllowlistedApp(appClass); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

func (s *Server) handleDisplayStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"enabled": false,
		"running": false,
		"window_id": 0,
	}

	if s.displayMgr != nil {
		status["enabled"] = true
		status["running"] = s.displayMgr.IsRunning()
		status["window_id"] = s.displayMgr.GetWindowID()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"version": "0.1.0",
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// For now, serve a simple HTML page
	// This will be replaced with the React app build
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
