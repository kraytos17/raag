package mocks

import (
	"context"
	"sync"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type MockLibraryRepository struct {
	mu       sync.RWMutex
	tracks   map[domain.TrackID]*domain.Track
	byPath   map[string]*domain.Track
	coverArt map[domain.TrackID][]byte
}

func NewMockLibraryRepository() *MockLibraryRepository {
	return &MockLibraryRepository{
		tracks:   make(map[domain.TrackID]*domain.Track),
		byPath:   make(map[string]*domain.Track),
		coverArt: make(map[domain.TrackID][]byte),
	}
}

func (m *MockLibraryRepository) Save(ctx context.Context, track *domain.Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks[track.ID] = track
	m.byPath[track.Path] = track
	return nil
}

func (m *MockLibraryRepository) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *MockLibraryRepository) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tracks := make([]*domain.Track, 0, len(ids))
	for _, id := range ids {
		if track, ok := m.tracks[id]; ok {
			tracks = append(tracks, track)
		}
	}
	return tracks, nil
}

func (m *MockLibraryRepository) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if track, ok := m.byPath[path]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *MockLibraryRepository) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if art, ok := m.coverArt[id]; ok {
		return art, nil
	}
	return nil, nil
}

func (m *MockLibraryRepository) Search(ctx context.Context, query app.SearchQuery) ([]*domain.Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var results []*domain.Track
	for _, track := range m.tracks {
		if len(results) >= query.Limit {
			break
		}
		results = append(results, track)
	}
	return results, nil
}

func (m *MockLibraryRepository) Delete(ctx context.Context, id domain.TrackID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if track, ok := m.tracks[id]; ok {
		delete(m.byPath, track.Path)
	}
	
	delete(m.tracks, id)
	return nil
}

func (m *MockLibraryRepository) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, track := range tracks {
		m.tracks[track.ID] = track
		m.byPath[track.Path] = track
	}
	return nil
}

func (m *MockLibraryRepository) ListAll(ctx context.Context) ([]*domain.Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tracks := make([]*domain.Track, 0, len(m.tracks))
	for _, track := range m.tracks {
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (m *MockLibraryRepository) ListAllPaths(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	paths := make([]string, 0, len(m.byPath))
	for path := range m.byPath {
		paths = append(paths, path)
	}
	return paths, nil
}

func (m *MockLibraryRepository) AddTrack(track *domain.Track) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks[track.ID] = track
	m.byPath[track.Path] = track
}

func (m *MockLibraryRepository) SetCoverArt(id domain.TrackID, art []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coverArt[id] = art
}
