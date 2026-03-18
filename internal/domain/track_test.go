package domain

import (
	"testing"
)

func TestTrackID_Validate(t *testing.T) {
	tests := []struct {
		name   string
		id     TrackID
		expect bool
	}{
		{"valid id", TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd"), true},
		{"empty id", TrackID(""), false},
		{"too short", TrackID("abc123"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.Validate(); got != tt.expect {
				t.Errorf("TrackID.Validate() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestGenerateTrackID(t *testing.T) {
	id1 := GenerateTrackID("/path/to/song.mp3")
	id2 := GenerateTrackID("/path/to/song.mp3")

	if id1 != id2 {
		t.Errorf("GenerateTrackID() should return same ID for same path")
	}

	id3 := GenerateTrackID("/path/to/other.mp3")
	if id1 == id3 {
		t.Errorf("GenerateTrackID() should return different IDs for different paths")
	}

	if !id1.Validate() {
		t.Errorf("GenerateTrackID() should return valid TrackID")
	}
}

func TestNewTrack(t *testing.T) {
	track := NewTrack("/path/to/song.mp3")

	if track.ID == "" {
		t.Errorf("NewTrack() should generate TrackID")
	}

	if track.Path != "/path/to/song.mp3" {
		t.Errorf("NewTrack() Path = %v, want %v", track.Path, "/path/to/song.mp3")
	}

	if track.AddedAt == 0 {
		t.Errorf("NewTrack() should set AddedAt")
	}

	if track.Genres == nil {
		t.Errorf("NewTrack() should initialize Genres slice")
	}
}

func TestTrack_Validate(t *testing.T) {
	tests := []struct {
		name    string
		track   *Track
		wantErr bool
	}{
		{
			name: "valid track",
			track: &Track{
				ID:         GenerateTrackID("/path/to/song.mp3"),
				Path:       "/path/to/song.mp3",
				DurationMs: 180000,
			},
			wantErr: false,
		},
		{
			name: "empty path",
			track: &Track{
				ID:         GenerateTrackID("/path/to/song.mp3"),
				Path:       "",
				DurationMs: 180000,
			},
			wantErr: true,
		},
		{
			name: "zero duration",
			track: &Track{
				ID:         GenerateTrackID("/path/to/song.mp3"),
				Path:       "/path/to/song.mp3",
				DurationMs: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.track.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Track.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTrack_IncrementPlayCount(t *testing.T) {
	track := NewTrack("/path/to/song.mp3")
	track.DurationMs = 180000

	if track.PlayCount != 0 {
		t.Errorf("Initial PlayCount should be 0")
	}

	track.IncrementPlayCount()

	if track.PlayCount != 1 {
		t.Errorf("After IncrementPlayCount(), PlayCount = %v, want 1", track.PlayCount)
	}

	if track.LastPlayed == 0 {
		t.Errorf("After IncrementPlayCount(), LastPlayed should be set")
	}
}

func TestTrack_Duration(t *testing.T) {
	track := NewTrack("/path/to/song.mp3")
	track.DurationMs = 180000

	duration := track.Duration()

	if duration.Seconds() != 180 {
		t.Errorf("Track.Duration() = %v, want 180s", duration)
	}
}

func TestTrack_HasCoverArt(t *testing.T) {
	track := NewTrack("/path/to/song.mp3")

	if track.HasCoverArt() {
		t.Errorf("Track with no cover art should return false")
	}

	track.CoverArt = []byte{0xFF, 0xD8}

	if !track.HasCoverArt() {
		t.Errorf("Track with cover art should return true")
	}
}
