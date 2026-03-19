package db

import (
	"context"
	"testing"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type mockLibraryRepository struct {
	tracks map[domain.TrackID]*domain.Track
}

func (m *mockLibraryRepository) Save(ctx context.Context, track *domain.Track) error {
	m.tracks[track.ID] = track
	return nil
}

func (m *mockLibraryRepository) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepository) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	for _, track := range m.tracks {
		if track.Path == path {
			return track, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepository) Search(ctx context.Context, query app.SearchQuery) ([]*domain.Track, error) {
	var results []*domain.Track
	for _, track := range m.tracks {
		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}
		results = append(results, track)
	}
	return results, nil
}

func (m *mockLibraryRepository) Delete(ctx context.Context, id domain.TrackID) error {
	delete(m.tracks, id)
	return nil
}

func (m *mockLibraryRepository) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	for _, track := range tracks {
		m.tracks[track.ID] = track
	}
	return nil
}

func (m *mockLibraryRepository) ListAll(ctx context.Context) ([]*domain.Track, error) {
	tracks := make([]*domain.Track, 0, len(m.tracks))
	for _, track := range m.tracks {
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (m *mockLibraryRepository) SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error {
	return nil
}

func (m *mockLibraryRepository) LoadFileStats(ctx context.Context) (map[string]*domain.FileStat, error) {
	return make(map[string]*domain.FileStat), nil
}

func newMockLibraryRepository() *mockLibraryRepository {
	return &mockLibraryRepository{
		tracks: make(map[domain.TrackID]*domain.Track),
	}
}

func TestCachedRepo_Save(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	track := domain.NewTrack("/music/test.mp3")
	track.Title = "Test Song"
	track.Artist = "Test Artist"
	track.DurationMs = 180000

	ctx := context.Background()
	if err := repo.Save(ctx, track); err != nil {
		t.Errorf("Save() error = %v", err)
	}

	cached, ok := repo.cache[track.ID]
	if !ok {
		t.Error("Save() should cache the track")
	}
	if cached.Title != track.Title {
		t.Errorf("Cached track title = %v, want %v", cached.Title, track.Title)
	}
}

func TestCachedRepo_FindByID_CacheHit(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	track := domain.NewTrack("/music/test.mp3")
	track.DurationMs = 180000
	mock.tracks[track.ID] = track

	ctx := context.Background()
	found, err := repo.FindByID(ctx, track.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}
	if found.Title != track.Title {
		t.Errorf("FindByID() = %v, want %v", found.Title, track.Title)
	}
}

func TestCachedRepo_FindByID_CacheMiss(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	track := domain.NewTrack("/music/test.mp3")
	track.DurationMs = 180000
	mock.tracks[track.ID] = track

	ctx := context.Background()
	_, err := repo.FindByID(ctx, track.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}

	_, ok := repo.cache[track.ID]
	if !ok {
		t.Error("FindByID() should cache the track on cache miss")
	}
}

func TestCachedRepo_FindByID_NotFound(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	ctx := context.Background()
	_, err := repo.FindByID(ctx, domain.GenerateTrackID("/nonexistent"))
	if err != domain.ErrTrackNotFound {
		t.Errorf("FindByID() error = %v, want %v", err, domain.ErrTrackNotFound)
	}
}

func TestCachedRepo_Delete(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	track := domain.NewTrack("/music/test.mp3")
	track.DurationMs = 180000
	mock.tracks[track.ID] = track
	repo.cache[track.ID] = track

	ctx := context.Background()
	if err := repo.Delete(ctx, track.ID); err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	if _, ok := repo.cache[track.ID]; ok {
		t.Error("Delete() should remove track from cache")
	}
}

func TestCachedRepo_Invalidate(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	track := domain.NewTrack("/music/test.mp3")
	track.DurationMs = 180000
	repo.cache[track.ID] = track

	repo.Invalidate()

	if len(repo.cache) != 0 {
		t.Error("Invalidate() should clear the cache")
	}
}

func TestCachedRepo_BulkSave(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/song1.mp3"), Path: "/music/song1.mp3", Title: "Song 1", DurationMs: 180000},
		{ID: domain.GenerateTrackID("/music/song2.mp3"), Path: "/music/song2.mp3", Title: "Song 2", DurationMs: 200000},
	}

	ctx := context.Background()
	if err := repo.BulkSave(ctx, tracks); err != nil {
		t.Errorf("BulkSave() error = %v", err)
	}

	if len(repo.cache) != 2 {
		t.Errorf("BulkSave() should cache all tracks, got %d, want 2", len(repo.cache))
	}
}

func TestCachedRepo_ListAll(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/song1.mp3"), Path: "/music/song1.mp3", Title: "Song 1", DurationMs: 180000},
		{ID: domain.GenerateTrackID("/music/song2.mp3"), Path: "/music/song2.mp3", Title: "Song 2", DurationMs: 200000},
	}
	for _, tr := range tracks {
		mock.tracks[tr.ID] = tr
	}

	ctx := context.Background()
	result, err := repo.ListAll(ctx)
	if err != nil {
		t.Errorf("ListAll() error = %v", err)
	}

	if len(result) != 2 {
		t.Errorf("ListAll() returned %d tracks, want 2", len(result))
	}
}

func TestCachedRepo_SaveFileStats(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	stats := map[string]*domain.FileStat{
		"/music/song1.mp3": {Path: "/music/song1.mp3", Size: 5000},
	}

	ctx := context.Background()
	if err := repo.SaveFileStats(ctx, stats); err != nil {
		t.Errorf("SaveFileStats() error = %v", err)
	}
}

func TestCachedRepo_LoadFileStats(t *testing.T) {
	mock := newMockLibraryRepository()
	repo := newCachedLibraryRepo(mock)

	ctx := context.Background()
	stats, err := repo.LoadFileStats(ctx)
	if err != nil {
		t.Errorf("LoadFileStats() error = %v", err)
	}

	if stats == nil {
		t.Error("LoadFileStats() should return empty map, not nil")
	}
}
