package engine

import (
	"math"
	"math/rand"
	"testing"

	"github.com/gopxl/beep"
)

func testChanges() []FrequencyChange {
	return []FrequencyChange{
		{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0},
		{Time: 1, Frequency: 300, BeatFrequency: 4, ToneVolume: 0.5, PinkNoiseVolume: 0.8},
		{Time: 1, Frequency: 300, BeatFrequency: 4, ToneVolume: 0.5, PinkNoiseVolume: 0.8},
		{Time: 3, Frequency: 150, BeatFrequency: 7, ToneVolume: 0.8, PinkNoiseVolume: 0},
	}
}

func seededNoise() *PinkNoise {
	return &PinkNoise{rand: rand.New(rand.NewSource(1)), maxKey: 0x1F}
}

// render streams n frames through buffers of size block, prefilling every
// buffer with fill before each Stream call.
func render(s beep.Streamer, n, block int, fill float64) [][2]float64 {
	out := make([][2]float64, 0, n)
	buf := make([][2]float64, block)
	for len(out) < n {
		m := block
		if n-len(out) < m {
			m = n - len(out)
		}
		for i := range buf[:m] {
			buf[i] = [2]float64{fill, -fill}
		}
		got, ok := s.Stream(buf[:m])
		if !ok || got != m {
			panic("short stream")
		}
		out = append(out, buf[:m]...)
	}
	return out
}

func TestStreamIgnoresPrefilledBuffer(t *testing.T) {
	sr := beep.SampleRate(44100)
	const n = 44100 * 2

	clean := render(NewBinauralStream(sr, testChanges(), seededNoise()), n, 512, 0)
	dirty := render(NewBinauralStream(sr, testChanges(), seededNoise()), n, 512, 0.75)

	for i := range clean {
		if clean[i] != dirty[i] {
			t.Fatalf("frame %d depends on buffer prefill: clean=%v dirty=%v", i, clean[i], dirty[i])
		}
	}
}

func TestStreamThroughMixerIgnoresPriorStreamers(t *testing.T) {
	sr := beep.SampleRate(44100)
	const n = 44100

	alone := render(NewBinauralStream(sr, testChanges(), seededNoise()), n, 512, 0)

	// beep.Mixer reuses one scratch buffer for every streamer; put a loud
	// streamer first so its samples would leak if the generator accumulated.
	loud := beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		for i := range s {
			s[i] = [2]float64{0.9, 0.9}
		}
		return len(s), true
	})
	m := &beep.Mixer{}
	m.Add(loud, NewBinauralStream(sr, testChanges(), seededNoise()))
	mixed := render(m, n, 512, 0)

	for i := range mixed {
		if math.Abs(mixed[i][0]-0.9-alone[i][0]) > 1e-12 || math.Abs(mixed[i][1]-0.9-alone[i][1]) > 1e-12 {
			t.Fatalf("frame %d differs through mixer: alone=%v mixed-0.9=%v", i, alone[i],
				[2]float64{mixed[i][0] - 0.9, mixed[i][1] - 0.9})
		}
	}
}

func TestTonesAudibleWithZeroNoiseVolume(t *testing.T) {
	sr := beep.SampleRate(44100)
	changes := []FrequencyChange{
		{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0},
		{Time: 2, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0},
	}
	out := render(NewBinauralStream(sr, changes, seededNoise()), 44100, 512, 0.5)

	var peakL, peakR float64
	for i, f := range out {
		peakL = math.Max(peakL, math.Abs(f[0]))
		peakR = math.Max(peakR, math.Abs(f[1]))
		// With no noise, each channel is exactly the tone.
		wantL := math.Sin(2*math.Pi*200*float64(i+1)/44100) * 0.5
		if math.Abs(f[0]-wantL) > 1e-6 {
			t.Fatalf("frame %d left = %v, want %v", i, f[0], wantL)
		}
	}
	if peakL < 0.49 || peakR < 0.49 {
		t.Fatalf("tones not audible with zero noise volume: peakL=%v peakR=%v", peakL, peakR)
	}
}

func TestIndependentVolumesAndChannels(t *testing.T) {
	sr := beep.SampleRate(44100)

	// Tone muted, noise on: channels carry identical noise.
	noiseOnly := []FrequencyChange{{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 0, PinkNoiseVolume: 1}}
	out := render(NewBinauralStream(sr, noiseOnly, seededNoise()), 4410, 512, 0.3)
	var energy float64
	for i, f := range out {
		if f[0] != f[1] {
			t.Fatalf("frame %d: noise differs between channels: %v", i, f)
		}
		energy += f[0] * f[0]
	}
	if energy == 0 {
		t.Fatal("noise silent with tone volume 0")
	}

	// Noise off: left and right differ by the beat frequency.
	tones := []FrequencyChange{{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0}}
	out = render(NewBinauralStream(sr, tones, seededNoise()), 44100, 512, 0)
	for i := 0; i < len(out); i += 997 {
		wantR := math.Sin(2*math.Pi*210*float64(i+1)/44100) * 0.5
		if math.Abs(out[i][1]-wantR) > 1e-6 {
			t.Fatalf("frame %d right = %v, want %v", i, out[i][1], wantR)
		}
	}
}

func TestParamsMatchInterpolate(t *testing.T) {
	changes := testChanges()
	bs := NewBinauralStream(44100, changes, seededNoise())
	freq := CreateFreqFunc(changes)
	beat := CreateBeatFreqFunc(changes)
	vol := CreateVolumeFunc(changes)
	pink := CreatePinkNoiseFunc(changes)
	for _, tm := range []float64{-1, 0, 0.25, 0.999, 1, 1.5, 2.999, 3, 5, 0.5} {
		f, b, v, p := bs.params(tm)
		if f != freq(tm) || b != beat(tm) || v != vol(tm) || p != pink(tm) {
			t.Fatalf("t=%v: params=(%v %v %v %v) interpolate=(%v %v %v %v)",
				tm, f, b, v, p, freq(tm), beat(tm), vol(tm), pink(tm))
		}
	}
}
