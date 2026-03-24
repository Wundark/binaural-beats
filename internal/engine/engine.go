package engine

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/wav"
	"gopkg.in/yaml.v3"
)

// Config represents the structure of the YAML configuration file.
type Config struct {
	FrequencyChanges []FrequencyChange `yaml:"frequency_changes"`
}

// FrequencyChange represents a frequency change event.
type FrequencyChange struct {
	Time            float64 `yaml:"time" json:"time"`
	Frequency       float64 `yaml:"frequency" json:"frequency"`
	BeatFrequency   float64 `yaml:"beat_frequency" json:"beat_frequency"`
	PinkNoiseVolume float64 `yaml:"pink_noise_volume" json:"pink_noise_volume"`
	ToneVolume      float64 `yaml:"tone_volume" json:"tone_volume"`
}

// Status represents the current playback status.
type Status struct {
	Time            float64 `json:"time"`
	Frequency       float64 `json:"frequency"`
	BeatFrequency   float64 `json:"beat_frequency"`
	ToneVolume      float64 `json:"tone_volume"`
	PinkNoiseVolume float64 `json:"pink_noise_volume"`
	TotalDuration   float64 `json:"total_duration"`
	IsPlaying       bool    `json:"is_playing"`
	ConfigLoaded    bool    `json:"config_loaded"`
}

// PinkNoise implements a pink noise generator using the Voss-McCartney algorithm.
type PinkNoise struct {
	rand   *rand.Rand
	maxKey uint32
	key    uint32
	white  [5]float64
}

func NewPinkNoise() *PinkNoise {
	return &PinkNoise{
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
		maxKey: 0x1F,
	}
}

func (pn *PinkNoise) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		sample := pn.nextSample()
		samples[i][0] += sample
		samples[i][1] += sample
	}
	return len(samples), true
}

func (pn *PinkNoise) Err() error { return nil }

func (pn *PinkNoise) nextSample() float64 {
	lastKey := pn.key
	pn.key++
	if pn.key > pn.maxKey {
		pn.key = 0
	}
	diff := lastKey ^ pn.key
	for i := 0; i < 5; i++ {
		if diff&(1<<uint(i)) != 0 {
			pn.white[i] = pn.rand.Float64()*2 - 1
		}
	}
	return (pn.white[0] + pn.white[1] + pn.white[2] + pn.white[3] + pn.white[4]) * 0.1
}

// VariableTone generates a sine wave with a frequency that changes over time.
type VariableTone struct {
	sr         beep.SampleRate
	pos        int
	phase      float64
	freqFunc   func(t float64) float64
	volumeFunc func(t float64) float64
	channel    int
}

func (vt *VariableTone) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(vt.pos) / float64(vt.sr)
		f := vt.freqFunc(t)
		vol := vt.volumeFunc(t)
		deltaPhase := 2 * math.Pi * f / float64(vt.sr)
		vt.phase += deltaPhase
		s := math.Sin(vt.phase) * vol * 0.5
		if vt.channel == 0 {
			samples[i][0] += s
		} else {
			samples[i][1] += s
		}
		vt.pos++
	}
	return len(samples), true
}

func (vt *VariableTone) Err() error { return nil }

// PinkNoiseControl controls the pink noise based on time.
type PinkNoiseControl struct {
	stream     beep.Streamer
	volumeFunc func(t float64) float64
	sr         beep.SampleRate
	pos        int
}

func (pnc *PinkNoiseControl) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = pnc.stream.Stream(samples)
	for i := range samples[:n] {
		t := float64(pnc.pos) / float64(pnc.sr)
		vol := pnc.volumeFunc(t)
		if vol <= 0 {
			samples[i][0] = 0
			samples[i][1] = 0
		} else {
			s := samples[i][0] * vol * 0.5
			samples[i][0] = s
			samples[i][1] = s
		}
		pnc.pos++
	}
	return n, ok
}

func (pnc *PinkNoiseControl) Err() error { return pnc.stream.Err() }

// ParseConfig reads and parses a YAML configuration file.
func ParseConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	sort.Slice(cfg.FrequencyChanges, func(i, j int) bool {
		return cfg.FrequencyChanges[i].Time < cfg.FrequencyChanges[j].Time
	})
	return &cfg, nil
}

func CreateFreqFunc(changes []FrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		return interpolate(changes, t, func(c FrequencyChange) float64 { return c.Frequency })
	}
}

func CreateBeatFreqFunc(changes []FrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		return interpolate(changes, t, func(c FrequencyChange) float64 { return c.BeatFrequency })
	}
}

func CreateVolumeFunc(changes []FrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		if len(changes) == 0 {
			return 1.0
		}
		return interpolate(changes, t, func(c FrequencyChange) float64 { return c.ToneVolume })
	}
}

func CreatePinkNoiseFunc(changes []FrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		return interpolate(changes, t, func(c FrequencyChange) float64 { return c.PinkNoiseVolume })
	}
}

