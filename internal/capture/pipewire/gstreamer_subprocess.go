package pipewire

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
)

// GStreamerSubprocess manages a GStreamer pipeline via subprocess for PipeWire capture
// This avoids CGO issues by running gst-launch-1.0 as a separate process
type GStreamerSubprocess struct {
	nodeID      uint32
	cmd         *exec.Cmd
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	mu          sync.RWMutex
	latestFrame *image.RGBA
	frameWidth  int
	frameHeight int
	running     bool
	stopChan    chan struct{}
}

// NewGStreamerSubprocess creates a new subprocess-based GStreamer pipeline
func NewGStreamerSubprocess(nodeID uint32) (*GStreamerSubprocess, error) {
	return &GStreamerSubprocess{
		nodeID:   nodeID,
		stopChan: make(chan struct{}),
	}, nil
}

// Start initializes and starts the GStreamer subprocess
func (g *GStreamerSubprocess) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return fmt.Errorf("pipeline already running")
	}

	log := logger.WithComponent("gstreamer-subprocess")

	// First, probe for the video dimensions using gst-launch with fakesink
	width, height, err := g.probeVideoDimensions()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to probe video dimensions, using defaults")
		width, height = 1920, 1080
	}
	g.frameWidth = width
	g.frameHeight = height
	log.Info().Int("width", width).Int("height", height).Msg("Video dimensions")

	// Build the pipeline command
	// Pipeline: pipewiresrc -> videoconvert -> scale -> RGBA format -> raw output to stdout
	pipelineStr := fmt.Sprintf(
		"pipewiresrc path=%d do-timestamp=true ! "+
			"videoconvert ! "+
			"videoscale ! "+
			"video/x-raw,format=RGBA,width=%d,height=%d ! "+
			"fdsink fd=1 sync=false",
		g.nodeID, width, height,
	)

	log.Debug().Str("pipeline", pipelineStr).Msg("Starting GStreamer subprocess")

	// Use sh -c to properly parse the pipeline string with ! separators
	g.cmd = exec.Command("sh", "-c", "gst-launch-1.0 -q "+pipelineStr)

	// Capture stdout for frame data
	stdout, err := g.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	g.stdout = stdout

	// Capture stderr for errors
	stderr, err := g.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}
	g.stderr = stderr

	// Start the process
	if err := g.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gst-launch: %w", err)
	}

	g.running = true
	g.stopChan = make(chan struct{})

	// Start goroutine to read frames
	go g.readFrames()

	// Start goroutine to log stderr
	go g.logStderr()

	log.Info().Uint32("node_id", g.nodeID).Int("pid", g.cmd.Process.Pid).Msg("GStreamer subprocess started")

	return nil
}

// probeVideoDimensions runs a short pipeline to detect video dimensions
func (g *GStreamerSubprocess) probeVideoDimensions() (int, int, error) {
	log := logger.WithComponent("gstreamer-subprocess")

	// Run pipeline with caps filter to get dimensions
	// Use timeout to avoid hanging, but give it more time for PipeWire to initialize
	pipelineStr := fmt.Sprintf(
		"pipewiresrc path=%d num-buffers=1 ! "+
			"fakesink",
		g.nodeID,
	)

	cmd := exec.Command("sh", "-c", "timeout 10 gst-launch-1.0 -v "+pipelineStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try to parse output anyway - it might have caps info before error
		log.Debug().Str("output", string(output)).Msg("Probe command output")
	}

	// Parse output for video dimensions
	// Look for lines like: /GstPipeline:pipeline0/GstPipeWireSrc:pipewiresrc0.GstPad:src: caps = video/x-raw, format=(string)BGRx, width=(int)2560, height=(int)1440
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "video/x-raw") && strings.Contains(line, "width=") {
			// Extract width and height
			width := extractIntFromCaps(line, "width")
			height := extractIntFromCaps(line, "height")
			if width > 0 && height > 0 {
				return width, height, nil
			}
		}
	}

	// Fallback: try to get screen dimensions from xdpyinfo
	log.Debug().Msg("GStreamer probe failed, trying xdpyinfo fallback")
	width, height := getScreenDimensionsFromSystem()
	if width > 0 && height > 0 {
		log.Info().Int("width", width).Int("height", height).Msg("Using screen dimensions from system")
		return width, height, nil
	}

	return 0, 0, fmt.Errorf("could not determine video dimensions")
}

