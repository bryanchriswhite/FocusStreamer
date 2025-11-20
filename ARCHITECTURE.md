# FocusStreamer Architecture

## Overview
FocusStreamer is a tool that creates a virtual display for Discord screen sharing, dynamically showing only the currently focused application window based on configurable filters.

## Architecture Components

### 1. Go Backend (`cmd/server/`)
- **HTTP Server**: Serves the React UI and provides REST API endpoints
- **X11 Integration**: Monitors window focus events and retrieves window information
- **Window Manager**: Tracks active windows and applies whitelist filters
- **Virtual Display Manager**: Creates and manages the virtual display output
- **Configuration Manager**: Handles application whitelist and pattern matching

### 2. React Frontend (`web/`)
- **Vite + React**: Modern development setup with hot reload
- **Application List**: Displays currently running applications
- **Whitelist Manager**: UI to add/remove applications from whitelist
- **Pattern Matching**: Configure regex patterns for auto-whitelisting
- **Status Display**: Shows current virtual display state and focused window

### 3. Virtual Display Strategy

#### Phase 1: Window Capture and Composition (Initial Implementation)
- Monitor X11 for focused window changes
- Capture the focused window if it matches whitelist
- Composite the window content onto a designated display area
- Use existing display with a dedicated window/area that can be shared

#### Phase 2: True Virtual Display (Future Enhancement)
- Create actual virtual monitor using X11 RandR or similar
- Render filtered content to this virtual monitor
- More seamless Discord sharing experience

## Technology Stack

### Backend
- **Language**: Go 1.21+
- **X11 Library**: `github.com/BurntSushi/xgb` or `github.com/jezek/xgb`
- **HTTP Framework**: Standard library `net/http` + `gorilla/mux`
- **WebSocket**: `gorilla/websocket` for real-time updates

### Frontend
- **Framework**: React 18
- **Build Tool**: Vite
- **State Management**: React Context API or Zustand
- **HTTP Client**: Fetch API
- **UI Library**: TBD (can use headless UI components)

## API Endpoints

### Application Management
- `GET /api/applications` - List all running applications
- `GET /api/applications/whitelisted` - Get whitelisted applications
- `POST /api/applications/whitelist` - Add application to whitelist
- `DELETE /api/applications/whitelist/:id` - Remove from whitelist

### Window State
- `GET /api/window/current` - Get currently focused window
- `GET /api/window/stream` - WebSocket for real-time window updates

### Configuration
- `GET /api/config` - Get current configuration
- `PUT /api/config` - Update configuration (patterns, settings)

### Virtual Display
- `GET /api/display/status` - Get virtual display status
- `POST /api/display/start` - Start virtual display streaming
- `POST /api/display/stop` - Stop virtual display streaming

## Data Models

```go
type Application struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    WindowClass string `json:"window_class"`
    PID         int    `json:"pid"`
    Whitelisted bool   `json:"whitelisted"`
}

type WindowInfo struct {
    ID          uint32 `json:"id"`
    Title       string `json:"title"`
    Class       string `json:"class"`
    PID         int    `json:"pid"`
    Focused     bool   `json:"focused"`
    Geometry    Rect   `json:"geometry"`
}

type Config struct {
    WhitelistPatterns []string          `json:"whitelist_patterns"`
    WhitelistedApps   map[string]bool   `json:"whitelisted_apps"`
    VirtualDisplay    VirtualDisplayConfig `json:"virtual_display"`
}

type VirtualDisplayConfig struct {
    Width      int  `json:"width"`
    Height     int  `json:"height"`
    RefreshHz  int  `json:"refresh_hz"`
    Enabled    bool `json:"enabled"`
}
```

## Workflow

1. **Startup**:
   - Go server starts and initializes X11 connection
   - Loads configuration from file
   - Starts HTTP server on port 8080
   - Begins monitoring window focus events

2. **User Interaction**:
   - User opens web UI at http://localhost:8080
   - UI displays list of running applications
   - User whitelists desired applications or sets patterns
   - Configuration is saved to disk

3. **Window Filtering**:
   - X11 monitor detects window focus changes
   - Checks if focused window matches whitelist
   - If matched, captures window content
   - Updates virtual display/stream window

4. **Discord Sharing**:
   - User shares the virtual display window in Discord
   - Only whitelisted focused windows appear in the stream
   - Other windows/desktop are hidden from stream

## Development Phases

### Phase 1: Core Backend (Current)
- [x] Project structure
- [ ] Go HTTP server with basic endpoints
- [ ] X11 window detection and monitoring
- [ ] Whitelist management (in-memory)

### Phase 2: Frontend UI
- [ ] React + Vite setup
- [ ] Application list display
- [ ] Whitelist management UI
- [ ] Real-time updates via WebSocket

### Phase 3: Window Capture
- [ ] Capture focused window content
- [ ] Create dedicated output window/area
- [ ] Implement window composition

### Phase 4: Persistence & Configuration
- [ ] Save/load configuration from JSON file
- [ ] Pattern matching implementation
- [ ] User preferences

### Phase 5: Virtual Display (Advanced)
- [ ] True virtual monitor creation
- [ ] Advanced window composition
- [ ] Performance optimization

## Build and Deployment

### Development
```bash
# Backend
cd cmd/server
go run main.go

# Frontend
cd web
npm install
npm run dev
```

### Production
```bash
# Build frontend
cd web
npm run build

# Build and run backend (embeds frontend)
go build -o focusstreamer ./cmd/server
./focusstreamer
```

## Platform Support

**Initial Target**: Linux with X11
**Future**: Wayland support, macOS, Windows

## Dependencies

### System Requirements
- X11 display server
- Linux with X11 libraries installed
- Go 1.21+
- Node.js 18+ (for frontend development)

### Go Dependencies
- X11 bindings (xgb)
- HTTP router (gorilla/mux)
- WebSocket support (gorilla/websocket)
- Image processing (standard library)
