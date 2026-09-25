package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseConfigRejectsInvalid(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"empty file", "", "empty"},
		{"misspelled key", "frequency_change:\n  - time: 0\n", "frequency_change"},
		{"misspelled field", "frequency_changes:\n  - time: 0\n    frequncy: 200\n", "frequncy"},
		{"no entries", "frequency_changes: []\n", "no frequency_changes"},
		{"single entry has no length", "frequency_changes:\n  - time: 0\n    frequency: 200\n    tone_volume: 1\n", "no length"},
		{"negative time", "frequency_changes:\n  - time: -1\n  - time: 10\n", "entry 1: time"},
		{"negative frequency", "frequency_changes:\n  - time: 0\n    frequency: -5\n  - time: 10\n", "entry 1: frequency"},
		{"tone volume too high", "frequency_changes:\n  - time: 0\n  - time: 10\n    tone_volume: 1.5\n", "entry 2: tone_volume"},
		{"negative noise volume", "frequency_changes:\n  - time: 0\n    pink_noise_volume: -0.1\n  - time: 10\n", "entry 1: pink_noise_volume"},
		{"not a number", "frequency_changes:\n  - time: 0\n    frequency: .nan\n  - time: 10\n", "entry 1: frequency must be a finite"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseConfig(writeConfig(t, c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("got error %v, want one containing %q", err, c.wantErr)
			}
		})
	}
}

func TestParseConfigAcceptsValid(t *testing.T) {
	cfg, err := ParseConfig(writeConfig(t, `frequency_changes:
  - time: 60
    frequency: 150
    beat_frequency: -4
    tone_volume: 1
  - time: 0
    frequency: 200
    beat_frequency: 10
    pink_noise_volume: 0.5
    tone_volume: 0.5
  - time: 60
    frequency: 100
`))
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.FrequencyChanges
	if c[0].Time != 0 || c[1].Frequency != 150 || c[2].Frequency != 100 {
		t.Fatalf("want entries sorted by time, keeping file order for equal times: %+v", c)
	}
}

func TestExampleConfigsAreValid(t *testing.T) {
	paths, _ := filepath.Glob("../../example_config/*.yaml")
	if len(paths) == 0 {
		t.Fatal("no example configs found")
	}
	for _, p := range paths {
		if _, err := ParseConfig(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}
