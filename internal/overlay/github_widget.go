package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"sync"
	"time"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// GitHubWorkflowRun represents a simplified GitHub Actions workflow run
type GitHubWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued, in_progress, completed
	Conclusion string `json:"conclusion"` // success, failure, cancelled, skipped, etc.
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// GitHubWorkflowRunsResponse represents the GitHub API response
type GitHubWorkflowRunsResponse struct {
	TotalCount   int                  `json:"total_count"`
	WorkflowRuns []GitHubWorkflowRun  `json:"workflow_runs"`
}

// GitHubWidget displays GitHub Actions workflow status
type GitHubWidget struct {
	*BaseWidget
	owner      string
	repo       string
	branch     string // Optional: filter by branch
	token      string // Optional: GitHub token for private repos
	status     string
	conclusion string
	lastUpdate time.Time
	pollInterval time.Duration
	mu         sync.RWMutex
	stopChan   chan struct{}
	bgColor    color.RGBA
	padding    int
}

// NewGitHubWidget creates a new GitHub Actions status widget
func NewGitHubWidget(id string, config map[string]interface{}) (*GitHubWidget, error) {
	w := &GitHubWidget{
		BaseWidget:   NewBaseWidget(id, 0, 0, 1.0),
		pollInterval: 60 * time.Second, // Poll every 60 seconds by default
		status:       "unknown",
		conclusion:   "",
		bgColor:      color.RGBA{30, 30, 40, 220}, // Semi-transparent dark background
		padding:      8,
		stopChan:     make(chan struct{}),
	}

	if err := w.UpdateConfig(config); err != nil {
		return nil, err
	}

	// Validate required fields
	if w.owner == "" || w.repo == "" {
		return nil, fmt.Errorf("github widget requires 'owner' and 'repo' fields")
	}

	// Start polling in background
	go w.pollStatus()

	return w, nil
}

// Type returns the widget type
func (w *GitHubWidget) Type() string {
	return "github-actions"
}

// Render draws the GitHub Actions status widget
func (w *GitHubWidget) Render(img *image.RGBA) error {
	if !w.IsEnabled() {
		return nil
	}

	w.mu.RLock()
	status := w.status
	conclusion := w.conclusion
	w.mu.RUnlock()

	// Determine display text and color
	var statusText string
	var statusColor color.RGBA

	if status == "completed" {
		switch conclusion {
		case "success":
			statusText = "✓ Passing"
			statusColor = color.RGBA{46, 160, 67, 255} // Green
		case "failure":
			statusText = "✗ Failing"
			statusColor = color.RGBA{203, 36, 49, 255} // Red
		case "cancelled":
			statusText = "○ Cancelled"
			statusColor = color.RGBA{158, 158, 158, 255} // Gray
		default:
			statusText = fmt.Sprintf("○ %s", conclusion)
			statusColor = color.RGBA{158, 158, 158, 255} // Gray
		}
	} else if status == "in_progress" {
		statusText = "● Running"
		statusColor = color.RGBA{219, 154, 4, 255} // Yellow/Orange
	} else if status == "queued" {
		statusText = "○ Queued"
		statusColor = color.RGBA{158, 158, 158, 255} // Gray
	} else {
		statusText = "? Unknown"
		statusColor = color.RGBA{158, 158, 158, 255} // Gray
	}

	// Add repo info
	repoText := fmt.Sprintf("%s/%s", w.owner, w.repo)
	if w.branch != "" {
		repoText = fmt.Sprintf("%s:%s", repoText, w.branch)
	}

	// Measure text
	face := basicfont.Face7x13
	d := &font.Drawer{Face: face}

	repoWidth := d.MeasureString(repoText)
	statusWidth := d.MeasureString(statusText)
	maxWidth := int(repoWidth >> 6)
	if int(statusWidth>>6) > maxWidth {
		maxWidth = int(statusWidth >> 6)
	}

	// Calculate widget dimensions
	widgetWidth := maxWidth + w.padding*2
	widgetHeight := 13*2 + w.padding*3 // Two lines of text

	// Draw background
	bgImg := image.NewRGBA(image.Rect(0, 0, widgetWidth, widgetHeight))
	draw.Draw(bgImg, bgImg.Bounds(), &image.Uniform{w.bgColor}, image.Point{}, draw.Src)
	BlendImage(img, bgImg, w.x, w.y, w.opacity)

	// Draw repo text (white)
	repoImg := image.NewRGBA(image.Rect(0, 0, int(repoWidth>>6), 13))
	repoDrawer := &font.Drawer{
		Dst:  repoImg,
		Src:  image.NewUniform(color.RGBA{200, 200, 200, 255}),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: fixed.I(13)},
	}
	repoDrawer.DrawString(repoText)
	BlendImage(img, repoImg, w.x+w.padding, w.y+w.padding, w.opacity)

	// Draw status text (colored)
	statusImg := image.NewRGBA(image.Rect(0, 0, int(statusWidth>>6), 13))
	statusDrawer := &font.Drawer{
		Dst:  statusImg,
		Src:  image.NewUniform(statusColor),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: fixed.I(13)},
	}
	statusDrawer.DrawString(statusText)
	BlendImage(img, statusImg, w.x+w.padding, w.y+w.padding+13+w.padding, w.opacity)

	return nil
}

