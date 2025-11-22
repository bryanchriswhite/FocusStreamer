package overlay

import (
	"image"
	"image/color"
	"image/draw"
)

// Widget represents a renderable overlay widget
type Widget interface {
	// ID returns the unique identifier for this widget instance
	ID() string

	// Type returns the widget type name
	Type() string

	// Render draws the widget onto the provided image at the configured position
	Render(img *image.RGBA) error

	// GetConfig returns the widget's configuration as a map
	GetConfig() map[string]interface{}

	// UpdateConfig updates the widget's configuration
	UpdateConfig(config map[string]interface{}) error

	// IsEnabled returns whether the widget should be rendered
	IsEnabled() bool

	// SetEnabled sets whether the widget should be rendered
	SetEnabled(enabled bool)
}

// BaseWidget provides common functionality for all widgets
type BaseWidget struct {
	id      string
	enabled bool
	x       int
	y       int
	opacity float64 // 0.0 to 1.0
}

// NewBaseWidget creates a new base widget
func NewBaseWidget(id string, x, y int, opacity float64) *BaseWidget {
	return &BaseWidget{
		id:      id,
		enabled: true,
		x:       x,
		y:       y,
		opacity: opacity,
	}
}

// ID returns the widget's unique identifier
func (w *BaseWidget) ID() string {
	return w.id
}

// IsEnabled returns whether the widget should be rendered
func (w *BaseWidget) IsEnabled() bool {
	return w.enabled
}

// SetEnabled sets whether the widget should be rendered
func (w *BaseWidget) SetEnabled(enabled bool) {
	w.enabled = enabled
}

// GetPosition returns the widget's position
func (w *BaseWidget) GetPosition() (int, int) {
	return w.x, w.y
}

// SetPosition sets the widget's position
func (w *BaseWidget) SetPosition(x, y int) {
	w.x = x
	w.y = y
}

// GetOpacity returns the widget's opacity
func (w *BaseWidget) GetOpacity() float64 {
	return w.opacity
}

// SetOpacity sets the widget's opacity (0.0 to 1.0)
func (w *BaseWidget) SetOpacity(opacity float64) {
	if opacity < 0.0 {
		opacity = 0.0
	}
	if opacity > 1.0 {
		opacity = 1.0
	}
	w.opacity = opacity
}

// BlendImage blends a source image onto a destination image at the given position
// with the specified opacity
func BlendImage(dst *image.RGBA, src image.Image, x, y int, opacity float64) {
	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()

	// Calculate intersection to handle clipping
	for sy := srcBounds.Min.Y; sy < srcBounds.Max.Y; sy++ {
		dy := y + (sy - srcBounds.Min.Y)
		if dy < dstBounds.Min.Y || dy >= dstBounds.Max.Y {
			continue
		}

		for sx := srcBounds.Min.X; sx < srcBounds.Max.X; sx++ {
			dx := x + (sx - srcBounds.Min.X)
			if dx < dstBounds.Min.X || dx >= dstBounds.Max.X {
				continue
			}

			// Get source pixel
			srcColor := src.At(sx, sy)
			sr, sg, sb, sa := srcColor.RGBA()

			// Apply opacity to source alpha
			alpha := float64(sa) * opacity / 65535.0

			if alpha > 0 {
				// Get destination pixel
				dstColor := dst.At(dx, dy)
				dr, dg, db, da := dstColor.RGBA()

				// Alpha blending
				outAlpha := alpha + float64(da)/65535.0*(1-alpha)
				if outAlpha > 0 {
					outR := uint8((float64(sr)*alpha + float64(dr)/65535.0*float64(da)/65535.0*(1-alpha)) / outAlpha / 256)
					outG := uint8((float64(sg)*alpha + float64(dg)/65535.0*float64(da)/65535.0*(1-alpha)) / outAlpha / 256)
					outB := uint8((float64(sb)*alpha + float64(db)/65535.0*float64(da)/65535.0*(1-alpha)) / outAlpha / 256)
					outA := uint8(outAlpha * 255)

					dst.SetRGBA(dx, dy, color.RGBA{R: outR, G: outG, B: outB, A: outA})
				}
			}
		}
	}
}

// DrawRectangle draws a filled rectangle with the specified color and opacity
func DrawRectangle(dst *image.RGBA, x, y, width, height int, color image.Image, opacity float64) {
	rect := image.Rect(x, y, x+width, y+height)

	// Create a temporary image for the rectangle
	tmp := image.NewRGBA(rect)
	draw.Draw(tmp, rect, color, image.Point{}, draw.Src)

	// Blend onto destination with opacity
	BlendImage(dst, tmp, x, y, opacity)
}
