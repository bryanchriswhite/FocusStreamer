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

// GetViewerHandler returns an HTTP handler that displays a clean stream viewer with subtle hover nav
func (m *MJPEGOutput) GetViewerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FocusStreamer</title>
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
        .stream-container {
            position: relative;
            display: flex;
            justify-content: center;
            align-items: center;
        }
        img {
            width: 100vw;
            height: 100vh;
            object-fit: contain;
            display: block;
            background: #000;
        }
        .nav-trigger {
            position: fixed;
            bottom: 0;
            left: 0;
            width: 100px;
            height: 100px;
            z-index: 900;
        }
        .nav-menu {
            position: fixed;
            bottom: 16px;
            left: 16px;
            display: flex;
            gap: 8px;
            opacity: 0;
            transform: translateY(10px);
            transition: opacity 0.2s ease, transform 0.2s ease;
            pointer-events: none;
            z-index: 1000;
        }
        .nav-trigger:hover ~ .nav-menu,
        .nav-menu:hover {
            opacity: 1;
            transform: translateY(0);
            pointer-events: auto;
        }
        .nav-link {
            display: flex;
            align-items: center;
            gap: 6px;
            padding: 8px 14px;
            background: rgba(40, 40, 40, 0.9);
            color: #ccc;
            text-decoration: none;
            border-radius: 20px;
            font-family: system-ui, -apple-system, sans-serif;
            font-size: 13px;
            transition: background 0.15s ease, color 0.15s ease;
        }
        .nav-link:hover {
            background: rgba(60, 60, 60, 0.95);
            color: #fff;
        }
        .fade-overlay {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: #000;
            opacity: 0;
            transition: opacity 250ms ease;
            pointer-events: none;
            z-index: 500;
        }
        .fade-overlay.active {
            opacity: 1;
        }
    </style>
</head>
<body>
    <div class="fade-overlay" id="fadeOverlay"></div>
    <div class="stream-container">
        <img src="/stream" alt="FocusStreamer Live Stream">
    </div>
    <div class="nav-trigger"></div>
    <div class="nav-menu">
        <a href="/settings" class="nav-link">⚙ Settings</a>
        <a href="/control" class="nav-link">🎛 Control</a>
    </div>
    <script>
        // Listen for standby state changes and trigger fade
        let lastStandbyState = null;

        async function checkStandbyState() {
            try {
                const response = await fetch('/api/stream/standby');
                const data = await response.json();

                if (lastStandbyState !== null && lastStandbyState !== data.enabled) {
                    // State changed - trigger fade
                    const overlay = document.getElementById('fadeOverlay');
                    overlay.classList.add('active');
                    setTimeout(() => {
                        overlay.classList.remove('active');
                    }, 350);
                }
                lastStandbyState = data.enabled;
            } catch (err) {
                console.error('Failed to check standby state:', err);
            }
        }

        // Poll for standby state changes
        setInterval(checkStandbyState, 500);
        checkStandbyState();
    </script>
