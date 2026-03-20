package audio

import (
	"math/rand"
	"sync"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type Queue struct {
	mu      sync.Mutex
	tracks  []*domain.Track
	pos     int
	shuffle bool
	repeat  app.RepeatMode
}

func NewQueue() *Queue {
	return &Queue{
		tracks: make([]*domain.Track, 0),
		pos:    -1,
		repeat: app.RepeatModeNone,
	}
}

func (q *Queue) Peek() *domain.Track {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return nil
	}

	next := q.pos + 1
	if next >= len(q.tracks) {
		if q.repeat == app.RepeatModeAll {
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
	if q.repeat == app.RepeatModeOne {
		return q.tracks[q.pos]
	}

	next := q.pos + 1
	if next >= len(q.tracks) {
		if q.repeat == app.RepeatModeAll {
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
		if q.repeat == app.RepeatModeAll {
			prev = len(q.tracks) - 1
		} else {
			return nil
		}
	}

	q.pos = prev
	return q.tracks[q.pos]
}

func (q *Queue) Current() *domain.Track {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.pos < 0 || q.pos >= len(q.tracks) {
		return nil
	}
	return q.tracks[q.pos]
}

func (q *Queue) Length() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tracks)
}

func (q *Queue) Position() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pos
}

func (q *Queue) Add(track *domain.Track) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.tracks = append(q.tracks, track)
	if q.shuffle {
		last := len(q.tracks) - 1
		j := rand.Intn(last + 1)
		q.tracks[last], q.tracks[j] = q.tracks[j], q.tracks[last]
	}
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

	q.tracks = append(q.tracks[:position], append([]*domain.Track{track}, q.tracks[position:]...)...)
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

	q.tracks = append(q.tracks[:position], q.tracks[position+1:]...)
	if q.pos > position {
		q.pos--
	} else if q.pos == position {
		if q.pos >= len(q.tracks) {
			q.pos = len(q.tracks) - 1
		}
	}
}

func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.tracks = make([]*domain.Track, 0)
	q.pos = -1
}

func (q *Queue) SetRepeat(mode app.RepeatMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.repeat = mode
}

func (q *Queue) GetRepeat() app.RepeatMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.repeat
}

func (q *Queue) ToggleShuffle() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shuffle = !q.shuffle
}

func (q *Queue) GetShuffle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shuffle
}

func (q *Queue) Tracks() []*domain.Track {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*domain.Track, len(q.tracks))
	copy(result, q.tracks)
	return result
}