// getScreenDimensionsFromSystem tries to get screen dimensions from xdpyinfo
func getScreenDimensionsFromSystem() (int, int) {
	cmd := exec.Command("xdpyinfo")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	// Parse output for dimensions line like: "dimensions:    4000x2560 pixels"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "dimensions:") {
			// Extract WxH from "dimensions:    4000x2560 pixels"
			parts := strings.Fields(line)
			for _, part := range parts {
				if strings.Contains(part, "x") && !strings.Contains(part, "pixels") {
					dims := strings.Split(part, "x")
					if len(dims) == 2 {
						w, err1 := strconv.Atoi(dims[0])
						h, err2 := strconv.Atoi(dims[1])
						if err1 == nil && err2 == nil {
							return w, h
						}
					}
				}
			}
		}
	}
	return 0, 0
}

// extractIntFromCaps extracts an integer value from GStreamer caps string
func extractIntFromCaps(caps, key string) int {
	// Look for patterns like "width=(int)1920" or "width=1920"
	patterns := []string{
		key + "=(int)",
		key + "=",
	}

	for _, pattern := range patterns {
		idx := strings.Index(caps, pattern)
		if idx >= 0 {
			start := idx + len(pattern)
			end := start
			for end < len(caps) && (caps[end] >= '0' && caps[end] <= '9') {
				end++
			}
			if end > start {
				val, err := strconv.Atoi(caps[start:end])
				if err == nil {
					return val
				}
			}
		}
	}
	return 0
}

// readFrames continuously reads raw RGBA frames from stdout
func (g *GStreamerSubprocess) readFrames() {
	log := logger.WithComponent("gstreamer-subprocess")

	g.mu.RLock()
	width := g.frameWidth
	height := g.frameHeight
	g.mu.RUnlock()

	frameSize := width * height * 4 // RGBA = 4 bytes per pixel
	reader := bufio.NewReaderSize(g.stdout, frameSize*2)

	frameBuffer := make([]byte, frameSize)
	frameCount := 0

	for {
		select {
		case <-g.stopChan:
			log.Debug().Msg("Frame reader stopping")
			return
		default:
		}

		// Read exactly one frame
		n, err := io.ReadFull(reader, frameBuffer)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				log.Debug().Msg("EOF from GStreamer subprocess")
				return
			}
			log.Error().Err(err).Int("bytes_read", n).Msg("Error reading frame")
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Create image from buffer
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		copy(img.Pix, frameBuffer)

		// Store latest frame
		g.mu.Lock()
		g.latestFrame = img
		g.mu.Unlock()

		frameCount++
	}
}

// logStderr logs any errors from the GStreamer subprocess
func (g *GStreamerSubprocess) logStderr() {
	log := logger.WithComponent("gstreamer-subprocess")
	scanner := bufio.NewScanner(g.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "ERROR") || strings.Contains(line, "WARN") {
			log.Warn().Str("gst", line).Msg("GStreamer message")
		} else {
			log.Debug().Str("gst", line).Msg("GStreamer output")
		}
	}
}

// Stop stops the GStreamer subprocess
func (g *GStreamerSubprocess) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	log := logger.WithComponent("gstreamer-subprocess")

	// Signal reader goroutine to stop
	close(g.stopChan)

	// Kill the process
	if g.cmd != nil && g.cmd.Process != nil {
		log.Debug().Int("pid", g.cmd.Process.Pid).Msg("Killing GStreamer subprocess")
		g.cmd.Process.Kill()
		g.cmd.Wait()
	}

	g.running = false
	log.Info().Msg("GStreamer subprocess stopped")

	return nil
}

// GetLatestFrame returns the most recent captured frame
func (g *GStreamerSubprocess) GetLatestFrame() *image.RGBA {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.latestFrame == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	copyImg := image.NewRGBA(g.latestFrame.Bounds())
	copy(copyImg.Pix, g.latestFrame.Pix)
	return copyImg
}

// GetFrameSize returns the current frame dimensions
func (g *GStreamerSubprocess) GetFrameSize() (width, height int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.frameWidth, g.frameHeight
}

// CropFrame extracts a region from the current frame
func (g *GStreamerSubprocess) CropFrame(x, y, width, height int) *image.RGBA {
	g.mu.RLock()
	frame := g.latestFrame
	g.mu.RUnlock()

	if frame == nil {
		return nil
	}

	// Validate bounds
	frameBounds := frame.Bounds()
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x+width > frameBounds.Dx() {
		width = frameBounds.Dx() - x
	}
	if y+height > frameBounds.Dy() {
		height = frameBounds.Dy() - y
	}

	if width <= 0 || height <= 0 {
		return nil
	}

	// Create cropped image
	cropped := image.NewRGBA(image.Rect(0, 0, width, height))

	for dy := 0; dy < height; dy++ {
		srcStart := (y+dy)*frame.Stride + x*4
		dstStart := dy * cropped.Stride
		copy(cropped.Pix[dstStart:dstStart+width*4], frame.Pix[srcStart:srcStart+width*4])
	}

	return cropped
}

// IsRunning returns whether the subprocess is running
func (g *GStreamerSubprocess) IsRunning() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}