</body>
</html>`
		w.Write([]byte(html))
	}
}

// GetControlHandler returns an HTTP handler that displays the stream with control UI
func (m *MJPEGOutput) GetControlHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FocusStreamer - Control</title>
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
        .stream-container {
            position: relative;
            display: flex;
            justify-content: center;
            align-items: center;
        }
        img {
            width: 100vw;
            height: 100vh;
            object-fit: contain;
            display: block;
            background: #000;
            user-select: none;
            -webkit-user-drag: none;
        }
        .fade-overlay {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: #000;
            opacity: 0;
            transition: opacity 250ms ease;
            pointer-events: none;
            z-index: 500;
        }
        .fade-overlay.active {
            opacity: 1;
        }
        .fab {
            position: fixed;
            bottom: 24px;
            right: 24px;
            width: 56px;
            height: 56px;
            border-radius: 50%;
            border: none;
            background: rgba(70, 130, 180, 0.9);
            color: white;
            font-size: 24px;
            cursor: pointer;
            box-shadow: 0 4px 12px rgba(0,0,0,0.4);
            transition: all 0.2s ease;
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 1000;
        }
        .fab:hover {
            transform: scale(1.1);
            background: rgba(100, 149, 237, 0.95);
        }
        .fab:active {
            transform: scale(0.95);
        }
        .fab.standby {
            background: rgba(220, 80, 80, 0.9);
        }
        .fab.standby:hover {
            background: rgba(240, 100, 100, 0.95);
        }
        .fab-tooltip {
            position: fixed;
            bottom: 90px;
            right: 24px;
            background: rgba(0,0,0,0.8);
            color: white;
            padding: 8px 12px;
            border-radius: 6px;
            font-size: 14px;
            font-family: system-ui, sans-serif;
            opacity: 0;
            transition: opacity 0.2s ease;
            pointer-events: none;
            white-space: nowrap;
        }
        .fab:hover + .fab-tooltip {
            opacity: 1;
        }
        .cycle-buttons {
            position: fixed;
            bottom: 92px;
            right: 24px;
            display: flex;
            gap: 8px;
            z-index: 1000;
            opacity: 0;
            transition: opacity 0.2s ease;
            pointer-events: none;
        }
        .cycle-buttons.visible {
            opacity: 1;
            pointer-events: auto;
        }
        .cycle-btn {
            width: 36px;
            height: 36px;
            border-radius: 50%;
            border: none;
            background: rgba(60, 60, 60, 0.9);
            color: white;
            font-size: 16px;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: all 0.15s ease;
        }
        .cycle-btn:hover {
            background: rgba(80, 80, 80, 0.95);
            transform: scale(1.1);
        }
        .cycle-btn:active {
            transform: scale(0.95);
        }
        .nav-trigger {
            position: fixed;
            bottom: 0;
            left: 0;
            width: 100px;
            height: 100px;
            z-index: 900;
        }
        .nav-menu {
            position: fixed;
            bottom: 16px;
            left: 16px;
            display: flex;
            gap: 8px;
            opacity: 0;
            transform: translateY(10px);
            transition: opacity 0.2s ease, transform 0.2s ease;
            pointer-events: none;
            z-index: 1000;
        }
        .nav-trigger:hover ~ .nav-menu,
        .nav-menu:hover {
            opacity: 1;
            transform: translateY(0);
            pointer-events: auto;
        }
        .nav-link {
            display: flex;
            align-items: center;
            gap: 6px;
            padding: 8px 14px;
            background: rgba(40, 40, 40, 0.9);
            color: #ccc;
            text-decoration: none;
            border-radius: 20px;
            font-family: system-ui, -apple-system, sans-serif;
            font-size: 13px;
            transition: background 0.15s ease, color 0.15s ease;
        }
        .nav-link:hover {
            background: rgba(60, 60, 60, 0.95);
            color: #fff;
        }
        .minimap {
            position: fixed;
            top: 16px;
            right: 16px;
            width: 180px;
            background: rgba(0, 0, 0, 0.75);
            border: 1px solid rgba(255, 255, 255, 0.2);
            border-radius: 8px;
            padding: 8px;
            z-index: 1000;
            display: none;
            font-family: system-ui, -apple-system, sans-serif;
        }
        .minimap.visible {
            display: block;
        }
        .minimap-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 8px;
            color: #aaa;
            font-size: 11px;
        }
        .minimap-canvas-container {
            position: relative;
            width: 100%;
            background: #111;
            border-radius: 4px;
            overflow: hidden;
        }
        .minimap-canvas {
            width: 100%;
            display: block;
        }
        .minimap-viewport {
            position: absolute;
            border: 2px solid #4CAF50;
            background: rgba(76, 175, 80, 0.15);
            cursor: move;
            box-sizing: border-box;
        }
        .minimap-canvas-container {
            cursor: pointer;
        }
        .zoom-indicator {
            position: fixed;
            top: 16px;
            left: 50%;
            transform: translateX(-50%);
            background: rgba(0, 0, 0, 0.7);
            color: #fff;
            padding: 6px 14px;
            border-radius: 16px;
            font-family: system-ui, -apple-system, sans-serif;
            font-size: 13px;
            z-index: 1000;
            opacity: 0;
            transition: opacity 0.2s ease;
            pointer-events: none;
        }
        .zoom-indicator.visible {
            opacity: 1;
        }
    </style>
</head>
<body>
    <div class="stream-container" id="streamContainer">
        <img id="streamImg" src="/stream" alt="FocusStreamer Live Stream">
    </div>
    <div class="fade-overlay" id="fadeOverlay"></div>
    <div class="cycle-buttons" id="cycleButtons">
        <button class="cycle-btn" onclick="cyclePrev()" title="Previous Image">◀</button>
        <button class="cycle-btn" onclick="cycleNext()" title="Next Image">▶</button>
    </div>
    <button class="fab" id="standbyBtn" onclick="toggleStandby()" title="Toggle Standby">⏸</button>
    <div class="fab-tooltip" id="tooltip">Toggle Standby</div>
    <div class="nav-trigger"></div>
    <div class="nav-menu">
        <a href="/" class="nav-link">📺 Stream</a>
        <a href="/settings" class="nav-link">⚙ Settings</a>
    </div>
    <div class="minimap" id="minimap">
        <div class="minimap-header">
            <span>Zoom: <span id="zoomLevel">1.0</span>x</span>
            <span style="color:#666;cursor:pointer" onclick="resetZoom()">Reset</span>
        </div>
        <div class="minimap-canvas-container">
            <canvas id="minimapCanvas" class="minimap-canvas"></canvas>
            <div id="minimapViewport" class="minimap-viewport"></div>
        </div>
    </div>
    <div class="zoom-indicator" id="zoomIndicator">1.0x</div>
    <script>
        // Standby state
        let isStandby = false;
        let isTransitioning = false;

        // Zoom state
        let zoomState = { scale: 1.0, offsetX: 0.5, offsetY: 0.5 };
        let isDragging = false;
        let dragStart = { x: 0, y: 0 };
        let dragStartOffset = { x: 0, y: 0 };
        let zoomIndicatorTimeout = null;
        let zoomUpdateTimeout = null;
        let pendingZoomUpdate = false;

        // Elements
        const streamContainer = document.getElementById('streamContainer');
        const streamImg = document.getElementById('streamImg');
        const minimap = document.getElementById('minimap');
        const minimapCanvas = document.getElementById('minimapCanvas');
        const minimapViewport = document.getElementById('minimapViewport');
        const zoomIndicator = document.getElementById('zoomIndicator');
        const zoomLevelSpan = document.getElementById('zoomLevel');

        // Initialize
        fetch('/api/stream/standby')
            .then(r => r.json())
            .then(data => {
                isStandby = data.enabled;
                updateButton();
            })
            .catch(console.error);

        fetch('/api/stream/zoom')
            .then(r => r.json())
            .then(data => {
                zoomState = data;
                updateMinimap();
            })
            .catch(console.error);

        // Zoom with mouse wheel (shared handler)
        function handleWheel(e) {
            e.preventDefault();

            const delta = e.deltaY > 0 ? -0.25 : 0.25;
            let newScale = zoomState.scale + delta;
            newScale = Math.max(1.0, Math.min(4.0, newScale));

            if (newScale !== zoomState.scale) {
                // Adjust offset to zoom toward cursor position (only when zooming in on stream)
                if (newScale > zoomState.scale && e.currentTarget === streamContainer) {
                    const rect = streamImg.getBoundingClientRect();
                    const relX = (e.clientX - rect.left) / rect.width;
                    const relY = (e.clientY - rect.top) / rect.height;

                    // Move offset toward cursor
                    const factor = 0.1;
                    zoomState.offsetX += (relX - 0.5) * factor;
                    zoomState.offsetY += (relY - 0.5) * factor;
                }

                zoomState.scale = newScale;
                updateZoom();
            }
        }
        streamContainer.addEventListener('wheel', handleWheel, { passive: false });
        minimap.addEventListener('wheel', handleWheel, { passive: false });

        // Double-click to reset
        streamContainer.addEventListener('dblclick', (e) => {
            e.preventDefault();
            resetZoom();
        });

        // Get minimap canvas container for drag handling
        const minimapContainer = document.querySelector('.minimap-canvas-container');

        // Clamp offset values to valid range based on current scale
        function clampOffset() {
            if (zoomState.scale <= 1.0) {
                zoomState.offsetX = 0.5;
                zoomState.offsetY = 0.5;
                return;
            }
            const viewportSize = 1.0 / zoomState.scale;
            const minOffset = viewportSize / 2;
            const maxOffset = 1.0 - viewportSize / 2;
            zoomState.offsetX = Math.max(minOffset, Math.min(maxOffset, zoomState.offsetX));
            zoomState.offsetY = Math.max(minOffset, Math.min(maxOffset, zoomState.offsetY));
        }

        // Click/drag anywhere on minimap to pan - jump immediately on mousedown and follow mouse
        minimapContainer.addEventListener('mousedown', (e) => {
            if (e.button !== 0) return; // Left click only

            const rect = minimapContainer.getBoundingClientRect();
            const relX = (e.clientX - rect.left) / rect.width;
            const relY = (e.clientY - rect.top) / rect.height;

            // Jump to clicked position immediately
            zoomState.offsetX = relX;
            zoomState.offsetY = relY;
            clampOffset();
            updateZoom();

            // Start dragging from this position
            isDragging = true;
            dragStart = { x: e.clientX, y: e.clientY };
            dragStartOffset = { x: zoomState.offsetX, y: zoomState.offsetY };
            e.preventDefault();
        });

        document.addEventListener('mousemove', (e) => {
            if (!isDragging) return;

            const rect = minimapContainer.getBoundingClientRect();

            const dx = (e.clientX - dragStart.x) / rect.width;
            const dy = (e.clientY - dragStart.y) / rect.height;

            zoomState.offsetX = dragStartOffset.x + dx;
            zoomState.offsetY = dragStartOffset.y + dy;
            clampOffset();

            updateZoom();
        });

        document.addEventListener('mouseup', () => {
            if (isDragging) {
                isDragging = false;
            }
        });

        function updateZoom() {
            // Debounce API calls to prevent overwhelming the server
            pendingZoomUpdate = true;
            updateMinimap(); // Update viewport rectangle immediately
            showZoomIndicator();

            if (zoomUpdateTimeout) return; // Already scheduled

            zoomUpdateTimeout = setTimeout(() => {
                zoomUpdateTimeout = null;
                if (!pendingZoomUpdate) return;
                pendingZoomUpdate = false;

                fetch('/api/stream/zoom', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(zoomState)
                })
                .then(r => r.json())
                .then(data => {
                    zoomState = data;
                    updateMinimap();
                    fetchMinimapThumbnail(); // Fetch unzoomed thumbnail after zoom applied
                })
                .catch(console.error);
            }, 50); // 50ms debounce
        }

        function resetZoom() {
            fetch('/api/stream/zoom/reset', { method: 'POST' })
                .then(r => r.json())
                .then(data => {
                    zoomState = data;
                    updateMinimap();
                    showZoomIndicator();
                })
                .catch(console.error);
        }

        function updateMinimap() {
            const isZoomed = zoomState.scale > 1.0;
            minimap.classList.toggle('visible', isZoomed);
            zoomLevelSpan.textContent = zoomState.scale.toFixed(1);

            if (isZoomed) {
                // Update viewport rectangle position
                const viewportSize = 1.0 / zoomState.scale;
                const vpWidth = viewportSize * 100;
                const vpHeight = viewportSize * 100;
                const vpLeft = (zoomState.offsetX - viewportSize / 2) * 100;
                const vpTop = (zoomState.offsetY - viewportSize / 2) * 100;

                minimapViewport.style.width = vpWidth + '%';
                minimapViewport.style.height = vpHeight + '%';
                minimapViewport.style.left = vpLeft + '%';
                minimapViewport.style.top = vpTop + '%';
            }
        }

        // Fetch unzoomed thumbnail for minimap (separate from updateMinimap to avoid too many requests)
        function fetchMinimapThumbnail() {
            if (zoomState.scale <= 1.0) return;

            const ctx = minimapCanvas.getContext('2d');
            const img = new Image();
            img.onload = () => {
                minimapCanvas.width = img.width;
                minimapCanvas.height = img.height;
                ctx.drawImage(img, 0, 0);
            };
            img.src = '/api/stream/thumbnail?' + Date.now();
        }

        function showZoomIndicator() {
            zoomIndicator.textContent = zoomState.scale.toFixed(1) + 'x';
            zoomIndicator.classList.add('visible');

            clearTimeout(zoomIndicatorTimeout);
            zoomIndicatorTimeout = setTimeout(() => {
                zoomIndicator.classList.remove('visible');
            }, 1000);
        }

        // Periodically fetch unzoomed thumbnail for minimap
        setInterval(fetchMinimapThumbnail, 500);

        function toggleStandby() {
            if (isTransitioning) return;
            isTransitioning = true;

            const overlay = document.getElementById('fadeOverlay');
            overlay.classList.add('active');

            overlay.addEventListener('transitionend', function onFadeIn(e) {
                if (e.propertyName !== 'opacity') return;
                overlay.removeEventListener('transitionend', onFadeIn);

                fetch('/api/stream/standby', { method: 'POST' })
                    .then(r => r.json())
                    .then(data => {
                        isStandby = data.enabled;
                        updateButton();
                        setTimeout(() => {
                            overlay.classList.remove('active');
                            isTransitioning = false;
                        }, 350);
                    })
                    .catch(err => {
                        console.error(err);
                        overlay.classList.remove('active');
                        isTransitioning = false;
                    });
            });
        }

        function updateButton() {
            const btn = document.getElementById('standbyBtn');
            const tooltip = document.getElementById('tooltip');
            const cycleButtons = document.getElementById('cycleButtons');
            if (isStandby) {
                btn.classList.add('standby');
                btn.innerHTML = '⏺';
                tooltip.textContent = 'Resume Stream';
                cycleButtons.classList.add('visible');
            } else {
                btn.classList.remove('standby');
                btn.innerHTML = '⏸';
                tooltip.textContent = 'Show Standby';
                cycleButtons.classList.remove('visible');
            }
        }

        let isCycling = false;

        function cyclePlaceholder(direction) {
            if (isCycling) return;
            isCycling = true;

            const overlay = document.getElementById('fadeOverlay');
            overlay.classList.add('active');

            overlay.addEventListener('transitionend', function onFadeIn(e) {
                if (e.propertyName !== 'opacity') return;
                overlay.removeEventListener('transitionend', onFadeIn);

                const endpoint = direction === 'next' ? '/api/stream/placeholder/next' : '/api/stream/placeholder/prev';
                fetch(endpoint, { method: 'POST' })
                    .then(() => {
                        setTimeout(() => {
                            overlay.classList.remove('active');
                            isCycling = false;
                        }, 350);
                    })
                    .catch(err => {
                        console.error(err);
                        overlay.classList.remove('active');
                        isCycling = false;
                    });
            });
        }

        function cyclePrev() {
            cyclePlaceholder('prev');
        }

        function cycleNext() {
            cyclePlaceholder('next');
        }
    </script>
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
