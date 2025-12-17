package pipewire

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"time"

	"github.com/bryanchriswhite/FocusStreamer/internal/logger"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// GStreamerPipeline manages a GStreamer pipeline for PipeWire capture
type GStreamerPipeline struct {
	pipeline    *gst.Pipeline
	appsink     *app.Sink
	nodeID      uint32
	mu          sync.RWMutex
	latestFrame *image.RGBA
	frameWidth  int
	frameHeight int
	running     bool
	ready       bool // Set to true when pipeline is fully initialized
	stopChan    chan struct{}
}

// NewGStreamerPipeline creates a new GStreamer pipeline for PipeWire capture
func NewGStreamerPipeline(nodeID uint32) (*GStreamerPipeline, error) {
	return &GStreamerPipeline{
		nodeID: nodeID,
	}, nil
}

// Start initializes and starts the GStreamer pipeline
func (p *GStreamerPipeline) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("pipeline already running")
	}

	log := logger.WithComponent("gstreamer")

	// Initialize GStreamer
	gst.Init(nil)

	// Create pipeline from string
	// Pipeline: pipewiresrc -> videoconvert -> RGBA format -> appsink
	// Using emit-signals=false and polling mode to avoid CGO callback issues
	pipelineStr := fmt.Sprintf(
		"pipewiresrc path=%d do-timestamp=true ! "+
			"videoconvert ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink emit-signals=false max-buffers=2 drop=true",
		p.nodeID,
	)

	log.Debug().Str("pipeline", pipelineStr).Msg("Creating GStreamer pipeline")

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}
	p.pipeline = pipeline

	// Get appsink element
	sinkElement, err := pipeline.GetElementByName("sink")
	if err != nil {
		return fmt.Errorf("failed to get appsink: %w", err)
	}
	p.appsink = app.SinkFromElement(sinkElement)

	// Start pipeline
	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	// Mark as running/ready
	p.running = true
	p.ready = true
	p.stopChan = make(chan struct{})

	// Start polling goroutine to pull samples (avoids CGO callback issues)
	go p.pollSamples()

	log.Info().Uint32("node_id", p.nodeID).Msg("GStreamer pipeline started")

	return nil
}

// Stop stops the GStreamer pipeline
func (p *GStreamerPipeline) Stop() error {
	p.mu.Lock()

	if !p.running {
		p.mu.Unlock()
		return nil
	}

	log := logger.WithComponent("gstreamer")

	// Mark as not ready before stopping to prevent callback issues
	p.ready = false
	p.running = false

	// Signal polling goroutine to stop
	if p.stopChan != nil {
		close(p.stopChan)
		p.stopChan = nil
	}

	p.mu.Unlock()

	// Give polling goroutine time to exit
	time.Sleep(100 * time.Millisecond)

	p.mu.Lock()
	if p.pipeline != nil {
		p.pipeline.SetState(gst.StateNull)
		p.pipeline.Unref()
		p.pipeline = nil
	}
	p.mu.Unlock()

	log.Info().Msg("GStreamer pipeline stopped")

	return nil
}

// pollSamples polls for new samples from the pipeline (avoids CGO callback issues)
func (p *GStreamerPipeline) pollSamples() {
	log := logger.WithComponent("gstreamer")
	ticker := time.NewTicker(16 * time.Millisecond) // ~60fps polling
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.mu.RLock()
			appsink := p.appsink
			ready := p.ready
			p.mu.RUnlock()

			if !ready || appsink == nil {
				continue
			}

			// Try to pull a sample (non-blocking with small timeout)
			// Use 1ms timeout to avoid busy-looping
			sample := appsink.TryPullSample(time.Millisecond)
			if sample == nil {
				continue
			}

			// Process the sample
			// Note: Don't call Unref() - go-gst library handles this internally
			// Calling Unref() causes double-free crashes
			p.processSample(sample)
		}
	}
	log.Debug().Msg("Sample polling stopped")
}

// processSample extracts and stores frame data from a GStreamer sample
func (p *GStreamerPipeline) processSample(sample *gst.Sample) {
	buffer := sample.GetBuffer()
	if buffer == nil {
		return
	}

	// Get video info from caps
	caps := sample.GetCaps()
	if caps == nil {
		return
	}

	structure := caps.GetStructureAt(0)
	if structure == nil {
		return
	}

	width, _ := structure.GetValue("width")
	height, _ := structure.GetValue("height")

	w, ok := width.(int)
	if !ok {
		return
	}
	h, ok := height.(int)
	if !ok {
		return
	}

	// Map buffer to read pixel data
	mapInfo := buffer.Map(gst.MapRead)
	if mapInfo == nil {
		return
	}
	defer buffer.Unmap()

	// Create image from buffer data
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	data := mapInfo.Bytes()
	expectedSize := w * h * 4 // RGBA = 4 bytes per pixel

	if len(data) >= expectedSize {
		// Copy pixel data (already in RGBA format)
		copy(img.Pix, data[:expectedSize])
	}

	// Store latest frame
	p.mu.Lock()
	p.latestFrame = img
	p.frameWidth = w
	p.frameHeight = h
	p.mu.Unlock()
}

// GetLatestFrame returns the most recent captured frame
func (p *GStreamerPipeline) GetLatestFrame() *image.RGBA {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.latestFrame == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	copyImg := image.NewRGBA(p.latestFrame.Bounds())
	copy(copyImg.Pix, p.latestFrame.Pix)
	return copyImg
}

// GetFrameSize returns the current frame dimensions
func (p *GStreamerPipeline) GetFrameSize() (width, height int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.frameWidth, p.frameHeight
}

// CropFrame extracts a region from the current frame
func (p *GStreamerPipeline) CropFrame(x, y, width, height int) *image.RGBA {
	p.mu.RLock()
	frame := p.latestFrame
	p.mu.RUnlock()

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
		for dx := 0; dx < width; dx++ {
			c := frame.RGBAAt(x+dx, y+dy)
			cropped.SetRGBA(dx, dy, c)
		}
	}

	return cropped
}

// IsRunning returns whether the pipeline is running
func (p *GStreamerPipeline) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// CreatePlaceholder creates a placeholder frame when capture isn't ready
func CreatePlaceholder(width, height int, message string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with dark gray
	bgColor := color.RGBA{40, 40, 50, 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, bgColor)
		}
	}

	return img
}
