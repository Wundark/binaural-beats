package engine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/wav"
	"gopkg.in/yaml.v3"

	"github.com/Wundark/binaural-beats/internal/sbagen"
)

// Config represents the structure of the YAML configuration file.
type Config struct {
	Name             string            `yaml:"name,omitempty"`
	Description      string            `yaml:"description,omitempty"`
	FrequencyChanges []FrequencyChange `yaml:"frequency_changes"`
}

// SessionInfo describes a loaded session.
type SessionInfo struct {
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	TotalDuration float64 `json:"total_duration"`
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
	IsPaused        bool    `json:"is_paused"`
	Volume          float64 `json:"volume"`
	Stretch         float64 `json:"stretch"`
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

// Ramp times that keep pause, resume and volume changes free of clicks.
const (
	pauseRampSeconds  = 0.03
	volumeRampSeconds = 0.02
)

// BinauralStream generates the complete stereo signal: a base-frequency tone on
// the left channel, a base+beat tone on the right channel, and pink noise on
// both. Every frame it returns is written from scratch, so the output never
// depends on what the caller's buffer held before (beep.Mixer reuses its
// scratch buffer between streamers and blocks).
//
// The stream ends after total samples. Pause, Resume, Seek and SetVolume may
// be called from any goroutine while it plays; they take effect at the next
// buffer, with short ramps to avoid clicks.
type BinauralStream struct {
	sr      beep.SampleRate
	pos     int
	total   int
	phaseL  float64
	phaseR  float64
	changes []FrequencyChange
	seg     int
	noise   *PinkNoise

	env  float64 // pause envelope, 0 (paused) to 1
	gain float64 // smoothed master volume

	position atomic.Int64  // pos, published after each buffer
	paused   atomic.Bool   // requested pause state
	seekTo   atomic.Int64  // requested position in samples, or -1
	volume   atomic.Uint64 // requested master volume (float64 bits)
}

// NewBinauralStream creates a stereo generator for the given (time-sorted)
// changes that ends after total samples, at full volume.
func NewBinauralStream(sr beep.SampleRate, changes []FrequencyChange, total int, noise *PinkNoise) *BinauralStream {
	bs := &BinauralStream{sr: sr, changes: changes, total: total, noise: noise, env: 1, gain: 1}
	bs.seekTo.Store(-1)
	bs.volume.Store(math.Float64bits(1))
	return bs
}

// Position returns the playback position in samples.
func (bs *BinauralStream) Position() int { return int(bs.position.Load()) }

// Pause fades the output out and holds the position until Resume.
func (bs *BinauralStream) Pause() { bs.paused.Store(true) }

// Resume continues playback after Pause.
func (bs *BinauralStream) Resume() { bs.paused.Store(false) }

// Paused reports whether a pause was requested.
func (bs *BinauralStream) Paused() bool { return bs.paused.Load() }

// Seek moves playback to the given sample, clamped to the stream length.
func (bs *BinauralStream) Seek(sample int) {
	if sample < 0 {
		sample = 0
	}
	if sample > bs.total {
		sample = bs.total
	}
	bs.seekTo.Store(int64(sample))
	bs.position.Store(int64(sample))
}

// SetVolume sets the master volume (0 to 1). The change is ramped.
func (bs *BinauralStream) SetVolume(v float64) { bs.volume.Store(math.Float64bits(v)) }

// setVolumeNow sets the master volume without a ramp, for a stream that has
// not started yet.
func (bs *BinauralStream) setVolumeNow(v float64) {
	bs.SetVolume(v)
	bs.gain = v
}

// params returns the interpolated parameters at time t. It matches interpolate
// but keeps a cursor into changes, since t mostly moves forward while streaming.
func (bs *BinauralStream) params(t float64) (freq, beat, toneVol, noiseVol float64) {
	c := bs.changes
	if len(c) == 0 {
		return 0, 0, 1.0, 0
	}
	if t <= c[0].Time {
		return c[0].Frequency, c[0].BeatFrequency, c[0].ToneVolume, c[0].PinkNoiseVolume
	}
	last := c[len(c)-1]
	if t >= last.Time {
		return last.Frequency, last.BeatFrequency, last.ToneVolume, last.PinkNoiseVolume
	}
	if bs.seg >= len(c)-1 || t < c[bs.seg].Time {
		bs.seg = 0
	}
	for bs.seg < len(c)-2 && t >= c[bs.seg+1].Time {
		bs.seg++
	}
	a, b := c[bs.seg], c[bs.seg+1]
	f := (t - a.Time) / (b.Time - a.Time)
	return a.Frequency + (b.Frequency-a.Frequency)*f,
		a.BeatFrequency + (b.BeatFrequency-a.BeatFrequency)*f,
		a.ToneVolume + (b.ToneVolume-a.ToneVolume)*f,
		a.PinkNoiseVolume + (b.PinkNoiseVolume-a.PinkNoiseVolume)*f
}

func (bs *BinauralStream) Stream(samples [][2]float64) (n int, ok bool) {
	sr := float64(bs.sr)
	if s := bs.seekTo.Swap(-1); s >= 0 {
		bs.pos = int(s)
	}
	paused := bs.paused.Load()
	targetGain := math.Float64frombits(bs.volume.Load())
	envStep := 1 / (pauseRampSeconds * sr)
	gainCoef := 1 - math.Exp(-1/(volumeRampSeconds*sr))

	for i := range samples {
		if paused {
			bs.env -= envStep
			if bs.env <= 0 {
				// Fully paused: hold the position and output silence.
				bs.env = 0
				samples[i] = [2]float64{}
				continue
			}
		} else if bs.env < 1 {
			bs.env = math.Min(1, bs.env+envStep)
		}

		if bs.pos >= bs.total {
			bs.position.Store(int64(bs.pos))
			return i, i > 0
		}

		t := float64(bs.pos) / sr
		freq, beat, toneVol, noiseVol := bs.params(t)

		bs.phaseL = math.Mod(bs.phaseL+2*math.Pi*freq/sr, 2*math.Pi)
		bs.phaseR = math.Mod(bs.phaseR+2*math.Pi*(freq+beat)/sr, 2*math.Pi)

		// The noise generator always advances so its state is independent of volume.
		noise := bs.noise.nextSample()
		if noiseVol <= 0 {
			noise = 0
		} else {
			noise *= noiseVol * 0.5
		}

		bs.gain += (targetGain - bs.gain) * gainCoef
		g := bs.gain * bs.env
		samples[i][0] = (math.Sin(bs.phaseL)*toneVol*0.5 + noise) * g
		samples[i][1] = (math.Sin(bs.phaseR)*toneVol*0.5 + noise) * g
		bs.pos++
	}
	bs.position.Store(int64(bs.pos))
	return len(samples), true
}

func (bs *BinauralStream) Err() error { return nil }

// ParseConfig reads and parses a session file: YAML, or SBaGen (.sbg).
func ParseConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseConfigData(filename, data)
}

