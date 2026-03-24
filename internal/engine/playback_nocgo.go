//go:build (linux || android) && !cgo

package engine

import "fmt"

// Play is not available without CGO on Linux/Android (ALSA/oboe requires CGO).
// Use ExportWAV or build with CGO_ENABLED=1 for real-time playback.
func (e *Engine) Play() error {
	return fmt.Errorf("real-time playback not available: this binary was built without CGO (required for audio output on this platform). Use export_wav instead, or build from source with CGO_ENABLED=1")
}

// Stop is a no-op when playback is not available.
func (e *Engine) Stop() error {
	return fmt.Errorf("not playing")
}
