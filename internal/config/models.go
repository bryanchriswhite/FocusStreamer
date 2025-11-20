package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Application represents a running application
type Application struct {
	ID          string `json:"id" mapstructure:"id"`
	Name        string `json:"name" mapstructure:"name"`
	WindowClass string `json:"window_class" mapstructure:"window_class"`
	PID         int    `json:"pid" mapstructure:"pid"`
	Whitelisted bool   `json:"whitelisted" mapstructure:"whitelisted"`
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
	WhitelistPatterns []string        `json:"whitelist_patterns" mapstructure:"whitelist_patterns"`
	WhitelistedApps   map[string]bool `json:"whitelisted_apps" mapstructure:"whitelisted_apps"`
	VirtualDisplay    DisplayConfig   `json:"virtual_display" mapstructure:"virtual_display"`
	ServerPort        int             `json:"server_port" mapstructure:"server_port"`
	LogLevel          string          `json:"log_level" mapstructure:"log_level"`
}

// DisplayConfig represents virtual display configuration
type DisplayConfig struct {
	Width     int  `json:"width" mapstructure:"width"`
	Height    int  `json:"height" mapstructure:"height"`
	RefreshHz int  `json:"refresh_hz" mapstructure:"refresh_hz"`
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
	v.SetDefault("whitelist_patterns", []string{})
	v.SetDefault("whitelisted_apps", map[string]bool{})
	v.SetDefault("virtual_display.width", 1920)
	v.SetDefault("virtual_display.height", 1080)
	v.SetDefault("virtual_display.refresh_hz", 60)
	v.SetDefault("virtual_display.enabled", true)
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
			WhitelistPatterns: []string{},
			WhitelistedApps:   make(map[string]bool),
			VirtualDisplay: DisplayConfig{
				Width:     1920,
				Height:    1080,
				RefreshHz: 60,
				Enabled:   true,
			},
		}
	}

	// Ensure maps are initialized
	if cfg.WhitelistedApps == nil {
		cfg.WhitelistedApps = make(map[string]bool)
	}

	return &cfg
}

// Save saves the current configuration to disk
func (m *Manager) Save() error {
	return m.v.WriteConfigAs(m.configPath)
}

// Update updates the entire configuration
func (m *Manager) Update(cfg *Config) error {
	m.v.Set("whitelist_patterns", cfg.WhitelistPatterns)
	m.v.Set("whitelisted_apps", cfg.WhitelistedApps)
	m.v.Set("virtual_display", cfg.VirtualDisplay)
	m.v.Set("server_port", cfg.ServerPort)
	m.v.Set("log_level", cfg.LogLevel)
	return m.Save()
}

// AddWhitelistedApp adds an application to the whitelist
func (m *Manager) AddWhitelistedApp(appClass string) error {
	apps := m.v.GetStringMap("whitelisted_apps")
	apps[appClass] = true
	m.v.Set("whitelisted_apps", apps)
	return m.Save()
}

// RemoveWhitelistedApp removes an application from the whitelist
func (m *Manager) RemoveWhitelistedApp(appClass string) error {
	apps := m.v.GetStringMap("whitelisted_apps")
	delete(apps, appClass)
	m.v.Set("whitelisted_apps", apps)
	return m.Save()
}

// IsWhitelisted checks if an application is whitelisted
func (m *Manager) IsWhitelisted(appClass string) bool {
	apps := m.v.GetStringMap("whitelisted_apps")
	val, exists := apps[appClass]
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

// AddPattern adds a whitelist pattern
func (m *Manager) AddPattern(pattern string) error {
	patterns := m.v.GetStringSlice("whitelist_patterns")
	patterns = append(patterns, pattern)
	m.v.Set("whitelist_patterns", patterns)
	return m.Save()
}

// RemovePattern removes a whitelist pattern
func (m *Manager) RemovePattern(pattern string) error {
	patterns := m.v.GetStringSlice("whitelist_patterns")
	for i, p := range patterns {
		if p == pattern {
			patterns = append(patterns[:i], patterns[i+1:]...)
			break
		}
	}
	m.v.Set("whitelist_patterns", patterns)
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
