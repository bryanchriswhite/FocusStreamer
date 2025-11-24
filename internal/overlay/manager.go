package overlay

import (
	"fmt"
	"image"
	"sync"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// Manager handles overlay widgets and rendering
type Manager struct {
	widgets map[string]Widget
	mu      sync.RWMutex
	enabled bool
}

// NewManager creates a new overlay manager
func NewManager() *Manager {
	return &Manager{
		widgets: make(map[string]Widget),
		enabled: true,
	}
}

// AddWidget adds a widget to the overlay
func (m *Manager) AddWidget(widget Widget) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.widgets[widget.ID()]; exists {
		return fmt.Errorf("widget with ID %s already exists", widget.ID())
	}

	m.widgets[widget.ID()] = widget
	logger.WithComponent("overlay").Info().Msgf("[Overlay] Added widget: %s (type: %s)", widget.ID(), widget.Type())
	return nil
}

// RemoveWidget removes a widget from the overlay
func (m *Manager) RemoveWidget(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	widget, exists := m.widgets[id]
	if !exists {
		return fmt.Errorf("widget with ID %s not found", id)
	}

	// Clean up widget resources if needed
	if ghWidget, ok := widget.(*GitHubWidget); ok {
		ghWidget.Stop()
	}

	delete(m.widgets, id)
	logger.WithComponent("overlay").Info().Msgf("[Overlay] Removed widget: %s", id)
	return nil
}

// GetWidget retrieves a widget by ID
func (m *Manager) GetWidget(id string) (Widget, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	widget, exists := m.widgets[id]
	return widget, exists
}

// GetAllWidgets returns all widgets
func (m *Manager) GetAllWidgets() []Widget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	widgets := make([]Widget, 0, len(m.widgets))
	for _, widget := range m.widgets {
		widgets = append(widgets, widget)
	}
	return widgets
}

// UpdateWidget updates a widget's configuration
func (m *Manager) UpdateWidget(id string, config map[string]interface{}) error {
	m.mu.RLock()
	widget, exists := m.widgets[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("widget with ID %s not found", id)
	}

	if err := widget.UpdateConfig(config); err != nil {
		return fmt.Errorf("failed to update widget config: %w", err)
	}

	logger.WithComponent("overlay").Info().Msgf("[Overlay] Updated widget: %s", id)
	return nil
}

// SetEnabled enables or disables the entire overlay
func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
	logger.WithComponent("overlay").Info().Msgf("[Overlay] Overlay %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

// IsEnabled returns whether the overlay is enabled
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// Render renders all enabled widgets onto the provided image
func (m *Manager) Render(img *image.RGBA) error {
	if !m.IsEnabled() {
		return nil
	}

	m.mu.RLock()
	widgets := make([]Widget, 0, len(m.widgets))
	for _, widget := range m.widgets {
		widgets = append(widgets, widget)
	}
	m.mu.RUnlock()

	// Render each widget (widgets are rendered in arbitrary order for now)
	// TODO: Add z-index support for layer ordering in Phase 2
	for _, widget := range widgets {
		if widget.IsEnabled() {
			if err := widget.Render(img); err != nil {
				logger.WithComponent("overlay").Info().Msgf("[Overlay] Failed to render widget %s: %v", widget.ID(), err)
			}
		}
	}

	return nil
}

// CreateWidget creates a new widget instance from configuration
func (m *Manager) CreateWidget(widgetType string, id string, config map[string]interface{}) (Widget, error) {
	var widget Widget
	var err error

	switch widgetType {
	case "text":
		widget, err = NewTextWidget(id, config)
	case "github-actions":
		widget, err = NewGitHubWidget(id, config)
	default:
		return nil, fmt.Errorf("unknown widget type: %s", widgetType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s widget: %w", widgetType, err)
	}

	return widget, nil
}

// LoadFromConfig loads widget configurations and creates widget instances
func (m *Manager) LoadFromConfig(configs []map[string]interface{}) error {
	for _, config := range configs {
		widgetType, ok := config["type"].(string)
		if !ok {
			logger.WithComponent("overlay").Info().Msgf("[Overlay] Skipping widget with missing type")
			continue
		}

		id, ok := config["id"].(string)
		if !ok {
			logger.WithComponent("overlay").Info().Msgf("[Overlay] Skipping widget with missing ID")
			continue
		}

		widget, err := m.CreateWidget(widgetType, id, config)
		if err != nil {
			logger.WithComponent("overlay").Info().Msgf("[Overlay] Failed to create widget %s: %v", id, err)
			continue
		}

		if err := m.AddWidget(widget); err != nil {
			logger.WithComponent("overlay").Info().Msgf("[Overlay] Failed to add widget %s: %v", id, err)
		}
	}

	return nil
}

// ExportConfig exports all widget configurations
func (m *Manager) ExportConfig() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := make([]map[string]interface{}, 0, len(m.widgets))
	for _, widget := range m.widgets {
		configs = append(configs, widget.GetConfig())
	}

	return configs
}

// Clear removes all widgets
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clean up resources for each widget
	for _, widget := range m.widgets {
		if ghWidget, ok := widget.(*GitHubWidget); ok {
			ghWidget.Stop()
		}
	}

	m.widgets = make(map[string]Widget)
	logger.WithComponent("overlay").Info().Msgf("[Overlay] Cleared all widgets")
}

// GetAvailableWidgetTypes returns a list of available widget types
func (m *Manager) GetAvailableWidgetTypes() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type":        "text",
			"name":        "Text Label",
			"description": "Display custom text on the overlay",
			"config_schema": map[string]interface{}{
				"text":       "string (required)",
				"x":          "int (position)",
				"y":          "int (position)",
				"opacity":    "float (0.0-1.0)",
				"enabled":    "bool",
				"color":      "object {r, g, b, a}",
				"background": "object {r, g, b, a} (optional)",
				"padding":    "int",
			},
		},
		{
			"type":        "github-actions",
			"name":        "GitHub Actions Status",
			"description": "Display CI/CD status from GitHub Actions",
			"config_schema": map[string]interface{}{
				"owner":         "string (required) - GitHub repo owner",
				"repo":          "string (required) - GitHub repo name",
				"branch":        "string (optional) - Filter by branch",
				"token":         "string (optional) - GitHub token for private repos",
				"x":             "int (position)",
				"y":             "int (position)",
				"opacity":       "float (0.0-1.0)",
				"enabled":       "bool",
				"poll_interval": "int (seconds, default: 60)",
			},
		},
	}
}