// GetConfig returns the widget configuration
func (w *GitHubWidget) GetConfig() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	config := map[string]interface{}{
		"id":            w.id,
		"type":          w.Type(),
		"enabled":       w.enabled,
		"x":             w.x,
		"y":             w.y,
		"opacity":       w.opacity,
		"owner":         w.owner,
		"repo":          w.repo,
		"branch":        w.branch,
		"poll_interval": int(w.pollInterval.Seconds()),
		"status":        w.status,
		"conclusion":    w.conclusion,
	}

	if !w.lastUpdate.IsZero() {
		config["last_update"] = w.lastUpdate.Format(time.RFC3339)
	}

	return config
}

// UpdateConfig updates the widget configuration
func (w *GitHubWidget) UpdateConfig(config map[string]interface{}) error {
	if owner, ok := config["owner"].(string); ok {
		w.owner = owner
	}

	if repo, ok := config["repo"].(string); ok {
		w.repo = repo
	}

	if branch, ok := config["branch"].(string); ok {
		w.branch = branch
	}

	if token, ok := config["token"].(string); ok {
		w.token = token
	}

	if x, ok := config["x"].(float64); ok {
		w.x = int(x)
	} else if x, ok := config["x"].(int); ok {
		w.x = x
	}

	if y, ok := config["y"].(float64); ok {
		w.y = int(y)
	} else if y, ok := config["y"].(int); ok {
		w.y = y
	}

	if opacity, ok := config["opacity"].(float64); ok {
		w.SetOpacity(opacity)
	}

	if enabled, ok := config["enabled"].(bool); ok {
		w.SetEnabled(enabled)
	}

	if interval, ok := config["poll_interval"].(float64); ok {
		w.pollInterval = time.Duration(interval) * time.Second
	} else if interval, ok := config["poll_interval"].(int); ok {
		w.pollInterval = time.Duration(interval) * time.Second
	}

	return nil
}

// pollStatus polls the GitHub API for workflow status
func (w *GitHubWidget) pollStatus() {
	// Initial fetch
	if err := w.fetchStatus(); err != nil {
		logger.WithComponent("overlay").Info().Msgf("[GitHubWidget %s] Initial fetch failed: %v", w.id, err)
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			if err := w.fetchStatus(); err != nil {
				logger.WithComponent("overlay").Info().Msgf("[GitHubWidget %s] Failed to fetch status: %v", w.id, err)
			}
		}
	}
}

// fetchStatus fetches the latest workflow run status from GitHub API
func (w *GitHubWidget) fetchStatus() error {
	// Build API URL
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs?per_page=1", w.owner, w.repo)
	if w.branch != "" {
		url += fmt.Sprintf("&branch=%s", w.branch)
	}

	// Create request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if w.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", w.token))
	}

	// Make request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch from GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	// Parse response
	var apiResp GitHubWorkflowRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Update status
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(apiResp.WorkflowRuns) > 0 {
		run := apiResp.WorkflowRuns[0]
		w.status = run.Status
		w.conclusion = run.Conclusion
		w.lastUpdate = time.Now()
		logger.WithComponent("overlay").Info().Msgf("[GitHubWidget %s] Updated status: %s/%s", w.id, w.status, w.conclusion)
	} else {
		w.status = "no_runs"
		w.conclusion = ""
		w.lastUpdate = time.Now()
	}

	return nil
}

// Stop stops the background polling
func (w *GitHubWidget) Stop() {
	close(w.stopChan)
}
