package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Application represents a running application
type Application struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	WindowClass string `json:"window_class"`
	PID         int    `json:"pid"`
	Whitelisted bool   `json:"whitelisted"`
}

// WindowInfo represents information about a window
type WindowInfo struct {
	ID       uint32   `json:"id"`
	Title    string   `json:"title"`
	Class    string   `json:"class"`
	PID      int      `json:"pid"`
	Focused  bool     `json:"focused"`
	Geometry Geometry `json:"geometry"`
}

// Geometry represents window geometry
type Geometry struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Config represents the application configuration
type Config struct {
	WhitelistPatterns []string        `json:"whitelist_patterns"`
	WhitelistedApps   map[string]bool `json:"whitelisted_apps"`
	VirtualDisplay    DisplayConfig   `json:"virtual_display"`
	ServerPort        int             `json:"server_port"`
}

// DisplayConfig represents virtual display configuration
type DisplayConfig struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	RefreshHz int  `json:"refresh_hz"`
	Enabled   bool `json:"enabled"`
}

// Manager handles configuration persistence
type Manager struct {
	configPath string
	config     *Config
}

// NewManager creates a new configuration manager
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configDir := filepath.Join(homeDir, ".config", "focusstreamer")
	configPath := filepath.Join(configDir, "config.json")

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, err
	}

	m := &Manager{
		configPath: configPath,
		config:     getDefaultConfig(),
	}

	// Load existing config if it exists
	if _, err := os.Stat(configPath); err == nil {
		if err := m.Load(); err != nil {
			return nil, err
		}
	} else {
		// Save default config
		if err := m.Save(); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// getDefaultConfig returns the default configuration
func getDefaultConfig() *Config {
	return &Config{
		WhitelistPatterns: []string{},
		WhitelistedApps:   make(map[string]bool),
		VirtualDisplay: DisplayConfig{
			Width:     1920,
			Height:    1080,
			RefreshHz: 60,
			Enabled:   true,
		},
		ServerPort: 8080,
	}
}

// Load loads the configuration from disk
func (m *Manager) Load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, m.config)
}

// Save saves the configuration to disk
func (m *Manager) Save() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.configPath, data, 0644)
}

// Get returns the current configuration
func (m *Manager) Get() *Config {
	return m.config
}

// Update updates the configuration and saves it
func (m *Manager) Update(config *Config) error {
	m.config = config
	return m.Save()
}

// AddWhitelistedApp adds an application to the whitelist
func (m *Manager) AddWhitelistedApp(appClass string) error {
	m.config.WhitelistedApps[appClass] = true
	return m.Save()
}

// RemoveWhitelistedApp removes an application from the whitelist
func (m *Manager) RemoveWhitelistedApp(appClass string) error {
	delete(m.config.WhitelistedApps, appClass)
	return m.Save()
}

// IsWhitelisted checks if an application is whitelisted
func (m *Manager) IsWhitelisted(appClass string) bool {
	return m.config.WhitelistedApps[appClass]
}

// AddPattern adds a whitelist pattern
func (m *Manager) AddPattern(pattern string) error {
	m.config.WhitelistPatterns = append(m.config.WhitelistPatterns, pattern)
	return m.Save()
}

// RemovePattern removes a whitelist pattern
func (m *Manager) RemovePattern(pattern string) error {
	for i, p := range m.config.WhitelistPatterns {
		if p == pattern {
			m.config.WhitelistPatterns = append(
				m.config.WhitelistPatterns[:i],
				m.config.WhitelistPatterns[i+1:]...,
			)
			return m.Save()
		}
	}
	return nil
}
