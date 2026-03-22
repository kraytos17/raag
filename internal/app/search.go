package app

import (
	"cmp"
	"context"
	"fmt"
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
		return nil, fmt.Errorf("search index failed: %w", err)
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

	normalizedQuery := domain.Normalize(query)
	all := make([]scoredTrack, 0, len(tracks))
	for _, track := range tracks {
		all = append(all, scoredTrack{
			track: track,
			score: s.calculateRankScore(normalizedQuery, track),
		})
	}

	slices.SortFunc(all, func(a, b scoredTrack) int {
		return cmp.Compare(b.score, a.score)
	})

	n := min(len(all), limit)
	result := make([]*domain.Track, n)
	for i := range n {
		result[i] = all[i].track
	}
	return result, nil
}

func (s *SearchService) calculateRankScore(normalizedQuery string, track *domain.Track) float64 {
	matchScore := s.calculateMatchScore(normalizedQuery, track)
	playScore := s.calculatePlayScore(track)
	recencyScore := s.calculateRecencyScore(track)
	return matchScore*rankMatchWeight + playScore*rankPlayWeight + recencyScore*rankRecencyWeight
}

func (s *SearchService) calculateMatchScore(normalizedQuery string, track *domain.Track) float64 {
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
