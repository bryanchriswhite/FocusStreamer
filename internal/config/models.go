package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Application represents a running application
type Application struct {
	ID          string `json:"id" mapstructure:"id"`
	Name        string `json:"name" mapstructure:"name"`
	WindowClass string `json:"window_class" mapstructure:"window_class"`
	PID         int    `json:"pid" mapstructure:"pid"`
	Allowlisted bool   `json:"allowlisted" mapstructure:"allowlisted"`
}

// WindowInfo represents information about a window
type WindowInfo struct {
	ID       uint32   `json:"id" mapstructure:"id"`
	Title    string   `json:"title" mapstructure:"title"`
	Class    string   `json:"class" mapstructure:"class"`
	PID      int      `json:"pid" mapstructure:"pid"`
	Focused  bool     `json:"focused" mapstructure:"focused"`
	Geometry Geometry `json:"geometry" mapstructure:"geometry"`
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
	AllowlistPatterns []string        `json:"allowlist_patterns" mapstructure:"allowlist_patterns"`
	AllowlistedApps   map[string]bool `json:"allowlisted_apps" mapstructure:"allowlisted_apps"`
	VirtualDisplay    DisplayConfig   `json:"virtual_display" mapstructure:"virtual_display"`
	Overlay           OverlayConfig   `json:"overlay" mapstructure:"overlay"`
	ServerPort        int             `json:"server_port" mapstructure:"server_port"`
	LogLevel          string          `json:"log_level" mapstructure:"log_level"`
}

// OverlayConfig represents overlay configuration
type OverlayConfig struct {
	Enabled bool                     `json:"enabled" mapstructure:"enabled"`
	Widgets []map[string]interface{} `json:"widgets" mapstructure:"widgets"`
}

// DisplayConfig represents virtual display configuration
type DisplayConfig struct {
	Width     int  `json:"width" mapstructure:"width"`
	Height    int  `json:"height" mapstructure:"height"`
	RefreshHz int  `json:"refresh_hz" mapstructure:"refresh_hz"`
	FPS       int  `json:"fps" mapstructure:"fps"`
	Enabled   bool `json:"enabled" mapstructure:"enabled"`
}

// Manager handles configuration with Viper
type Manager struct {
	v          *viper.Viper
	configPath string
}