func interpolate(changes []FrequencyChange, t float64, getValue func(FrequencyChange) float64) float64 {
	if len(changes) == 0 {
		return 0
	}
	if t <= changes[0].Time {
		return getValue(changes[0])
	}
	if t >= changes[len(changes)-1].Time {
		return getValue(changes[len(changes)-1])
	}
	for i := 0; i < len(changes)-1; i++ {
		if t >= changes[i].Time && t < changes[i+1].Time {
			t1 := changes[i].Time
			t2 := changes[i+1].Time
			v1 := getValue(changes[i])
			v2 := getValue(changes[i+1])
			return v1 + (v2-v1)*(t-t1)/(t2-t1)
		}
	}
	return getValue(changes[len(changes)-1])
}

func GetTotalPlaybackTime(changes []FrequencyChange) float64 {
	if len(changes) == 0 {
		return 0
	}
	maxTime := changes[0].Time
	for _, c := range changes {
		if c.Time > maxTime {
			maxTime = c.Time
		}
	}
	return maxTime
}

// createMixer creates the audio mixer from the current config state.
func (e *Engine) createMixer() (beep.Streamer, beep.SampleRate) {
	sr := beep.SampleRate(44100)

	leftTone := &VariableTone{
		sr:         sr,
		freqFunc:   e.baseFreqFunc,
		volumeFunc: e.volumeFunc,
		channel:    0,
	}
	rightTone := &VariableTone{
		sr:         sr,
		freqFunc:   func(t float64) float64 { return e.baseFreqFunc(t) + e.beatFreqFunc(t) },
		volumeFunc: e.volumeFunc,
		channel:    1,
	}
	pinkNoiseControl := &PinkNoiseControl{
		stream:     NewPinkNoise(),
		volumeFunc: e.pinkNoiseFunc,
		sr:         sr,
	}

	mixed := &beep.Mixer{}
	mixed.Add(leftTone, rightTone, pinkNoiseControl)

	totalSamples := sr.N(time.Duration(e.totalDuration * float64(time.Second)))
	return beep.Take(totalSamples, mixed), sr
}

// Engine manages the audio generation and playback lifecycle.
type Engine struct {
	Mu            sync.Mutex
	config        *Config
	stretch       float64
	IsPlaying     bool
	StartTime     time.Time
	Done          chan struct{}
	baseFreqFunc  func(float64) float64
	beatFreqFunc  func(float64) float64
	volumeFunc    func(float64) float64
	pinkNoiseFunc func(float64) float64
	totalDuration float64
}

func NewEngine() *Engine {
	return &Engine{stretch: 1.0}
}

func (e *Engine) LoadConfig(path string) error {
	cfg, err := ParseConfig(path)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	e.Mu.Lock()
	defer e.Mu.Unlock()

	if e.IsPlaying {
		return fmt.Errorf("cannot load config while playing")
	}

	e.config = cfg
	e.applyStretch()
	return nil
}

func (e *Engine) applyStretch() {
	if e.config == nil {
		return
	}
	changes := make([]FrequencyChange, len(e.config.FrequencyChanges))
	copy(changes, e.config.FrequencyChanges)
	for i := range changes {
		changes[i].Time *= e.stretch
	}
	e.baseFreqFunc = CreateFreqFunc(changes)
	e.beatFreqFunc = CreateBeatFreqFunc(changes)
	e.volumeFunc = CreateVolumeFunc(changes)
	e.pinkNoiseFunc = CreatePinkNoiseFunc(changes)
	e.totalDuration = GetTotalPlaybackTime(changes)
}

func (e *Engine) SetStretch(factor float64) error {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if e.IsPlaying {
		return fmt.Errorf("cannot change stretch while playing")
	}
	if factor <= 0 {
		return fmt.Errorf("stretch factor must be positive")
	}
	e.stretch = factor
	e.applyStretch()
	return nil
}

func (e *Engine) GetStatus() Status {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	s := Status{
		IsPlaying:    e.IsPlaying,
		ConfigLoaded: e.config != nil,
	}
	if e.config != nil {
		s.TotalDuration = e.totalDuration
	}
	if e.IsPlaying {
		t := time.Since(e.StartTime).Seconds()
		if t > e.totalDuration {
			t = e.totalDuration
		}
		s.Time = t
		s.Frequency = e.baseFreqFunc(t)
		s.BeatFrequency = e.beatFreqFunc(t)
		s.ToneVolume = e.volumeFunc(t)
		s.PinkNoiseVolume = e.pinkNoiseFunc(t)
	}
	return s
}

func (e *Engine) ConfigLoaded() bool {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	return e.config != nil
}

func (e *Engine) WaitDone() {
	e.Mu.Lock()
	done := e.Done
	e.Mu.Unlock()
	if done != nil {
		<-done
	}
}

func (e *Engine) ExportWAV(outputPath string) error {
	e.Mu.Lock()
	if e.IsPlaying {
		e.Mu.Unlock()
		return fmt.Errorf("cannot export while playing")
	}
	if e.config == nil {
		e.Mu.Unlock()
		return fmt.Errorf("no config loaded")
	}
	e.Mu.Unlock()

	mixedStreamer, sr := e.createMixer()

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	format := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   2,
	}

	if err := wav.Encode(outFile, mixedStreamer, format); err != nil {
		return fmt.Errorf("failed to encode WAV: %w", err)
	}
	return nil
}
