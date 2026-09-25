package engine

import (
	"math"
	"testing"

	"github.com/gopxl/beep"
)

func steadyChanges() []FrequencyChange {
	return []FrequencyChange{
		{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0.5},
		{Time: 100, Frequency: 200, BeatFrequency: 10, ToneVolume: 1, PinkNoiseVolume: 0.5},
	}
}

func streamN(s beep.Streamer, n int) [][2]float64 {
	buf := make([][2]float64, n)
	got, _ := s.Stream(buf)
	return buf[:got]
}

func TestStreamEndsAtTotal(t *testing.T) {
	s := NewBinauralStream(44100, steadyChanges(), 1000, seededNoise())
	buf := make([][2]float64, 512)
	if n, ok := s.Stream(buf); n != 512 || !ok {
		t.Fatalf("first buffer: n=%d ok=%v", n, ok)
	}
	if n, ok := s.Stream(buf); n != 488 || !ok {
		t.Fatalf("partial buffer: n=%d ok=%v", n, ok)
	}
	if n, ok := s.Stream(buf); n != 0 || ok {
		t.Fatalf("drained stream: n=%d ok=%v", n, ok)
	}
	if s.Position() != 1000 {
		t.Fatalf("position %d, want 1000", s.Position())
	}
}

func TestPauseHoldsPositionAndResumeContinues(t *testing.T) {
	ref := NewBinauralStream(44100, steadyChanges(), 1<<30, seededNoise())
	s := NewBinauralStream(44100, steadyChanges(), 1<<30, seededNoise())
	refOut := streamN(ref, 20000)

	streamN(s, 1000)
	s.Pause()
	out := streamN(s, 4000)
	ramp := int(pauseRampSeconds * 44100)
	held := s.Position()
	if held != 1000+ramp && held != 1000+ramp+1 {
		t.Fatalf("paused position %d, want about %d", held, 1000+ramp)
	}
	for i := ramp + 1; i < len(out); i++ {
		if out[i] != ([2]float64{}) {
			t.Fatalf("sample %d after the pause ramp is not silent: %v", i, out[i])
		}
	}
	streamN(s, 4000)
	if s.Position() != held {
		t.Fatalf("position moved while paused: %d -> %d", held, s.Position())
	}

	s.Resume()
	out = streamN(s, 3000)
	// After the resume ramp the output matches uninterrupted playback at the
	// same position, so the tones and noise pick up where they stopped.
	for i := ramp + 2; i < len(out); i++ {
		want := refOut[held+i]
		if math.Abs(out[i][0]-want[0]) > 1e-9 || math.Abs(out[i][1]-want[1]) > 1e-9 {
			t.Fatalf("resumed sample %d = %v, want %v", i, out[i], want)
		}
	}
}

func TestPauseRampHasNoJump(t *testing.T) {
	s := NewBinauralStream(44100, steadyChanges(), 1<<30, seededNoise())
	prev := streamN(s, 500)[499]
	s.Pause()
	for _, f := range streamN(s, 2000) {
		// A full-scale step would be ~1; the ramp keeps steps small.
		if math.Abs(f[0]-prev[0]) > 0.2 {
			t.Fatalf("pause produced a jump from %v to %v", prev, f)
		}
		prev = f
	}
}

func TestSeek(t *testing.T) {
	changes := []FrequencyChange{
		{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1},
		{Time: 1, Frequency: 200, BeatFrequency: 10, ToneVolume: 1},
		{Time: 1, Frequency: 200, BeatFrequency: 10, ToneVolume: 0}, // silent from 1s
		{Time: 2, Frequency: 200, BeatFrequency: 10, ToneVolume: 0},
	}
	s := NewBinauralStream(44100, changes, 2*44100, seededNoise())
	streamN(s, 100)
	s.Seek(60000)
	if s.Position() != 60000 {
		t.Fatalf("position after seek %d", s.Position())
	}
	for _, f := range streamN(s, 1000) {
		if f != ([2]float64{}) {
			t.Fatalf("expected silence after seeking past 1s, got %v", f)
		}
	}
	// Seeking back re-finds the earlier, audible segment.
	s.Seek(0)
	var peak float64
	for _, f := range streamN(s, 1000) {
		peak = math.Max(peak, math.Abs(f[0]))
	}
	if peak < 0.4 {
		t.Fatalf("expected the tone after seeking back to 0, peak %v", peak)
	}
	s.Seek(1 << 40)
	if s.Position() != 2*44100 {
		t.Fatalf("seek past the end should clamp, got %d", s.Position())
	}
}

func TestVolumeRampsToTarget(t *testing.T) {
	ref := NewBinauralStream(44100, steadyChanges(), 1<<30, seededNoise())
	s := NewBinauralStream(44100, steadyChanges(), 1<<30, seededNoise())
	refOut := streamN(ref, 44100)
	s.SetVolume(0.25)
	out := streamN(s, 44100)
	// First sample barely changes (ramped), later samples are scaled by 0.25.
	if math.Abs(out[0][0]-refOut[0][0]) > 0.01 {
		t.Fatalf("volume change was not ramped: %v vs %v", out[0], refOut[0])
	}
	for i := 22050; i < len(out); i += 101 {
		if math.Abs(out[i][0]-0.25*refOut[i][0]) > 1e-6 {
			t.Fatalf("sample %d = %v, want %v", i, out[i][0], 0.25*refOut[i][0])
		}
	}
}

func TestEngineSeekAndVolumeWhileStopped(t *testing.T) {
	e := NewEngine()
	if err := e.Seek(10); err == nil {
		t.Fatal("seek without a config should fail")
	}
	if _, err := e.LoadConfig(writeConfig(t, `frequency_changes:
  - time: 0
    frequency: 100
    beat_frequency: 4
    tone_volume: 1
  - time: 100
    frequency: 300
    beat_frequency: 12
    tone_volume: 1
`)); err != nil {
		t.Fatal(err)
	}
	if err := e.Seek(50); err != nil {
		t.Fatal(err)
	}
	st := e.GetStatus()
	if st.Time != 50 || st.Frequency != 200 || st.BeatFrequency != 8 || st.IsPlaying {
		t.Fatalf("status at the start position: %+v", st)
	}
	// Stretching keeps the start position at the same point in the session.
	if err := e.SetStretch(2); err != nil {
		t.Fatal(err)
	}
	if st := e.GetStatus(); st.Time != 100 || st.Frequency != 200 {
		t.Fatalf("status after stretch: %+v", st)
	}
	if err := e.Seek(1e9); err != nil || e.GetStatus().Time != 200 {
		t.Fatalf("seek should clamp to the session length: %v %+v", err, e.GetStatus())
	}

	for _, bad := range []float64{-0.1, 1.1, math.NaN()} {
		if err := e.SetVolume(bad); err == nil {
			t.Errorf("SetVolume(%v) should fail", bad)
		}
	}
	if err := e.SetVolume(0.3); err != nil || e.GetStatus().Volume != 0.3 {
		t.Fatalf("SetVolume(0.3): %v %+v", err, e.GetStatus())
	}
	if err := e.Pause(); err == nil {
		t.Fatal("pause while stopped should fail")
	}
}
