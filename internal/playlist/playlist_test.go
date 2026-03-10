package playlist

import (
	"testing"

	"github.com/p-society/raag/internal/metadata"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Error("NewManager() should not return nil")
	}
	if len(m.Playlists) != 0 {
		t.Error("NewManager() should return empty playlists")
	}
}

func TestCreatePlaylist(t *testing.T) {
	m := NewManager()

	err := m.Create("test")
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	err = m.Create("test")
	if err == nil {
		t.Error("Create() should return error for duplicate playlist")
	}
}

func TestDeletePlaylist(t *testing.T) {
	m := NewManager()

	m.Create("test")
	err := m.Delete("test")
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	err = m.Delete("nonexistent")
	if err == nil {
		t.Error("Delete() should return error for nonexistent playlist")
	}
}

func TestAddSong(t *testing.T) {
	m := NewManager()
	m.Create("test")

	song := metadata.Song{
		Title:  "Test Song",
		Artist: "Test Artist",
	}

	err := m.AddSong("test", song)
	if err != nil {
		t.Errorf("AddSong() error = %v", err)
	}

	err = m.AddSong("nonexistent", song)
	if err == nil {
		t.Error("AddSong() should return error for nonexistent playlist")
	}
}

func TestRemoveSong(t *testing.T) {
	m := NewManager()
	m.Create("test")

	song := metadata.Song{Title: "Test Song"}
	m.AddSong("test", song)

	err := m.RemoveSong("test", 0)
	if err != nil {
		t.Errorf("RemoveSong() error = %v", err)
	}

	err = m.RemoveSong("test", 100)
	if err == nil {
		t.Error("RemoveSong() should return error for invalid index")
	}
}

func TestListPlaylists(t *testing.T) {
	m := NewManager()

	m.Create("playlist1")
	m.Create("playlist2")

	names := m.List()
	if len(names) != 2 {
		t.Errorf("List() = %d, want 2", len(names))
	}
}

func TestGetSongs(t *testing.T) {
	m := NewManager()
	m.Create("test")

	m.AddSong("test", metadata.Song{Title: "Song 1"})
	m.AddSong("test", metadata.Song{Title: "Song 2"})

	songs, err := m.GetSongs("test")
	if err != nil {
		t.Errorf("GetSongs() error = %v", err)
	}
	if len(songs) != 2 {
		t.Errorf("GetSongs() = %d, want 2", len(songs))
	}

	_, err = m.GetSongs("nonexistent")
	if err == nil {
		t.Error("GetSongs() should return error for nonexistent playlist")
	}
}