// ParseConfigData parses a session from data. The name's extension selects
// the format (.yaml/.yml or .sbg); without one, the content decides. A session
// without a name of its own is named after the file.
func ParseConfigData(name string, data []byte) (*Config, error) {
	var cfg *Config
	var err error
	if isSbagen(name, data) {
		cfg, err = parseSbagen(data)
	} else {
		cfg, err = parseYAML(data)
	}
	if err != nil {
		return nil, err
	}
	// Stable, so entries sharing a time keep their order (an instant change).
	sort.SliceStable(cfg.FrequencyChanges, func(i, j int) bool {
		return cfg.FrequencyChanges[i].Time < cfg.FrequencyChanges[j].Time
	})
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		base := filepath.Base(name)
		cfg.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return cfg, nil
}

func isSbagen(name string, data []byte) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sbg":
		return true
	case ".yaml", ".yml":
		return false
	}
	// Every YAML session has this key; SBaGen files never do.
	return !bytes.Contains(data, []byte("frequency_changes"))
}

func parseYAML(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject misspelled keys instead of ignoring them
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config file is empty")
		}
		return nil, err
	}
	return &cfg, nil
}

func parseSbagen(data []byte) (*Config, error) {
	session, err := sbagen.Convert(bytes.NewReader(data), sbagen.DefaultFade)
	if err != nil {
		return nil, fmt.Errorf("SBaGen file: %w", err)
	}
	cfg := &Config{Name: session.Name, Description: session.Description}
	for _, c := range session.Changes {
		cfg.FrequencyChanges = append(cfg.FrequencyChanges, FrequencyChange(c))
	}
	return cfg, nil
}

