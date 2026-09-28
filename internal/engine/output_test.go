//go:build !android || cgo

package engine

import (
	"encoding/binary"
	"math"
	"testing"
)

func readFrames(o *output, frames int) [][2]float32 {
	p := make([]byte, frames*bytesPerFrame)
	o.Read(p)
	out := make([][2]float32, frames)
	for i := range out {
		for c := 0; c < 2; c++ {
			out[i][c] = math.Float32frombits(binary.LittleEndian.Uint32(p[i*bytesPerFrame+c*4:]))
		}
	}
	return out
}

func TestClearFadesOutWithoutCallingOnEnd(t *testing.T) {
	o := &output{}
	ended := make(chan struct{}, 1)
	s := NewBinauralStream(sampleRate, steadyChanges(), 1<<30, seededNoise())
	o.Play(s, func() { ended <- struct{}{} })
	readFrames(o, 2048)

	o.Clear()
	ramp := int(pauseRampSeconds * float64(sampleRate))
	out := readFrames(o, ramp*2)
	// The fade starts near full level and reaches silence by the ramp's end.
	peak := func(from, to int) float64 {
		m := 0.0
		for _, f := range out[from:to] {
			m = math.Max(m, math.Abs(float64(f[0])))
		}
		return m
	}
	if first, last := peak(0, 50), peak(ramp-50, ramp); first < 0.2 || last > first*0.1 {
		t.Fatalf("fade: first %.3f, last %.3f", first, last)
	}
	if peak(ramp, 2*ramp) != 0 {
		t.Fatal("output not silent after the fade")
	}
	if o.stream != nil {
		t.Fatal("stream kept after the fade")
	}
	select {
	case <-ended:
		t.Fatal("onEnd called after Clear")
	default:
	}
}

func TestPlayAfterClearCancelsFade(t *testing.T) {
	o := &output{}
	o.Play(NewBinauralStream(sampleRate, steadyChanges(), 1<<30, seededNoise()), nil)
	o.Clear()
	o.Play(NewBinauralStream(sampleRate, steadyChanges(), 1<<30, seededNoise()), nil)
	readFrames(o, 4096)
	if o.stream == nil {
		t.Fatal("new stream dropped by the earlier Clear")
	}
}
