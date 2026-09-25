package engine

import (
	"fmt"
	"math"
)

// PlaylistItem is one session in the playlist.
type PlaylistItem struct {
	SessionInfo
	// Config is the session as loaded, before stretching, so the playlist
	// can be saved and added back later.
	Config *Config `json:"config"`
}

// Playlist is the playlist and how it plays.
type Playlist struct {
	Items []PlaylistItem `json:"items"`
	// Current is the loaded session's index, or -1 if it is not in the playlist.
	Current   int     `json:"current"`
	Crossfade float64 `json:"crossfade"`
	Loop      bool    `json:"loop"`
}

// syncPlaying makes the loaded session the one playing, which changes when
// the playlist moves on. The caller must hold e.Mu.
func (e *Engine) syncPlaying() {
	if !e.IsPlaying {
		return
	}
	if i := e.player.Index(); i != e.plIndex {
		e.selectLocked(i)
	}
}

// selectLocked makes playlist item i the loaded session (or, for i < 0,
// keeps the loaded session outside the playlist). The caller must hold e.Mu.
func (e *Engine) selectLocked(i int) {
	e.plIndex = i
	if i >= 0 {
		e.config = e.playlist[i]
		e.applyStretch()
	}
}

// editPlaylist applies edit, which changes e.playlist and returns the loaded
// session's new index given its current one. While playing, the change
// reaches the player atomically, so it cannot race with the playlist moving on.
func (e *Engine) editPlaylist(edit func(cur int) int) {
	if !e.IsPlaying {
		e.plIndex = edit(e.plIndex)
		return
	}
	e.player.update(func(idx int) ([]playItem, int) {
		e.selectLocked(idx)
		n := edit(idx)
		e.plIndex = n
		return e.playItems(), n
	})
}

// Playlist returns the playlist.
func (e *Engine) Playlist() Playlist {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	e.syncPlaying()
	return e.playlistLocked()
}

func (e *Engine) playlistLocked() Playlist {
	p := Playlist{Items: []PlaylistItem{}, Current: e.plIndex, Crossfade: e.crossfade, Loop: e.loop}
	for _, cfg := range e.playlist {
		p.Items = append(p.Items, PlaylistItem{
			SessionInfo: SessionInfo{
				Name:          cfg.Name,
				Description:   cfg.Description,
				TotalDuration: GetTotalPlaybackTime(cfg.FrequencyChanges) * e.stretch,
			},
			Config: cfg,
		})
	}
	return p
}

// PlaylistAdd adds cfg to the end of the playlist. With a nil cfg it adds
// the loaded session, which then plays as part of the playlist.
func (e *Engine) PlaylistAdd(cfg *Config) (Playlist, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	e.syncPlaying()
	fromLoaded := cfg == nil
	if fromLoaded {
		if e.config == nil {
			return Playlist{}, fmt.Errorf("no config loaded")
		}
		cfg = e.config
	} else {
		if err := cfg.Validate(); err != nil {
			return Playlist{}, err
		}
	}
	e.editPlaylist(func(cur int) int {
		e.playlist = append(e.playlist, cfg)
		if fromLoaded && cur < 0 {
			return len(e.playlist) - 1
		}
		return cur
	})
	return e.playlistLocked(), nil
}

// PlaylistRemove removes item i. If it is the loaded session, that stays
// loaded (and playing) on its own.
func (e *Engine) PlaylistRemove(i int) (Playlist, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if i < 0 || i >= len(e.playlist) {
		return Playlist{}, fmt.Errorf("no playlist item %d", i)
	}
	e.editPlaylist(func(cur int) int {
		e.playlist = append(e.playlist[:i:i], e.playlist[i+1:]...)
		switch {
		case cur == i:
			return -1
		case cur > i:
			return cur - 1
		}
		return cur
	})
	return e.playlistLocked(), nil
}

// PlaylistMove moves item from to position to.
func (e *Engine) PlaylistMove(from, to int) (Playlist, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	n := len(e.playlist)
	if from < 0 || from >= n || to < 0 || to >= n {
		return Playlist{}, fmt.Errorf("playlist move out of range")
	}
	e.editPlaylist(func(cur int) int {
		item := e.playlist[from]
		rest := append(e.playlist[:from:from], e.playlist[from+1:]...)
		e.playlist = append(rest[:to:to], append([]*Config{item}, rest[to:]...)...)
		switch {
		case cur == from:
			return to
		case from < cur && cur <= to:
			return cur - 1
		case to <= cur && cur < from:
			return cur + 1
		}
		return cur
	})
	return e.playlistLocked(), nil
}

// PlaylistClear empties the playlist. The loaded session stays loaded.
func (e *Engine) PlaylistClear() Playlist {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	e.editPlaylist(func(int) int {
		e.playlist = nil
		return -1
	})
	return e.playlistLocked()
}

// PlaylistSelect loads item i. While playing, it crossfades to it.
func (e *Engine) PlaylistSelect(i int) (SessionInfo, error) {
	e.Mu.Lock()
	defer e.Mu.Unlock()
	if i < 0 || i >= len(e.playlist) {
		return SessionInfo{}, fmt.Errorf("no playlist item %d", i)
	}
	if e.IsPlaying {
		e.player.jump(i)
	} else {
		e.startAt = 0
	}
	e.selectLocked(i)
	return e.sessionInfo(), nil
}

// SetPlaylistOptions sets the crossfade between sessions, in seconds, and
// whether the playlist starts again after its last session.
func (e *Engine) SetPlaylistOptions(crossfade float64, loop bool) error {
	if math.IsNaN(crossfade) || crossfade < 0 || crossfade > MaxCrossfade {
		return fmt.Errorf("crossfade must be between 0 and %d seconds", MaxCrossfade)
	}
	e.Mu.Lock()
	defer e.Mu.Unlock()
	e.crossfade, e.loop = crossfade, loop
	if e.IsPlaying {
		e.player.setOptions(samples(crossfade), loop)
	}
	return nil
}
