package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToneSet represents a single tone-set definition.
type ToneSet struct {
	Name            string
	Frequency       float64
	BeatFrequency   float64
	PinkNoiseVolume float64
	ToneVolume      float64
}

// FrequencyChange represents a single frequency change in the YAML output.
type FrequencyChange struct {
	Time            float64 `yaml:"time"`
	Frequency       float64 `yaml:"frequency"`
	BeatFrequency   float64 `yaml:"beat_frequency"`
	PinkNoiseVolume float64 `yaml:"pink_noise_volume"`
	ToneVolume      float64 `yaml:"tone_volume"`
}

// Config represents the overall YAML configuration.
type Config struct {
	FrequencyChanges []FrequencyChange `yaml:"frequency_changes"`
}

func main() {
	// Parse command-line arguments
	inputFile := flag.String("input", "", "Path to the Sbagen input file")
	outputFile := flag.String("output", "", "Path to the YAML output file (optional, defaults to stdout)")
	fade := flag.Float64("fade", defaultFadeSeconds, "Seconds to fade between tone-sets not joined by '->' (like SBaGen's -F)")
	flag.Parse()

	if *fade < 0 {
		log.Fatal("-fade must not be negative")
	}

	// Validate input
	if *inputFile == "" {
		log.Fatal("Input file is required. Use -input <path> to specify the Sbagen file.")
	}

	// Open input file
	file, err := os.Open(*inputFile)
	if err != nil {
		log.Fatalf("Failed to open input file: %v", err)
	}
	defer file.Close()

	// Read and parse the Sbagen file
	toneSets, timeSequence, err := parseSbagen(file)
	if err != nil {
		log.Fatalf("Failed to parse Sbagen file: %v", err)
	}

	// Convert time-sequence to frequency changes
	frequencyChanges, err := convertToFrequencyChanges(toneSets, timeSequence, *fade)
	if err != nil {
		log.Fatalf("Failed to convert to frequency changes: %v", err)
	}

	// Create YAML configuration
	config := Config{
		FrequencyChanges: frequencyChanges,
	}

	// Marshal to YAML
	yamlData, err := yaml.Marshal(&config)
	if err != nil {
		log.Fatalf("Failed to marshal YAML: %v", err)
	}

	// Output YAML
	if *outputFile == "" {
		fmt.Println(string(yamlData))
	} else {
		err = os.WriteFile(*outputFile, yamlData, 0644)
		if err != nil {
			log.Fatalf("Failed to write YAML to file: %v", err)
		}
	}
}

// defaultFadeSeconds matches SBaGen's default fade interval (-F 60000).
const defaultFadeSeconds = 60.0

