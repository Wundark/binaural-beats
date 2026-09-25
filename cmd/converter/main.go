package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/Wundark/binaural-beats/internal/sbagen"
)

// Config represents the overall YAML configuration.
type Config struct {
	Name             string          `yaml:"name,omitempty"`
	Description      string          `yaml:"description,omitempty"`
	FrequencyChanges []sbagen.Change `yaml:"frequency_changes"`
}

func main() {
	// Parse command-line arguments
	inputFile := flag.String("input", "", "Path to the Sbagen input file")
	outputFile := flag.String("output", "", "Path to the YAML output file (optional, defaults to stdout)")
	fade := flag.Float64("fade", sbagen.DefaultFade, "Seconds to fade between tone-sets not joined by '->' (like SBaGen's -F)")
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

	session, err := sbagen.Convert(file, *fade)
	if err != nil {
		log.Fatalf("Failed to convert Sbagen file: %v", err)
	}
	for _, w := range session.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	yamlData, err := yaml.Marshal(&Config{
		Name:             session.Name,
		Description:      session.Description,
		FrequencyChanges: session.Changes,
	})
	if err != nil {
		log.Fatalf("Failed to marshal YAML: %v", err)
	}

	// Output YAML
	if *outputFile == "" {
		fmt.Println(string(yamlData))
	} else if err := os.WriteFile(*outputFile, yamlData, 0644); err != nil {
		log.Fatalf("Failed to write YAML to file: %v", err)
	}
}
