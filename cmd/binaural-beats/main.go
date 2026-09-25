package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Wundark/binaural-beats/internal/engine"
	"github.com/Wundark/binaural-beats/internal/presets"
	"github.com/Wundark/binaural-beats/internal/rpc"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to the session file (YAML or SBaGen .sbg)")
	preset := flag.String("preset", "", "Play a built-in session instead of -config (see -list-presets)")
	listPresets := flag.Bool("list-presets", false, "List the built-in sessions and exit")
	outputPath := flag.String("output", "", "Path to the output WAV file (if empty, audio will be played)")
	stretchFactor := flag.Float64("stretch", 1.0, "Stretch factor for playback time (default 1.0)")
	rpcMode := flag.Bool("rpc", false, "Start in JSON-RPC server mode (stdin/stdout)")
	volume := flag.Float64("volume", 1.0, "Playback volume from 0 to 1 (playback only)")
	start := flag.Float64("start", 0, "Start playback this many seconds into the session (after stretching)")
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

	if *listPresets {
		list, err := presets.List()
		if err != nil {
			log.Fatalf("Error listing presets: %v", err)
		}
		for _, p := range list {
			fmt.Printf("%-12s %-12s %s\n", p.ID, formatDuration(p.TotalDuration), p.Name)
		}
		return
	}

	// CLI mode: load config and play or export
	var info engine.SessionInfo
	var err error
	if *preset != "" {
		info, err = presets.Load(eng, *preset)
	} else {
		info, err = eng.LoadConfig(*configPath)
	}
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	fmt.Printf("Session: %s (%s)\n", info.Name, formatDuration(info.TotalDuration))

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
	if err := eng.SetVolume(*volume); err != nil {
		log.Fatalf("Error setting volume: %v", err)
	}
	if *start != 0 {
		if err := eng.Seek(*start); err != nil {
			log.Fatalf("Error setting start position: %v", err)
		}
	}
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

// formatDuration formats seconds as h:mm:ss or m:ss.
func formatDuration(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	h, m, sec := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}
