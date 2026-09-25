//go:build !android || cgo

package engine

import (
	"fmt"
)

// Play starts real-time audio playback. If the loaded session is in the
// playlist, the rest of the playlist follows it.
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
	stream.Seek(samples(e.startAt))
	e.startAt = 0
	e.player = newSequence(stream, e.playItems(), e.plIndex, e.loop, samples(e.crossfade), e.volume)

	// onEnd runs on its own goroutine, so it never waits on e.Mu while the
	// output holds its lock (Stop holds e.Mu and takes the output lock).
	out.Play(e.player, func() { e.finish(id) })
	return nil
}

// finish marks playback run id as ended, unless it was already stopped or superseded.
func (e *Engine) finish(id uint64) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if e.playID != id || !e.IsPlaying {
		return
	}
	e.stopLocked()
}

// Stop stops audio playback. The session that was playing stays loaded.
func (e *Engine) Stop() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}

	if out, err := audioOutput(); err == nil {
		out.Clear()
	}
	e.stopLocked()
	return nil
}

func (e *Engine) stopLocked() {
	e.syncPlaying()
	e.IsPlaying = false
	e.player = nil
	close(e.Done)
}
