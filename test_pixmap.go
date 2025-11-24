package main

import (
	"image"
	"image/color"
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
)

func main() {
	conn, err := xgb.NewConn()
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	// Create window
	win, _ := xproto.NewWindowId(conn)
	xproto.CreateWindow(conn, screen.RootDepth, win, screen.Root,
		0, 0, 1920, 1080, 0,
		xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{0x000000, xproto.EventMaskExposure})
	xproto.MapWindow(conn, win)

	// Create pixmap
	pix, _ := xproto.NewPixmapId(conn)
	xproto.CreatePixmap(conn, screen.RootDepth, pix, xproto.Drawable(win), 1920, 1080)
	log.Printf("Created pixmap %d with depth %d", pix, screen.RootDepth)

	// Create GC
	gc, _ := xproto.NewGcontextId(conn)
	xproto.CreateGC(conn, gc, xproto.Drawable(pix), 0, nil)
	
	conn.Sync()
	time.Sleep(100 * time.Millisecond)

	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			img.Set(x, y, color.RGBA{R: uint8((x * 255) / 1920), G: uint8((y * 255) / 1080), B: 128, A: 255})
		}
	}

	// Convert to BGRx format
	data := make([]byte, 1920*1080*4)
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			srcIdx := (y*1920 + x) * 4
			dstIdx := (y*1920 + x) * 4
			data[dstIdx] = img.Pix[srcIdx+2]     // B
			data[dstIdx+1] = img.Pix[srcIdx+1]   // G
			data[dstIdx+2] = img.Pix[srcIdx]     // R
			data[dstIdx+3] = 0                    // padding
		}
	}

	log.Printf("Trying PutImage to pixmap (not window)...")
	
	// Try PutImage to pixmap instead of window
	err = xproto.PutImageChecked(conn,
		xproto.ImageFormatZPixmap,
		xproto.Drawable(pix),  // Draw to pixmap, not window
		gc,
		1920, 1080,
		0, 0, 0,
		screen.RootDepth,
		data).Check()

	if err != nil {
		log.Fatalf("PutImage to pixmap failed: %v", err)
	}

	log.Println("SUCCESS! PutImage to pixmap worked!")

	// Now copy from pixmap to window
	err = xproto.CopyAreaChecked(conn,
		xproto.Drawable(pix),
		xproto.Drawable(win),
		gc,
		0, 0, 0, 0,
		1920, 1080).Check()

	if err != nil {
		log.Fatalf("CopyArea failed: %v", err)
	}

	log.Println("SUCCESS! CopyArea worked! Window should now show the image!")
	conn.Sync()
	
	time.Sleep(5 * time.Second)  // Keep window visible
}
