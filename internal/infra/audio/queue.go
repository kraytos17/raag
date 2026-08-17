package audio

import (
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

type Queue struct {
	mu      sync.RWMutex
	tracks  []*domain.Track
	pos     int
	shuffle bool
	repeat  domain.RepeatMode
}

func NewQueue() *Queue {
	return &Queue{
		tracks: make([]*domain.Track, 0),
		pos:    -1,
		repeat: domain.RepeatModeNone,
	}
}

func (q *Queue) Peek() *domain.Track {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.tracks) == 0 {
		return nil
	}

	next := q.pos + 1
	if next >= len(q.tracks) {
		if q.repeat == domain.RepeatModeAll {
			next = 0
		} else {
			return nil
		}
	}
	return q.tracks[next]
}

func (q *Queue) Next() *domain.Track {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return nil
	}
	if q.repeat == domain.RepeatModeOne {
		if q.pos < 0 {
			return nil
		}
		return q.tracks[q.pos]
	}

	next := q.pos + 1
	if next >= len(q.tracks) {
		if q.repeat == domain.RepeatModeAll {
			next = 0
		} else {
			return nil
		}
	}

	q.pos = next
	return q.tracks[q.pos]
}

func (q *Queue) Previous() *domain.Track {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return nil
	}

	prev := q.pos - 1
	if prev < 0 {
		if q.repeat == domain.RepeatModeAll {
			prev = len(q.tracks) - 1
		} else {
			return nil
		}
	}

	q.pos = prev
	return q.tracks[q.pos]
}

func (q *Queue) Current() *domain.Track {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.pos < 0 || q.pos >= len(q.tracks) {
		return nil
	}
	return q.tracks[q.pos]
}

func (q *Queue) Length() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracks)
}

func (q *Queue) Position() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.pos
}

func (q *Queue) Add(track *domain.Track) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.tracks = append(q.tracks, track)
	if q.shuffle {
		last := len(q.tracks) - 1
		j := rand.IntN(last + 1)
		q.tracks[last], q.tracks[j] = q.tracks[j], q.tracks[last]
	}
}

// MoveTo makes the given track the current queue position. If the track is not
// already queued it is appended first (preserving its metadata). Returns true
// when the track is now current (always, unless the queue is nil-backed).
func (q *Queue) MoveTo(track *domain.Track) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if track == nil {
		return false
	}
	for i, t := range q.tracks {
		if t.ID == track.ID {
			q.pos = i
			return true
		}
	}

	q.tracks = append(q.tracks, track)
	if q.shuffle {
		last := len(q.tracks) - 1
		j := rand.IntN(last + 1)
		q.tracks[last], q.tracks[j] = q.tracks[j], q.tracks[last]
	}
	// Re-find after shuffle so pos points at the moved track.
	for i, t := range q.tracks {
		if t.ID == track.ID {
			q.pos = i
			break
		}
	}
	return true
}

func (q *Queue) Insert(position int, track *domain.Track) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if position < 0 {
		position = 0
	}
	if position > len(q.tracks) {
		position = len(q.tracks)
	}

	q.tracks = slices.Insert(q.tracks, position, track)
	if q.pos >= position {
		q.pos++
	}
}

func (q *Queue) Remove(position int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if position < 0 || position >= len(q.tracks) {
		return
	}

	q.tracks = slices.Delete(q.tracks, position, position+1)
	if q.pos > position {
		q.pos--
	} else if q.pos == position {
		if q.pos >= len(q.tracks) {
			q.pos = len(q.tracks) - 1
		}
	}
}

// Clear removes all tracks from the queue and resets the position.
// The underlying capacity is retained to avoid reallocations when the queue is refilled.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	clear(q.tracks)
	q.tracks = q.tracks[:0]
	q.pos = -1
}

func (q *Queue) SetRepeat(mode domain.RepeatMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.repeat = mode
}

func (q *Queue) GetRepeat() domain.RepeatMode {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.repeat
}

func (q *Queue) ToggleShuffle() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shuffle = !q.shuffle
}

func (q *Queue) SetShuffle(shuffle bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shuffle = shuffle
}

func (q *Queue) GetShuffle() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.shuffle
}

func (q *Queue) Tracks() []*domain.Track {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]*domain.Track, len(q.tracks))
	copy(result, q.tracks)
	return result
}
