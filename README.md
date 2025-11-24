# FocusStreamer

A Go-based tool that creates a virtual display for Discord screen sharing, dynamically showing only the currently focused application window based on configurable filters.

## Features

- 🎯 **Focus-Aware Streaming**: Automatically shows only the focused window
- 🔒 **Application Allowlisting**: Control which applications can appear on the stream
- 🎨 **Web UI**: Modern React-based interface for configuration
- ⚡ **Powerful CLI**: Built with Cobra & Viper for comprehensive command-line control
- 🔄 **Real-time Updates**: Live window state via WebSocket
- 📝 **Pattern Matching**: Use regex patterns to auto-allowlist applications
- ⚙️ **Flexible Configuration**: YAML-based config with environment variable support
- 🖥️ **Virtual Display**: Real X11 window that captures and displays allowlisted focused windows
- 🎬 **Window Capture**: Direct X11 window capture with automatic scaling and composition

## Use Cases

- Share only specific applications during Discord streams
- Prevent accidental exposure of sensitive windows
- Create professional streaming setups with selective window sharing
- Focus-based presentation mode

## Quick Start

### Prerequisites

- Linux with X11
- Go 1.21 or later
- Node.js 18+ (for frontend development)
- X11 development libraries

```bash
# Ubuntu/Debian
sudo apt-get install libx11-dev libxrandr-dev libxinerama-dev libxcursor-dev libxi-dev

# Fedora
sudo dnf install libX11-devel libXrandr-devel libXinerama-devel libXcursor-devel libXi-devel
```

### Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/FocusStreamer.git
cd FocusStreamer

# Build the application
make build

# Start the server (creates virtual display window)
./build/focusstreamer serve

# Or disable virtual display (API/web UI only)
./build/focusstreamer serve --no-display
```

### Development

```bash
# Run backend in development mode
make dev-backend

# In another terminal, run frontend in development mode
make dev-frontend

# Or run both concurrently
make dev
```

## Usage

### Web UI

1. Start the server:
   ```bash
   ./build/focusstreamer serve
   # Or with custom port
   ./build/focusstreamer serve --port 9090
   ```

2. Open the web UI:
   ```
   http://localhost:8080
   ```

3. Configure your allowlist:
   - View running applications
   - Add applications to allowlist
   - Set up pattern matching rules

4. Share in Discord:
   - Start Discord screen share
   - Look for "FocusStreamer - Virtual Display" window
   - Select that window to share
   - Only allowlisted focused windows will appear in the shared display
   - The display updates automatically at 10 FPS when you switch windows

### Command Line

FocusStreamer provides a comprehensive CLI for all operations:

```bash
# List all running applications
focusstreamer list

# Add application to allowlist
focusstreamer allowlist add firefox

# Add a pattern to auto-allowlist terminals
focusstreamer pattern add ".*Terminal.*"

# View current focused window
focusstreamer list --current

# Change server port
focusstreamer config set server_port 9090

# View all configuration
focusstreamer config show
```

See [CLI.md](CLI.md) for complete CLI documentation.

## Configuration

Configuration is stored in `~/.config/focusstreamer/config.yaml`:

```yaml
server_port: 8080
log_level: info

allowlist_patterns:
  - ".*Terminal.*"
  - ".*Code.*"

allowlisted_apps:
  firefox: true
  chromium: true
  code: true

virtual_display:
  width: 1920
  height: 1080
  refresh_hz: 60
  fps: 10
  enabled: true
```

### Managing Configuration

```bash
# View current configuration
focusstreamer config show

# Change server port
focusstreamer config set server_port 9090

# Adjust display FPS (1-60)
focusstreamer config set virtual_display.fps 30

# Change display resolution
focusstreamer config set virtual_display.width 2560
focusstreamer config set virtual_display.height 1440

# Get specific value
focusstreamer config get log_level

# Show config file location
focusstreamer config path
```

## How It Works

FocusStreamer creates a real X11 window that acts as your virtual display:

1. **Window Detection**: Monitors all X11 windows and tracks focus changes
2. **Allowlist Filtering**: Only captures windows that match your allowlist or patterns
3. **Window Capture**: Uses X11's GetImage to capture the focused window's content
4. **Smart Scaling**: Automatically scales captured content to fit the display (maintains aspect ratio)
5. **Real-time Updates**: Refreshes at configurable FPS when allowlisted windows are focused
6. **Black Screen**: Shows black when no allowlisted window is focused (protects privacy)

### Virtual Display Features

- **Resolution**: Configurable (default: 1920x1080)
- **Update Rate**: Configurable FPS (default: 10 FPS / 100ms intervals)
  - Low FPS (5-10): Better CPU usage, suitable for most use cases
  - Medium FPS (15-20): Smoother for dynamic content
  - High FPS (25-30): Smoothest, higher CPU usage
- **Scaling**: Automatic aspect-ratio-preserving scaling
- **Centering**: Captured windows are centered in the display
- **Background**: Black background for non-allowlisted windows

## Documentation

- **[CLI.md](CLI.md)** - Complete CLI command reference
- **[ARCHITECTURE.md](ARCHITECTURE.md)** - Architecture and API documentation
- **[TESTING.md](TESTING.md)** - Testing guide
- **[CONTRIBUTING.md](CONTRIBUTING.md)** - Contribution guidelines

## Architecture

FocusStreamer consists of:

- **Go Backend**: Handles X11 window management, filtering, and HTTP API
- **React Frontend**: Provides the configuration UI
- **Virtual Display**: Dedicated output for Discord sharing

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed architecture documentation.

## Building from Source

```bash
# Build everything
make build

# Build only backend
make build-backend

# Build only frontend
make build-frontend

# Run tests
make test

# Clean build artifacts
make clean
```

## Platform Support

- ✅ **Linux (X11)**: Full support
- 🚧 **Linux (Wayland)**: Planned
- 🚧 **macOS**: Planned
- 🚧 **Windows**: Planned

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

See [LICENSE.md](LICENSE.md) for details.

## Roadmap

- [x] Basic project structure
- [ ] X11 window detection and monitoring
- [ ] Web UI for application management
- [ ] Window capture and composition
- [ ] Pattern matching
- [ ] Configuration persistence
- [ ] Virtual display creation
- [ ] Wayland support
- [ ] macOS support
- [ ] Windows support

## Troubleshooting

### X11 Connection Issues
```bash
# Ensure DISPLAY is set
echo $DISPLAY

# Test X11 connection
xdpyinfo
```

### Permission Issues
```bash
# Ensure user has access to X11
xhost +local:
```

## Credits

Built with:
- [xgb](https://github.com/BurntSushi/xgb) - X11 bindings for Go
- [React](https://react.dev/) - UI framework
- [Vite](https://vitejs.dev/) - Frontend build tool
- [Gorilla](https://github.com/gorilla) - Go web toolkit
