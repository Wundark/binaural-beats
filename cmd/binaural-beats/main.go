package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Wundark/binaural-beats/internal/engine"
	"github.com/Wundark/binaural-beats/internal/rpc"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	outputPath := flag.String("output", "", "Path to the output WAV file (if empty, audio will be played)")
	stretchFactor := flag.Float64("stretch", 1.0, "Stretch factor for playback time (default 1.0)")
	rpcMode := flag.Bool("rpc", false, "Start in JSON-RPC server mode (stdin/stdout)")
	flag.Parse()

	eng := engine.NewEngine()

	// RPC mode: start JSON-RPC server over stdin/stdout
	if *rpcMode {
		server := rpc.NewServer(eng)
		if err := server.Run(); err != nil {
			log.Fatalf("RPC server error: %v", err)
		}
		return
	}

	// CLI mode: load config and play or export
	if err := eng.LoadConfig(*configPath); err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	if *stretchFactor != 1.0 {
		if err := eng.SetStretch(*stretchFactor); err != nil {
			log.Fatalf("Error setting stretch factor: %v", err)
		}
	}

	if *outputPath != "" {
		fmt.Printf("Exporting audio to %s...\n", *outputPath)
		if err := eng.ExportWAV(*outputPath); err != nil {
			log.Fatalf("Error exporting WAV: %v", err)
		}
		fmt.Println("Export completed successfully.")
		return
	}

	// Real-time playback
	if err := eng.Play(); err != nil {
		log.Fatalf("Error starting playback: %v", err)
	}

	// Status ticker
	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			status := eng.GetStatus()
			if !status.IsPlaying {
				ticker.Stop()
				return
			}
			fmt.Printf("Time: %.2f s, Base Frequency: %.2f Hz, Beat Frequency: %.2f Hz, Tone Volume: %.2f, Pink Noise Volume: %.2f\n",
				status.Time, status.Frequency, status.BeatFrequency, status.ToneVolume, status.PinkNoiseVolume)
		}
	}()

	eng.WaitDone()
}
