package main

import (
	"reflect"
	"strings"
	"testing"
)

func convert(t *testing.T, sbg string, fade float64) []FrequencyChange {
	t.Helper()
	toneSets, seq, err := parseSbagen(strings.NewReader(sbg))
	if err != nil {
		t.Fatalf("parseSbagen: %v", err)
	}
	changes, err := convertToFrequencyChanges(toneSets, seq, fade)
	if err != nil {
		t.Fatalf("convertToFrequencyChanges: %v", err)
	}
	return changes
}

func TestHoldsThenFades(t *testing.T) {
	got := convert(t, `
## alpha for 15 minutes, then theta, then off
alpha: pink/40 300+10/10
theta: pink/20 150+6/15
off: -

NOW alpha
+00:15 theta
+00:30 off
`, 60)
	want := []FrequencyChange{
		{Time: 0, Frequency: 300, BeatFrequency: 10, PinkNoiseVolume: 0.4, ToneVolume: 0.1},
		{Time: 900, Frequency: 300, BeatFrequency: 10, PinkNoiseVolume: 0.4, ToneVolume: 0.1},
		{Time: 960, Frequency: 150, BeatFrequency: 6, PinkNoiseVolume: 0.2, ToneVolume: 0.15},
		{Time: 1800, Frequency: 150, BeatFrequency: 6, PinkNoiseVolume: 0.2, ToneVolume: 0.15},
		// Fading out keeps the pitch instead of sweeping it to 0 Hz.
		{Time: 1860, Frequency: 150, BeatFrequency: 6, PinkNoiseVolume: 0, ToneVolume: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestSlideAcrossInterval(t *testing.T) {
	got := convert(t, `
a: 300+10/10
b: 150+4/20
NOW a ->
+00:10 b
`, 60)
	want := []FrequencyChange{
		{Time: 0, Frequency: 300, BeatFrequency: 10, ToneVolume: 0.1},
		{Time: 600, Frequency: 150, BeatFrequency: 4, ToneVolume: 0.2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestFadeShortenedByNextEntry(t *testing.T) {
	got := convert(t, `
a: 300+10/10
b: 150+4/20
NOW a
+00:01 b
+00:01:20 a
`, 60)
	// b starts at 60s and a follows 20s later, so the fade into b takes 20s.
	if got[2].Time != 80 || got[2].Frequency != 150 {
		t.Fatalf("fade into b should end at 80s: %+v", got)
	}
}

func TestFadeInFromSilenceUsesNextPitch(t *testing.T) {
	got := convert(t, `
off: -
a: 200+8/30
NOW off
+00:00:30 a
`, 10)
	for _, c := range got {
		if c.Frequency != 200 || c.BeatFrequency != 8 {
			t.Fatalf("silent start should take the pitch of the tone it fades into: %+v", got)
		}
	}
}

func TestClockTimesStartAtZeroAndWrapMidnight(t *testing.T) {
	got := convert(t, `
a: 300+10/10
b: 150+4/10
23:30 a
00:30 b
`, 0)
	if got[0].Time != 0 || got[len(got)-1].Time != 3600 {
		t.Fatalf("clock times should run from 0 to 3600s: %+v", got)
	}
}

func TestNowPlusOffset(t *testing.T) {
	got := convert(t, `
a: 300+10/10
b: 150+4/10
NOW a
NOW+00:10 b
`, 0)
	if got[len(got)-1].Time != 600 || got[len(got)-1].Frequency != 150 {
		t.Fatalf("NOW+00:10 should be at 600s: %+v", got)
	}
}

func TestToneSpecifications(t *testing.T) {
	cases := []struct {
		spec string
		want ToneSet
	}{
		{"300+10/60", ToneSet{Frequency: 300, BeatFrequency: 10, ToneVolume: 0.6}},
		{"200-4/50", ToneSet{Frequency: 200, BeatFrequency: -4, ToneVolume: 0.5}},
		{"100+.5/95", ToneSet{Frequency: 100, BeatFrequency: 0.5, ToneVolume: 0.95}},
		{"150/.5", ToneSet{Frequency: 150, ToneVolume: 0.005}},
		{"pink/40 300+10/10", ToneSet{Frequency: 300, BeatFrequency: 10, PinkNoiseVolume: 0.4, ToneVolume: 0.1}},
		{"-", ToneSet{}},
		{"pink/150 400+14/300", ToneSet{Frequency: 400, BeatFrequency: 14, PinkNoiseVolume: 1, ToneVolume: 1}},
	}
	for _, c := range cases {
		got, err := parseToneSet("x", c.spec)
		if err != nil {
			t.Fatalf("%q: %v", c.spec, err)
		}
		c.want.Name = "x"
		if got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.spec, got, c.want)
		}
	}
}

func TestInlineComments(t *testing.T) {
	got := convert(t, "a: 300+10/10   # alpha\nNOW a # start\n", 60)
	if len(got) != 1 || got[0].Frequency != 300 {
		t.Fatalf("got %+v", got)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"undefined tone-set":  "a: 300+10/10\nNOW b\n",
		"mixed NOW and clock": "a: 300+10/10\nNOW a\n22:00 a\n",
		"bad time line":       "a: 300+10/10\nsoon a\n",
		"bad tone":            "a: 300x10\nNOW a\n",
	}
	for name, sbg := range cases {
		toneSets, seq, err := parseSbagen(strings.NewReader(sbg))
		if err == nil {
			_, err = convertToFrequencyChanges(toneSets, seq, 60)
		}
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