// Validate checks that the config describes a playable session. It expects
// the changes to be sorted by time.
func (c *Config) Validate() error {
	changes := c.FrequencyChanges
	if len(changes) == 0 {
		return fmt.Errorf("config has no frequency_changes entries")
	}
	for i, fc := range changes {
		n := i + 1
		for _, v := range []struct {
			name string
			val  float64
		}{
			{"time", fc.Time},
			{"frequency", fc.Frequency},
			{"beat_frequency", fc.BeatFrequency},
			{"pink_noise_volume", fc.PinkNoiseVolume},
			{"tone_volume", fc.ToneVolume},
		} {
			if math.IsNaN(v.val) || math.IsInf(v.val, 0) {
				return fmt.Errorf("frequency_changes entry %d: %s must be a finite number", n, v.name)
			}
		}
		if fc.Time < 0 {
			return fmt.Errorf("frequency_changes entry %d: time must not be negative (got %g)", n, fc.Time)
		}
		if fc.Frequency < 0 {
			return fmt.Errorf("frequency_changes entry %d: frequency must not be negative (got %g)", n, fc.Frequency)
		}
		if fc.ToneVolume < 0 || fc.ToneVolume > 1 {
			return fmt.Errorf("frequency_changes entry %d: tone_volume must be between 0 and 1 (got %g)", n, fc.ToneVolume)
		}
		if fc.PinkNoiseVolume < 0 || fc.PinkNoiseVolume > 1 {
			return fmt.Errorf("frequency_changes entry %d: pink_noise_volume must be between 0 and 1 (got %g)", n, fc.PinkNoiseVolume)
		}
	}
	if changes[len(changes)-1].Time <= 0 {
		return fmt.Errorf("session has no length: the last frequency_changes entry sets when playback ends, so its time must be greater than 0")
	}
	return nil
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

// sampleRate is the rate sessions are generated at.
const sampleRate = beep.SampleRate(44100)

// newStream creates the audio stream from the current config state. The
// caller must hold e.Mu.
func (e *Engine) newStream() *BinauralStream {
	total := sampleRate.N(time.Duration(e.totalDuration * float64(time.Second)))
	return NewBinauralStream(sampleRate, e.changes, total, NewPinkNoise())
}

// Engine manages the audio generation and playback lifecycle.
type Engine struct {
	Mu            sync.Mutex
	config        *Config
	stretch       float64
	IsPlaying     bool
	Done          chan struct{}
	stream        *BinauralStream // the stream being played, while IsPlaying
	volume        float64         // master volume for playback, 0 to 1
	startAt       float64         // where the next Play starts, in seconds
	baseFreqFunc  func(float64) float64
	beatFreqFunc  func(float64) float64
	volumeFunc    func(float64) float64
	pinkNoiseFunc func(float64) float64
	changes       []FrequencyChange
	totalDuration float64
	playID        uint64
}

func NewEngine() *Engine {
	return &Engine{stretch: 1.0, volume: 1.0}
}

// LoadConfig loads a session file (YAML or SBaGen).
func (e *Engine) LoadConfig(path string) (SessionInfo, error) {
	cfg, err := ParseConfig(path)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("failed to parse config: %w", err)
	}
	return e.load(cfg)
}

