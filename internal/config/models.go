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
	AllowlistSourceNone     AllowlistSource = ""         // Not allowlisted
	AllowlistSourceExplicit AllowlistSource = "explicit" // Explicitly added to allowlist
	AllowlistSourcePattern  AllowlistSource = "pattern"  // Matched by a pattern
	AllowlistSourceURL      AllowlistSource = "url"      // Matched by URL rule
)

// UrlRuleType indicates the type of URL allowlist rule
type UrlRuleType string

const (
	UrlRuleTypePage      UrlRuleType = "page"
	UrlRuleTypeDomain    UrlRuleType = "domain"
	UrlRuleTypeSubdomain UrlRuleType = "subdomain"
)

// UrlRule represents a URL allowlist rule
type UrlRule struct {
	ID          string      `json:"id" yaml:"id"`
	Type        UrlRuleType `json:"type" yaml:"type"`
	Pattern     string      `json:"pattern" yaml:"pattern"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
}

// Profile represents a named configuration profile with its own allowlists and placeholders
type Profile struct {
	ID                     string    `json:"id" yaml:"id"`
	Name                   string    `json:"name" yaml:"name"`
	AllowlistPatterns      []string  `json:"allowlist_patterns" yaml:"allowlist_patterns"`
	AllowlistTitlePatterns []string  `json:"allowlist_title_patterns" yaml:"allowlist_title_patterns"`
	AllowlistedApps        []string  `json:"allowed_apps" yaml:"allowed_apps"`
	AllowlistURLRules      []UrlRule `json:"allowlist_url_rules" yaml:"allowlist_url_rules"`
	BrowserWindowClasses   []string  `json:"browser_window_classes" yaml:"browser_window_classes"`
	BrowserBlockedClasses  []string  `json:"browser_blocked_classes" yaml:"browser_blocked_classes"`
	PlaceholderImagePaths  []string  `json:"placeholder_image_paths" yaml:"placeholder_image_paths"`
}

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
	// Global settings (not per-profile)
	VirtualDisplay DisplayConfig `json:"virtual_display" yaml:"virtual_display"`
	Overlay        OverlayConfig `json:"overlay" yaml:"overlay"`
	ServerPort     int           `json:"server_port" yaml:"server_port"`
	LogLevel       string        `json:"log_level" yaml:"log_level"`

	// Profile management
	ActiveProfileID string    `json:"active_profile_id" yaml:"active_profile_id"`
	Profiles        []Profile `json:"profiles" yaml:"profiles"`

	// Legacy fields - populated by Get() from active profile for backwards compat
	// These are read during migration but not serialized to new config files
	AllowlistPatterns      []string  `json:"-" yaml:"allowlist_patterns,omitempty"`
	AllowlistTitlePatterns []string  `json:"-" yaml:"allowlist_title_patterns,omitempty"`
	AllowlistedApps        []string  `json:"-" yaml:"allowed_apps,omitempty"`
	AllowlistURLRules      []UrlRule `json:"-" yaml:"allowlist_url_rules,omitempty"`
	BrowserWindowClasses   []string  `json:"-" yaml:"browser_window_classes,omitempty"`
	BrowserBlockedClasses  []string  `json:"-" yaml:"browser_blocked_classes,omitempty"`
	PlaceholderImagePath   string    `json:"-" yaml:"placeholder_image_path,omitempty"`
	PlaceholderImagePaths  []string  `json:"-" yaml:"placeholder_image_paths,omitempty"`
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
	defaultProfile := Profile{
		ID:                     "default",
		Name:                   "Default",
		AllowlistPatterns:      []string{},
		AllowlistTitlePatterns: []string{},
		AllowlistedApps:        []string{},
		AllowlistURLRules:      []UrlRule{},
		BrowserWindowClasses:   []string{},
		BrowserBlockedClasses:  []string{},
		PlaceholderImagePaths:  []string{},
	}

	return &Config{
		ServerPort:      8080,
		LogLevel:        "info",
		ActiveProfileID: "default",
		Profiles:        []Profile{defaultProfile},
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

	// Initialize global slices if nil
	if cfg.Overlay.Widgets == nil {
		cfg.Overlay.Widgets = []map[string]interface{}{}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = []Profile{}
	}

	// Check if this is a legacy config (no profiles) and needs migration
	needsMigration := len(cfg.Profiles) == 0

	if needsMigration {
		logger.WithComponent("config").Info().Msg("Detected legacy config format, migrating to profile-based config")

		// Initialize legacy slices if nil (needed for migration)
		if cfg.AllowlistedApps == nil {
			cfg.AllowlistedApps = []string{}
		}
		if cfg.AllowlistPatterns == nil {
			cfg.AllowlistPatterns = []string{}
		}
		if cfg.AllowlistTitlePatterns == nil {
			cfg.AllowlistTitlePatterns = []string{}
		}
		if cfg.AllowlistURLRules == nil {
			cfg.AllowlistURLRules = []UrlRule{}
		}
		if cfg.BrowserWindowClasses == nil {
			cfg.BrowserWindowClasses = []string{}
		}
		if cfg.BrowserBlockedClasses == nil {
			cfg.BrowserBlockedClasses = []string{}
		}
		if cfg.PlaceholderImagePaths == nil {
			cfg.PlaceholderImagePaths = []string{}
		}

		// Migrate old single placeholder path to slice format first
		if cfg.PlaceholderImagePath != "" && len(cfg.PlaceholderImagePaths) == 0 {
			cfg.PlaceholderImagePaths = []string{cfg.PlaceholderImagePath}
		}

		// Create default profile from legacy fields
		defaultProfile := Profile{
			ID:                     "default",
			Name:                   "Default",
			AllowlistPatterns:      cfg.AllowlistPatterns,
			AllowlistTitlePatterns: cfg.AllowlistTitlePatterns,
			AllowlistedApps:        cfg.AllowlistedApps,
			AllowlistURLRules:      cfg.AllowlistURLRules,
			BrowserWindowClasses:   cfg.BrowserWindowClasses,
			BrowserBlockedClasses:  cfg.BrowserBlockedClasses,
			PlaceholderImagePaths:  cfg.PlaceholderImagePaths,
		}

		cfg.Profiles = []Profile{defaultProfile}
		cfg.ActiveProfileID = "default"

		// Clear legacy fields (they're now in the profile)
		cfg.AllowlistPatterns = nil
		cfg.AllowlistTitlePatterns = nil
		cfg.AllowlistedApps = nil
		cfg.AllowlistURLRules = nil
		cfg.BrowserWindowClasses = nil
		cfg.BrowserBlockedClasses = nil
		cfg.PlaceholderImagePath = ""
		cfg.PlaceholderImagePaths = nil

		logger.WithComponent("config").Info().
			Str("profile_id", "default").
			Msg("Migration complete - created Default profile from existing settings")
	}

	// Ensure active profile ID is set
	if cfg.ActiveProfileID == "" && len(cfg.Profiles) > 0 {
		cfg.ActiveProfileID = cfg.Profiles[0].ID
	}

	// Initialize nil slices in all profiles
	for i := range cfg.Profiles {
		if cfg.Profiles[i].AllowlistPatterns == nil {
			cfg.Profiles[i].AllowlistPatterns = []string{}
		}
		if cfg.Profiles[i].AllowlistTitlePatterns == nil {
			cfg.Profiles[i].AllowlistTitlePatterns = []string{}
		}
		if cfg.Profiles[i].AllowlistedApps == nil {
			cfg.Profiles[i].AllowlistedApps = []string{}
		}
		if cfg.Profiles[i].AllowlistURLRules == nil {
			cfg.Profiles[i].AllowlistURLRules = []UrlRule{}
		}
		if cfg.Profiles[i].BrowserWindowClasses == nil {
			cfg.Profiles[i].BrowserWindowClasses = []string{}
		}
		if cfg.Profiles[i].BrowserBlockedClasses == nil {
			cfg.Profiles[i].BrowserBlockedClasses = []string{}
		}
		if cfg.Profiles[i].PlaceholderImagePaths == nil {
			cfg.Profiles[i].PlaceholderImagePaths = []string{}
		}
	}

	m.mu.Lock()
	m.config = &cfg
	m.mu.Unlock()

	// Save migrated config if migration occurred
	if needsMigration {
		if err := m.Save(); err != nil {
			logger.WithComponent("config").Warn().Err(err).Msg("Failed to save migrated config")
		}
	}

	return nil
}

// Get returns the current configuration with legacy fields populated from active profile
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config == nil {
		return m.getDefaults()
	}

	// Return a copy to prevent external modification
	cfg := *m.config

	// Populate legacy fields from active profile for backwards compatibility
	if profile := m.getActiveProfileLocked(); profile != nil {
		cfg.AllowlistPatterns = profile.AllowlistPatterns
		cfg.AllowlistTitlePatterns = profile.AllowlistTitlePatterns
		cfg.AllowlistedApps = profile.AllowlistedApps
		cfg.AllowlistURLRules = profile.AllowlistURLRules
		cfg.BrowserWindowClasses = profile.BrowserWindowClasses
		cfg.BrowserBlockedClasses = profile.BrowserBlockedClasses
		cfg.PlaceholderImagePaths = profile.PlaceholderImagePaths
	}

	return &cfg
}

// getActiveProfileLocked returns the active profile (caller must hold at least read lock)
func (m *Manager) getActiveProfileLocked() *Profile {
	if m.config == nil {
		return nil
	}
	for i := range m.config.Profiles {
		if m.config.Profiles[i].ID == m.config.ActiveProfileID {
			return &m.config.Profiles[i]
		}
	}
	// Fallback to first profile if active not found
	if len(m.config.Profiles) > 0 {
		return &m.config.Profiles[0]
	}
	return nil
}

// GetActiveProfile returns a copy of the active profile
func (m *Manager) GetActiveProfile() *Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile := m.getActiveProfileLocked()
	if profile == nil {
		return nil
	}
	// Return a copy
	p := *profile
	return &p
}

// GetActiveProfileID returns the active profile ID
func (m *Manager) GetActiveProfileID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config == nil {
		return "default"
	}
	return m.config.ActiveProfileID
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
		Int("profile_count", len(cfg.Profiles)).
		Str("active_profile", cfg.ActiveProfileID).
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

// AddAllowlistedApp adds an application to the allowlist of the active profile
func (m *Manager) AddAllowlistedApp(appClass string) error {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	// Check if already exists (prevent duplicates)
	for _, app := range profile.AllowlistedApps {
		if app == normalized {
			m.mu.Unlock()
			logger.WithComponent("config").Debug().
				Str("app_class", appClass).
				Msg("App already in allowlist, skipping")
			return nil
		}
	}

	// Append new app
	profile.AllowlistedApps = append(profile.AllowlistedApps, normalized)
	totalCount := len(profile.AllowlistedApps)
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

// RemoveAllowlistedApp removes an application from the allowlist of the active profile
func (m *Manager) RemoveAllowlistedApp(appClass string) error {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	// Filter out the app to remove
	filtered := make([]string, 0, len(profile.AllowlistedApps))
	for _, app := range profile.AllowlistedApps {
		if app != normalized {
			filtered = append(filtered, app)
		}
	}
	profile.AllowlistedApps = filtered
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

// IsAllowlisted checks if an application is allowlisted in the active profile
func (m *Manager) IsAllowlisted(appClass string) bool {
	// Normalize to lowercase for case-insensitive matching
	normalized := strings.ToLower(appClass)

	m.mu.RLock()
	defer m.mu.RUnlock()

	profile := m.getActiveProfileLocked()
	if profile == nil {
		return false
	}

	for _, app := range profile.AllowlistedApps {
		if app == normalized {
			return true
		}
	}
	return false
}

// AddURLRule adds a URL allowlist rule to the active profile
func (m *Manager) AddURLRule(rule UrlRule) error {
	if rule.ID == "" {
		return fmt.Errorf("url rule id is required")
	}
	if rule.Pattern == "" {
		return fmt.Errorf("url rule pattern is required")
	}

	switch rule.Type {
	case UrlRuleTypePage, UrlRuleTypeDomain, UrlRuleTypeSubdomain:
		// Valid
	default:
		return fmt.Errorf("invalid url rule type: %s", rule.Type)
	}

	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	for _, existing := range profile.AllowlistURLRules {
		if existing.ID == rule.ID {
			m.mu.Unlock()
			return nil
		}
	}
	profile.AllowlistURLRules = append(profile.AllowlistURLRules, rule)
	m.mu.Unlock()

	return m.Save()
}

// RemoveURLRule removes a URL allowlist rule by ID from the active profile
func (m *Manager) RemoveURLRule(ruleID string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	filtered := make([]UrlRule, 0, len(profile.AllowlistURLRules))
	for _, rule := range profile.AllowlistURLRules {
		if rule.ID != ruleID {
			filtered = append(filtered, rule)
		}
	}
	profile.AllowlistURLRules = filtered
	m.mu.Unlock()
	return m.Save()
}

// AddBrowserWindowClass stores a browser window class for URL-based allowlisting in the active profile
func (m *Manager) AddBrowserWindowClass(windowClass string) error {
	normalized := strings.ToLower(windowClass)
	if normalized == "" {
		return fmt.Errorf("window class is required")
	}

	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	for _, existing := range profile.BrowserWindowClasses {
		if existing == normalized {
			m.mu.Unlock()
			return nil
		}
	}
	profile.BrowserWindowClasses = append(profile.BrowserWindowClasses, normalized)
	m.mu.Unlock()
	return m.Save()
}

// SetBrowserBlocked sets whether a browser window class is blocked in the active profile
func (m *Manager) SetBrowserBlocked(windowClass string, blocked bool) error {
	normalized := strings.ToLower(windowClass)
	if normalized == "" {
		return fmt.Errorf("window class is required")
	}

	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}

	filtered := make([]string, 0, len(profile.BrowserBlockedClasses))
	for _, existing := range profile.BrowserBlockedClasses {
		if existing != normalized {
			filtered = append(filtered, existing)
		}
	}
	if blocked {
		filtered = append(filtered, normalized)
	}
	profile.BrowserBlockedClasses = filtered
	m.mu.Unlock()
	return m.Save()
}

// IsBrowserBlocked checks if a browser window class is blocked in the active profile
func (m *Manager) IsBrowserBlocked(windowClass string) bool {
	normalized := strings.ToLower(windowClass)
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile := m.getActiveProfileLocked()
	if profile == nil {
		return false
	}

	for _, existing := range profile.BrowserBlockedClasses {
		if existing == normalized {
			return true
		}
	}
	return false
}

// IsBrowserWindowClass checks if a window class is recognized as a browser in the active profile
func (m *Manager) IsBrowserWindowClass(windowClass string) bool {
	normalized := strings.ToLower(windowClass)
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile := m.getActiveProfileLocked()
	if profile == nil {
		return false
	}

	for _, existing := range profile.BrowserWindowClasses {
		if existing == normalized {
			return true
		}
	}
	return false
}

// AddPattern adds an allowlist pattern to the active profile
func (m *Manager) AddPattern(pattern string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	profile.AllowlistPatterns = append(profile.AllowlistPatterns, pattern)
	m.mu.Unlock()
	return m.Save()
}

// RemovePattern removes an allowlist pattern from the active profile
func (m *Manager) RemovePattern(pattern string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	for i, p := range profile.AllowlistPatterns {
		if p == pattern {
			profile.AllowlistPatterns = append(profile.AllowlistPatterns[:i], profile.AllowlistPatterns[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return m.Save()
}

// AddTitlePattern adds a title-only allowlist pattern to the active profile
func (m *Manager) AddTitlePattern(pattern string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	// Check for duplicates
	for _, p := range profile.AllowlistTitlePatterns {
		if p == pattern {
			m.mu.Unlock()
			return nil // Already exists
		}
	}
	profile.AllowlistTitlePatterns = append(profile.AllowlistTitlePatterns, pattern)
	m.mu.Unlock()
	return m.Save()
}

// RemoveTitlePattern removes a title-only allowlist pattern from the active profile
func (m *Manager) RemoveTitlePattern(pattern string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	for i, p := range profile.AllowlistTitlePatterns {
		if p == pattern {
			profile.AllowlistTitlePatterns = append(profile.AllowlistTitlePatterns[:i], profile.AllowlistTitlePatterns[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return m.Save()
}

// SetPlaceholderImage sets the custom placeholder image path (legacy, adds to active profile)
func (m *Manager) SetPlaceholderImage(path string) error {
	return m.AddPlaceholderImage(path)
}

// ClearPlaceholderImage clears all placeholder images from the active profile
func (m *Manager) ClearPlaceholderImage() error {
	return m.ClearAllPlaceholderImages()
}

// GetPlaceholderImagePath returns the first placeholder image path
// Deprecated: Use GetPlaceholderImagePaths instead
func (m *Manager) GetPlaceholderImagePath() string {
	paths := m.GetPlaceholderImagePaths()
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// AddPlaceholderImage adds a placeholder image path to the active profile
func (m *Manager) AddPlaceholderImage(path string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	// Check if already exists (prevent duplicates)
	for _, p := range profile.PlaceholderImagePaths {
		if p == path {
			m.mu.Unlock()
			return nil
		}
	}
	profile.PlaceholderImagePaths = append(profile.PlaceholderImagePaths, path)
	m.mu.Unlock()
	return m.Save()
}

// RemovePlaceholderImage removes a placeholder image path from the active profile
func (m *Manager) RemovePlaceholderImage(path string) error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	filtered := make([]string, 0, len(profile.PlaceholderImagePaths))
	for _, p := range profile.PlaceholderImagePaths {
		if p != path {
			filtered = append(filtered, p)
		}
	}
	profile.PlaceholderImagePaths = filtered
	m.mu.Unlock()
	return m.Save()
}

// GetPlaceholderImagePaths returns all placeholder image paths from the active profile
func (m *Manager) GetPlaceholderImagePaths() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		return []string{}
	}
	// Return a copy to prevent external modification
	paths := make([]string, len(profile.PlaceholderImagePaths))
	copy(paths, profile.PlaceholderImagePaths)
	return paths
}

// IsPlaceholderImageUsedByOtherProfiles checks if the given image path is used by any profile
// other than the active profile
func (m *Manager) IsPlaceholderImageUsedByOtherProfiles(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeID := m.config.ActiveProfileID
	for i := range m.config.Profiles {
		if m.config.Profiles[i].ID == activeID {
			continue // Skip active profile
		}
		for _, p := range m.config.Profiles[i].PlaceholderImagePaths {
			if p == path {
				return true
			}
		}
	}
	return false
}

// CleanupBrokenPlaceholderPaths removes placeholder image paths that point to non-existent files
// from all profiles. Returns the number of paths removed.
func (m *Manager) CleanupBrokenPlaceholderPaths() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log := logger.WithComponent("config")
	totalRemoved := 0

	for i := range m.config.Profiles {
		profile := &m.config.Profiles[i]
		validPaths := make([]string, 0, len(profile.PlaceholderImagePaths))

		for _, path := range profile.PlaceholderImagePaths {
			if _, err := os.Stat(path); err == nil {
				validPaths = append(validPaths, path)
			} else {
				log.Info().
					Str("profile", profile.Name).
					Str("path", path).
					Msg("Removing broken placeholder image path")
				totalRemoved++
			}
		}

		profile.PlaceholderImagePaths = validPaths
	}

	if totalRemoved > 0 {
		// Save without holding lock
		m.mu.Unlock()
		err := m.Save()
		m.mu.Lock()
		return totalRemoved, err
	}

	return 0, nil
}

// ClearAllPlaceholderImages removes all placeholder image paths from the active profile
func (m *Manager) ClearAllPlaceholderImages() error {
	m.mu.Lock()
	profile := m.getActiveProfileLocked()
	if profile == nil {
		m.mu.Unlock()
		return fmt.Errorf("no active profile")
	}
	profile.PlaceholderImagePaths = []string{}
	m.mu.Unlock()
	return m.Save()
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

// SetActiveProfile switches to a different profile
func (m *Manager) SetActiveProfile(profileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify profile exists
	found := false
	for _, p := range m.config.Profiles {
		if p.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	m.config.ActiveProfileID = profileID
	logger.WithComponent("config").Info().
		Str("profile_id", profileID).
		Msg("Switched to profile")

	// Save is called without lock since we defer unlock
	m.mu.Unlock()
	err := m.Save()
	m.mu.Lock()
	return err
}

// ListProfiles returns all profiles
func (m *Manager) ListProfiles() []Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config == nil {
		return []Profile{}
	}

	// Return a copy
	profiles := make([]Profile, len(m.config.Profiles))
	copy(profiles, m.config.Profiles)
	return profiles
}

// GetProfile returns a profile by ID
func (m *Manager) GetProfile(profileID string) (*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.config.Profiles {
		if m.config.Profiles[i].ID == profileID {
			p := m.config.Profiles[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("profile not found: %s", profileID)
}

// CreateProfile creates a new profile with the given name
func (m *Manager) CreateProfile(name string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate unique ID
	id := m.generateProfileID(name)

	profile := Profile{
		ID:                     id,
		Name:                   name,
		AllowlistPatterns:      []string{},
		AllowlistTitlePatterns: []string{},
		AllowlistedApps:        []string{},
		AllowlistURLRules:      []UrlRule{},
		BrowserWindowClasses:   []string{},
		BrowserBlockedClasses:  []string{},
		PlaceholderImagePaths:  []string{},
	}

	m.config.Profiles = append(m.config.Profiles, profile)

	logger.WithComponent("config").Info().
		Str("profile_id", id).
		Str("profile_name", name).
		Msg("Created new profile")

	// Save without lock
	m.mu.Unlock()
	err := m.Save()
	m.mu.Lock()
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

// DeleteProfile deletes a profile by ID
func (m *Manager) DeleteProfile(profileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if profileID == "default" {
		return fmt.Errorf("cannot delete the default profile")
	}

	// Find and remove the profile
	found := false
	filtered := make([]Profile, 0, len(m.config.Profiles))
	for _, p := range m.config.Profiles {
		if p.ID != profileID {
			filtered = append(filtered, p)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	m.config.Profiles = filtered

	// If we deleted the active profile, switch to default
	if m.config.ActiveProfileID == profileID {
		m.config.ActiveProfileID = "default"
	}

	logger.WithComponent("config").Info().
		Str("profile_id", profileID).
		Msg("Deleted profile")

	// Save without lock
	m.mu.Unlock()
	err := m.Save()
	m.mu.Lock()
	return err
}

// UpdateProfile updates an existing profile
func (m *Manager) UpdateProfile(profile *Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and update the profile
	found := false
	for i := range m.config.Profiles {
		if m.config.Profiles[i].ID == profile.ID {
			m.config.Profiles[i] = *profile
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("profile not found: %s", profile.ID)
	}

	logger.WithComponent("config").Info().
		Str("profile_id", profile.ID).
		Str("profile_name", profile.Name).
		Msg("Updated profile")

	// Save without lock
	m.mu.Unlock()
	err := m.Save()
	m.mu.Lock()
	return err
}

// DuplicateProfile creates a copy of an existing profile with a new name
func (m *Manager) DuplicateProfile(profileID, newName string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find the source profile
	var source *Profile
	for i := range m.config.Profiles {
		if m.config.Profiles[i].ID == profileID {
			source = &m.config.Profiles[i]
			break
		}
	}

	if source == nil {
		return nil, fmt.Errorf("profile not found: %s", profileID)
	}

	// Generate unique ID for the new profile
	newID := m.generateProfileID(newName)

	// Create a copy with new ID and name
	newProfile := Profile{
		ID:                     newID,
		Name:                   newName,
		AllowlistPatterns:      make([]string, len(source.AllowlistPatterns)),
		AllowlistTitlePatterns: make([]string, len(source.AllowlistTitlePatterns)),
		AllowlistedApps:        make([]string, len(source.AllowlistedApps)),
		AllowlistURLRules:      make([]UrlRule, len(source.AllowlistURLRules)),
		BrowserWindowClasses:   make([]string, len(source.BrowserWindowClasses)),
		BrowserBlockedClasses:  make([]string, len(source.BrowserBlockedClasses)),
		PlaceholderImagePaths:  make([]string, len(source.PlaceholderImagePaths)),
	}

	copy(newProfile.AllowlistPatterns, source.AllowlistPatterns)
	copy(newProfile.AllowlistTitlePatterns, source.AllowlistTitlePatterns)
	copy(newProfile.AllowlistedApps, source.AllowlistedApps)
	copy(newProfile.AllowlistURLRules, source.AllowlistURLRules)
	copy(newProfile.BrowserWindowClasses, source.BrowserWindowClasses)
	copy(newProfile.BrowserBlockedClasses, source.BrowserBlockedClasses)
	copy(newProfile.PlaceholderImagePaths, source.PlaceholderImagePaths)

	m.config.Profiles = append(m.config.Profiles, newProfile)

	logger.WithComponent("config").Info().
		Str("source_id", profileID).
		Str("new_id", newID).
		Str("new_name", newName).
		Msg("Duplicated profile")

	// Save without lock
	m.mu.Unlock()
	err := m.Save()
	m.mu.Lock()
	if err != nil {
		return nil, err
	}

	return &newProfile, nil
}

// generateProfileID generates a unique profile ID from a name
func (m *Manager) generateProfileID(name string) string {
	// Convert name to lowercase and replace spaces with dashes
	base := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	// Remove any non-alphanumeric characters except dashes
	var result strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	id := result.String()
	if id == "" {
		id = "profile"
	}

	// Ensure uniqueness
	originalID := id
	counter := 1
	for m.profileIDExists(id) {
		id = fmt.Sprintf("%s-%d", originalID, counter)
		counter++
	}

	return id
}

// profileIDExists checks if a profile ID already exists (caller must hold lock)
func (m *Manager) profileIDExists(id string) bool {
	for _, p := range m.config.Profiles {
		if p.ID == id {
			return true
		}
	}
	return false
}

// GetProfilePlaceholderDir returns the placeholder directory for a profile
func (m *Manager) GetProfilePlaceholderDir(profileID string) string {
	configDir := m.GetConfigDir()
	return filepath.Join(configDir, "profiles", profileID, "placeholders")
}

// EnsureProfileDirectory creates the profile directory structure if it doesn't exist
func (m *Manager) EnsureProfileDirectory(profileID string) error {
	placeholderDir := m.GetProfilePlaceholderDir(profileID)
	return os.MkdirAll(placeholderDir, 0755)
}
