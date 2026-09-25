package engine

import (
	"testing"
)

func cfgNamed(name string, seconds float64) *Config {
	return &Config{Name: name, FrequencyChanges: []FrequencyChange{
		{Time: 0, Frequency: 200, BeatFrequency: 10, ToneVolume: 1},
		{Time: seconds, Frequency: 200, BeatFrequency: 4, ToneVolume: 1},
	}}
}

func names(p Playlist) []string {
	var n []string
	for _, it := range p.Items {
		n = append(n, it.Name)
	}
	return n
}

func TestPlaylistAddLoadedJoinsPlaylist(t *testing.T) {
	e := NewEngine()
	if _, err := e.PlaylistAdd(nil); err == nil {
		t.Fatal("adding with nothing loaded should fail")
	}
	e.load(cfgNamed("a", 60))
	p, err := e.PlaylistAdd(nil)
	if err != nil || p.Current != 0 || len(p.Items) != 1 {
		t.Fatalf("add loaded: %+v %v", p, err)
	}
	p, _ = e.PlaylistAdd(cfgNamed("b", 30))
	if p.Current != 0 || len(p.Items) != 2 {
		t.Fatalf("add config: %+v", p)
	}
	if _, err := e.PlaylistAdd(&Config{}); err == nil {
		t.Fatal("adding an invalid config should fail")
	}
	// Loading another session takes it out of the playlist.
	e.load(cfgNamed("c", 10))
	if e.Playlist().Current != -1 {
		t.Fatal("a newly loaded session should not be in the playlist")
	}
}

func TestPlaylistMoveAndRemoveTrackCurrent(t *testing.T) {
	e := NewEngine()
	for _, n := range []string{"a", "b", "c", "d"} {
		e.PlaylistAdd(cfgNamed(n, 10))
	}
	if _, err := e.PlaylistSelect(1); err != nil {
		t.Fatal(err)
	}
	if st := e.GetStatus(); st.PlaylistIndex != 1 || !st.ConfigLoaded {
		t.Fatalf("select: %+v", st)
	}
	p, _ := e.PlaylistMove(1, 3)
	if got := names(p); got[3] != "b" || p.Current != 3 {
		t.Fatalf("move current: %v current %d", got, p.Current)
	}
	p, _ = e.PlaylistMove(0, 3)
	if got := names(p); got[3] != "a" || p.Current != 2 {
		t.Fatalf("move before current: %v current %d", got, p.Current)
	}
	p, _ = e.PlaylistRemove(0)
	if p.Current != 1 || len(p.Items) != 3 {
		t.Fatalf("remove before current: %+v", p)
	}
	p, _ = e.PlaylistRemove(1)
	if p.Current != -1 || !e.ConfigLoaded() {
		t.Fatalf("removing the current item should keep it loaded alone: %+v", p)
	}
	if _, err := e.PlaylistMove(0, 5); err == nil {
		t.Fatal("out of range move should fail")
	}
	if p := e.PlaylistClear(); len(p.Items) != 0 || p.Current != -1 {
		t.Fatalf("clear: %+v", p)
	}
}

func TestPlaylistEditWhilePlayingReachesPlayer(t *testing.T) {
	e := NewEngine()
	for _, n := range []string{"a", "b", "c"} {
		e.PlaylistAdd(cfgNamed(n, 10))
	}
	e.PlaylistSelect(1)
	// Stand in for Play without an audio device.
	e.IsPlaying = true
	e.Done = make(chan struct{})
	e.player = newSequence(e.newStream(), e.playItems(), e.plIndex, false, samples(e.crossfade), 1)

	e.PlaylistRemove(0)
	if e.player.Index() != 0 || len(e.player.items) != 2 {
		t.Fatalf("player index %d with %d items", e.player.Index(), len(e.player.items))
	}
	// The player moving on shows up in the engine.
	e.player.jump(1)
	if st := e.GetStatus(); st.PlaylistIndex != 1 {
		t.Fatalf("status index %d, want 1", st.PlaylistIndex)
	}
	if tl, _ := e.Timeline(); tl.Name != "c" {
		t.Fatalf("timeline shows %q, want c", tl.Name)
	}
	if err := e.SetPlaylistOptions(5, true); err != nil || !e.player.loop || e.player.fade != samples(5) {
		t.Fatalf("options did not reach the player: %v", err)
	}
	if err := e.SetPlaylistOptions(-1, false); err == nil {
		t.Fatal("negative crossfade should fail")
	}
}

func TestExportPlaylistWAV(t *testing.T) {
	e := NewEngine()
	if err := e.ExportPlaylistWAV(t.TempDir() + "/x.wav"); err == nil {
		t.Fatal("exporting an empty playlist should fail")
	}
	e.PlaylistAdd(cfgNamed("a", 1))
	e.PlaylistAdd(cfgNamed("b", 1))
	e.SetPlaylistOptions(0.5, false)
	if err := e.ExportPlaylistWAV(t.TempDir() + "/x.wav"); err != nil {
		t.Fatal(err)
	}
}
