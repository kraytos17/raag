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
	rankMatchWeight   = domain.RankMatchWeight
	rankPlayWeight    = domain.RankPlayWeight
	rankRecencyWeight = domain.RankRecencyWeight

	BoostTitle  = domain.BoostTitle
	BoostArtist = domain.BoostArtist
	BoostAlbum  = domain.BoostAlbum
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
		if d := cmp.Compare(b.score, a.score); d != 0 {
			return d
		}
		return cmp.Compare(a.track.ID, b.track.ID)
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
	title := track.NormalizedTitle
	if title == "" {
		title = domain.Normalize(track.Title)
	}

	artist := track.NormalizedArtist
	if artist == "" {
		artist = domain.Normalize(track.Artist)
	}

	album := track.NormalizedAlbum
	if album == "" {
		album = domain.Normalize(track.Album)
	}
	if strings.Contains(title, normalizedQuery) {
		score += BoostTitle
	}
	if strings.Contains(artist, normalizedQuery) {
		score += BoostArtist
	}
	if strings.Contains(album, normalizedQuery) {
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
	if hoursSince < 0 {
		hoursSince = 0
	}
	return 1.0 / (1.0 + hoursSince/24.0)
}
