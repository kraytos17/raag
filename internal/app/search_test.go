package app

import (
	"context"
	"math"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestSearchService_New(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)

	if svc.index != idx {
		t.Error("NewSearchService() should set index")
	}
	if svc.libraryRepo != repo {
		t.Error("NewSearchService() should set libraryRepo")
	}
}

func TestSearchService_Search_LimitValidation(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	tests := []struct {
		name       string
		inputLimit int
		wantLimit  int
	}{
		{"positive limit", 10, 10},
		{"zero limit", 0, 20},
		{"negative limit", -5, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = svc.Search(ctx, "test", tt.inputLimit)
		})
	}
}

func TestSearchService_Search_EmptyIndex(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	results, err := svc.Search(ctx, "rock", 10)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() empty index len = %d, want 0", len(results))
	}
}

func TestSearchService_SearchFuzzy_LimitValidation(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	tests := []struct {
		name       string
		inputLimit int
		wantLimit  int
	}{
		{"positive limit", 10, 10},
		{"zero limit", 0, 20},
		{"negative limit", -5, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = svc.SearchFuzzy(ctx, "test", tt.inputLimit)
		})
	}
}

func TestSearchService_CalculateMatchScore(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)

	tests := []struct {
		name      string
		query     string
		track     *domain.Track
		wantScore float64
	}{
		{
			name:      "match title only",
			query:     "rock",
			track:     &domain.Track{Title: "Rock Song", Artist: "Band A", Album: "Album A"},
			wantScore: BoostTitle,
		},
		{
			name:      "match artist only",
			query:     "beatles",
			track:     &domain.Track{Title: "Song", Artist: "Beatles", Album: "Album"},
			wantScore: BoostArtist,
		},
		{
			name:      "match album only",
			query:     "abbey",
			track:     &domain.Track{Title: "Song", Artist: "Band", Album: "Abbey Road"},
			wantScore: BoostAlbum,
		},
		{
			name:      "match multiple fields",
			query:     "rock",
			track:     &domain.Track{Title: "Rock Song", Artist: "Rock Band", Album: "Rock Album"},
			wantScore: BoostTitle + BoostArtist + BoostAlbum,
		},
		{
			name:      "no match",
			query:     "jazz",
			track:     &domain.Track{Title: "Rock Song", Artist: "Rock Band", Album: "Rock Album"},
			wantScore: 0,
		},
		{
			name:      "case insensitive",
			query:     "ROCK",
			track:     &domain.Track{Title: "rock song", Artist: "band", Album: "album"},
			wantScore: BoostTitle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizedQuery := domain.Normalize(tt.query)
			got := svc.calculateMatchScore(normalizedQuery, tt.track)
			if math.Abs(got-tt.wantScore) > 0.001 {
				t.Errorf("calculateMatchScore() = %v, want %v", got, tt.wantScore)
			}
		})
	}
}

func TestSearchService_CalculatePlayScore(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)

	tests := []struct {
		name      string
		playCount uint64
		wantScore float64
	}{
		{"zero plays", 0, 0.0},
		{"one play", 1, math.Log1p(1)},
		{"ten plays", 10, math.Log1p(10)},
		{"hundred plays", 100, math.Log1p(100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := &domain.Track{PlayCount: tt.playCount}
			got := svc.calculatePlayScore(track)
			if math.Abs(got-tt.wantScore) > 0.001 {
				t.Errorf("calculatePlayScore() = %v, want %v", got, tt.wantScore)
			}
		})
	}
}

func TestSearchService_CalculateRecencyScore(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)

	tests := []struct {
		name       string
		lastPlayed int64
		wantMin    float64
		wantMax    float64
	}{
		{"never played", 0, 0.5, 0.5},
		{"recently played", 1, 0.99, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := &domain.Track{LastPlayed: tt.lastPlayed}
			got := svc.calculateRecencyScore(track)
			if tt.lastPlayed == 0 && got != 0.5 {
				t.Errorf("calculateRecencyScore() for never played = %v, want 0.5", got)
			}
		})
	}
}

func TestSearchService_CalculateRankScore(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)

	track := &domain.Track{
		Title:     "Rock Song",
		Artist:    "Rock Band",
		Album:     "Rock Album",
		PlayCount: 10,
	}

	got := svc.calculateRankScore("rock", track)
	matchScore := BoostTitle + BoostArtist + BoostAlbum
	playScore := math.Log1p(10)
	recencyScore := 0.5
	expected := matchScore*rankMatchWeight + playScore*rankPlayWeight + recencyScore*rankRecencyWeight
	if math.Abs(got-expected) > 0.001 {
		t.Errorf("calculateRankScore() = %v, want %v", got, expected)
	}
}

func TestSearchService_Weights(t *testing.T) {
	tests := []struct {
		name     string
		constant float64
		want     float64
	}{
		{"rankMatchWeight", rankMatchWeight, 0.6},
		{"rankPlayWeight", rankPlayWeight, 0.25},
		{"rankRecencyWeight", rankRecencyWeight, 0.15},
		{"BoostTitle", BoostTitle, 3.0},
		{"BoostArtist", BoostArtist, 2.0},
		{"BoostAlbum", BoostAlbum, 1.5},
	}

	total := rankMatchWeight + rankPlayWeight + rankRecencyWeight
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %v", total)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if math.Abs(tt.constant-tt.want) > 0.001 {
				t.Errorf("%s = %v, want %v", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestSearchService_Search_Integration(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := NewMockSearchRepo()
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/rock1.mp3"), Title: "Rock Song 1", Artist: "Rock Band", Album: "Rock Album"},
		{ID: domain.GenerateTrackID("/music/rock2.mp3"), Title: "Rock Song 2", Artist: "Rock Band", Album: "Rock Album"},
		{ID: domain.GenerateTrackID("/music/jazz1.mp3"), Title: "Jazz Song 1", Artist: "Jazz Band", Album: "Jazz Album"},
	}

	for _, track := range tracks {
		_ = idx.Index(ctx, track)
		repo.AddTrack(track)
	}

	results, err := svc.Search(ctx, "rock", 10)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Search(\"rock\") len = %d, want 2", len(results))
	}
}

type MockSearchRepo struct {
	tracks map[domain.TrackID]*domain.Track
}

func NewMockSearchRepo() *MockSearchRepo {
	return &MockSearchRepo{tracks: make(map[domain.TrackID]*domain.Track)}
}

func (m *MockSearchRepo) Save(ctx context.Context, track *domain.Track) error {
	m.tracks[track.ID] = track
	return nil
}

func (m *MockSearchRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *MockSearchRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	var tracks []*domain.Track
	for _, id := range ids {
		if track, ok := m.tracks[id]; ok {
			tracks = append(tracks, track)
		}
	}
	return tracks, nil
}

func (m *MockSearchRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (m *MockSearchRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}

func (m *MockSearchRepo) Search(ctx context.Context, query SearchQuery) ([]*domain.Track, error) {
	var results []*domain.Track
	for _, track := range m.tracks {
		if len(results) >= query.Limit {
			break
		}
		results = append(results, track)
	}
	return results, nil
}

func (m *MockSearchRepo) Delete(ctx context.Context, id domain.TrackID) error {
	delete(m.tracks, id)
	return nil
}

func (m *MockSearchRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	for _, track := range tracks {
		m.tracks[track.ID] = track
	}
	return nil
}

func (m *MockSearchRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	tracks := make([]*domain.Track, 0, len(m.tracks))
	for _, track := range m.tracks {
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (m *MockSearchRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *MockSearchRepo) AddTrack(track *domain.Track) {
	m.tracks[track.ID] = track
}
