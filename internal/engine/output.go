//go:build !android || cgo

package engine

import (
	"encoding/binary"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gopxl/beep"
)

// output plays one stream at a time through the system audio device.
//
// It replaces beep's speaker package, which cannot report a failed audio
// device: oto initialises asynchronously, and the speaker never checked the
// result, so playback appeared to start but never advanced.
type output struct {
	player *oto.Player

	mu     sync.Mutex
	stream beep.Streamer // nil when idle (silence)
	onEnd  func()
	buf    [][2]float64
}

// outputBuffer is the total output latency, split between driver and player.
const outputBuffer = 100 * time.Millisecond

const bytesPerFrame = 2 * 4 // stereo float32

var (
	outputOnce sync.Once
	theOutput  *output
	outputErr  error
)

// audioOutput returns the process-wide audio output, opening the device on
// first use. oto supports only one context per process.
func audioOutput() (*output, error) {
	outputOnce.Do(func() {
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:      int(sampleRate),
			ChannelCount:    2,
			Format:          oto.FormatFloat32LE,
			BufferSize:      outputBuffer / 2,
			ApplicationName: "Binaural Beats",
		})
		if err != nil {
			log.Printf("audio output: %v", err)
			outputErr = err
			return
		}
		<-ready
		if err := ctx.Err(); err != nil {
			log.Printf("audio output: %v", err)
			outputErr = err
			return
		}
		o := &output{}
		o.player = ctx.NewPlayer(o)
		o.player.SetBufferSize(sampleRate.N(outputBuffer/2) * bytesPerFrame)
		o.player.Play()
		theOutput = o
	})
	if outputErr != nil {
		// oto's error lists every device it tried; keep that out of the UI.
		return nil, errors.New("no audio output device available")
	}
	return theOutput, nil
}

// Play starts s, replacing anything playing. onEnd is called (on its own
// goroutine) when s runs out, but not if it is replaced or cleared first.
func (o *output) Play(s beep.Streamer, onEnd func()) {
	o.mu.Lock()
	o.stream, o.onEnd = s, onEnd
	o.mu.Unlock()
}

// Clear stops the current stream.
func (o *output) Clear() {
	o.mu.Lock()
	o.stream, o.onEnd = nil, nil
	o.mu.Unlock()
}

// Err reports an audio device error after playback started, if any.
func (o *output) Err() error { return o.player.Err() }

// Read implements io.Reader for the oto player, which pulls continuously.
func (o *output) Read(p []byte) (int, error) {
	frames := len(p) / bytesPerFrame
	if cap(o.buf) < frames {
		o.buf = make([][2]float64, frames)
	}
	buf := o.buf[:frames]

	o.mu.Lock()
	n := 0
	if o.stream != nil {
		var ok bool
		n, ok = o.stream.Stream(buf)
		if !ok || n < frames {
			// The stream ran out: report it once, then play silence.
			if onEnd := o.onEnd; onEnd != nil {
				go onEnd()
			}
			o.stream, o.onEnd = nil, nil
		}
	}
	o.mu.Unlock()

	for i := n; i < frames; i++ {
		buf[i] = [2]float64{}
	}
	for i, f := range buf {
		for c := 0; c < 2; c++ {
			v := math.Max(-1, math.Min(1, f[c]))
			binary.LittleEndian.PutUint32(p[i*bytesPerFrame+c*4:], math.Float32bits(float32(v)))
		}
	}
	return frames * bytesPerFrame, nil
}
