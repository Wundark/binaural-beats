package engine

import (
	"math"
	"sync"
)

// playItem is one playlist session, ready to play: its changes after
// stretching and its length in samples.
type playItem struct {
	changes []FrequencyChange
	total   int
}

// sequence plays a session and then the playlist items after it, crossfading
// from one to the next. The session being played is either a playlist item
// (idx >= 0) or a session outside the playlist (idx < 0), which plays alone.
//
// Pause, Resume, Seek, SetVolume, jump and update may be called from any
// goroutine while it plays. sequence never takes another lock while holding
// its own, so it can be called with the engine's or the output's lock held.
type sequence struct {
	mu     sync.Mutex
	sr     int
	items  []playItem
	idx    int // playlist index of cur, or -1
	loop   bool
	fade   int // crossfade length in samples
	volume float64
	paused bool

	cur *BinauralStream
	// out is the previous session while it fades out under cur.
	out              *BinauralStream
	fadePos, fadeLen int
	buf              [][2]float64
}

func newSequence(cur *BinauralStream, items []playItem, idx int, loop bool, fade int, volume float64) *sequence {
	cur.setVolumeNow(volume)
	return &sequence{sr: int(cur.sr), items: items, idx: idx, loop: loop, fade: fade, volume: volume, cur: cur}
}

// newStream starts item i at the current volume and pause state. The caller
// must hold q.mu.
func (q *sequence) newStream(i int) *BinauralStream {
	it := q.items[i]
	s := NewBinauralStream(q.cur.sr, it.changes, it.total, NewPinkNoise())
	s.setVolumeNow(q.volume)
	if q.paused {
		s.Pause()
		s.env = 0
	}
	return s
}

// next returns the playlist index that follows cur, or -1 at the end.
func (q *sequence) next() int {
	if q.idx < 0 || q.idx >= len(q.items) {
		return -1
	}
	if q.idx+1 < len(q.items) {
		return q.idx + 1
	}
	if q.loop {
		return 0
	}
	return -1
}

// crossfadeTo starts item i under cur, fading over at most maxLen samples.
// A fade never takes more than half of the incoming session.
func (q *sequence) crossfadeTo(i, maxLen int) {
	next := q.newStream(i)
	n := min(q.fade, maxLen, next.total/2)
	q.out, q.cur, q.idx = q.cur, next, i
	q.fadePos, q.fadeLen = 0, n
	if n <= 0 {
		q.out = nil
	}
}

func (q *sequence) Stream(samples [][2]float64) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	filled, skips := 0, 0
	for filled < len(samples) {
		chunk := samples[filled:]
		if q.out == nil {
			if i := q.next(); i >= 0 {
				// Start the crossfade exactly its length before cur ends.
				fade := min(q.fade, q.items[i].total/2)
				rem := q.cur.total - q.cur.Position()
				if rem <= fade {
					q.crossfadeTo(i, rem)
				} else if rem-fade < len(chunk) {
					chunk = chunk[:rem-fade]
				}
			}
		}
		if q.out != nil && q.fadeLen-q.fadePos < len(chunk) {
			chunk = chunk[:q.fadeLen-q.fadePos]
		}

		n := q.mix(chunk)
		if n == 0 {
			// cur ended without a crossfade: go straight to the next item,
			// unless every item is empty.
			i := q.next()
			if i < 0 || skips > len(q.items) {
				break
			}
			skips++
			q.cur, q.idx, q.out = q.newStream(i), i, nil
			continue
		}
		filled += n
	}
	return filled, filled > 0
}

// mix streams cur into samples, blended with out while it fades out.
func (q *sequence) mix(samples [][2]float64) int {
	n, _ := q.cur.Stream(samples)
	if q.out == nil || n == 0 {
		return n
	}
	if cap(q.buf) < n {
		q.buf = make([][2]float64, n)
	}
	old := q.buf[:n]
	before := q.out.Position()
	m, _ := q.out.Stream(old)
	// Equal-power fade. Progress follows the outgoing stream, so it holds
	// still while paused.
	for i := 0; i < n; i++ {
		p := math.Min(1, float64(q.fadePos+i)/float64(q.fadeLen)) * math.Pi / 2
		var o [2]float64
		if i < m {
			o = old[i]
		}
		in, fo := math.Sin(p), math.Cos(p)
		samples[i][0] = samples[i][0]*in + o[0]*fo
		samples[i][1] = samples[i][1]*in + o[1]*fo
	}
	q.fadePos += q.out.Position() - before
	if m < n || q.fadePos >= q.fadeLen {
		q.out = nil
	}
	return n
}

func (q *sequence) Err() error { return nil }

// Position returns the position in the current session, in samples.
func (q *sequence) Position() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.cur.Position()
}

// Index returns the playlist index of the current session, or -1.
func (q *sequence) Index() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.idx
}

func (q *sequence) Pause()  { q.setPaused(true) }
func (q *sequence) Resume() { q.setPaused(false) }

func (q *sequence) setPaused(p bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paused = p
	for _, s := range []*BinauralStream{q.cur, q.out} {
		if s == nil {
			continue
		}
		if p {
			s.Pause()
		} else {
			s.Resume()
		}
	}
}

func (q *sequence) Paused() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.paused
}

// Seek moves the current session to the given sample, ending any crossfade.
func (q *sequence) Seek(sample int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.out = nil
	q.cur.Seek(sample)
}

func (q *sequence) SetVolume(v float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.volume = v
	q.cur.SetVolume(v)
	if q.out != nil {
		q.out.SetVolume(v)
	}
}

// jump crossfades to playlist item i now.
func (q *sequence) jump(i int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.out = nil
	q.crossfadeTo(i, math.MaxInt)
}

// update changes the playlist while it plays. edit receives the current
// session's index and returns the new items and that session's new index.
func (q *sequence) update(edit func(idx int) ([]playItem, int)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items, q.idx = edit(q.idx)
}

// setOptions changes the crossfade length (in samples) and looping.
func (q *sequence) setOptions(fade int, loop bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fade, q.loop = fade, loop
}

// Remaining returns the samples left until playback ends, or -1 if the
// playlist loops.
func (q *sequence) Remaining() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	rem := q.cur.total - q.cur.Position()
	if q.idx < 0 {
		return rem
	}
	if q.loop && len(q.items) > 0 {
		return -1
	}
	prev := rem
	for _, it := range q.items[q.idx+1:] {
		// Each crossfade overlaps two sessions (see crossfadeTo).
		rem += it.total - min(q.fade, it.total/2, prev)
		prev = it.total
	}
	return rem
}
