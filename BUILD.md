# Building FocusStreamer

This guide explains how to build FocusStreamer with its React web UI.

## Quick Start

### Using Make (Recommended)

The easiest way to build everything:

```bash
make build
```

This automatically:
1. Installs npm dependencies
2. Builds the React frontend (`web/dist/`)
3. Builds the Go backend (`build/focusstreamer`)

### Using Docker (Most Reliable)

For a completely isolated and reproducible build:

```bash
# Build the Docker image
make docker-build

# Run the container
make docker-run

# View logs
make docker-logs

# Stop the container
make docker-stop
```

## Build Process Explained

### What Happens During Build

1. **Frontend Build** (`make build-frontend`):
   ```bash
   cd web
   npm install          # Install React dependencies
   npm run build        # Build React app → web/dist/
   ```

2. **Backend Build** (`make build-backend`):
   ```bash
   go build -o build/focusstreamer ./cmd/focusstreamer
   ```

   The Go server expects `web/dist/` to exist and serves it as static files.

### Manual Build (Not Recommended)

If you need to build manually:

```bash
# 1. Build frontend first
cd web
npm install
npm run build
cd ..

# 2. Then build backend
go build -o build/focusstreamer ./cmd/focusstreamer

# 3. Run (must be in project root!)
./build/focusstreamer serve
```

**Important:** Always run from the project root so the server can find `web/dist/`.

## Docker Multi-Stage Build

The Dockerfile uses a 3-stage build:

```dockerfile
Stage 1: Node.js → Build React app
Stage 2: Go      → Build Go binary (includes web/dist from Stage 1)
Stage 3: Alpine  → Final minimal runtime image
```

This ensures:
- ✅ All builds are reproducible
- ✅ No manual npm commands needed
- ✅ Minimal final image size (~50MB)
- ✅ Works anywhere Docker runs

## Why Not Build Frontend at Runtime?

You asked about building via Docker from the Go API. Here's why we don't do that:

### ❌ Runtime Build (Not Recommended)
```go
// Don't do this:
func buildFrontend() {
    exec.Command("docker", "run", "node:18", "npm", "run", "build").Run()
}
```

**Problems:**
- Requires Docker daemon access (security risk)
- Slow (rebuilds every time)
- Resource intensive
- Complex deployment
- Breaks "build once, deploy anywhere"

### ✅ Build-Time Build (Recommended)
```dockerfile
# Do this:
FROM node:18 AS frontend-builder
COPY web/ /app/web/
RUN npm install && npm run build
```

**Benefits:**
- Fast (build once, run many times)
- Secure (no runtime Docker needed)
- Simple deployment
- Standard practice

## Deployment Options

### 1. Docker (Recommended)
```bash
# On your server
git clone https://github.com/youruser/FocusStreamer.git
cd FocusStreamer
make docker-run
```

### 2. Binary Distribution
```bash
# Build once
make build

# Copy binary + web/dist to server
scp -r build/focusstreamer web/dist/ user@server:/app/
ssh user@server 'cd /app && ./focusstreamer serve'
```

### 3. Local Development
```bash
# Terminal 1: Backend
make dev-backend

# Terminal 2: Frontend (with hot reload)
make dev-frontend
```

## Troubleshooting

### "Serving fallback HTML" Message

If you see the basic HTML page instead of React UI:

1. **Check logs** - New version shows:
   ```
   Looking for web UI at: /full/path/web/dist
   ✅ Found web UI build at: /full/path/web/dist
   ```

2. **Run from project root:**
   ```bash
   cd /path/to/FocusStreamer
   ./build/focusstreamer serve
   ```

3. **Verify build exists:**
   ```bash
   ls web/dist/index.html
   ```

### Build Failed

```bash
# Clean everything
make clean

# Try again
make build
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: docker/build-push-action@v4
        with:
          context: .
          push: true
          tags: myrepo/focusstreamer:latest
```

## Summary

**For most users:** Use `make build` or `make docker-build`

**Why this approach:**
- ✅ Automated - no manual npm steps
- ✅ Reliable - reproducible builds
- ✅ Fast - frontend builds once, not every run
- ✅ Standard - follows Go/Docker best practices

**The Go API never builds the frontend at runtime.** The frontend is built once during the application build process, either by Make or Docker.
