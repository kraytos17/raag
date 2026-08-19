package app

import (
	"context"
	"io"
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
	idx := &spySearchIndex{}
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
			idx.lastLimit = 0
			_, _ = svc.Search(ctx, "test", tt.inputLimit)
			if idx.lastLimit != tt.wantLimit {
				t.Errorf("Search(limit=%d) passed limit=%d to index, want %d", tt.inputLimit, idx.lastLimit, tt.wantLimit)
			}
		})
	}
}

func TestSearchService_Search_EmptyIndex(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := &mockEmptyLibraryRepo{}
	svc := NewSearchService(idx, repo)
	ctx := context.Background()
	results, err := svc.Search(ctx, rockQuery, 10)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() empty index len = %d, want 0", len(results))
	}
}

type spySearchIndex struct {
	lastLimit int
}

func (s *spySearchIndex) Index(ctx context.Context, track *domain.Track) error { return nil }

func (s *spySearchIndex) IndexBatch(ctx context.Context, tracks []*domain.Track) error { return nil }

func (s *spySearchIndex) Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error) {
	s.lastLimit = limit
	return nil, nil
}
func (s *spySearchIndex) Delete(ctx context.Context, id domain.TrackID) error       { return nil }
func (s *spySearchIndex) Stats(ctx context.Context) (IndexStats, error)             { return IndexStats{}, nil }
func (s *spySearchIndex) Rebuild(ctx context.Context, repo LibraryRepository) error { return nil }

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
			query:     rockQuery,
			track:     &domain.Track{Title: rockSong, Artist: "Band A", Album: "Album A"},
			wantScore: BoostTitle,
		},
		{
			name:      "match artist only",
			query:     "beatles",
			track:     &domain.Track{Title: "Song", Artist: "Beatles", Album: albumToken},
			wantScore: BoostArtist,
		},
		{
			name:      "match album only",
			query:     "abbey",
			track:     &domain.Track{Title: "Song", Artist: bandToken, Album: "Abbey Road"},
			wantScore: BoostAlbum,
		},
		{
			name:      "match multiple fields",
			query:     rockQuery,
			track:     &domain.Track{Title: rockSong, Artist: rockBand, Album: rockAlbum},
			wantScore: BoostTitle + BoostArtist + BoostAlbum,
		},
		{
			name:      "no match",
			query:     "jazz",
			track:     &domain.Track{Title: rockSong, Artist: rockBand, Album: rockAlbum},
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
		Title:     rockSong,
		Artist:    rockBand,
		Album:     rockAlbum,
		PlayCount: 10,
	}

	got := svc.calculateRankScore(rockQuery, track)
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

// TestSearchService_Rebuild_FromRepo verifies Rebuild populates the index from
// the repository, so search works without a scan
func TestSearchService_Rebuild_FromRepo(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := NewMockSearchRepo()
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	repo.AddTrack(&domain.Track{ID: domain.GenerateTrackID("/music/rock1.mp3"), Title: "Rock Song 1", Artist: rockBand, Album: rockAlbum})
	repo.AddTrack(&domain.Track{ID: domain.GenerateTrackID("/music/rock2.mp3"), Title: "Rock Song 2", Artist: rockBand, Album: rockAlbum})
	repo.AddTrack(&domain.Track{ID: domain.GenerateTrackID("/music/jazz1.mp3"), Title: "Jazz Song 1", Artist: jazzBand, Album: jazzAlbum})

	// Before rebuild the empty index returns nothing.
	results, err := svc.Search(ctx, rockQuery, 10)
	if err != nil {
		t.Fatalf("Search() before rebuild error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() before rebuild len = %d, want 0", len(results))
	}
	if err := svc.Index().Rebuild(ctx, repo); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	results, err = svc.Search(ctx, rockQuery, 10)
	if err != nil {
		t.Fatalf("Search() after rebuild error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Search(\"rock\") after rebuild len = %d, want 2", len(results))
	}
}

func TestSearchService_Search_Integration(t *testing.T) {
	idx := NewSearchIndex(nil)
	repo := NewMockSearchRepo()
	svc := NewSearchService(idx, repo)
	ctx := context.Background()

	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/rock1.mp3"), Title: "Rock Song 1", Artist: rockBand, Album: rockAlbum},
		{ID: domain.GenerateTrackID("/music/rock2.mp3"), Title: "Rock Song 2", Artist: rockBand, Album: rockAlbum},
		{ID: domain.GenerateTrackID("/music/jazz1.mp3"), Title: "Jazz Song 1", Artist: jazzBand, Album: jazzAlbum},
	}

	for _, track := range tracks {
		_ = idx.Index(ctx, track)
		repo.AddTrack(track)
	}

	results, err := svc.Search(ctx, rockQuery, 10)
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

// mockRemoteSearcher returns a fixed set of remote hits.
type mockRemoteSearcher struct {
	hits []RemoteSearchHit
}

func (m *mockRemoteSearcher) SearchRemote(_ context.Context, _ string, _ int) []RemoteSearchHit {
	return m.hits
}

