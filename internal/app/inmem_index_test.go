package app

import (
	"context"
	"sync"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestIndex_New(t *testing.T) {
	repo := &mockEmptyLibraryRepo{}
	idx := NewSearchIndex(repo).(*inmemoryIndex)
	if idx.terms == nil {
		t.Error("NewSearchIndex() terms map should be initialized")
	}
	if idx.trigram == nil {
		t.Error("NewSearchIndex() trigram map should be initialized")
	}
	if idx.trackToks == nil {
		t.Error("NewSearchIndex() trackToks map should be initialized")
	}
	if idx.trackTris == nil {
		t.Error("NewSearchIndex() trackTris map should be initialized")
	}
}

func TestIndex_Index_Single(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()
	track := &domain.Track{
		ID:     domain.GenerateTrackID("/music/song.mp3"),
		Title:  helloWorldTitle,
		Artist: testArtist,
		Album:  testAlbum,
	}

	if err := idx.Index(ctx, track); err != nil {
		t.Errorf("Index() error = %v", err)
	}
	if len(idx.terms) == 0 {
		t.Error("Index() should populate terms map")
	}
	if len(idx.trackToks[track.ID]) == 0 {
		t.Error("Index() should store track tokens")
	}
}

func TestIndex_Index_Batch(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()
	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/song1.mp3"), Title: "Song One", Artist: artistA, Album: "Album 1"},
		{ID: domain.GenerateTrackID("/music/song2.mp3"), Title: "Song Two", Artist: "Artist B", Album: "Album 2"},
		{ID: domain.GenerateTrackID("/music/song3.mp3"), Title: "Song Three", Artist: "Artist C", Album: "Album 3"},
	}
	if err := idx.IndexBatch(ctx, tracks); err != nil {
		t.Errorf("IndexBatch() error = %v", err)
	}
	if len(idx.trackToks) != 3 {
		t.Errorf("IndexBatch() trackToks length = %d, want 3", len(idx.trackToks))
	}
}

func TestIndex_Index_Duplicate(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()
	track := &domain.Track{
		ID:     domain.GenerateTrackID("/music/song.mp3"),
		Title:  helloWorldTitle,
		Artist: testArtist,
		Album:  testAlbum,
	}

	if err := idx.Index(ctx, track); err != nil {
		t.Errorf("Index() error = %v", err)
	}
	if len(idx.trackToks[track.ID]) == 0 {
		t.Errorf("Index() first call token count = %d, want > 0", len(idx.trackToks[track.ID]))
	}

	track.Title = "Updated Title"
	if err := idx.Index(ctx, track); err != nil {
		t.Errorf("Index() re-index error = %v", err)
	}

	tokens := idx.trackToks[track.ID]
	if len(tokens) == 0 {
		t.Errorf("Index() re-index token count = %d, want > 0", len(tokens))
	}

	hasUpdated := false
	for _, tok := range tokens {
		if tok == "updated" || tok == "title" {
			hasUpdated = true
			break
		}
	}
	if !hasUpdated {
		t.Errorf("Index() re-index should contain updated tokens, got %v", tokens)
	}
}

