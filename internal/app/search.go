package app

import (
	"context"
	"log/slog"

	"github.com/p-society/raag/internal/domain"
)

type SearchService struct {
	index       SearchIndex
	libraryRepo LibraryRepository
}

func NewSearchService(index SearchIndex, libraryRepo LibraryRepository) *SearchService {
	return &SearchService{
		index:       index,
		libraryRepo: libraryRepo,
	}
}

func (s *SearchService) Search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	if limit <= 0 {
		limit = 20
	}

	trackIDs, err := s.index.Search(ctx, query, limit)
	if err != nil {
		slog.Warn("search index failed, falling back to library search", "error", err)
		return s.libraryRepo.Search(ctx, SearchQuery{Query: query, Limit: limit})
	}
	return s.resolveTrackIDs(ctx, trackIDs)
}

func (s *SearchService) SearchFuzzy(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	if limit <= 0 {
		limit = 20
	}

	trackIDs, err := s.index.SearchFuzzy(ctx, query, limit)
	if err != nil {
		slog.Warn("fuzzy search failed, falling back to exact search", "error", err)
		return s.Search(ctx, query, limit)
	}
	return s.resolveTrackIDs(ctx, trackIDs)
}

func (s *SearchService) resolveTrackIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	tracks := make([]*domain.Track, 0, len(ids))
	for _, id := range ids {
		track, err := s.libraryRepo.FindByID(ctx, id)
		if err != nil {
			slog.Warn("failed to resolve track", "id", id, "error", err)
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}