var (
	toneSetRegex = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*):\s*(.*)$`)
	// A time is NOW or hh:mm[:ss], optionally followed by +hh:mm[:ss], or just
	// +hh:mm[:ss] (relative to the last NOW or hh:mm time).
	timeSeqRegex = regexp.MustCompile(`^((?:NOW|\d+:\d+(?::\d+)?)?(?:\+\d+:\d+(?::\d+)?)?)\s+([a-zA-Z0-9_-]+)(\s*->)?$`)
	toneRegex    = regexp.MustCompile(`^(\d*\.?\d+)(?:([+-])(\d*\.?\d+))?(?:/(\d*\.?\d+))?$`)
)

// sequenceEntry is one parsed time-sequence line.
type sequenceEntry struct {
	Time    float64 // seconds from the start of the session
	ToneSet string
	Slide   bool // "->": slide gradually into the next entry
}

// parseSbagen parses the Sbagen configuration from r.
// It returns a map of tone-set names to ToneSet structs and the time-sequence lines.
func parseSbagen(r io.Reader) (map[string]ToneSet, []string, error) {
	scanner := bufio.NewScanner(r)
	toneSets := make(map[string]ToneSet)
	var timeSequence []string

	// Parsing state
	parsingToneSets := true

	for scanner.Scan() {
		line := scanner.Text()
		// Strip comments, including ones at the end of a line
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		if parsingToneSets {
			// Check if the line is a tone-set definition
			if matches := toneSetRegex.FindStringSubmatch(line); matches != nil {
				name := matches[1]
				specs := matches[2]
				toneSet, err := parseToneSet(name, specs)
				if err != nil {
					return nil, nil, fmt.Errorf("error parsing tone-set '%s': %v", name, err)
				}
				toneSets[name] = toneSet
			} else {
				// Assume that tone-set definitions are done, switch to parsing time-sequence
				parsingToneSets = false
			}
		}

		if !parsingToneSets {
			// Parse time-sequence lines
			if matches := timeSeqRegex.FindStringSubmatch(line); matches != nil && matches[1] != "" {
				timeSequence = append(timeSequence, line)
			} else {
				return nil, nil, fmt.Errorf("invalid time-sequence line: '%s'", line)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading input file: %v", err)
	}

	if len(toneSets) == 0 {
		return nil, nil, errors.New("no tone-set definitions found")
	}

	if len(timeSequence) == 0 {
		return nil, nil, errors.New("no time-sequence definitions found")
	}

	return toneSets, timeSequence, nil
}

// parseToneSet parses a single tone-set definition line.
func parseToneSet(name, specs string) (ToneSet, error) {
	toneSet := ToneSet{
		Name: name,
	}

	if specs == "-" {
		// All off
		return toneSet, nil
	}

	tones := 0
	for _, part := range strings.Fields(specs) {
		if strings.HasPrefix(part, "pink/") {
			// Pink noise specification
			ampStr := strings.TrimPrefix(part, "pink/")
			amp, err := strconv.ParseFloat(ampStr, 64)
			if err != nil {
				return toneSet, fmt.Errorf("invalid pink noise amplitude: '%s'", ampStr)
			}
			toneSet.PinkNoiseVolume = amp / 100.0
		} else if strings.HasPrefix(part, "mix/") {
			// Soundtrack input mix (not handled in frequency_changes)
			continue
		} else if strings.HasPrefix(part, "bell") || strings.HasPrefix(part, "spin:") || strings.HasPrefix(part, "wave") {
			// Other sound types (not handled in frequency_changes)
			continue
		} else {
			// Binaural tone or sine-wave: <carrier>[<+|-><beat>][/<amp>]
			// Examples: 300+10/60, 200-4/50, 150/.5
			matches := toneRegex.FindStringSubmatch(part)
			if matches == nil {
				return toneSet, fmt.Errorf("invalid tone specification: '%s'", part)
			}

			carrier, err := strconv.ParseFloat(matches[1], 64)
			if err != nil {
				return toneSet, fmt.Errorf("invalid carrier frequency: '%s'", matches[1])
			}

			var beatFreq float64
			if matches[3] != "" {
				beatFreq, err = strconv.ParseFloat(matches[3], 64)
				if err != nil {
					return toneSet, fmt.Errorf("invalid beat frequency: '%s'", matches[3])
				}
				// SBaGen's '-' puts the lower frequency in the right ear
				if matches[2] == "-" {
					beatFreq = -beatFreq
				}
			}

			var amp float64
			if matches[4] != "" {
				amp, err = strconv.ParseFloat(matches[4], 64)
				if err != nil {
					return toneSet, fmt.Errorf("invalid tone amplitude: '%s'", matches[4])
				}
				amp = amp / 100.0
			}

			tones++
			if tones > 1 {
				fmt.Fprintf(os.Stderr, "warning: tone-set '%s' has more than one tone; only the last carrier and beat are used\n", name)
			}
			toneSet.Frequency = carrier
			toneSet.BeatFrequency = beatFreq
			toneSet.ToneVolume += amp // Accumulate if multiple tones
		}
	}

	toneSet.ToneVolume = clampVolume(name, "tone", toneSet.ToneVolume)
	toneSet.PinkNoiseVolume = clampVolume(name, "pink noise", toneSet.PinkNoiseVolume)
	return toneSet, nil
}

// clampVolume limits a volume to the engine's 0-1 range, warning when the
// SBaGen amplitude asked for more than 100%.
func clampVolume(toneSet, kind string, v float64) float64 {
	if v > 1 {
		fmt.Fprintf(os.Stderr, "warning: tone-set '%s' %s amplitude %g%% exceeds 100%%; using 100%%\n", toneSet, kind, v*100)
		return 1
	}
	return v
}

// parseSequence resolves the time-sequence lines into entries with times in
// seconds from the start of the session, sorted by time.
//
// Times relative to NOW start at 0. Clock times (hh:mm) are taken relative to
// the first clock time in the file and may wrap past midnight. "+hh:mm" is
// relative to the last NOW or clock time.
func parseSequence(toneSets map[string]ToneSet, timeSequence []string) ([]sequenceEntry, error) {
	var entries []sequenceEntry
	var anchor float64 // time of the last NOW or clock time
	usesNow, usesClock := false, false
	var clockOrigin, lastClock, days float64

	for _, line := range timeSequence {
		matches := timeSeqRegex.FindStringSubmatch(line)
		if matches == nil {
			return nil, fmt.Errorf("invalid time-sequence line: '%s'", line)
		}
		timeSpec, toneSetName, slide := matches[1], matches[2], matches[3] != ""

		base, offset, _ := strings.Cut(timeSpec, "+")
		switch {
		case base == "NOW":
			usesNow = true
			anchor = 0
		case base != "":
			clock, err := parseTimeToSeconds(base)
			if err != nil {
				return nil, fmt.Errorf("invalid time '%s': %v", base, err)
			}
			if !usesClock {
				usesClock = true
				clockOrigin = clock
			} else if clock < lastClock {
				days++ // wrapped past midnight
			}
			lastClock = clock
			anchor = clock + days*24*3600 - clockOrigin
		}
		if usesNow && usesClock {
			return nil, fmt.Errorf("cannot mix NOW and clock times: '%s'", line)
		}

		t := anchor
		if offset != "" {
			rel, err := parseTimeToSeconds(offset)
			if err != nil {
				return nil, fmt.Errorf("invalid relative time '%s': %v", offset, err)
			}
			t += rel
		}

		if _, exists := toneSets[toneSetName]; !exists {
			return nil, fmt.Errorf("tone-set '%s' not defined", toneSetName)
		}
		entries = append(entries, sequenceEntry{Time: t, ToneSet: toneSetName, Slide: slide})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time < entries[j].Time })
	return entries, nil
}

// convertToFrequencyChanges converts the parsed tone-sets and time-sequence into frequency changes.
//
// The engine interpolates linearly between consecutive changes, so each
// tone-set is held until the next entry's time and then faded into it over
// fade seconds (shortened if the next entry comes sooner). An entry ending in
// "->" instead slides gradually into the next one over the whole interval.
func convertToFrequencyChanges(toneSets map[string]ToneSet, timeSequence []string, fade float64) ([]FrequencyChange, error) {
	entries, err := parseSequence(toneSets, timeSequence)
	if err != nil {
		return nil, err
	}

	var changes []FrequencyChange
	add := func(t float64, ts ToneSet) {
		fc := FrequencyChange{
			Time:            t,
			Frequency:       ts.Frequency,
			BeatFrequency:   ts.BeatFrequency,
			PinkNoiseVolume: ts.PinkNoiseVolume,
			ToneVolume:      ts.ToneVolume,
		}
		n := len(changes)
		if n > 0 && changes[n-1] == fc {
			return
		}
		// A third identical setting in a row only extends the flat stretch.
		if n >= 2 && sameSetting(changes[n-1], fc) && sameSetting(changes[n-2], fc) {
			changes[n-1].Time = t
			return
		}
		changes = append(changes, fc)
	}

	add(entries[0].Time, toneSets[entries[0].ToneSet])
	for i := 1; i < len(entries); i++ {
		prev, cur := entries[i-1], entries[i]
		if prev.Slide || prev.ToneSet == cur.ToneSet {
			add(cur.Time, toneSets[cur.ToneSet])
			continue
		}
		f := fade
		if i+1 < len(entries) && entries[i+1].Time-cur.Time < f {
			f = entries[i+1].Time - cur.Time
		}
		add(cur.Time, toneSets[prev.ToneSet])
		add(cur.Time+f, toneSets[cur.ToneSet])
	}

	inheritSilentFrequencies(changes)
	return changes, nil
}

// sameSetting reports whether a and b differ only in time.
func sameSetting(a, b FrequencyChange) bool {
	a.Time = b.Time
	return a == b
}

// inheritSilentFrequencies gives silent changes (no tone, e.g. an "off"
// tone-set) the frequencies of the neighbouring tone, so fading in or out
// changes only the volume instead of also sweeping the pitch to 0 Hz.
func inheritSilentFrequencies(changes []FrequencyChange) {
	silent := func(c FrequencyChange) bool { return c.ToneVolume == 0 && c.Frequency == 0 }
	for i := range changes {
		if !silent(changes[i]) {
			continue
		}
		src := -1
		for j := i - 1; j >= 0; j-- {
			if !silent(changes[j]) {
				src = j
				break
			}
		}
		if src < 0 {
			for j := i + 1; j < len(changes); j++ {
				if !silent(changes[j]) {
					src = j
					break
				}
			}
		}
		if src >= 0 {
			changes[i].Frequency = changes[src].Frequency
			changes[i].BeatFrequency = changes[src].BeatFrequency
		}
	}
}

// parseTimeToSeconds parses a time string in "hh:mm" or "hh:mm:ss" format to total seconds.
func parseTimeToSeconds(timeStr string) (float64, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("time must be in 'hh:mm' or 'hh:mm:ss' format")
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hours in time '%s'", timeStr)
	}

	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minutes in time '%s'", timeStr)
	}

	var seconds int
	if len(parts) == 3 {
		seconds, err = strconv.Atoi(parts[2])
		if err != nil {
			return 0, fmt.Errorf("invalid seconds in time '%s'", timeStr)
		}
	}

	return float64(hours*3600 + minutes*60 + seconds), nil
}
