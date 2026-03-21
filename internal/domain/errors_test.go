package domain

import (
	"testing"
)

func TestErrors_AllSentinels(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrTrackNotFound", ErrTrackNotFound},
		{"ErrPlaylistNotFound", ErrPlaylistNotFound},
		{"ErrPeerUnavailable", ErrPeerUnavailable},
		{"ErrInvalidStateTransition", ErrInvalidStateTransition},
		{"ErrSearchIndexCorrupt", ErrSearchIndexCorrupt},
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrStreamTimeout", ErrStreamTimeout},
		{"ErrBufferOverflow", ErrBufferOverflow},
		{"ErrUnauthorized", ErrUnauthorized},
		{"ErrTrackExists", ErrTrackExists},
		{"ErrPlaylistExists", ErrPlaylistExists},
		{"ErrInvalidTrackID", ErrInvalidTrackID},
		{"ErrInvalidPlaylistName", ErrInvalidPlaylistName},
		{"ErrEmptyLibrary", ErrEmptyLibrary},
		{"ErrNoPeersFound", ErrNoPeersFound},
		{"ErrCodecNotSupported", ErrCodecNotSupported},
		{"ErrFileNotAccessible", ErrFileNotAccessible},
		{"ErrInvalidTrack", ErrInvalidTrack},
		{"ErrZeroDuration", ErrZeroDuration},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Error("sentinel error should not be nil")
			}
			if tt.err.Error() == "" {
				t.Error("sentinel error should have message")
			}
		})
	}
}

func TestErrors_AllDistinct(t *testing.T) {
	errors := []error{
		ErrTrackNotFound,
		ErrPlaylistNotFound,
		ErrPeerUnavailable,
		ErrInvalidStateTransition,
		ErrSearchIndexCorrupt,
		ErrInvalidConfig,
		ErrStreamTimeout,
		ErrBufferOverflow,
		ErrUnauthorized,
		ErrTrackExists,
		ErrPlaylistExists,
		ErrInvalidTrackID,
		ErrInvalidPlaylistName,
		ErrEmptyLibrary,
		ErrNoPeersFound,
		ErrCodecNotSupported,
		ErrFileNotAccessible,
		ErrInvalidTrack,
		ErrZeroDuration,
	}

	for i, err1 := range errors {
		for j, err2 := range errors {
			if i != j && err1.Error() == err2.Error() {
				t.Errorf("duplicate error message: %q", err1.Error())
			}
		}
	}
}

func TestErrors_ErrTrackNotFound(t *testing.T) {
	if ErrTrackNotFound.Error() != "track not found" {
		t.Errorf("ErrTrackNotFound message = %q, want %q", ErrTrackNotFound.Error(), "track not found")
	}
}

func TestErrors_ErrPlaylistNotFound(t *testing.T) {
	if ErrPlaylistNotFound.Error() != "playlist not found" {
		t.Errorf("ErrPlaylistNotFound message = %q, want %q", ErrPlaylistNotFound.Error(), "playlist not found")
	}
}

func TestErrors_ErrInvalidTrackID(t *testing.T) {
	if ErrInvalidTrackID.Error() != "invalid track ID" {
		t.Errorf("ErrInvalidTrackID message = %q, want %q", ErrInvalidTrackID.Error(), "invalid track ID")
	}
}

func TestErrors_ErrZeroDuration(t *testing.T) {
	if ErrZeroDuration.Error() != "track has zero duration" {
		t.Errorf("ErrZeroDuration message = %q, want %q", ErrZeroDuration.Error(), "track has zero duration")
	}
}

func TestErrors_ErrorMessages(t *testing.T) {
	tests := []struct {
		err      error
		expected string
	}{
		{ErrTrackNotFound, "track not found"},
		{ErrPlaylistNotFound, "playlist not found"},
		{ErrPeerUnavailable, "peer unavailable"},
		{ErrInvalidStateTransition, "invalid state transition"},
		{ErrSearchIndexCorrupt, "search index corrupted"},
		{ErrInvalidConfig, "invalid configuration"},
		{ErrStreamTimeout, "stream timeout"},
		{ErrBufferOverflow, "buffer overflow"},
		{ErrUnauthorized, "unauthorized access"},
		{ErrTrackExists, "track already exists"},
		{ErrPlaylistExists, "playlist already exists"},
		{ErrInvalidTrackID, "invalid track ID"},
		{ErrInvalidPlaylistName, "invalid playlist name"},
		{ErrEmptyLibrary, "library is empty"},
		{ErrNoPeersFound, "no peers found"},
		{ErrCodecNotSupported, "codec not supported"},
		{ErrFileNotAccessible, "file not accessible"},
		{ErrInvalidTrack, "invalid track"},
		{ErrZeroDuration, "track has zero duration"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %q, want %q", tt.err.Error(), tt.expected)
			}
		})
	}
}
