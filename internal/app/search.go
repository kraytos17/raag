package app

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
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
	remote      RemoteSearcher
}

func NewSearchService(index SearchIndex, libraryRepo LibraryRepository) *SearchService {
	return &SearchService{
		index:       index,
		libraryRepo: libraryRepo,
	}
}

// SetRemote wires the remote searcher (typically the MultiSourceResolver),
// which is constructed only after the P2P node exists.
func (s *SearchService) SetRemote(remote RemoteSearcher) {
	s.remote = remote
}

// Index exposes the underlying search index (used by the scanner).
func (s *SearchService) Index() SearchIndex {
	return s.index
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

// SearchWithRemote runs a local search and, if a remote searcher is available,
// merges parallel per-peer search results, tagging each remote track with its
// peer ID. Local results are ranked first; remote results follow, capped at
// limit each.
func (s *SearchService) SearchWithRemote(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	local, err := s.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if s.remote == nil {
		return local, nil
	}

	merged := append([]*domain.Track(nil), local...)
	seen := make(map[domain.TrackID]struct{}, len(local))
	for _, t := range local {
		seen[t.ID] = struct{}{}
	}
	for _, hit := range s.remote.SearchRemote(ctx, query, limit) {
		for _, t := range hit.Tracks {
			if _, ok := seen[t.ID]; ok {
				continue
			}

			t.PeerID = hit.PeerID
			merged = append(merged, t)
			seen[t.ID] = struct{}{}
		}
	}
	return merged, nil
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

	strippedQuery := domain.StripAudioExtension(query)
	normalizedQuery := domain.Normalize(strippedQuery)
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

	filename := domain.Normalize(filepath.Base(track.Path))
	if strings.Contains(title, normalizedQuery) {
		score += BoostTitle
	}
	if strings.Contains(artist, normalizedQuery) {
		score += BoostArtist
	}
	if strings.Contains(album, normalizedQuery) {
		score += BoostAlbum
	}
	if strings.Contains(filename, normalizedQuery) {
		score += BoostTitle
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

	hoursSince := max(0, time.Since(time.Unix(track.LastPlayed, 0)).Hours())
	return 1.0 / (1.0 + hoursSince/24.0)
}
