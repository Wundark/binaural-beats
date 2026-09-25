package presets

import (
	"testing"

	"github.com/Wundark/binaural-beats/internal/engine"
)

func TestListAndLoad(t *testing.T) {
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 6 {
		t.Fatalf("expected at least 6 presets, got %d", len(list))
	}
	for i, p := range list {
		if p.Name == "" || p.Description == "" || p.TotalDuration <= 0 {
			t.Errorf("preset %s is missing a name, description or length: %+v", p.ID, p)
		}
		if i > 0 && p.TotalDuration < list[i-1].TotalDuration {
			t.Errorf("presets not sorted by length at %s", p.ID)
		}
		eng := engine.NewEngine()
		info, err := Load(eng, p.ID)
		if err != nil {
			t.Fatalf("Load(%s): %v", p.ID, err)
		}
		if info.Name != p.Name || info.TotalDuration != p.TotalDuration {
			t.Errorf("Load(%s) info %+v does not match list entry %+v", p.ID, info, p)
		}
	}
}

func TestLoadRejectsUnknownAndPaths(t *testing.T) {
	for _, id := range []string{"", "nope", "../go", "focus.yaml", "a/b"} {
		if _, err := Load(engine.NewEngine(), id); err == nil {
			t.Errorf("Load(%q) should fail", id)
		}
	}
}
