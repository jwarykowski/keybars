// Package rgb is what keybars needs from a keyboard backend: colours, and a
// way to set them one LED at a time.
package rgb

// Colour is an RGB colour.
type Colour struct{ R, G, B uint8 }

// LEDs is what the renderer needs from a keyboard, so tests can record calls.
type LEDs interface {
	// Set colours one LED.
	Set(led int, c Colour) error
	// RestoreAll returns every LED to its layout colour.
	RestoreAll() error
}

// Flusher is a backend that buffers Set calls and sends them together, as
// OpenRGB does: one device update per frame instead of one per LED.
type Flusher interface {
	Flush() error
}

// Flush sends what l has buffered, if it buffers.
func Flush(l LEDs) error {
	if f, ok := l.(Flusher); ok {
		return f.Flush()
	}
	return nil
}
