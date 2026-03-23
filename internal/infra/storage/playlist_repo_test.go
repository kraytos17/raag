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
	playlistID, err := domain.GeneratePlaylistID()
	if err != nil {
		t.Fatalf("GeneratePlaylistID() error = %v", err)
	}

	_, err = repo.FindByID(ctx, playlistID)
	if err != domain.ErrPlaylistNotFound {
		t.Errorf("FindByID() error = %v, want %v", err, domain.ErrPlaylistNotFound)
	}
}

func TestPlaylistRepo_List(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewPlaylistRepo(db)
	ctx := context.Background()
	id1, _ := domain.GeneratePlaylistID()
	id2, _ := domain.GeneratePlaylistID()
	playlists := []*domain.Playlist{
		{ID: id1, Name: "Playlist 1"},
		{ID: id2, Name: "Playlist 2"},
	}
	for _, pl := range playlists {
		if err := repo.Save(ctx, pl); err != nil {
			t.Errorf("Save() error = %v", err)
		}
	}

	var result []*domain.Playlist
	for pl, err := range repo.ListAll(ctx) {
		if err != nil {
			continue
		}
		result = append(result, pl)
	}
	if len(result) != 2 {
		t.Errorf("ListAll() returned %d playlists, want 2", len(result))
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
