package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/wav"
	"gopkg.in/yaml.v3"
)

// Config represents the structure of the YAML configuration file.
type Config struct {
	FrequencyChanges []ConfigFrequencyChange `yaml:"frequency_changes"`
}

// ConfigFrequencyChange represents a frequency change event.
type ConfigFrequencyChange struct {
	Time            float64 `yaml:"time"`              // Time in seconds
	Frequency       float64 `yaml:"frequency"`         // Base frequency in Hz
	BeatFrequency   float64 `yaml:"beat_frequency"`    // Beat frequency in Hz
	PinkNoiseOn     bool    `yaml:"pink_noise_on"`     // Pink noise on or off
	PinkNoiseVolume float64 `yaml:"pink_noise_volume"` // Volume for pink noise (0.0 to 1.0)
	ToneVolume      float64 `yaml:"tone_volume"`       // Volume for the sine wave (0.0 to 1.0)
}

// PinkNoise implements a pink noise generator using the Voss-McCartney algorithm.
type PinkNoise struct {
	rand   *rand.Rand
	maxKey uint32
	key    uint32
	white  [5]float64
}

// NewPinkNoise creates a new PinkNoise generator.
func NewPinkNoise() *PinkNoise {
	return &PinkNoise{
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
		maxKey: 0x1F, // Five bits set
	}
}

// Stream generates pink noise samples.
func (pn *PinkNoise) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		sample := pn.nextSample()
		samples[i][0] += sample // Left channel
		samples[i][1] += sample // Right channel
	}
	return len(samples), true
}

// Err returns nil, as PinkNoise doesn't produce any errors.
func (pn *PinkNoise) Err() error {
	return nil
}

// nextSample generates the next pink noise sample.
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
	sum := pn.white[0] + pn.white[1] + pn.white[2] + pn.white[3] + pn.white[4]
	return sum * 0.1 // Reduced amplitude to prevent clipping
}

// VariableTone generates a sine wave with a frequency that changes over time.
type VariableTone struct {
	sr         beep.SampleRate
	pos        int
	phase      float64
	freqFunc   func(t float64) float64
	volumeFunc func(t float64) float64
	channel    int // 0 for left, 1 for right
}

// Stream generates the sine wave samples.
func (vt *VariableTone) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(vt.pos) / float64(vt.sr) // Time in seconds
		f := vt.freqFunc(t)                   // Frequency at time t
		vol := vt.volumeFunc(t)               // Volume at time t
		deltaPhase := 2 * math.Pi * f / float64(vt.sr)
		vt.phase += deltaPhase
		s := math.Sin(vt.phase) * vol * 0.5 // Scaled down to prevent clipping
		if vt.channel == 0 {
			samples[i][0] += s
		} else {
			samples[i][1] += s
		}
		vt.pos++
	}
	return len(samples), true
}

// Err returns nil, as VariableTone doesn't produce any errors.
func (vt *VariableTone) Err() error {
	return nil
}

// PinkNoiseControl controls the pink noise based on time.
type PinkNoiseControl struct {
	stream     beep.Streamer
	volumeFunc func(t float64) (on bool, vol float64)
	sr         beep.SampleRate
	pos        int
}

// Stream processes the pink noise samples with volume control.
func (pnc *PinkNoiseControl) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = pnc.stream.Stream(samples)
	for i := range samples[:n] {
		t := float64(pnc.pos) / float64(pnc.sr)
		on, vol := pnc.volumeFunc(t)
		if !on {
			samples[i][0] = 0
			samples[i][1] = 0
		} else {
			s := samples[i][0] * vol * 0.5 // Scaled down to prevent clipping
			samples[i][0] = s
			samples[i][1] = s
		}
		pnc.pos++
	}
	return n, ok
}

// Err returns the error state of the pink noise stream.
func (pnc *PinkNoiseControl) Err() error {
	return pnc.stream.Err()
}

// parseConfig reads and parses the YAML configuration file.
func parseConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	// Sort frequency changes by time
	sort.Slice(cfg.FrequencyChanges, func(i, j int) bool {
		return cfg.FrequencyChanges[i].Time < cfg.FrequencyChanges[j].Time
	})

	return &cfg, nil
}

