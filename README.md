# FocusStreamer

A Go-based tool that creates a virtual display for Discord screen sharing, dynamically showing only the currently focused application window based on configurable filters.

## Features

- 🎯 **Focus-Aware Streaming**: Automatically shows only the focused window
- 🔒 **Application Whitelisting**: Control which applications can appear on the stream
- 🎨 **Web UI**: Modern React-based interface for configuration
- 🔄 **Real-time Updates**: Live window state via WebSocket
- 📝 **Pattern Matching**: Use regex patterns to auto-whitelist applications
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

# Run the application
./focusstreamer
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

1. Start FocusStreamer:
   ```bash
   ./focusstreamer
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

## Configuration

Configuration is stored in `~/.config/focusstreamer/config.json`:

```json
{
  "whitelist_patterns": [
    ".*Terminal.*",
    ".*Code.*"
  ],
  "whitelisted_apps": {
    "firefox": true,
    "chromium": true
  },
  "virtual_display": {
    "width": 1920,
    "height": 1080,
    "refresh_hz": 60,
    "enabled": true
  }
}
```

## API Reference

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed API documentation.

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
