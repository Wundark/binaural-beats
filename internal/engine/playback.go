//go:build darwin || windows || ((linux || android) && cgo)

package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// playbackSampleRate is the rate the speaker is initialised with. Beep's speaker
// can only be initialised once per process, so every stream must use this rate.
const playbackSampleRate = beep.SampleRate(44100)

// playbackBuffer is the total speaker buffer, split between driver and player.
const playbackBuffer = 100 * time.Millisecond

var (
	speakerOnce sync.Once
	speakerErr  error
)

func initSpeaker() error {
	speakerOnce.Do(func() {
		speakerErr = speaker.Init(playbackSampleRate, playbackSampleRate.N(playbackBuffer))
	})
	return speakerErr
}

// Play starts real-time audio playback. On Linux/Android this requires CGO (ALSA/oboe).
func (e *Engine) Play() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if e.IsPlaying {
		return fmt.Errorf("already playing")
	}
	if e.config == nil {
		return fmt.Errorf("no config loaded")
	}

	mixedStreamer, sr := e.createMixer()
	if sr != playbackSampleRate {
		return fmt.Errorf("stream sample rate %d does not match speaker rate %d", sr, playbackSampleRate)
	}
	if err := initSpeaker(); err != nil {
		return fmt.Errorf("failed to initialise audio output: %w", err)
	}

	e.playID++
	id := e.playID
	done := make(chan struct{})
	e.Done = done
	e.IsPlaying = true
	e.StartTime = time.Now()

	speaker.Play(beep.Seq(mixedStreamer, beep.Callback(func() {
		// The callback runs with the speaker lock held; finish asynchronously so
		// we never wait on e.Mu while Stop may hold it and wait on the speaker.
		go e.finish(id)
	})))

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
	close(e.Done)
}

// Stop stops audio playback.
func (e *Engine) Stop() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}

	// Safe to take the speaker lock here: the end-of-stream callback never
	// blocks on e.Mu while holding it.
	speaker.Clear()
	e.IsPlaying = false
	close(e.Done)
	return nil
}
