package db

import (
	"context"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestPlaylistRepo_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	playlist, err := domain.NewPlaylist("Test Playlist")
	if err != nil {
		t.Fatalf("NewPlaylist() error = %v", err)
	}
	if err := repo.Save(ctx, playlist); err != nil {
		t.Errorf("Save() error = %v", err)
	}

	found, err := repo.FindByID(ctx, playlist.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}
	if found.Name != playlist.Name {
		t.Errorf("FindByID() name = %v, want %v", found.Name, playlist.Name)
	}
}

func TestPlaylistRepo_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	_, err := repo.FindByID(ctx, domain.GeneratePlaylistID())
	if err != domain.ErrPlaylistNotFound {
		t.Errorf("FindByID() error = %v, want %v", err, domain.ErrPlaylistNotFound)
	}
}

func TestPlaylistRepo_List(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	playlists := []*domain.Playlist{
		{ID: domain.GeneratePlaylistID(), Name: "Playlist 1"},
		{ID: domain.GeneratePlaylistID(), Name: "Playlist 2"},
	}
	for _, pl := range playlists {
		if err := repo.Save(ctx, pl); err != nil {
			t.Errorf("Save() error = %v", err)
		}
	}

	result, err := repo.List(ctx)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("List() returned %d playlists, want 2", len(result))
	}
}

func TestPlaylistRepo_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	playlist, err := domain.NewPlaylist("Test Playlist")
	if err != nil {
		t.Fatalf("NewPlaylist() error = %v", err)
	}
	if err := repo.Save(ctx, playlist); err != nil {
		t.Errorf("Save() error = %v", err)
	}
	if err := repo.Delete(ctx, playlist.ID); err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.FindByID(ctx, playlist.ID)
	if err != domain.ErrPlaylistNotFound {
		t.Errorf("FindByID() after delete error = %v, want %v", err, domain.ErrPlaylistNotFound)
	}
}

func TestPlaylistRepo_AddTrack(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	playlist, err := domain.NewPlaylist("Test Playlist")
	if err != nil {
		t.Fatalf("NewPlaylist() error = %v", err)
	}

	trackID := domain.GenerateTrackID("/music/song.mp3")
	playlist.AddTrack(trackID)
	if err := repo.Save(ctx, playlist); err != nil {
		t.Errorf("Save() error = %v", err)
	}

	found, err := repo.FindByID(ctx, playlist.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}
	if found.TrackCount() != 1 {
		t.Errorf("TrackCount() = %d, want 1", found.TrackCount())
	}
}
