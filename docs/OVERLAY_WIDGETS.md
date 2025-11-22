# Overlay Widget System

FocusStreamer includes a customizable overlay system that allows you to add widgets on top of your focused window stream, similar to Twitch/OBS overlays.

## Features

- **Extensible widget system** - Easy-to-use plugin architecture
- **Built-in widgets** - Text labels and GitHub Actions status
- **REST API** - Full control via HTTP endpoints
- **Persistent configuration** - Widgets save automatically
- **Alpha blending** - Smooth transparency support
- **Minimal performance impact** - Optimized rendering pipeline

## Built-in Widgets

### Text Label Widget

Display custom text on your overlay with configurable styling.

**Type**: `text`

**Configuration**:
```json
{
  "id": "my-label",
  "type": "text",
  "text": "Hello World",
  "x": 50,
  "y": 50,
  "opacity": 1.0,
  "enabled": true,
  "color": {
    "r": 255,
    "g": 255,
    "b": 255,
    "a": 255
  },
  "background": {
    "r": 0,
    "g": 0,
    "b": 0,
    "a": 180
  },
  "padding": 5
}
```

**Fields**:
- `id` (string, required) - Unique identifier for this widget instance
- `type` (string, required) - Must be `"text"`
- `text` (string, required) - Text to display
- `x` (int) - X position in pixels (default: 0)
- `y` (int) - Y position in pixels (default: 0)
- `opacity` (float) - Widget opacity from 0.0 to 1.0 (default: 1.0)
- `enabled` (bool) - Whether to render the widget (default: true)
- `color` (object) - Text color RGBA (default: white)
- `background` (object, optional) - Background color RGBA
- `padding` (int) - Padding around text in pixels (default: 5)

### GitHub Actions Widget

Display CI/CD status from GitHub Actions workflows.

**Type**: `github-actions`

**Configuration**:
```json
{
  "id": "ci-status",
  "type": "github-actions",
  "owner": "bryanchriswhite",
  "repo": "FocusStreamer",
  "branch": "main",
  "token": "",
  "x": 10,
  "y": 10,
  "opacity": 0.9,
  "enabled": true,
  "poll_interval": 60
}
```

**Fields**:
- `id` (string, required) - Unique identifier
- `type` (string, required) - Must be `"github-actions"`
- `owner` (string, required) - GitHub repository owner
- `repo` (string, required) - GitHub repository name
- `branch` (string, optional) - Filter by specific branch
- `token` (string, optional) - GitHub personal access token (for private repos)
- `x` (int) - X position (default: 0)
- `y` (int) - Y position (default: 0)
- `opacity` (float) - Widget opacity (default: 1.0)
- `enabled` (bool) - Whether to render (default: true)
- `poll_interval` (int) - Update interval in seconds (default: 60)

**Status Display**:
- ✓ Passing (green) - Workflow succeeded
- ✗ Failing (red) - Workflow failed
- ● Running (yellow) - Workflow in progress
- ○ Queued (gray) - Workflow queued
- ○ Cancelled (gray) - Workflow cancelled

## API Reference

### Get Available Widget Types

Get a list of all available widget types and their configuration schemas.

```
GET /api/overlay/types
```

**Response**:
```json
[
  {
    "type": "text",
    "name": "Text Label",
    "description": "Display custom text on the overlay",
    "config_schema": { ... }
  },
  {
    "type": "github-actions",
    "name": "GitHub Actions Status",
    "description": "Display CI/CD status from GitHub Actions",
    "config_schema": { ... }
  }
]
```

### Get Widget Instances

Get all configured widget instances and overlay state.

```
GET /api/overlay/instances
```

**Response**:
```json
{
  "enabled": true,
  "instances": [
    {
      "id": "my-label",
      "type": "text",
      "text": "Hello",
      ...
    }
  ]
}
```

### Create Widget

Add a new widget to the overlay.

```
POST /api/overlay/instances
Content-Type: application/json

{
  "id": "my-widget",
  "type": "text",
  "text": "Hello World",
  "x": 100,
  "y": 100
}
```

**Response**: The created widget configuration

### Update Widget

Update an existing widget's configuration.

```
PUT /api/overlay/instances/{id}
Content-Type: application/json

{
  "text": "Updated text",
  "x": 150,
  "y": 150,
  "enabled": true
}
```

**Response**: The updated widget configuration

### Delete Widget

Remove a widget from the overlay.

```
DELETE /api/overlay/instances/{id}
```

**Response**:
```json
{
  "status": "success"
}
```

### Toggle Overlay

Enable or disable the entire overlay system.

```
PUT /api/overlay/enabled
Content-Type: application/json

{
  "enabled": true
}
```

**Response**:
```json
{
  "enabled": true,
  "status": "success"
}
```

## Configuration File

Widgets are automatically saved to `~/.config/focusstreamer/config.yaml`:

```yaml
overlay:
  enabled: true
  widgets:
    - id: "welcome-message"
      type: "text"
      text: "Welcome to FocusStreamer"
      x: 50
      y: 50
      color:
        r: 255
        g: 255
        b: 255
        a: 255
    - id: "github-ci"
      type: "github-actions"
      owner: "bryanchriswhite"
      repo: "FocusStreamer"
      x: 10
      y: 10
```

## Creating Custom Widgets

You can create custom widgets by implementing the `Widget` interface in Go.

### Widget Interface

```go
type Widget interface {
    // ID returns the unique identifier
    ID() string

    // Type returns the widget type name
    Type() string

    // Render draws the widget onto the provided image
    Render(img *image.RGBA) error

    // GetConfig returns the widget configuration
    GetConfig() map[string]interface{}

    // UpdateConfig updates the widget configuration
    UpdateConfig(config map[string]interface{}) error

    // IsEnabled returns whether the widget should render
    IsEnabled() bool

    // SetEnabled sets whether the widget should render
    SetEnabled(enabled bool)
}
```

### Example: Simple Clock Widget

```go
package overlay

import (
    "image"
    "time"
)

type ClockWidget struct {
    *BaseWidget
    format string
}

func NewClockWidget(id string, config map[string]interface{}) (*ClockWidget, error) {
    w := &ClockWidget{
        BaseWidget: NewBaseWidget(id, 0, 0, 1.0),
        format:     "15:04:05",
    }
    w.UpdateConfig(config)
    return w, nil
}

func (w *ClockWidget) Type() string {
    return "clock"
}

func (w *ClockWidget) Render(img *image.RGBA) error {
    if !w.IsEnabled() {
        return nil
    }

    // Get current time
    timeStr := time.Now().Format(w.format)

    // Render time text at widget position
    // ... implementation ...

    return nil
}

func (w *ClockWidget) GetConfig() map[string]interface{} {
    return map[string]interface{}{
        "id":      w.id,
        "type":    w.Type(),
        "enabled": w.enabled,
        "x":       w.x,
        "y":       w.y,
        "format":  w.format,
    }
}

func (w *ClockWidget) UpdateConfig(config map[string]interface{}) error {
    if format, ok := config["format"].(string); ok {
        w.format = format
    }
    // ... handle other common fields ...
    return nil
}
```

### Registering Custom Widgets

Add your widget to the manager's `CreateWidget` function:

```go
func (m *Manager) CreateWidget(widgetType string, id string, config map[string]interface{}) (Widget, error) {
    switch widgetType {
    case "text":
        return NewTextWidget(id, config)
    case "github-actions":
        return NewGitHubWidget(id, config)
    case "clock":
        return NewClockWidget(id, config)
    default:
        return nil, fmt.Errorf("unknown widget type: %s", widgetType)
    }
}
```

## Usage Examples

### Using curl

**List available widget types:**
```bash
curl http://localhost:8080/api/overlay/types
```

**Create a text label:**
```bash
curl -X POST http://localhost:8080/api/overlay/instances \
  -H "Content-Type: application/json" \
  -d '{
    "id": "status-label",
    "type": "text",
    "text": "Recording",
    "x": 20,
    "y": 20,
    "color": {"r": 255, "g": 0, "b": 0, "a": 255}
  }'
```

**Create a GitHub Actions widget:**
```bash
curl -X POST http://localhost:8080/api/overlay/instances \
  -H "Content-Type: application/json" \
  -d '{
    "id": "ci-badge",
    "type": "github-actions",
    "owner": "your-username",
    "repo": "your-repo",
    "branch": "main",
    "x": 10,
    "y": 10
  }'
```

**Update widget position:**
```bash
curl -X PUT http://localhost:8080/api/overlay/instances/status-label \
  -H "Content-Type: application/json" \
  -d '{
    "x": 100,
    "y": 50
  }'
```

**Delete a widget:**
```bash
curl -X DELETE http://localhost:8080/api/overlay/instances/status-label
```

**Disable overlay:**
```bash
curl -X PUT http://localhost:8080/api/overlay/enabled \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

## Performance Considerations

The overlay system is designed for minimal performance impact:

- **Render on capture**: Overlays are rendered only when frames are captured
- **Alpha blending**: Efficient pixel-level blending for transparency
- **Lazy updates**: GitHub widget polls at configurable intervals (default: 60s)
- **No extra allocations**: Widgets render directly onto frame buffers

Typical performance impact: **<5% FPS reduction** with 2-3 active widgets.

## Future Enhancements (Phase 2+)

- Drag-and-drop positioning UI
- Widget resize handles
- Z-index/layer management
- Application notification widgets
- System stats widgets (CPU/Memory)
- Widget templates and presets
- Custom CSS-like styling
- Animation support
