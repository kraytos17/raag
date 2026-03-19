package db

import (
	"context"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type cachedLibraryRepo struct {
	source app.LibraryRepository
	cache  map[domain.TrackID]*domain.Track
}

func newCachedLibraryRepo(source app.LibraryRepository) *cachedLibraryRepo {
	return &cachedLibraryRepo{
		source: source,
		cache:  make(map[domain.TrackID]*domain.Track),
	}
}

func (r *cachedLibraryRepo) Save(ctx context.Context, track *domain.Track) error {
	r.cache[track.ID] = track
	return r.source.Save(ctx, track)
}

func (r *cachedLibraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := r.cache[id]; ok {
		return track, nil
	}

	track, err := r.source.FindByID(ctx, id)
	if err == nil {
		r.cache[id] = track
	}
	return track, err
}

func (r *cachedLibraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return r.source.FindByPath(ctx, path)
}

func (r *cachedLibraryRepo) Search(ctx context.Context, query app.SearchQuery) ([]*domain.Track, error) {
	return r.source.Search(ctx, query)
}

func (r *cachedLibraryRepo) Delete(ctx context.Context, id domain.TrackID) error {
	delete(r.cache, id)
	return r.source.Delete(ctx, id)
}

func (r *cachedLibraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	for _, track := range tracks {
		r.cache[track.ID] = track
	}
	return r.source.BulkSave(ctx, tracks)
}

func (r *cachedLibraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	return r.source.ListAll(ctx)
}

func (r *cachedLibraryRepo) SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error {
	return r.source.SaveFileStats(ctx, stats)
}

func (r *cachedLibraryRepo) LoadFileStats(ctx context.Context) (map[string]*domain.FileStat, error) {
	return r.source.LoadFileStats(ctx)
}

func (r *cachedLibraryRepo) Invalidate() {
	r.cache = make(map[domain.TrackID]*domain.Track)
}
