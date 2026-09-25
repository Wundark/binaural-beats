//go:build android && !cgo

package engine

import "fmt"

// Play is not available on Android without CGO (Oboe requires it).
// Use ExportWAV or build with CGO_ENABLED=1 for real-time playback.
func (e *Engine) Play() error {
	return fmt.Errorf("real-time playback not available: this Android build was made without CGO, which Oboe requires. Use export_wav instead, or build with CGO_ENABLED=1")
}

// Stop is a no-op when playback is not available.
func (e *Engine) Stop() error {
	return fmt.Errorf("not playing")
}
