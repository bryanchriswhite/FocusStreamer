package output

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"sync"
	"time"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// MJPEGOutput streams frames as Motion JPEG over HTTP
// This allows users to open the stream in a browser tab and share that tab in Discord
type MJPEGOutput struct {
	config  Config
	running bool
	mu      sync.RWMutex

	// Current frame buffer
	frameMu      sync.RWMutex
	currentFrame *image.RGBA
	lastUpdate   time.Time

	// Connected clients
	clientsMu sync.RWMutex
	clients   map[chan []byte]struct{}

	// Stats
	frameCount uint64
	startTime  time.Time
}

// NewMJPEGOutput creates a new MJPEG stream output
func NewMJPEGOutput(config Config) *MJPEGOutput {
	return &MJPEGOutput{
		config:  config,
		clients: make(map[chan []byte]struct{}),
	}
}

// Start initializes the MJPEG output
// Note: The HTTP handler is registered separately via GetHTTPHandler()
func (m *MJPEGOutput) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("MJPEG output already running")
	}

	m.running = true
	m.startTime = time.Now()
	m.frameCount = 0

	logger.WithComponent("overlay").Info().Msgf("[MJPEG] Output started: %dx%d @ %d FPS", m.config.Width, m.config.Height, m.config.FPS)
	return nil
}

// Stop cleanly shuts down the output
func (m *MJPEGOutput) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false

	// Close all client connections
	m.clientsMu.Lock()
	for ch := range m.clients {
		close(ch)
	}
	m.clients = make(map[chan []byte]struct{})
	m.clientsMu.Unlock()

	logger.WithComponent("overlay").Info().Msgf("[MJPEG] Output stopped after %v frames", m.frameCount)
	return nil
}

// WriteFrame sends a frame to all connected clients
func (m *MJPEGOutput) WriteFrame(frame *image.RGBA) error {
	if !m.IsRunning() {
		return fmt.Errorf("MJPEG output not running")
	}

	// Encode frame as JPEG
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, frame, &jpeg.Options{Quality: 90}); err != nil {
		return fmt.Errorf("failed to encode JPEG: %w", err)
	}

	jpegData := buf.Bytes()

	// Update current frame
	m.frameMu.Lock()
	m.currentFrame = frame
	m.lastUpdate = time.Now()
	m.frameMu.Unlock()

	m.frameCount++

	// Broadcast to all clients
	m.clientsMu.RLock()
	for ch := range m.clients {
		select {
		case ch <- jpegData:
			// Sent successfully
		default:
			// Client is slow, skip this frame
		}
	}
	m.clientsMu.RUnlock()

	return nil
}

// Name returns the output type name
func (m *MJPEGOutput) Name() string {
	return "MJPEG HTTP Stream"
}

// IsRunning returns true if the output is active
func (m *MJPEGOutput) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetHTTPHandler returns an http.Handler for the MJPEG stream
// Mount this at /stream or similar endpoint
func (m *MJPEGOutput) GetHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set headers for MJPEG stream
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("Connection", "close")

		// Create channel for this client
		frameChan := make(chan []byte, 2) // Buffer 2 frames

		// Register client
		m.clientsMu.Lock()
		m.clients[frameChan] = struct{}{}
		clientCount := len(m.clients)
		m.clientsMu.Unlock()

		logger.WithComponent("overlay").Info().Msgf("[MJPEG] New client connected (total: %d)", clientCount)

		// Cleanup on disconnect
		defer func() {
			m.clientsMu.Lock()
			delete(m.clients, frameChan)
			clientCount := len(m.clients)
			m.clientsMu.Unlock()
			logger.WithComponent("overlay").Info().Msgf("[MJPEG] Client disconnected (remaining: %d)", clientCount)
		}()

		// Stream frames to client
		for jpegData := range frameChan {
			// Write multipart boundary
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(jpegData)); err != nil {
				return
			}

			// Write JPEG data
			if _, err := w.Write(jpegData); err != nil {
				return
			}

			// Write closing boundary
			if _, err := fmt.Fprintf(w, "\r\n"); err != nil {
				return
			}

			// Flush to client
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// GetViewerHandler returns an HTTP handler that displays the stream in a responsive HTML page
func (m *MJPEGOutput) GetViewerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FocusStreamer - Live Stream</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            background: #000;
            overflow: hidden;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
        }
        img {
            max-width: 100vw;
            max-height: 100vh;
            width: auto;
            height: auto;
            object-fit: contain;
            display: block;
        }
    </style>
</head>
<body>
    <img src="/stream" alt="FocusStreamer Live Stream">
</body>
</html>`
		w.Write([]byte(html))
	}
}

// GetStatsHandler returns an HTTP handler that shows stream statistics
func (m *MJPEGOutput) GetStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		running := m.running
		frameCount := m.frameCount
		startTime := m.startTime
		m.mu.RUnlock()

		m.frameMu.RLock()
		lastUpdate := m.lastUpdate
		m.frameMu.RUnlock()

		m.clientsMu.RLock()
		clientCount := len(m.clients)
		m.clientsMu.RUnlock()

		var fps float64
		if running && !startTime.IsZero() {
			elapsed := time.Since(startTime).Seconds()
			if elapsed > 0 {
				fps = float64(frameCount) / elapsed
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>FocusStreamer - MJPEG Stats</title>
    <style>
        body { font-family: monospace; padding: 20px; background: #1e1e1e; color: #d4d4d4; }
        .stat { margin: 10px 0; }
        .label { color: #569cd6; }
        .value { color: #4ec9b0; }
        .status-running { color: #4ec9b0; }
        .status-stopped { color: #ce9178; }
    </style>
</head>
<body>
    <h1>FocusStreamer MJPEG Stream Stats</h1>
    <div class="stat">
        <span class="label">Status:</span>
        <span class="value %s">%s</span>
    </div>
    <div class="stat">
        <span class="label">Resolution:</span>
        <span class="value">%dx%d @ %d FPS (target)</span>
    </div>
    <div class="stat">
        <span class="label">Actual FPS:</span>
        <span class="value">%.2f</span>
    </div>
    <div class="stat">
        <span class="label">Total Frames:</span>
        <span class="value">%d</span>
    </div>
    <div class="stat">
        <span class="label">Connected Clients:</span>
        <span class="value">%d</span>
    </div>
    <div class="stat">
        <span class="label">Last Update:</span>
        <span class="value">%s</span>
    </div>
    <div class="stat">
        <span class="label">Uptime:</span>
        <span class="value">%s</span>
    </div>
    <p><a href="/stream" style="color: #569cd6;">View Stream</a></p>
</body>
</html>`,
			func() string {
				if running {
					return "status-running"
				}
				return "status-stopped"
			}(),
			func() string {
				if running {
					return "Running"
				}
				return "Stopped"
			}(),
			m.config.Width, m.config.Height, m.config.FPS,
			fps,
			frameCount,
			clientCount,
			func() string {
				if lastUpdate.IsZero() {
					return "Never"
				}
				return time.Since(lastUpdate).Round(time.Millisecond).String() + " ago"
			}(),
			func() string {
				if startTime.IsZero() {
					return "N/A"
				}
				return time.Since(startTime).Round(time.Second).String()
			}(),
		)
	}
}
