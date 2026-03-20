package app

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/p-society/raag/internal/domain"
)

const (
	rankMatchWeight   = 0.6
	rankPlayWeight    = 0.25
	rankRecencyWeight = 0.15

	BoostTitle  = 3.0
	BoostArtist = 2.0
	BoostAlbum  = 1.5
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
	return s.resolveAndRankTracks(ctx, query, trackIDs, limit)
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
	return s.resolveAndRankTracks(ctx, query, trackIDs, limit)
}

func (s *SearchService) resolveAndRankTracks(ctx context.Context, query string, ids []domain.TrackID, limit int) ([]*domain.Track, error) {
	type scoredTrack struct {
		track *domain.Track
		score float64
	}

	tracks, err := s.libraryRepo.FindByIDs(ctx, ids)
	if err != nil {
		slog.Warn("failed to batch resolve tracks", "error", err)
	}

	scored := make([]scoredTrack, 0, len(tracks))
	for _, track := range tracks {
		score := s.calculateRankScore(query, track)
		scored = append(scored, scoredTrack{track: track, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredTrack) int {
		return cmp.Compare(b.score, a.score)
	})

	result := make([]*domain.Track, 0, min(len(scored), limit))
	for i := 0; i < min(len(scored), limit); i++ {
		result = append(result, scored[i].track)
	}
	return result, nil
}

func (s *SearchService) calculateRankScore(query string, track *domain.Track) float64 {
	matchScore := s.calculateMatchScore(query, track)
	playScore := s.calculatePlayScore(track)
	recencyScore := s.calculateRecencyScore(track)
	return matchScore*rankMatchWeight + playScore*rankPlayWeight + recencyScore*rankRecencyWeight
}

func (s *SearchService) calculateMatchScore(query string, track *domain.Track) float64 {
	normalizedQuery := domain.Normalize(query)
	score := 0.0
	if strings.Contains(domain.Normalize(track.Title), normalizedQuery) {
		score += BoostTitle
	}
	if strings.Contains(domain.Normalize(track.Artist), normalizedQuery) {
		score += BoostArtist
	}
	if strings.Contains(domain.Normalize(track.Album), normalizedQuery) {
		score += BoostAlbum
	}
	return score
}

func (s *SearchService) calculatePlayScore(track *domain.Track) float64 {
	if track.PlayCount == 0 {
		return 0.0
	}
	return math.Log1p(float64(track.PlayCount))
}

func (s *SearchService) calculateRecencyScore(track *domain.Track) float64 {
	if track.LastPlayed == 0 {
		return 0.5
	}
	hoursSince := time.Since(time.Unix(track.LastPlayed, 0)).Hours()
	return 1.0 / (1.0 + hoursSince/24.0)
}