// LoadConfigData loads a session from data; see ParseConfigData.
func (e *Engine) LoadConfigData(name string, data []byte) (SessionInfo, error) {
	cfg, err := ParseConfigData(name, data)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("failed to parse config: %w", err)
	}
	return e.load(cfg)
}

func (e *Engine) load(cfg *Config) (SessionInfo, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	if e.IsPlaying {
		return SessionInfo{}, fmt.Errorf("cannot load config while playing")
	}

	e.config = cfg
	e.startAt = 0
	e.applyStretch()
	return e.sessionInfo(), nil
}

// Timeline is a loaded session's settings over time, after stretching.
type Timeline struct {
	SessionInfo
	Changes []FrequencyChange `json:"changes"`
}

// Timeline returns the loaded session's changes, after stretching.
func (e *Engine) Timeline() (Timeline, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if e.config == nil {
		return Timeline{}, fmt.Errorf("no config loaded")
	}
	changes := make([]FrequencyChange, len(e.changes))
	copy(changes, e.changes)
	return Timeline{SessionInfo: e.sessionInfo(), Changes: changes}, nil
}

// sessionInfo describes the loaded session. The caller must hold e.Mu.
func (e *Engine) sessionInfo() SessionInfo {
	return SessionInfo{Name: e.config.Name, Description: e.config.Description, TotalDuration: e.totalDuration}
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
	e.changes = changes
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
	// Keep the start position at the same point in the session.
	e.startAt *= factor / e.stretch
	e.stretch = factor
	e.applyStretch()
	return nil
}

// Pause pauses playback, keeping the position.
func (e *Engine) Pause() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}
	e.stream.Pause()
	return nil
}

// Resume continues paused playback.
func (e *Engine) Resume() error {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if !e.IsPlaying {
		return fmt.Errorf("not playing")
	}
	e.stream.Resume()
	return nil
}

// Seek moves playback to t seconds into the session. When stopped, it sets
// where the next Play starts.
func (e *Engine) Seek(t float64) error {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if e.config == nil {
		return fmt.Errorf("no config loaded")
	}
	if math.IsNaN(t) {
		return fmt.Errorf("invalid seek position")
	}
	t = math.Max(0, math.Min(t, e.totalDuration))
	if e.IsPlaying {
		e.stream.Seek(sampleRate.N(time.Duration(t * float64(time.Second))))
	} else {
		e.startAt = t
	}
	return nil
}

// SetVolume sets the master playback volume (0 to 1). Exports are unaffected.
func (e *Engine) SetVolume(v float64) error {
	if math.IsNaN(v) || v < 0 || v > 1 {
		return fmt.Errorf("volume must be between 0 and 1")
	}
	e.Mu.Lock()
	defer e.Mu.Unlock()
	e.volume = v
	if e.IsPlaying {
		e.stream.SetVolume(v)
	}
	return nil
}

func (e *Engine) GetStatus() Status {
	e.Mu.Lock()
	defer e.Mu.Unlock()

	s := Status{
		IsPlaying:    e.IsPlaying,
		Volume:       e.volume,
		Stretch:      e.stretch,
		ConfigLoaded: e.config != nil,
	}
	if e.config == nil {
		return s
	}
	s.TotalDuration = e.totalDuration
	t := e.startAt
	if e.IsPlaying {
		t = float64(e.stream.Position()) / float64(sampleRate)
		s.IsPaused = e.stream.Paused()
	}
	t = math.Min(t, e.totalDuration)
	s.Time = t
	s.Frequency = e.baseFreqFunc(t)
	s.BeatFrequency = e.beatFreqFunc(t)
	s.ToneVolume = e.volumeFunc(t)
	s.PinkNoiseVolume = e.pinkNoiseFunc(t)
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
	stream := e.newStream()
	e.Mu.Unlock()

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	format := beep.Format{
		SampleRate:  sampleRate,
		NumChannels: 2,
		Precision:   2,
	}

	if err := wav.Encode(outFile, stream, format); err != nil {
		return fmt.Errorf("failed to encode WAV: %w", err)
	}
	return nil
}