func TestIndex_Search_Exact(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	id1 := domain.GenerateTrackID("/music/song1.mp3")
	id2 := domain.GenerateTrackID("/music/song2.mp3")
	id3 := domain.GenerateTrackID("/music/song3.mp3")

	_ = idx.Index(ctx, &domain.Track{ID: id1, Title: rockSong, Artist: rockBand, Album: rockAlbum})
	_ = idx.Index(ctx, &domain.Track{ID: id2, Title: jazzSong, Artist: jazzBand, Album: jazzAlbum})
	_ = idx.Index(ctx, &domain.Track{ID: id3, Title: "Classical Piece", Artist: "Orchestra", Album: "Classical"})

	tests := []struct {
		name    string
		query   string
		wantLen int
		wantIDs []domain.TrackID
	}{
		{
			name:    "match title rock",
			query:   rockQuery,
			wantLen: 1,
			wantIDs: []domain.TrackID{id1},
		},
		{
			name:    "match artist jazz",
			query:   "jazz",
			wantLen: 1,
			wantIDs: []domain.TrackID{id2},
		},
		{
			name:    "match album classical",
			query:   "classical",
			wantLen: 1,
			wantIDs: []domain.TrackID{id3},
		},
		{
			name:    "no match",
			query:   "blues",
			wantLen: 0,
			wantIDs: nil,
		},
		{
			name:    "match multiple fields",
			query:   "song",
			wantLen: 2,
			wantIDs: []domain.TrackID{id1, id2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idx.Search(ctx, tt.query, 10)
			if err != nil {
				t.Errorf("Search() error = %v", err)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("Search() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestIndex_Search_CaseInsensitive(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	id := domain.GenerateTrackID("/music/song.mp3")
	_ = idx.Index(ctx, &domain.Track{ID: id, Title: helloWorldTitle, Artist: testArtist, Album: testAlbum})

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"lowercase", helloToken, 1},
		{"uppercase", "HELLO", 1},
		{"mixed case", "HeLLo", 1},
		{"capitalized", "Hello", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idx.Search(ctx, tt.query, 10)
			if err != nil {
				t.Errorf("Search() error = %v", err)
				return
			}
			if len(got) != tt.want {
				t.Errorf("Search(%q) len = %d, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestIndex_Search_MultipleTokens(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	id1 := domain.GenerateTrackID("/music/song1.mp3")
	id2 := domain.GenerateTrackID("/music/song2.mp3")

	_ = idx.Index(ctx, &domain.Track{ID: id1, Title: rockSong, Artist: bandToken, Album: albumToken})
	_ = idx.Index(ctx, &domain.Track{ID: id2, Title: jazzSong, Artist: bandToken, Album: albumToken})

	got, err := idx.Search(ctx, rockQuery, 10)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Search(\"rock\") len = %d, want 1 (only id1 has rock)", len(got))
	}
}

func TestIndex_Search_Limit(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()
	for i := range 10 {
		_ = idx.Index(ctx, &domain.Track{
			ID:     domain.GenerateTrackID("/music/song" + string(rune('a'+i)) + ".mp3"),
			Title:  rockSong,
			Artist: rockBand,
			Album:  rockAlbum,
		})
	}

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"limit 1", 1, 1},
		{"limit 5", 5, 5},
		{"limit 20", 20, 10},
		{"limit 0", 0, 10},
		{"limit negative", -1, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idx.Search(ctx, rockQuery, tt.limit)
			if err != nil {
				t.Errorf("Search() error = %v", err)
				return
			}
			if len(got) != tt.want {
				t.Errorf("Search(limit=%d) len = %d, want %d", tt.limit, len(got), tt.want)
			}
		})
	}
}

func TestIndex_Search_EmptyQuery(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	_ = idx.Index(ctx, &domain.Track{
		ID:     domain.GenerateTrackID("/music/song.mp3"),
		Title:  rockSong,
		Artist: rockBand,
		Album:  rockAlbum,
	})

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"empty string", "", 0},
		{"whitespace only", "   ", 0},
		{"special chars only", "!@#$%", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idx.Search(ctx, tt.query, 10)
			if err != nil {
				t.Errorf("Search() error = %v", err)
				return
			}
			if len(got) != tt.want {
				t.Errorf("Search(%q) len = %d, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestIndex_Delete(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	trackID := domain.GenerateTrackID("/music/song.mp3")
	_ = idx.Index(ctx, &domain.Track{ID: trackID, Title: rockSong, Artist: rockBand, Album: rockAlbum})

	if err := idx.Delete(ctx, trackID); err != nil {
		t.Errorf("Delete() error = %v", err)
	}
	if _, ok := idx.trackToks[trackID]; ok {
		t.Error("Delete() should remove track from trackToks")
	}
}

func TestIndex_Delete_NonExistent(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()
	if err := idx.Delete(ctx, domain.GenerateTrackID("/nonexistent")); err != nil {
		t.Errorf("Delete() non-existent error = %v", err)
	}
}

func TestIndex_Stats(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	_ = idx.Index(ctx, &domain.Track{ID: domain.GenerateTrackID("/music/song1.mp3"), Title: rockSong, Artist: rockBand, Album: rockAlbum})
	_ = idx.Index(ctx, &domain.Track{ID: domain.GenerateTrackID("/music/song2.mp3"), Title: jazzSong, Artist: jazzBand, Album: jazzAlbum})

	stats, err := idx.Stats(ctx)
	if err != nil {
		t.Errorf("Stats() error = %v", err)
	}
	if stats.TotalTerms == 0 {
		t.Error("Stats() should report non-zero terms")
	}
	if stats.TotalTrigrams == 0 {
		t.Error("Stats() should report non-zero trigrams")
	}
}

func TestIndex_Rebuild(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	_ = idx.Index(ctx, &domain.Track{ID: domain.GenerateTrackID("/music/song1.mp3"), Title: rockSong, Artist: rockBand, Album: rockAlbum})
	_ = idx.Index(ctx, &domain.Track{ID: domain.GenerateTrackID("/music/song2.mp3"), Title: jazzSong, Artist: jazzBand, Album: jazzAlbum})

	if err := idx.Delete(ctx, domain.GenerateTrackID("/music/song1.mp3")); err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	repo := &mockRebuildRepo{
		tracks: []*domain.Track{
			{ID: domain.GenerateTrackID("/music/song3.mp3"), Title: "Pop Song", Artist: "Pop Band", Album: "Pop Album"},
			{ID: domain.GenerateTrackID("/music/song4.mp3"), Title: "Classical", Artist: "Orchestra", Album: "Classical Album"},
		},
	}

	if err := idx.Rebuild(ctx, repo); err != nil {
		t.Errorf("Rebuild() error = %v", err)
	}

	if len(idx.trackToks) != 2 {
		t.Errorf("Rebuild() should have 2 tracks, got %d", len(idx.trackToks))
	}

	if _, ok := idx.trackToks[domain.GenerateTrackID("/music/song1.mp3")]; ok {
		t.Error("Rebuild() should not contain deleted track song1")
	}

	if _, ok := idx.trackToks[domain.GenerateTrackID("/music/song3.mp3")]; !ok {
		t.Error("Rebuild() should contain new track song3")
	}
}

type mockRebuildRepo struct {
	tracks []*domain.Track
}

func (m *mockRebuildRepo) Save(ctx context.Context, track *domain.Track) error { return nil }
func (m *mockRebuildRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	return nil, nil
}

func (m *mockRebuildRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockRebuildRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, nil
}

func (m *mockRebuildRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}
func (m *mockRebuildRepo) Delete(ctx context.Context, id domain.TrackID) error        { return nil }
func (m *mockRebuildRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error { return nil }
func (m *mockRebuildRepo) ListAll(ctx context.Context) ([]*domain.Track, error)       { return m.tracks, nil }

func (m *mockRebuildRepo) ListAllPaths(ctx context.Context) ([]string, error) { return nil, nil }

func TestIndex_Concurrent(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = idx.Index(ctx, &domain.Track{
				ID:     domain.GenerateTrackID("/music/song" + string(rune('0'+n)) + ".mp3"),
				Title:  rockSong,
				Artist: rockBand,
				Album:  rockAlbum,
			})
		}(i)
	}

	wg.Wait()
	if len(idx.trackToks) != 10 {
		t.Errorf("after concurrent indexing, trackToks length = %d, want 10", len(idx.trackToks))
	}
}

func TestIndex_Search_ScoreOrdering(t *testing.T) {
	idx := NewSearchIndex(nil).(*inmemoryIndex)
	ctx := context.Background()

	id1 := domain.GenerateTrackID("/music/song1.mp3")
	id2 := domain.GenerateTrackID("/music/song2.mp3")
	id3 := domain.GenerateTrackID("/music/song3.mp3")

	_ = idx.Index(ctx, &domain.Track{ID: id1, Title: "Rock", Artist: "Band A", Album: albumToken})
	_ = idx.Index(ctx, &domain.Track{ID: id2, Title: "Rock Rock", Artist: "Band B", Album: albumToken})
	_ = idx.Index(ctx, &domain.Track{ID: id3, Title: "Rock Rock Rock", Artist: "Band C", Album: albumToken})

	got, err := idx.Search(ctx, rockQuery, 10)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("Search() should return 3 results, got %d", len(got))
	}
	if got[0] == got[1] || got[1] == got[2] {
		t.Error("Search() results should be unique")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"simple words", helloWorldToken, []string{helloToken, "world"}},
		{"with numbers", "song 123", []string{"song", "123"}},
		{emptyToken, "", nil},
		{"whitespace only", "   ", nil},
		{"single word", helloToken, []string{helloToken}},
		{"multiple spaces", "hello    world", []string{helloToken, "world"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], w)
				}
			}
		})
	}
}

