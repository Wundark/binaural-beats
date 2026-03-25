//go:build darwin || windows

package engine

import (
	"fmt"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// Play starts real-time audio playback.
func (e *Engine) Play() error {
	e.Mu.Lock()
	if e.IsPlaying {
		e.Mu.Unlock()
		return fmt.Errorf("already playing")
	}
	if e.config == nil {
		e.Mu.Unlock()
		return fmt.Errorf("no config loaded")
	}

	mixedStreamer, sr := e.createMixer()

	speaker.Init(sr, sr.N(time.Second/10))

	e.Done = make(chan struct{})
	e.IsPlaying = true
	e.StartTime = time.Now()
	e.Mu.Unlock()

	speaker.Play(beep.Seq(mixedStreamer, beep.Callback(func() {
		e.Mu.Lock()
		e.IsPlaying = false
		close(e.Done)
		e.Mu.Unlock()
	})))

	return nil
}

// Stop stops audio playback.
func (e *Engine) Stop() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}

	speaker.Clear()
	e.IsPlaying = false
	close(e.Done)
	return nil
}
