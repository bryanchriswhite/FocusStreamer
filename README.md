# FocusStreamer

A Go-based tool that creates a virtual display for Discord screen sharing, dynamically showing only the currently focused application window based on configurable filters.

## Features

- 🎯 **Focus-Aware Streaming**: Automatically shows only the focused window
- 🔒 **Application Whitelisting**: Control which applications can appear on the stream
- 🎨 **Web UI**: Modern React-based interface for configuration
- ⚡ **Powerful CLI**: Built with Cobra & Viper for comprehensive command-line control
- 🔄 **Real-time Updates**: Live window state via WebSocket
- 📝 **Pattern Matching**: Use regex patterns to auto-whitelist applications
- ⚙️ **Flexible Configuration**: YAML-based config with environment variable support
- 🖥️ **Virtual Display**: Dedicated display area for screen sharing

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

# Start the server
./build/focusstreamer serve
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

3. Configure your whitelist:
   - View running applications
   - Add applications to whitelist
   - Set up pattern matching rules

4. Share in Discord:
   - Start Discord screen share
   - Select the FocusStreamer window
   - Only whitelisted focused windows will appear

### Command Line

FocusStreamer provides a comprehensive CLI for all operations:

```bash
# List all running applications
focusstreamer list

# Add application to whitelist
focusstreamer whitelist add firefox

# Add a pattern to auto-whitelist terminals
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

whitelist_patterns:
  - ".*Terminal.*"
  - ".*Code.*"

whitelisted_apps:
  firefox: true
  chromium: true
  code: true

virtual_display:
  width: 1920
  height: 1080
  refresh_hz: 60
  enabled: true
```

### Managing Configuration

```bash
# View current configuration
focusstreamer config show

# Change server port
focusstreamer config set server_port 9090

# Get specific value
focusstreamer config get log_level

# Show config file location
focusstreamer config path
```

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
