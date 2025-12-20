package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"gopkg.in/yaml.v3"
)

// AllowlistSource indicates how an application was allowlisted
type AllowlistSource string

const (
	AllowlistSourceNone     AllowlistSource = ""        // Not allowlisted
	AllowlistSourceExplicit AllowlistSource = "explicit" // Explicitly added to allowlist
	AllowlistSourcePattern  AllowlistSource = "pattern"  // Matched by a pattern
)

// Application represents a running application
type Application struct {
	ID              string          `json:"id" mapstructure:"id"`
	Name            string          `json:"name" mapstructure:"name"`
	WindowClass     string          `json:"window_class" mapstructure:"window_class"`
	PID             int             `json:"pid" mapstructure:"pid"`
	Allowlisted     bool            `json:"allowlisted" mapstructure:"allowlisted"`
	AllowlistSource AllowlistSource `json:"allowlist_source" mapstructure:"allowlist_source"`
}

// WindowInfo represents information about a window
type WindowInfo struct {
	ID              uint32   `json:"id" mapstructure:"id"`
	Title           string   `json:"title" mapstructure:"title"`
	Class           string   `json:"class" mapstructure:"class"`
	PID             int      `json:"pid" mapstructure:"pid"`
	Focused         bool     `json:"focused" mapstructure:"focused"`
	Geometry        Geometry `json:"geometry" mapstructure:"geometry"`
	IsNativeWayland bool     `json:"is_native_wayland" mapstructure:"is_native_wayland"` // True for native Wayland windows (no X11 ID)
	Desktop         int      `json:"desktop" mapstructure:"desktop"`                     // Virtual desktop number (-1 means all desktops/sticky)
}

// Geometry represents window geometry
type Geometry struct {
	X      int `json:"x" mapstructure:"x"`
	Y      int `json:"y" mapstructure:"y"`
	Width  int `json:"width" mapstructure:"width"`
	Height int `json:"height" mapstructure:"height"`
}

// Config represents the application configuration
type Config struct {
	AllowlistPatterns      []string      `json:"allowlist_patterns" yaml:"allowlist_patterns"`
	AllowlistTitlePatterns []string      `json:"allowlist_title_patterns" yaml:"allowlist_title_patterns"`
	AllowlistedApps        []string      `json:"allowed_apps" yaml:"allowed_apps"`
	VirtualDisplay         DisplayConfig `json:"virtual_display" yaml:"virtual_display"`
	Overlay                OverlayConfig `json:"overlay" yaml:"overlay"`
	ServerPort             int           `json:"server_port" yaml:"server_port"`
	LogLevel               string        `json:"log_level" yaml:"log_level"`
	PlaceholderImagePath   string        `json:"placeholder_image_path" yaml:"placeholder_image_path"`
}

// OverlayConfig represents overlay configuration
type OverlayConfig struct {
	Enabled bool                     `json:"enabled" yaml:"enabled"`
	Widgets []map[string]interface{} `json:"widgets" yaml:"widgets"`
}

// DisplayConfig represents virtual display configuration
type DisplayConfig struct {
	Width     int  `json:"width" yaml:"width"`
	Height    int  `json:"height" yaml:"height"`
	RefreshHz int  `json:"refresh_hz" yaml:"refresh_hz"`
	FPS       int  `json:"fps" yaml:"fps"`
	Enabled   bool `json:"enabled" yaml:"enabled"`
}

// Manager handles configuration
type Manager struct {
	configPath string
	config     *Config
	mu         sync.RWMutex
}