// createFreqFunc creates a function that returns the frequency at time t based on the frequency changes.
func createFreqFunc(changes []ConfigFrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		if len(changes) == 0 {
			return 0
		}

		// If t is before the first change
		if t <= changes[0].Time {
			return changes[0].Frequency
		}

		// If t is after the last change
		if t >= changes[len(changes)-1].Time {
			return changes[len(changes)-1].Frequency
		}

		// Find the interval in which t falls
		for i := 0; i < len(changes)-1; i++ {
			if t >= changes[i].Time && t < changes[i+1].Time {
				// Linear interpolation
				t1 := changes[i].Time
				t2 := changes[i+1].Time
				f1 := changes[i].Frequency
				f2 := changes[i+1].Frequency
				return f1 + (f2-f1)*(t-t1)/(t2-t1)
			}
		}

		return changes[len(changes)-1].Frequency
	}
}

// createBeatFreqFunc creates a function that returns the beat frequency at time t based on the frequency changes.
func createBeatFreqFunc(changes []ConfigFrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		if len(changes) == 0 {
			return 0
		}

		// If t is before the first change
		if t <= changes[0].Time {
			return changes[0].BeatFrequency
		}

		// If t is after the last change
		if t >= changes[len(changes)-1].Time {
			return changes[len(changes)-1].BeatFrequency
		}

		// Find the interval in which t falls
		for i := 0; i < len(changes)-1; i++ {
			if t >= changes[i].Time && t < changes[i+1].Time {
				// Linear interpolation
				t1 := changes[i].Time
				t2 := changes[i+1].Time
				bf1 := changes[i].BeatFrequency
				bf2 := changes[i+1].BeatFrequency
				return bf1 + (bf2-bf1)*(t-t1)/(t2-t1)
			}
		}

		return changes[len(changes)-1].BeatFrequency
	}
}

// createVolumeFunc creates a function that returns the volume at time t based on the tone volumes.
func createVolumeFunc(changes []ConfigFrequencyChange) func(t float64) float64 {
	return func(t float64) float64 {
		if len(changes) == 0 {
			return 1.0
		}

		// If t is before the first change
		if t <= changes[0].Time {
			return changes[0].ToneVolume
		}

		// If t is after the last change
		if t >= changes[len(changes)-1].Time {
			return changes[len(changes)-1].ToneVolume
		}

		// Find the interval in which t falls
		for i := 0; i < len(changes)-1; i++ {
			if t >= changes[i].Time && t < changes[i+1].Time {
				// Linear interpolation
				t1 := changes[i].Time
				t2 := changes[i+1].Time
				v1 := changes[i].ToneVolume
				v2 := changes[i+1].ToneVolume
				return v1 + (v2-v1)*(t-t1)/(t2-t1)
			}
		}

		return changes[len(changes)-1].ToneVolume
	}
}

// createPinkNoiseFunc creates a function that returns whether pink noise is on and its volume at time t.
func createPinkNoiseFunc(changes []ConfigFrequencyChange) func(t float64) (on bool, vol float64) {
	return func(t float64) (bool, float64) {
		if len(changes) == 0 {
			return false, 0.0
		}

		// If t is before the first change
		if t <= changes[0].Time {
			return changes[0].PinkNoiseOn, changes[0].PinkNoiseVolume
		}

		// If t is after the last change
		if t >= changes[len(changes)-1].Time {
			return changes[len(changes)-1].PinkNoiseOn, changes[len(changes)-1].PinkNoiseVolume
		}

		// Find the interval in which t falls
		for i := 0; i < len(changes)-1; i++ {
			if t >= changes[i].Time && t < changes[i+1].Time {
				// For pink noise, we will step change the settings
				return changes[i].PinkNoiseOn, changes[i].PinkNoiseVolume
			}
		}

		return changes[len(changes)-1].PinkNoiseOn, changes[len(changes)-1].PinkNoiseVolume
	}
}

// getTotalPlaybackTime calculates the total playback time based on the highest time in frequency changes.
func getTotalPlaybackTime(changes []ConfigFrequencyChange) float64 {
	if len(changes) == 0 {
		return 0
	}
	maxTime := changes[0].Time
	for _, change := range changes {
		if change.Time > maxTime {
			maxTime = change.Time
		}
	}
	return maxTime
}

