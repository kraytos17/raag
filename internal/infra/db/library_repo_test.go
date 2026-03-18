package db

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	dir := t.TempDir()
	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(dir)
	}

	return db, cleanup
}

func TestBadgerLibraryRepository_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	track := domain.NewTrack("/music/test.mp3")
	track.Title = "Test Song"
	track.Artist = "Test Artist"
	track.Album = "Test Album"
	track.DurationMs = 180000

	if err := repo.Save(ctx, track); err != nil {
		t.Errorf("Save() error = %v", err)
	}

	found, err := repo.FindByID(ctx, track.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}
	if found.Title != track.Title {
		t.Errorf("FindByID() title = %v, want %v", found.Title, track.Title)
	}

	foundByPath, err := repo.FindByPath(ctx, track.Path)
	if err != nil {
		t.Errorf("FindByPath() error = %v", err)
	}
	if foundByPath.ID != track.ID {
		t.Errorf("FindByPath() ID = %v, want %v", foundByPath.ID, track.ID)
	}
}

func TestBadgerLibraryRepository_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	_, err = repo.FindByID(ctx, domain.GenerateTrackID("/nonexistent"))
	if err != domain.ErrTrackNotFound {
		t.Errorf("FindByID() error = %v, want %v", err, domain.ErrTrackNotFound)
	}
}

func TestBadgerLibraryRepository_ListAll(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/song1.mp3"), Path: "/music/song1.mp3", Title: "Song 1", DurationMs: 180000},
		{ID: domain.GenerateTrackID("/music/song2.mp3"), Path: "/music/song2.mp3", Title: "Song 2", DurationMs: 200000},
	}
	for _, track := range tracks {
		if err := repo.Save(ctx, track); err != nil {
			t.Errorf("Save() error = %v", err)
		}
	}

	result, err := repo.ListAll(ctx)
	if err != nil {
		t.Errorf("ListAll() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("ListAll() returned %d tracks, want 2", len(result))
	}
}

func TestBadgerLibraryRepository_Search(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	tracks := []*domain.Track{
		{ID: domain.GenerateTrackID("/music/rock1.mp3"), Path: "/music/rock1.mp3", Title: "Rock Song 1", Artist: "Rock Band", DurationMs: 180000},
		{ID: domain.GenerateTrackID("/music/jazz1.mp3"), Path: "/music/jazz1.mp3", Title: "Jazz Song 1", Artist: "Jazz Band", DurationMs: 200000},
	}
	for _, track := range tracks {
		if err := repo.Save(ctx, track); err != nil {
			t.Errorf("Save() error = %v", err)
		}
	}

	results, err := repo.Search(ctx, app.SearchQuery{Query: "rock", Limit: 10})
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search() returned %d tracks, want 1", len(results))
	}
}

func TestBadgerLibraryRepository_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	track := domain.NewTrack("/music/test.mp3")
	track.DurationMs = 180000
	if err := repo.Save(ctx, track); err != nil {
		t.Errorf("Save() error = %v", err)
	}
	if err := repo.Delete(ctx, track.ID); err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.FindByID(ctx, track.ID)
	if err != domain.ErrTrackNotFound {
		t.Errorf("FindByID() after delete error = %v, want %v", err, domain.ErrTrackNotFound)
	}
}

func TestBadgerLibraryRepository_BulkSave(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	tracks := make([]*domain.Track, 100)
	for i := range 100 {
		tracks[i] = domain.NewTrack(fmt.Sprintf("/music/song%d.mp3", i))
		tracks[i].DurationMs = 180000
	}
	if err := repo.BulkSave(ctx, tracks); err != nil {
		t.Errorf("BulkSave() error = %v", err)
	}

	result, err := repo.ListAll(ctx)
	if err != nil {
		t.Errorf("ListAll() error = %v", err)
	}
	if len(result) != 100 {
		t.Errorf("ListAll() returned %d tracks, want 100", len(result))
	}
}

func TestBadgerLibraryRepository_FileStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}

	ctx := context.Background()
	stats := map[string]*domain.FileStat{
		"/music/song1.mp3": {Path: "/music/song1.mp3", Size: 5000, Mtime: 1000},
		"/music/song2.mp3": {Path: "/music/song2.mp3", Size: 6000, Mtime: 2000},
	}
	if err := repo.SaveFileStats(ctx, stats); err != nil {
		t.Errorf("SaveFileStats() error = %v", err)
	}

	loaded, err := repo.LoadFileStats(ctx)
	if err != nil {
		t.Errorf("LoadFileStats() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("LoadFileStats() returned %d stats, want 2", len(loaded))
	}
}

func TestBadgerLibraryRepository_InvalidateCache(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 100)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() error = %v", err)
	}
	repo.InvalidateCache()
}

func TestBadgerLibraryRepository_DefaultCacheSize(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo, err := NewBadgerLibraryRepository(db, 0)
	if err != nil {
		t.Fatalf("NewBadgerLibraryRepository() with 0 cacheSize error = %v", err)
	}
	if repo == nil {
		t.Error("NewBadgerLibraryRepository() should not return nil")
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello world"},
		{"  Test  ", "test"},
		{"Rock123", "rock123"},
		{"Hello!@#$World", "helloworld"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalize(tt.input)
		if result != tt.expected {
			t.Errorf("normalize(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMatchesQuery(t *testing.T) {
	track := &domain.Track{
		Title:  "Rock Song",
		Artist: "Rock Band",
		Album:  "Rock Album",
	}

	if !matchesQuery("rock", track) {
		t.Error("matchesQuery() should match partial title")
	}
	if !matchesQuery("band", track) {
		t.Error("matchesQuery() should match artist")
	}
	if !matchesQuery("album", track) {
		t.Error("matchesQuery() should match album")
	}
	if matchesQuery("jazz", track) {
		t.Error("matchesQuery() should not match unrelated query")
	}
}
