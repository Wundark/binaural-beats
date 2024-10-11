package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
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
	flag.Parse()

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
	frequencyChanges, err := convertToFrequencyChanges(toneSets, timeSequence)
	if err != nil {
		log.Fatalf("Failed to convert to frequency changes: %v", err)
	}

	// Sort frequencyChanges by Time
	sort.Slice(frequencyChanges, func(i, j int) bool {
		return frequencyChanges[i].Time < frequencyChanges[j].Time
	})

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

// parseSbagen parses the Sbagen configuration from the given file.
// It returns a map of tone-set names to ToneSet structs and a slice of time-sequence lines.
func parseSbagen(file *os.File) (map[string]ToneSet, []string, error) {
	scanner := bufio.NewScanner(file)
	toneSets := make(map[string]ToneSet)
	var timeSequence []string

	// Regular expressions
	toneSetRegex := regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*):\s*(.*)$`)
	timeSeqRegex := regexp.MustCompile(`^(NOW|[\+\d:.]+)\s+([a-zA-Z0-9_-]+)(\s*->)?$`)

	// Parsing state
	parsingToneSets := true

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "#") {
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
			if matches := timeSeqRegex.FindStringSubmatch(line); matches != nil {
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
		toneSet.Frequency = 0.0
		toneSet.BeatFrequency = 0.0
		toneSet.PinkNoiseVolume = 0.0
		toneSet.ToneVolume = 0.0
		return toneSet, nil
	}

	// Split the specs by space
	parts := strings.Fields(specs)
	for _, part := range parts {
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
			// Skipping as it's not relevant to frequency_changes
			continue
		} else if strings.HasPrefix(part, "bell") || strings.HasPrefix(part, "spin:") || strings.HasPrefix(part, "wave") {
			// Other sound types (not handled in frequency_changes)
			// Skipping as it's not relevant to frequency_changes
			continue
		} else {
			// Assume it's a binaural tone or sine-wave
			// Format: <carrier><sign><freq>/<amp> or <carrier>/<amp>
			// Example: 300+10/60 or 150/.5/95 or 100+.5/95
			// Use regex to parse
			re := regexp.MustCompile(`^(\d+(?:\.\d+)?)([+-])?(\d*(?:\.\d+)?)?(?:/(\d+(?:\.\d+)?))?$`)
			matches := re.FindStringSubmatch(part)
			if matches == nil {
				return toneSet, fmt.Errorf("invalid tone specification: '%s'", part)
			}

			carrierStr := matches[1]
			// sign := matches[2]
			freqStr := matches[3]
			ampStr := matches[4]

			carrier, err := strconv.ParseFloat(carrierStr, 64)
			if err != nil {
				return toneSet, fmt.Errorf("invalid carrier frequency: '%s'", carrierStr)
			}

			var beatFreq float64
			if freqStr != "" {
				beatFreq, err = strconv.ParseFloat(freqStr, 64)
				if err != nil {
					return toneSet, fmt.Errorf("invalid beat frequency: '%s'", freqStr)
				}
			} else {
				beatFreq = 0.0
			}

			var amp float64
			if ampStr != "" {
				amp, err = strconv.ParseFloat(ampStr, 64)
				if err != nil {
					return toneSet, fmt.Errorf("invalid tone amplitude: '%s'", ampStr)
				}
				amp = amp / 100.0
			} else {
				amp = 0.0
			}

			// If sign is '-', it doesn't affect frequency_changes, so we ignore it
			toneSet.Frequency = carrier
			toneSet.BeatFrequency = beatFreq
			toneSet.ToneVolume += amp // Accumulate if multiple tones
		}
	}

	return toneSet, nil
}

// convertToFrequencyChanges converts the parsed tone-sets and time-sequence into frequency changes.
func convertToFrequencyChanges(toneSets map[string]ToneSet, timeSequence []string) ([]FrequencyChange, error) {
	var frequencyChanges []FrequencyChange
	// var currentTime float64 = 0.0
	var lastAbsoluteTime float64 = 0.0

	// Regular expressions
	timeSeqRegex := regexp.MustCompile(`^(NOW|[\+\d:.]+)\s+([a-zA-Z0-9_-]+)(\s*->)?$`)

	for _, line := range timeSequence {
		matches := timeSeqRegex.FindStringSubmatch(line)
		if matches == nil {
			return nil, fmt.Errorf("invalid time-sequence line: '%s'", line)
		}

		timeSpec := matches[1]
		toneSetName := matches[2]
		// transition := matches[3] // Not used in frequency_changes

		var newTime float64
		if timeSpec == "NOW" {
			newTime = 0.0
			lastAbsoluteTime = newTime
		} else if strings.HasPrefix(timeSpec, "+") {
			// Relative time
			relTimeStr := strings.TrimPrefix(timeSpec, "+")
			relSeconds, err := parseTimeToSeconds(relTimeStr)
			if err != nil {
				return nil, fmt.Errorf("invalid relative time '%s': %v", relTimeStr, err)
			}
			newTime = lastAbsoluteTime + relSeconds
		} else {
			// Absolute time
			absSeconds, err := parseTimeToSeconds(timeSpec)
			if err != nil {
				return nil, fmt.Errorf("invalid absolute time '%s': %v", timeSpec, err)
			}
			newTime = absSeconds
			lastAbsoluteTime = newTime
		}

		// Retrieve the tone-set
		toneSet, exists := toneSets[toneSetName]
		if !exists {
			return nil, fmt.Errorf("tone-set '%s' not defined", toneSetName)
		}

		// Create FrequencyChange
		fc := FrequencyChange{
			Time:            newTime,
			Frequency:       toneSet.Frequency,
			BeatFrequency:   toneSet.BeatFrequency,
			PinkNoiseVolume: toneSet.PinkNoiseVolume,
			ToneVolume:      toneSet.ToneVolume,
		}
		frequencyChanges = append(frequencyChanges, fc)
	}

	return frequencyChanges, nil
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
	} else {
		seconds = 0
	}

	totalSeconds := float64(hours*3600 + minutes*60 + seconds)
	return totalSeconds, nil
}
