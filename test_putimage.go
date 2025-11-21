package main

import (
	"image"
	"image/color"
	"log"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/display"
)

func main() {
	// Create minimal config
	cfg := &config.DisplayConfig{
		Enabled: true,
		Width:   1920,
		Height:  1080,
		FPS:     10,
	}

	// Create display manager
	mgr, err := display.NewManager(cfg)
	if err != nil {
		log.Fatalf("Failed to create display manager: %v", err)
	}

	// Start display
	if err := mgr.Start(); err != nil {
		log.Fatalf("Failed to start display: %v", err)
	}
	defer mgr.Stop()

	log.Printf("Display created, window ID: %d", mgr.GetWindowID())

	// Create a test image with some color
	testImg := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			// Create a gradient
			r := uint8((x * 255) / 1920)
			g := uint8((y * 255) / 1080)
			b := uint8(128)
			testImg.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	log.Println("Created test image, attempting to render...")

	// This will call putImage internally and show diagnostics
	conn, _ := xgb.NewConn()
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	log.Printf("Screen depth: %d", screen.RootDepth)
	log.Printf("Available pixmap formats:")
	for _, format := range setup.PixmapFormats {
		log.Printf("  Depth=%d, BitsPerPixel=%d, ScanlinePad=%d",
			format.Depth, format.BitsPerPixel, format.ScanlinePad)
	}

	log.Printf("Root visual ID: %d", screen.RootVisual)

	// Now render - this should trigger the putImage diagnostics
	// We need to access the renderImage method, but it's private
	// So let's just call RenderWindow with a fake window ID to trigger ClearDisplay
	log.Println("Calling ClearDisplay to test putImage...")
	if err := mgr.ClearDisplay(); err != nil {
		log.Fatalf("Failed to clear display (this tests putImage): %v", err)
	}

	log.Println("SUCCESS! putImage worked without errors")
}
