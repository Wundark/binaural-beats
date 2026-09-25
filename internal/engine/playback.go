//go:build !android || cgo

package engine

import (
	"fmt"
	"time"
)

// Play starts real-time audio playback. On Android this requires CGO (Oboe).
func (e *Engine) Play() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if e.IsPlaying {
		return fmt.Errorf("already playing")
	}
	if e.config == nil {
		return fmt.Errorf("no config loaded")
	}

	out, err := audioOutput()
	if err != nil {
		return err
	}

	e.playID++
	id := e.playID
	done := make(chan struct{})
	e.Done = done
	e.IsPlaying = true

	stream := e.newStream()
	stream.setVolumeNow(e.volume)
	stream.Seek(sampleRate.N(time.Duration(e.startAt * float64(time.Second))))
	e.startAt = 0
	e.stream = stream

	// onEnd runs on its own goroutine, so it never waits on e.Mu while the
	// output holds its lock (Stop holds e.Mu and takes the output lock).
	out.Play(stream, func() { e.finish(id) })
	return nil
}

// finish marks playback run id as ended, unless it was already stopped or superseded.
func (e *Engine) finish(id uint64) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if e.playID != id || !e.IsPlaying {
		return
	}
	e.IsPlaying = false
	e.stream = nil
	close(e.Done)
}

// Stop stops audio playback.
func (e *Engine) Stop() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}

	if out, err := audioOutput(); err == nil {
		out.Clear()
	}
	e.IsPlaying = false
	e.stream = nil
	close(e.Done)
	return nil
}
