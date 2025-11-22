package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TextWidget displays text on the overlay
type TextWidget struct {
	*BaseWidget
	text      string
	fontSize  int
	textColor color.RGBA
	bgColor   *color.RGBA // Optional background color
	padding   int
}

// NewTextWidget creates a new text widget
func NewTextWidget(id string, config map[string]interface{}) (*TextWidget, error) {
	w := &TextWidget{
		BaseWidget: NewBaseWidget(id, 0, 0, 1.0),
		text:       "Text Widget",
		fontSize:   13, // basicfont size
		textColor:  color.RGBA{255, 255, 255, 255},
		padding:    5,
	}

	if err := w.UpdateConfig(config); err != nil {
		return nil, err
	}

	return w, nil
}

// Type returns the widget type
func (w *TextWidget) Type() string {
	return "text"
}

// Render draws the text widget
func (w *TextWidget) Render(img *image.RGBA) error {
	if !w.IsEnabled() || w.text == "" {
		return nil
	}

	// Use basicfont for now (simple, no external dependencies)
	face := basicfont.Face7x13

	// Measure text dimensions
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(w.textColor),
		Face: face,
	}

	textWidth := d.MeasureString(w.text)
	textWidthPx := int(textWidth >> 6) // Convert from fixed.Int26_6 to pixels

	// Calculate widget dimensions with padding
	widgetWidth := textWidthPx + w.padding*2
	widgetHeight := w.fontSize + w.padding*2

	// Draw background if configured
	if w.bgColor != nil {
		bgImg := image.NewRGBA(image.Rect(0, 0, widgetWidth, widgetHeight))
		draw.Draw(bgImg, bgImg.Bounds(), &image.Uniform{*w.bgColor}, image.Point{}, draw.Src)
		BlendImage(img, bgImg, w.x, w.y, w.opacity)
	}

	// Draw text
	textX := w.x + w.padding
	textY := w.y + w.padding + w.fontSize

	// Create a temporary image for the text with alpha
	textImg := image.NewRGBA(image.Rect(0, 0, textWidthPx, w.fontSize))
	textDrawer := &font.Drawer{
		Dst:  textImg,
		Src:  image.NewUniform(w.textColor),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: fixed.I(w.fontSize)},
	}
	textDrawer.DrawString(w.text)

	// Blend text onto image with opacity
	BlendImage(img, textImg, textX, textY-w.fontSize, w.opacity)

	return nil
}

// GetConfig returns the widget configuration
func (w *TextWidget) GetConfig() map[string]interface{} {
	config := map[string]interface{}{
		"id":      w.id,
		"type":    w.Type(),
		"enabled": w.enabled,
		"x":       w.x,
		"y":       w.y,
		"opacity": w.opacity,
		"text":    w.text,
		"padding": w.padding,
		"color": map[string]interface{}{
			"r": w.textColor.R,
			"g": w.textColor.G,
			"b": w.textColor.B,
			"a": w.textColor.A,
		},
	}

	if w.bgColor != nil {
		config["background"] = map[string]interface{}{
			"r": w.bgColor.R,
			"g": w.bgColor.G,
			"b": w.bgColor.B,
			"a": w.bgColor.A,
		}
	}

	return config
}

// UpdateConfig updates the widget configuration
func (w *TextWidget) UpdateConfig(config map[string]interface{}) error {
	if text, ok := config["text"].(string); ok {
		w.text = text
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

	if padding, ok := config["padding"].(float64); ok {
		w.padding = int(padding)
	} else if padding, ok := config["padding"].(int); ok {
		w.padding = padding
	}

	// Parse color
	if colorMap, ok := config["color"].(map[string]interface{}); ok {
		r := uint8(getInt(colorMap["r"]))
		g := uint8(getInt(colorMap["g"]))
		b := uint8(getInt(colorMap["b"]))
		a := uint8(getInt(colorMap["a"]))
		w.textColor = color.RGBA{R: r, G: g, B: b, A: a}
	}

	// Parse background color
	if bgMap, ok := config["background"].(map[string]interface{}); ok {
		r := uint8(getInt(bgMap["r"]))
		g := uint8(getInt(bgMap["g"]))
		b := uint8(getInt(bgMap["b"]))
		a := uint8(getInt(bgMap["a"]))
		w.bgColor = &color.RGBA{R: r, G: g, B: b, A: a}
	}

	return nil
}

// getInt extracts an integer value from an interface{} that might be int or float64
func getInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

// SetText updates the text content
func (w *TextWidget) SetText(text string) {
	w.text = text
}

// GetText returns the current text
func (w *TextWidget) GetText() string {
	return w.text
}

// SetColor sets the text color
func (w *TextWidget) SetColor(c color.RGBA) {
	w.textColor = c
}

// SetBackground sets the background color (nil for transparent)
func (w *TextWidget) SetBackground(c *color.RGBA) {
	w.bgColor = c
}

// Validate ensures the widget configuration is valid
func (w *TextWidget) Validate() error {
	if w.text == "" {
		return fmt.Errorf("text widget requires non-empty text")
	}
	return nil
}