func main() {
	// Command-line flags
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	outputPath := flag.String("output", "", "Path to the output WAV file (if empty, audio will be played)")
	stretchFactor := flag.Float64("stretch", 1.0, "Stretch factor for playback time (default 1.0)")
	flag.Parse()

	// Parse the configuration file
	cfg, err := parseConfig(*configPath)
	if err != nil {
		log.Fatalf("Error parsing configuration file: %v", err)
	}

	for i := range cfg.FrequencyChanges {
		cfg.FrequencyChanges[i].Time *= *stretchFactor
	}

	// Calculate the total playback time
	totalPlaybackTime := getTotalPlaybackTime(cfg.FrequencyChanges)
	if totalPlaybackTime == 0 {
		log.Fatalf("Total playback time is zero. Check your configuration.")
	}

	// Sample rate
	sr := beep.SampleRate(44100)

	// Create frequency functions based on configuration
	baseFreqFunc := createFreqFunc(cfg.FrequencyChanges)
	beatFreqFunc := createBeatFreqFunc(cfg.FrequencyChanges)
	volumeFunc := createVolumeFunc(cfg.FrequencyChanges)
	pinkNoiseFunc := createPinkNoiseFunc(cfg.FrequencyChanges)

	// Frequency functions for left and right channels
	freqFuncLeft := func(t float64) float64 {
		return baseFreqFunc(t)
	}

	freqFuncRight := func(t float64) float64 {
		return baseFreqFunc(t) + beatFreqFunc(t)
	}

	// Generate variable tones for left and right channels
	leftTone := &VariableTone{
		sr:         sr,
		pos:        0,
		phase:      0,
		freqFunc:   freqFuncLeft,
		volumeFunc: volumeFunc,
		channel:    0, // Left channel
	}

	rightTone := &VariableTone{
		sr:         sr,
		pos:        0,
		phase:      0,
		freqFunc:   freqFuncRight,
		volumeFunc: volumeFunc,
		channel:    1, // Right channel
	}

	// Generate pink noise
	pinkNoise := NewPinkNoise()

	// Control pink noise based on time
	pinkNoiseControl := &PinkNoiseControl{
		stream:     pinkNoise,
		volumeFunc: pinkNoiseFunc,
		sr:         sr,
		pos:        0,
	}

	// Mix the sine waves and pink noise
	mixed := &beep.Mixer{}
	mixed.Add(
		leftTone,
		rightTone,
		pinkNoiseControl,
	)

	// Limit playback to the total playback time
	totalSamples := sr.N(time.Duration(totalPlaybackTime * float64(time.Second)))
	mixedStreamer := beep.Take(totalSamples, mixed)

	// Handle output: either play or export to WAV
	if *outputPath == "" {
		// Initialize the speaker
		speaker.Init(sr, sr.N(time.Second/10))

		// Create a channel to signal when playback is done
		done := make(chan struct{})

		// Record the start time
		startTime := time.Now()

		// Play the audio
		speaker.Play(beep.Seq(mixedStreamer, beep.Callback(func() {
			close(done)
		})))

		// Create a ticker to output status every 3 seconds
		ticker := time.NewTicker(3 * time.Second)
		tick := func() {
			t := time.Since(startTime).Seconds()
			if t > totalPlaybackTime {
				return
			}
			freq := baseFreqFunc(t)
			beatFreq := beatFreqFunc(t)
			toneVol := volumeFunc(t)
			pinkOn, pinkVol := pinkNoiseFunc(t)
			fmt.Printf("Time: %.2f s / Total %.2f s, Base Frequency: %.2f Hz, Beat Frequency: %.2f Hz, Tone Volume: %.2f, Pink Noise On: %v, Pink Noise Volume: %.2f\n",
				t, totalPlaybackTime, freq, beatFreq, toneVol, pinkOn, pinkVol)
		}

		go func() {
			tick()
			for {
				select {
				case <-ticker.C:
					tick()
				case <-done:
					tick()
					ticker.Stop()
					return
				}
			}
		}()

		// Wait until playback is finished
		<-done
	} else {
		// Export to WAV file
		fmt.Printf("Exporting audio to %s...\n", *outputPath)

		// Create the output file
		outFile, err := os.Create(*outputPath)
		if err != nil {
			log.Fatalf("Error creating output file: %v", err)
		}
		defer outFile.Close()

		// Create WAV encoder format
		format := beep.Format{
			SampleRate:  sr,
			NumChannels: 2,
			Precision:   2, // 16-bit audio
		}

		// Encode and write the audio
		err = wav.Encode(outFile, mixedStreamer, format)
		if err != nil {
			log.Fatalf("Error encoding WAV: %v", err)
		}

		fmt.Println("Export completed successfully.")
	}
}
