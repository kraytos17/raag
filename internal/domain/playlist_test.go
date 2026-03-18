package domain

import (
	"testing"
)

func TestPlaylistID_Validate(t *testing.T) {
	tests := []struct {
		name   string
		id     PlaylistID
		expect bool
	}{
		{"valid id", "abc123", true},
		{"empty id", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.Validate(); got != tt.expect {
				t.Errorf("PlaylistID.Validate() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestNewPlaylist(t *testing.T) {
	playlist, err := NewPlaylist("My Favorites")
	if err != nil {
		t.Fatalf("NewPlaylist() error = %v", err)
	}

	if playlist.ID == "" {
		t.Errorf("NewPlaylist() should generate PlaylistID")
	}

	if playlist.Name != "My Favorites" {
		t.Errorf("NewPlaylist() Name = %v, want %v", playlist.Name, "My Favorites")
	}

	if playlist.TrackIDs == nil {
		t.Errorf("NewPlaylist() should initialize TrackIDs slice")
	}

	if playlist.TrackCount() != 0 {
		t.Errorf("NewPlaylist() TrackCount = %v, want 0", playlist.TrackCount())
	}
}

func TestNewPlaylist_EmptyName(t *testing.T) {
	_, err := NewPlaylist("")
	if err != ErrInvalidPlaylistName {
		t.Errorf("NewPlaylist(\"\") error = %v, want %v", err, ErrInvalidPlaylistName)
	}
}

func TestPlaylist_AddTrack(t *testing.T) {
	playlist, _ := NewPlaylist("Test")
	trackID := GenerateTrackID("/path/to/song.mp3")

	playlist.AddTrack(trackID)

	if playlist.TrackCount() != 1 {
		t.Errorf("After AddTrack(), TrackCount = %v, want 1", playlist.TrackCount())
	}
}

func TestPlaylist_RemoveTrack(t *testing.T) {
	playlist, _ := NewPlaylist("Test")
	trackID := GenerateTrackID("/path/to/song.mp3")
	playlist.AddTrack(trackID)
	playlist.AddTrack(GenerateTrackID("/path/to/other.mp3"))

	playlist.RemoveTrack(trackID)

	if playlist.TrackCount() != 1 {
		t.Errorf("After RemoveTrack(), TrackCount = %v, want 1", playlist.TrackCount())
	}
}

func TestPlaylist_Clear(t *testing.T) {
	playlist, _ := NewPlaylist("Test")
	playlist.AddTrack(GenerateTrackID("/path/to/song1.mp3"))
	playlist.AddTrack(GenerateTrackID("/path/to/song2.mp3"))

	playlist.Clear()

	if playlist.TrackCount() != 0 {
		t.Errorf("After Clear(), TrackCount = %v, want 0", playlist.TrackCount())
	}
}