func TestSearchService_SearchWithRemote(t *testing.T) {
	ctx := context.Background()
	idx := NewSearchIndex(nil)
	repo := &MockSearchRepo{tracks: make(map[domain.TrackID]*domain.Track)}
	local := &domain.Track{ID: domain.TrackID("local-1"), Title: "Local Song", Artist: artistA, Album: "Album X"}
	repo.AddTrack(local)

	// Remote hits: one local duplicate, one new remote track.
	remote := &domain.Track{ID: domain.TrackID("remote-1"), Title: remoteSong, Artist: "Artist B", Album: "Album Y"}
	dup := &domain.Track{ID: domain.TrackID("local-1"), Title: "Local Song", Artist: artistA, Album: "Album X"}
	remoteSearcher := &mockRemoteSearcher{hits: []RemoteSearchHit{
		{PeerID: "peer-1", Tracks: []*domain.Track{remote, dup}},
	}}

	svc := NewSearchService(idx, repo)
	svc.SetRemote(remoteSearcher)
	// Index the local track so local search returns it.
	if err := idx.Index(ctx, local); err != nil {
		t.Fatalf("index local: %v", err)
	}

	results, err := svc.SearchWithRemote(ctx, "song", 20)
	if err != nil {
		t.Fatalf("SearchWithRemote() error = %v", err)
	}
	// local-1 (local) + remote-1 (tagged). The duplicate remote local-1 is dropped.
	if len(results) != 2 {
		t.Fatalf("SearchWithRemote() = %d results, want 2: %+v", len(results), results)
	}

	var remoteTrack *domain.Track
	for _, tr := range results {
		if tr.ID == "remote-1" {
			remoteTrack = tr
		}
	}
	if remoteTrack == nil {
		t.Fatal("expected remote track in results")
	}
	if remoteTrack.PeerID != "peer-1" {
		t.Fatalf("remote track PeerID = %q, want peer-1", remoteTrack.PeerID)
	}
}

func TestSearchService_SearchWithRemote_NoRemote(t *testing.T) {
	ctx := context.Background()
	idx := NewSearchIndex(nil)
	repo := &MockSearchRepo{tracks: make(map[domain.TrackID]*domain.Track)}
	local := &domain.Track{ID: domain.TrackID("local-1"), Title: "Song A"}
	repo.AddTrack(local)
	if err := idx.Index(ctx, local); err != nil {
		t.Fatalf("index: %v", err)
	}

	// No remote searcher configured → falls back to local-only.
	svc := NewSearchService(idx, repo)
	results, err := svc.SearchWithRemote(ctx, "song", 20)
	if err != nil {
		t.Fatalf("SearchWithRemote() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "local-1" {
		t.Fatalf("SearchWithRemote() no-remote = %+v, want [local-1]", results)
	}
	if results[0].PeerID != "" {
		t.Fatalf("local track PeerID = %q, want empty", results[0].PeerID)
	}
}

// fakeRemoteAdapter fakes the P2PResolverAdapter for fan-out tests.
type fakeRemoteAdapter struct {
	peers         []domain.PeerID
	searchResults map[domain.PeerID][]*domain.Track
}

func (f *fakeRemoteAdapter) Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeRemoteAdapter) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	return nil
}

func (f *fakeRemoteAdapter) LastPeerID() string { return "" }
func (f *fakeRemoteAdapter) FetchTrackMetadata(ctx context.Context, trackID domain.TrackID, peerID string) (*domain.Track, error) {
	return nil, nil
}

func (f *fakeRemoteAdapter) Peers() []domain.PeerID { return f.peers }
func (f *fakeRemoteAdapter) SearchPeer(ctx context.Context, peerID domain.PeerID, query string, limit int) ([]*domain.Track, error) {
	return f.searchResults[peerID], nil
}

func TestMultiSourceResolver_SearchRemote_FanOut(t *testing.T) {
	repo := &MockSearchRepo{tracks: make(map[domain.TrackID]*domain.Track)}
	adapter := &fakeRemoteAdapter{
		peers: []domain.PeerID{"p1", "p2"},
		searchResults: map[domain.PeerID][]*domain.Track{
			"p1": {{ID: domain.TrackID("t1"), Title: "From One"}},
			"p2": {{ID: domain.TrackID("t2"), Title: "From Two"}},
		},
	}

	r := NewMultiSourceResolver(repo, adapter, nil)
	hits := r.(*MultiSourceResolver).SearchRemote(context.Background(), "x", 10)
	if len(hits) != 2 {
		t.Fatalf("SearchRemote() hits = %d, want 2: %+v", len(hits), hits)
	}

	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.PeerID] = true
	}

	want1, want2 := domain.PeerID("p1").String(), domain.PeerID("p2").String()
	if !seen[want1] || !seen[want2] {
		t.Fatalf("SearchRemote() peers = %v, want both %q and %q", seen, want1, want2)
	}
}

func TestMultiSourceResolver_SearchRemote_NoPeers(t *testing.T) {
	repo := &MockSearchRepo{tracks: make(map[domain.TrackID]*domain.Track)}
	adapter := &fakeRemoteAdapter{peers: nil}
	r := NewMultiSourceResolver(repo, adapter, nil)
	if hits := r.(*MultiSourceResolver).SearchRemote(context.Background(), "x", 10); len(hits) != 0 {
		t.Fatalf("SearchRemote() with no peers = %+v, want empty", hits)
	}

	r2 := NewMultiSourceResolver(repo, nil, nil)
	if hits := r2.(*MultiSourceResolver).SearchRemote(context.Background(), "x", 10); len(hits) != 0 {
		t.Fatalf("SearchRemote() with nil adapter = %+v, want empty", hits)
	}
}