// NewManager creates a new configuration manager using Viper
func NewManager(configFile string) (*Manager, error) {
	v := viper.New()

	// Set default configuration path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "focusstreamer")
	defaultConfigPath := filepath.Join(configDir, "config")

	// Use provided config file or default
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	// Set defaults
	setDefaults(v)

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	m := &Manager{
		v:          v,
		configPath: defaultConfigPath + ".yaml",
	}

	// Try to read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, create it with defaults
			if err := m.Save(); err != nil {
				return nil, fmt.Errorf("failed to create default config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	return m, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	v.SetDefault("server_port", 8080)
	v.SetDefault("log_level", "info")
	v.SetDefault("allowlist_patterns", []string{})
	v.SetDefault("allowlisted_apps", map[string]bool{})
	v.SetDefault("virtual_display.width", 1920)
	v.SetDefault("virtual_display.height", 1080)
	v.SetDefault("virtual_display.refresh_hz", 60)
	v.SetDefault("virtual_display.fps", 10)
	v.SetDefault("virtual_display.enabled", true)
	v.SetDefault("overlay.enabled", true)
	v.SetDefault("overlay.widgets", []map[string]interface{}{})
}

// GetViper returns the underlying Viper instance
func (m *Manager) GetViper() *viper.Viper {
	return m.v
}

// Get returns the current configuration
func (m *Manager) Get() *Config {
	var cfg Config
	if err := m.v.Unmarshal(&cfg); err != nil {
		// Return defaults if unmarshal fails
		return &Config{
			ServerPort:        m.v.GetInt("server_port"),
			LogLevel:          m.v.GetString("log_level"),
			AllowlistPatterns: []string{},
			AllowlistedApps:   make(map[string]bool),
			VirtualDisplay: DisplayConfig{
				Width:     1920,
				Height:    1080,
				RefreshHz: 60,
				FPS:       10,
				Enabled:   true,
			},
		}
	}

	// Ensure maps are initialized
	if cfg.AllowlistedApps == nil {
		cfg.AllowlistedApps = make(map[string]bool)
	}
	if cfg.Overlay.Widgets == nil {
		cfg.Overlay.Widgets = []map[string]interface{}{}
	}

	return &cfg
}

// Save saves the current configuration to disk
func (m *Manager) Save() error {
	log.Printf("Saving config to: %s", m.configPath)

	// Debug: log what we're trying to save
	allowlistedApps := m.v.GetStringMap("allowlisted_apps")
	log.Printf("Config contains %d allowlisted apps: %v", len(allowlistedApps), allowlistedApps)

	if err := m.v.WriteConfigAs(m.configPath); err != nil {
		log.Printf("Error saving config: %v", err)
		return err
	}

	log.Printf("Config saved successfully")
	return nil
}

// Update updates the entire configuration
func (m *Manager) Update(cfg *Config) error {
	m.v.Set("allowlist_patterns", cfg.AllowlistPatterns)
	m.v.Set("allowlisted_apps", cfg.AllowlistedApps)
	m.v.Set("virtual_display", cfg.VirtualDisplay)
	m.v.Set("overlay", cfg.Overlay)
	m.v.Set("server_port", cfg.ServerPort)
	m.v.Set("log_level", cfg.LogLevel)
	return m.Save()
}

// AddAllowlistedApp adds an application to the allowlist
func (m *Manager) AddAllowlistedApp(appClass string) error {
	// Viper lowercases all map keys, so we must do the same
	normalizedKey := strings.ToLower(appClass)
	log.Printf("AddAllowlistedApp called for: %s (normalized: %s)", appClass, normalizedKey)

	// Get existing apps - use map[string]interface{} for Viper compatibility
	apps := make(map[string]interface{})
	existingApps := m.v.GetStringMap("allowlisted_apps")
	log.Printf("Existing allowlisted apps before add: %v", existingApps)

	// Copy existing entries
	for k, v := range existingApps {
		apps[k] = v
	}

	// Add new app with normalized key
	apps[normalizedKey] = true
	log.Printf("Setting allowlisted_apps to: %v", apps)
	m.v.Set("allowlisted_apps", apps)

	// Verify the set worked
	verification := m.v.GetStringMap("allowlisted_apps")
	log.Printf("Verification after set: %v", verification)

	if err := m.Save(); err != nil {
		log.Printf("Error saving config after adding '%s': %v", appClass, err)
		return err
	}

	log.Printf("Successfully added '%s' (key: %s) to allowlist. Total: %d", appClass, normalizedKey, len(apps))
	return nil
}

// RemoveAllowlistedApp removes an application from the allowlist
func (m *Manager) RemoveAllowlistedApp(appClass string) error {
	// Viper lowercases all map keys, so we must do the same
	normalizedKey := strings.ToLower(appClass)

	// Get existing apps - use map[string]interface{} for Viper compatibility
	apps := make(map[string]interface{})
	existingApps := m.v.GetStringMap("allowlisted_apps")

	// Copy existing entries
	for k, v := range existingApps {
		apps[k] = v
	}

	// Remove app with normalized key
	delete(apps, normalizedKey)
	m.v.Set("allowlisted_apps", apps)

	if err := m.Save(); err != nil {
		return err
	}

	log.Printf("Removed '%s' (key: %s) from allowlist. Total: %d", appClass, normalizedKey, len(apps))
	return nil
}

// IsAllowlisted checks if an application is allowlisted
func (m *Manager) IsAllowlisted(appClass string) bool {
	// Viper lowercases all map keys, so we must normalize for lookup
	normalizedKey := strings.ToLower(appClass)
	apps := m.v.GetStringMap("allowlisted_apps")
	val, exists := apps[normalizedKey]

	if !exists {
		return false
	}
	// Handle both bool and interface{} types
	switch v := val.(type) {
	case bool:
		return v
	default:
		return true
	}
}

// AddPattern adds an allowlist pattern
func (m *Manager) AddPattern(pattern string) error {
	patterns := m.v.GetStringSlice("allowlist_patterns")
	patterns = append(patterns, pattern)
	m.v.Set("allowlist_patterns", patterns)
	return m.Save()
}

// RemovePattern removes an allowlist pattern
func (m *Manager) RemovePattern(pattern string) error {
	patterns := m.v.GetStringSlice("allowlist_patterns")
	for i, p := range patterns {
		if p == pattern {
			patterns = append(patterns[:i], patterns[i+1:]...)
			break
		}
	}
	m.v.Set("allowlist_patterns", patterns)
	return m.Save()
}

// SetPort sets the server port
func (m *Manager) SetPort(port int) error {
	m.v.Set("server_port", port)
	return m.Save()
}

// GetPort gets the server port
func (m *Manager) GetPort() int {
	return m.v.GetInt("server_port")
}

// SetLogLevel sets the log level
func (m *Manager) SetLogLevel(level string) error {
	m.v.Set("log_level", level)
	return m.Save()
}

// GetLogLevel gets the log level
func (m *Manager) GetLogLevel() string {
	return m.v.GetString("log_level")
}

// GetConfigPath returns the path to the config file
func (m *Manager) GetConfigPath() string {
	return m.configPath
}
