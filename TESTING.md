# Testing Guide for FocusStreamer

This guide will help you test the FocusStreamer application.

## Prerequisites

Before testing, ensure you have:

- Linux system with X11 (not Wayland)
- Go 1.21+ installed
- Node.js 18+ installed (for frontend)
- X11 development libraries installed

### Installing X11 Libraries

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install libx11-dev libxrandr-dev libxinerama-dev libxcursor-dev libxi-dev
```

**Fedora:**
```bash
sudo dnf install libX11-devel libXrandr-devel libXinerama-devel libXcursor-devel libXi-devel
```

## Quick Start Testing

### Option 1: Backend Only (Minimal Testing)

1. **Install dependencies and build:**
```bash
make install-deps
make build
```

2. **Run the server:**
```bash
./build/focusstreamer
```

3. **Test the API:**
```bash
# Check server health
curl http://localhost:8080/api/health

# List applications
curl http://localhost:8080/api/applications

# Get current window
curl http://localhost:8080/api/window/current

# Get configuration
curl http://localhost:8080/api/config
```

4. **Add an application to allowlist:**
```bash
# Replace "firefox" with an actual window class from /api/applications
curl -X POST http://localhost:8080/api/applications/allowlist \
  -H "Content-Type: application/json" \
  -d '{"app_class":"firefox"}'
```

5. **View in browser:**
Open `http://localhost:8080` to see the basic info page.

### Option 2: Full Stack (Backend + Frontend)

1. **Install all dependencies:**
```bash
# Install Go dependencies
make install-deps

# Install frontend dependencies
cd web
npm install
cd ..
```

2. **Run backend:**
```bash
# Terminal 1
make dev-backend
```

3. **Run frontend (in another terminal):**
```bash
# Terminal 2
cd web
npm run dev
```

4. **Open the web UI:**
Open `http://localhost:3000` in your browser.

## Testing Features

### 1. Window Detection

1. Open several applications (Firefox, Terminal, VSCode, etc.)
2. Check if they appear in:
   - API: `curl http://localhost:8080/api/applications`
   - UI: Applications section

### 2. Focus Tracking

1. Focus different windows by clicking on them
2. Observe the "Current Window" section in the UI
3. Or check via API: `curl http://localhost:8080/api/window/current`

### 3. Allowlisting

**Via UI:**
1. Find an application in the list
2. Click "Add" to allowlist it
3. The button should change to "Remove"
4. The application card should turn green

**Via API:**
```bash
# Add to allowlist
curl -X POST http://localhost:8080/api/applications/allowlist \
  -H "Content-Type: application/json" \
  -d '{"app_class":"gnome-terminal-server"}'

# Remove from allowlist
curl -X DELETE http://localhost:8080/api/applications/allowlist/gnome-terminal-server
```

### 4. Pattern Matching

**Via UI:**
1. Go to "Pattern Matching" section
2. Enter a regex pattern like `.*Terminal.*`
3. Click "Add Pattern"
4. Applications matching the pattern should auto-allowlist

**Via API:**
```bash
# Add pattern
curl -X POST http://localhost:8080/api/config/patterns \
  -H "Content-Type: application/json" \
  -d '{"pattern":".*Code.*"}'

# Remove pattern
curl -X DELETE http://localhost:8080/api/config/patterns \
  -H "Content-Type: application/json" \
  -d '{"pattern":".*Code.*"}'
```

### 5. Configuration Persistence

1. Add some applications to allowlist
2. Add some patterns
3. Stop the server (Ctrl+C)
4. Restart the server
5. Check that configuration persists:
   - Check `~/.config/focusstreamer/config.json`
   - Verify settings in UI or API

## Testing Scenarios

### Scenario 1: Basic Workflow

1. Start FocusStreamer
2. Open the web UI
3. Open Firefox and VSCode
4. Add both to allowlist
5. Focus Firefox - check "Current Window" shows Firefox
6. Focus VSCode - check "Current Window" shows VSCode
7. Focus another app (not allowlisted) - check "Current Window" shows it

### Scenario 2: Pattern Matching

1. Add pattern: `.*firefox.*` (case-insensitive)
2. Open multiple Firefox windows
3. All should auto-allowlist
4. Open Chrome - should not allowlist

### Scenario 3: Configuration Export/Import

1. Configure allowlist and patterns
2. Check `~/.config/focusstreamer/config.json`
3. Copy config to backup
4. Delete config file
5. Restart server - should create default config
6. Restore backup - should use backed up settings

## Troubleshooting

### X11 Connection Issues

```bash
# Check DISPLAY variable
echo $DISPLAY

# Test X11 connection
xdpyinfo

# If running via SSH
ssh -X user@host
```

### Application Not Detected

Some applications may not report proper window classes. Check with:
```bash
xprop | grep WM_CLASS
# Then click on the window
```

### Frontend Not Loading

```bash
# Check if backend is running
curl http://localhost:8080/api/health

# Check if frontend dev server is running
curl http://localhost:3000

# Check for port conflicts
lsof -i :8080
lsof -i :3000
```

### Build Errors

```bash
# Clean and rebuild
make clean
make install-deps
make build
```

## Performance Testing

### Memory Usage

```bash
# Monitor memory
top -p $(pgrep focusstreamer)

# Or use htop
htop -p $(pgrep focusstreamer)
```

### CPU Usage

The application should use minimal CPU when idle. Check with:
```bash
top -p $(pgrep focusstreamer)
```

## Expected Behavior

- ✅ Window focus changes detected within ~500ms
- ✅ UI updates in real-time (polling every 2 seconds)
- ✅ Configuration persists across restarts
- ✅ Pattern matching applies immediately
- ✅ Low CPU usage when idle (<1%)
- ✅ Low memory usage (<50MB for backend)

## Reporting Issues

When reporting issues, include:

1. FocusStreamer version
2. OS and X11 version
3. Steps to reproduce
4. Expected vs actual behavior
5. Relevant logs
6. Output of:
   ```bash
   echo $DISPLAY
   xdpyinfo | head -20
   go version
   node --version
   ```

## Next Steps

After testing the core functionality:

1. Test with Discord screen sharing (manual step)
2. Test with different window managers
3. Test performance with many windows
4. Test pattern edge cases
5. Test error handling (kill X server, etc.)

## Future Testing

As new features are added:

- [ ] Virtual display creation
- [ ] Window composition
- [ ] Video capture
- [ ] Multiple monitor support
- [ ] Wayland support