func TestNormalizeForIndex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase", helloWorldToken, helloWorldToken},
		{"uppercase", "HELLO WORLD", helloWorldToken},
		{"mixed case", "HeLLo WoRLD", helloWorldToken},
		{"with numbers", "song123", "song123"},
		{"with special chars", "hello!@#$world", "helloworld"},
		{emptyToken, "", ""},
		{"whitespace", "  hello  ", helloToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.Normalize(tt.input)
			if got != tt.want {
				t.Errorf("normalizeForIndex(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrigramsFromNormalizedString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{helloToken, helloToken, []string{"hel", "ell", "llo"}},
		{"ab", "ab", nil},
		{abcToken, abcToken, []string{abcToken}},
		{"a", "a", nil},
		{emptyToken, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trigramsFromNormalizedString(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("trigramsFromNormalizedString(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("trigramsFromNormalizedString(%q)[%d] = %q, want %q", tt.input, i, got[i], w)
				}
			}
		})
	}
}

type mockEmptyLibraryRepo struct{}

func (m *mockEmptyLibraryRepo) Save(ctx context.Context, track *domain.Track) error {
	return nil
}

func (m *mockEmptyLibraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) Search(ctx context.Context, query SearchQuery) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) Delete(ctx context.Context, id domain.TrackID) error {
	return nil
}

func (m *mockEmptyLibraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}

func (m *mockEmptyLibraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockEmptyLibraryRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	return nil, nil
}