// NewManager creates a new configuration manager
func NewManager(configFile string) (*Manager, error) {
	// Set default configuration path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "focusstreamer")
	defaultConfigPath := filepath.Join(configDir, "config.yaml")

	// Use provided config file or default
	actualConfigPath := defaultConfigPath
	if configFile != "" {
		actualConfigPath = configFile
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	m := &Manager{
		configPath: actualConfigPath,
	}

	// Try to read config file
	if err := m.load(); err != nil {
		if os.IsNotExist(err) {
			// Config file not found, create it with defaults
			logger.WithComponent("config").Info().
				Str("path", m.configPath).
				Msg("Config file not found, creating new config")
			m.config = m.getDefaults()
			if err := m.Save(); err != nil {
				return nil, fmt.Errorf("failed to create default config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	logger.WithComponent("config").Info().
		Str("path", m.configPath).
		Int("allowed_apps", len(m.config.AllowlistedApps)).
		Msg("Config loaded")

	return m, nil
}

// getDefaults returns default configuration
func (m *Manager) getDefaults() *Config {
	return &Config{
		ServerPort:             8080,
		LogLevel:               "info",
		AllowlistPatterns:      []string{},
		AllowlistTitlePatterns: []string{},
		AllowlistedApps:        []string{},
		VirtualDisplay: DisplayConfig{
			Width:     1920,
			Height:    1080,
			RefreshHz: 60,
			FPS:       10,
			Enabled:   true,
		},
		Overlay: OverlayConfig{
			Enabled: true,
			Widgets: []map[string]interface{}{},
		},
	}
}

// load reads the configuration from disk
func (m *Manager) load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Initialize slices if nil
	if cfg.AllowlistedApps == nil {
		cfg.AllowlistedApps = []string{}
	}
	if cfg.AllowlistPatterns == nil {
		cfg.AllowlistPatterns = []string{}
	}
	if cfg.AllowlistTitlePatterns == nil {
		cfg.AllowlistTitlePatterns = []string{}
	}
	if cfg.Overlay.Widgets == nil {
		cfg.Overlay.Widgets = []map[string]interface{}{}
	}

	m.mu.Lock()
	m.config = &cfg
	m.mu.Unlock()

	return nil
}

// Get returns the current configuration
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config == nil {
		return m.getDefaults()
	}

	// Return a copy to prevent external modification
	cfg := *m.config
	return &cfg
}

// Save saves the current configuration to disk
func (m *Manager) Save() error {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	if cfg == nil {
		cfg = m.getDefaults()
	}

	logger.WithComponent("config").Debug().
		Str("path", m.configPath).
		Int("allowlisted_count", len(cfg.AllowlistedApps)).
		Interface("allowed_apps", cfg.AllowlistedApps).
		Int("pattern_count", len(cfg.AllowlistPatterns)).
		Interface("patterns", cfg.AllowlistPatterns).
		Msg("Saving config")

	// Ensure the directory exists
	configDir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		logger.WithComponent("config").Error().
			Err(err).
			Str("config_dir", configDir).
			Msg("Failed to create config directory")
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		logger.WithComponent("config").Error().
			Err(err).
			Msg("Failed to marshal config")
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		logger.WithComponent("config").Error().
			Err(err).
			Str("path", m.configPath).
			Msg("Failed to write config")
		return err
	}

	logger.WithComponent("config").Info().
		Str("path", m.configPath).
		Msg("Config saved successfully")
	return nil
}

// Update updates the entire configuration
func (m *Manager) Update(cfg *Config) error {
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
	return m.Save()
}

// AddAllowlistedApp adds an application to the allowlist
func (m *Manager) AddAllowlistedApp(appClass string) error {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.Lock()
	// Check if already exists (prevent duplicates)
	for _, app := range m.config.AllowlistedApps {
		if app == normalized {
			m.mu.Unlock()
			logger.WithComponent("config").Debug().
				Str("app_class", appClass).
				Msg("App already in allowlist, skipping")
			return nil
		}
	}

	// Append new app
	m.config.AllowlistedApps = append(m.config.AllowlistedApps, normalized)
	totalCount := len(m.config.AllowlistedApps)
	m.mu.Unlock()

	if err := m.Save(); err != nil {
		logger.WithComponent("config").Error().
			Err(err).
			Str("app_class", appClass).
			Msg("Failed to save config after adding app")
		return err
	}

	logger.WithComponent("config").Info().
		Str("app_class", appClass).
		Int("total_count", totalCount).
		Msg("Successfully added app to allowlist")
	return nil
}

// RemoveAllowlistedApp removes an application from the allowlist
func (m *Manager) RemoveAllowlistedApp(appClass string) error {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.Lock()
	// Filter out the app to remove
	filtered := make([]string, 0, len(m.config.AllowlistedApps))
	for _, app := range m.config.AllowlistedApps {
		if app != normalized {
			filtered = append(filtered, app)
		}
	}
	m.config.AllowlistedApps = filtered
	totalCount := len(filtered)
	m.mu.Unlock()

	if err := m.Save(); err != nil {
		return err
	}

	logger.WithComponent("config").Info().
		Str("app_class", appClass).
		Int("total_count", totalCount).
		Msg("Removed app from allowlist")
	return nil
}

// IsAllowlisted checks if an application is allowlisted
func (m *Manager) IsAllowlisted(appClass string) bool {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, app := range m.config.AllowlistedApps {
		if app == normalized {
			return true
		}
	}
	return false
}

// AddPattern adds an allowlist pattern
func (m *Manager) AddPattern(pattern string) error {
	m.mu.Lock()
	m.config.AllowlistPatterns = append(m.config.AllowlistPatterns, pattern)
	m.mu.Unlock()
	return m.Save()
}

// RemovePattern removes an allowlist pattern
func (m *Manager) RemovePattern(pattern string) error {
	m.mu.Lock()
	for i, p := range m.config.AllowlistPatterns {
		if p == pattern {
			m.config.AllowlistPatterns = append(m.config.AllowlistPatterns[:i], m.config.AllowlistPatterns[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return m.Save()
}

// AddTitlePattern adds a title-only allowlist pattern
func (m *Manager) AddTitlePattern(pattern string) error {
	m.mu.Lock()
	// Check for duplicates
	for _, p := range m.config.AllowlistTitlePatterns {
		if p == pattern {
			m.mu.Unlock()
			return nil // Already exists
		}
	}
	m.config.AllowlistTitlePatterns = append(m.config.AllowlistTitlePatterns, pattern)
	m.mu.Unlock()
	return m.Save()
}

// RemoveTitlePattern removes a title-only allowlist pattern
func (m *Manager) RemoveTitlePattern(pattern string) error {
	m.mu.Lock()
	for i, p := range m.config.AllowlistTitlePatterns {
		if p == pattern {
			m.config.AllowlistTitlePatterns = append(m.config.AllowlistTitlePatterns[:i], m.config.AllowlistTitlePatterns[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return m.Save()
}

// SetPlaceholderImage sets the custom placeholder image path
func (m *Manager) SetPlaceholderImage(path string) error {
	m.mu.Lock()
	m.config.PlaceholderImagePath = path
	m.mu.Unlock()
	return m.Save()
}

// ClearPlaceholderImage clears the custom placeholder image path
func (m *Manager) ClearPlaceholderImage() error {
	m.mu.Lock()
	m.config.PlaceholderImagePath = ""
	m.mu.Unlock()
	return m.Save()
}

// GetPlaceholderImagePath returns the custom placeholder image path
func (m *Manager) GetPlaceholderImagePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.PlaceholderImagePath
}

// SetPort sets the server port
func (m *Manager) SetPort(port int) error {
	m.mu.Lock()
	m.config.ServerPort = port
	m.mu.Unlock()
	return m.Save()
}

// GetPort gets the server port
func (m *Manager) GetPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.ServerPort
}

// SetLogLevel sets the log level
func (m *Manager) SetLogLevel(level string) error {
	m.mu.Lock()
	m.config.LogLevel = level
	m.mu.Unlock()
	return m.Save()
}

// GetLogLevel gets the log level
func (m *Manager) GetLogLevel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.LogLevel
}

// GetConfigPath returns the path to the config file
func (m *Manager) GetConfigPath() string {
	return m.configPath
}

// GetConfigDir returns the config directory path
func (m *Manager) GetConfigDir() string {
	return filepath.Dir(m.configPath)
}
