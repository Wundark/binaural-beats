package engine

import (
	"math"
	"testing"
)

const testSR = 1000

func testItem(total int) playItem {
	return playItem{changes: []FrequencyChange{
		{Time: 0, Frequency: 100, BeatFrequency: 10, ToneVolume: 1},
		{Time: float64(total) / testSR, Frequency: 100, BeatFrequency: 10, ToneVolume: 1},
	}, total: total}
}

func testSequence(items []playItem, idx int, loop bool, fade int) *sequence {
	first := idx
	if first < 0 {
		first = 0
	}
	cur := NewBinauralStream(testSR, items[first].changes, items[first].total, seededNoise())
	return newSequence(cur, items, idx, loop, fade, 1)
}

// drain streams q to the end in blocks of n, recording the playlist index
// at each block.
func drain(t *testing.T, q *sequence, n, limit int) (total int, indexes []int) {
	t.Helper()
	buf := make([][2]float64, n)
	for total < limit {
		got, ok := q.Stream(buf)
		total += got
		indexes = append(indexes, q.Index())
		if !ok || got < n {
			return total, indexes
		}
	}
	return total, indexes
}

func TestSequenceStandalonePlaysOnce(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(1000)}, -1, false, 100)
	if total, _ := drain(t, q, 64, 10000); total != 1000 {
		t.Fatalf("played %d samples, want 1000", total)
	}
}

func TestSequenceCrossfadeOverlaps(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(2000), testItem(500)}, 0, false, 100)
	total, _ := drain(t, q, 64, 10000)
	// Each transition overlaps by the fade: 1000 + 2000 + 500 - 2*100.
	if total != 3300 {
		t.Fatalf("played %d samples, want 3300", total)
	}
	if q.Index() != 2 {
		t.Fatalf("ended on item %d, want 2", q.Index())
	}
}

func TestSequenceSwitchesAtFadeStart(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(1000)}, 0, false, 100)
	buf := make([][2]float64, 900)
	q.Stream(buf)
	if q.Index() != 0 {
		t.Fatalf("index %d before the fade, want 0", q.Index())
	}
	q.Stream(buf[:1])
	if q.Index() != 1 || q.out == nil {
		t.Fatalf("index %d, fading %v after the fade started", q.Index(), q.out != nil)
	}
}

func TestSequenceWithoutFadeIsGapless(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(700)}, 0, false, 0)
	if total, _ := drain(t, q, 64, 10000); total != 1700 {
		t.Fatalf("played %d samples, want 1700", total)
	}
}

func TestSequenceFadeIsEqualPower(t *testing.T) {
	// Same tone in and out: an equal-power fade keeps the level steady
	// within a factor of sqrt(2) (it is exact for uncorrelated signals).
	q := testSequence([]playItem{testItem(2000), testItem(2000)}, 0, false, 1000)
	buf := make([][2]float64, 4000)
	n, _ := q.Stream(buf)
	peak := func(from, to int) float64 {
		m := 0.0
		for _, s := range buf[from:to] {
			m = math.Max(m, math.Abs(s[0]))
		}
		return m
	}
	before, during := peak(0, 1000), peak(1000, 2000)
	if n != 3000 || during < before*0.9 || during > before*1.5 {
		t.Fatalf("n=%d, peak before fade %.3f, during %.3f", n, before, during)
	}
}

func TestSequenceShortItemLimitsFade(t *testing.T) {
	// The fade never takes more than half of the incoming session.
	q := testSequence([]playItem{testItem(1000), testItem(200)}, 0, false, 500)
	if total, _ := drain(t, q, 64, 10000); total != 1100 {
		t.Fatalf("played %d samples, want 1100", total)
	}
}

func TestSequenceLoops(t *testing.T) {
	q := testSequence([]playItem{testItem(500), testItem(500)}, 0, true, 50)
	total, idx := drain(t, q, 50, 5000)
	if total != 5000 {
		t.Fatalf("looping playlist ended after %d samples", total)
	}
	seen := map[int]bool{}
	for _, i := range idx {
		seen[i] = true
	}
	if !seen[0] || !seen[1] || q.Remaining() != -1 {
		t.Fatalf("looping indexes %v, remaining %d", seen, q.Remaining())
	}
}

func TestSequencePauseHoldsFade(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(1000)}, 0, false, 400)
	buf := make([][2]float64, 700)
	q.Stream(buf) // 100 samples into the fade
	q.Pause()
	q.Stream(buf)
	pos := q.fadePos
	q.Stream(buf)
	if q.fadePos != pos || q.out == nil {
		t.Fatalf("fade moved while paused: %d -> %d", pos, q.fadePos)
	}
	q.Resume()
	drain(t, q, 64, 10000)
	if q.Index() != 1 || q.out != nil {
		t.Fatalf("index %d after resuming, want 1", q.Index())
	}
}

func TestSequenceJumpAndRemaining(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(1000), testItem(1000)}, 0, false, 100)
	if r := q.Remaining(); r != 2800 {
		t.Fatalf("remaining %d, want 2800", r)
	}
	q.jump(2)
	if q.Index() != 2 || q.out == nil {
		t.Fatalf("jump: index %d, fading %v", q.Index(), q.out != nil)
	}
	if total, _ := drain(t, q, 64, 10000); total != 1000 {
		t.Fatalf("after jump played %d, want 1000", total)
	}
}

func TestSequenceSeekEndsFade(t *testing.T) {
	q := testSequence([]playItem{testItem(1000), testItem(1000)}, 0, false, 200)
	q.Stream(make([][2]float64, 900))
	q.Seek(0)
	if q.out != nil || q.Position() != 0 || q.Index() != 1 {
		t.Fatalf("after seek: fading %v, position %d, index %d", q.out != nil, q.Position(), q.Index())
	}
}
