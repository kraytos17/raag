package mocks

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type MockSearchIndex struct {
	mu      sync.RWMutex
	tracks  map[domain.TrackID]*domain.Track
	terms   map[string][]domain.TrackID
	trigram map[string][]domain.TrackID
}

func NewMockSearchIndex() *MockSearchIndex {
	return &MockSearchIndex{
		tracks:  make(map[domain.TrackID]*domain.Track),
		terms:   make(map[string][]domain.TrackID),
		trigram: make(map[string][]domain.TrackID),
	}
}

func (m *MockSearchIndex) Index(ctx context.Context, track *domain.Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks[track.ID] = track
	return nil
}

func (m *MockSearchIndex) IndexBatch(ctx context.Context, tracks []*domain.Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, track := range tracks {
		m.tracks[track.ID] = track
	}
	return nil
}

func (m *MockSearchIndex) Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var results []domain.TrackID
	for id := range m.tracks {
		if limit > 0 && len(results) >= limit {
			break
		}
		results = append(results, id)
	}
	return results, nil
}

func (m *MockSearchIndex) SearchFuzzy(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	return m.Search(ctx, query, limit)
}

func (m *MockSearchIndex) Delete(ctx context.Context, id domain.TrackID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tracks, id)
	return nil
}

func (m *MockSearchIndex) Stats(ctx context.Context) (app.IndexStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return app.IndexStats{}, nil
}

func (m *MockSearchIndex) Rebuild(ctx context.Context, repo app.LibraryRepository) error {
	return nil
}

type MockPlayer struct {
	playCalled   bool
	pauseCalled  bool
	resumeCalled bool
	stopCalled   bool
	seekCalled   bool
	SeekErr      error
	volume       int
	position     time.Duration
	state        domain.PlayerState
	lastMimeType string
	lastReader   io.Reader
}

func NewMockPlayer() *MockPlayer {
	return &MockPlayer{
		state:  domain.PlayerStateIdle,
		volume: 80,
	}
}

func (m *MockPlayer) Play(ctx context.Context, reader io.Reader, mimeType string) error {
	m.playCalled = true
	m.lastReader = reader
	m.lastMimeType = mimeType
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *MockPlayer) Pause(ctx context.Context) error {
	m.pauseCalled = true
	m.state = domain.PlayerStatePaused
	return nil
}

func (m *MockPlayer) Resume(ctx context.Context) error {
	m.resumeCalled = true
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *MockPlayer) Stop(ctx context.Context) error {
	m.stopCalled = true
	m.state = domain.PlayerStateIdle
	return nil
}

func (m *MockPlayer) Seek(ctx context.Context, position time.Duration) error {
	m.seekCalled = true
	m.position = position
	return m.SeekErr
}

func (m *MockPlayer) SetVolume(ctx context.Context, volume int) error {
	m.volume = volume
	return nil
}

func (m *MockPlayer) GetState() domain.PlayerState {
	return m.state
}

func (m *MockPlayer) GetPosition() time.Duration {
	return m.position
}

func (m *MockPlayer) Reset() {
	m.playCalled = false
	m.pauseCalled = false
	m.resumeCalled = false
	m.stopCalled = false
	m.seekCalled = false
	m.SeekErr = nil
	m.state = domain.PlayerStateIdle
	m.position = 0
}

type MockQueue struct {
	tracks   []*domain.Track
	position int
}

func NewMockQueue() *MockQueue {
	return &MockQueue{
		tracks:   []*domain.Track{},
		position: 0,
	}
}

func (m *MockQueue) Peek() *domain.Track {
	if len(m.tracks) == 0 {
		return nil
	}
	return m.tracks[0]
}

func (m *MockQueue) Next() *domain.Track {
	if m.position >= len(m.tracks)-1 {
		return nil
	}
	
	m.position++
	return m.tracks[m.position]
}

func (m *MockQueue) Previous() *domain.Track {
	if m.position <= 0 {
		return nil
	}
	
	m.position--
	return m.tracks[m.position]
}

func (m *MockQueue) Current() *domain.Track {
	if len(m.tracks) == 0 || m.position >= len(m.tracks) {
		return nil
	}
	return m.tracks[m.position]
}

func (m *MockQueue) Length() int {
	return len(m.tracks)
}

func (m *MockQueue) Add(track *domain.Track) {
	m.tracks = append(m.tracks, track)
}

func (m *MockQueue) SetTracks(tracks []*domain.Track) {
	m.tracks = tracks
	m.position = 0
}
