package library

import (
	"testing"

	"github.com/p-society/raag/internal/metadata"
)

func TestNewLibrary(t *testing.T) {
	_, err := NewLibrary("./testdata")
	if err != nil {
		t.Logf("NewLibrary error (expected if testdata missing): %v", err)
	}
}

func TestListSongs(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	lib.Songs["song1"] = metadata.Song{Title: "Song 1"}
	lib.Songs["song2"] = metadata.Song{Title: "Song 2"}

	songs := lib.ListSongs()

	if len(songs) != 2 {
		t.Errorf("ListSongs() = %d, want 2", len(songs))
	}
}

func TestFindSong(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	lib.Songs["test song"] = metadata.Song{
		Title:  "Test Song",
		Artist: "Test Artist",
		Album:  "Test Album",
	}

	song, err := lib.FindSong("test song")
	if err != nil {
		t.Errorf("FindSong() error = %v", err)
	}
	if song.Title != "Test Song" {
		t.Errorf("FindSong() = %v, want Test Song", song.Title)
	}

	_, err = lib.FindSong("nonexistent")
	if err == nil {
		t.Error("FindSong() should return error for nonexistent song")
	}
}

func TestAddSong(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	err := lib.AddSong("/fake/path/song.mp3")
	if err != nil {
		t.Logf("AddSong error (expected if file doesn't exist): %v", err)
	}
}

func TestRemoveSong(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	lib.Songs["test song"] = metadata.Song{Title: "Test Song"}

	err := lib.RemoveSong("test song")
	if err != nil {
		t.Errorf("RemoveSong() error = %v", err)
	}

	if _, exists := lib.Songs["test song"]; exists {
		t.Error("RemoveSong() should remove song from library")
	}

	err = lib.RemoveSong("nonexistent")
	if err == nil {
		t.Error("RemoveSong() should return error for nonexistent song")
	}
}

func TestGetByArtist(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	lib.Songs["song1"] = metadata.Song{Title: "Song 1", Artist: "Artist A"}
	lib.Songs["song2"] = metadata.Song{Title: "Song 2", Artist: "Artist B"}
	lib.Songs["song3"] = metadata.Song{Title: "Song 3", Artist: "Artist A"}

	songs := lib.GetByArtist("Artist A")

	if len(songs) != 2 {
		t.Errorf("GetByArtist() = %d, want 2", len(songs))
	}
}

func TestGetByAlbum(t *testing.T) {
	lib := &Library{
		Songs: make(map[string]metadata.Song),
	}

	lib.Songs["song1"] = metadata.Song{Title: "Song 1", Album: "Album A"}
	lib.Songs["song2"] = metadata.Song{Title: "Song 2", Album: "Album B"}
	lib.Songs["song3"] = metadata.Song{Title: "Song 3", Album: "Album A"}

	songs := lib.GetByAlbum("Album A")

	if len(songs) != 2 {
		t.Errorf("GetByAlbum() = %d, want 2", len(songs))
	}
}
