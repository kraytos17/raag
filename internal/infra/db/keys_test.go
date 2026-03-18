package db

import (
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestTrackKey(t *testing.T) {
	id := domain.TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	key := TrackKey(id)
	expected := "trk:data:abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	if string(key) != expected {
		t.Errorf("TrackKey() = %v, want %v", string(key), expected)
	}
}

func TestPathKey(t *testing.T) {
	path := "/music/song.mp3"
	key := PathKey(path)
	if len(key) == 0 {
		t.Error("PathKey() returned empty key")
	}

	expectedPrefix := string(PrefixTrackPath)
	if string(key[:len(expectedPrefix)]) != expectedPrefix {
		t.Errorf("PathKey() should start with prefix %v", PrefixTrackPath)
	}
}

func TestPathKey_Deterministic(t *testing.T) {
	path := "/music/song.mp3"
	key1 := PathKey(path)
	key2 := PathKey(path)
	if string(key1) != string(key2) {
		t.Errorf("PathKey() should be deterministic for same path")
	}
}

func TestArtistIndexKey(t *testing.T) {
	artist := "The Beatles"
	id := domain.TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	key := ArtistIndexKey(artist, id)
	if len(key) == 0 {
		t.Error("ArtistIndexKey() returned empty key")
	}
}

func TestAlbumIndexKey(t *testing.T) {
	album := "Abbey Road"
	id := domain.TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	key := AlbumIndexKey(album, id)
	if len(key) == 0 {
		t.Error("AlbumIndexKey() returned empty key")
	}
}

func TestTermIndexKey(t *testing.T) {
	term := "rock"
	id := domain.TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	key := TermIndexKey(term, id)
	expected := "idx:term:rock:abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	if string(key) != expected {
		t.Errorf("TermIndexKey() = %v, want %v", string(key), expected)
	}
}

func TestTrigramIndexKey(t *testing.T) {
	trigram := "roc"
	id := domain.TrackID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	key := TrigramIndexKey(trigram, id)
	expected := "idx:trigram:roc:abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	if string(key) != expected {
		t.Errorf("TrigramIndexKey() = %v, want %v", string(key), expected)
	}
}

func TestFileStatKey(t *testing.T) {
	path := "/music/song.mp3"
	key := FileStatKey(path)
	if len(key) == 0 {
		t.Error("FileStatKey() returned empty key")
	}

	expectedPrefix := string(PrefixFileStat)
	if string(key[:len(expectedPrefix)]) != expectedPrefix {
		t.Errorf("FileStatKey() should start with prefix %v", PrefixFileStat)
	}
}

func TestPlaylistKey(t *testing.T) {
	id := domain.PlaylistID("playlist123")
	key := PlaylistKey(id)
	expected := "pl:playlist123"
	if string(key) != expected {
		t.Errorf("PlaylistKey() = %v, want %v", string(key), expected)
	}
}

func TestPeerKey(t *testing.T) {
	t.Parallel()
	id := domain.PeerID("QmPeer123")
	key := PeerKey(id)
	expected := "peer:QmPeer123"
	if string(key) != expected {
		t.Errorf("PeerKey() = %v, want %v", string(key), expected)
	}
}

func TestPeerLibraryKey(t *testing.T) {
	id := domain.PeerID("QmPeer123")
	key := PeerLibraryKey(id)
	expected := "peer:lib:QmPeer123"
	if string(key) != expected {
		t.Errorf("PeerLibraryKey() = %v, want %v", string(key), expected)
	}
}

func TestPeerScoreKey(t *testing.T) {
	id := domain.PeerID("QmPeer123")
	key := PeerScoreKey(id)
	expected := "peer:score:QmPeer123"
	if string(key) != expected {
		t.Errorf("PeerScoreKey() = %v, want %v", string(key), expected)
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase", "abc", "abc"},
		{"uppercase", "ABC", "abc"},
		{"mixed", "AbC123", "abc123"},
		{"with spaces", "hello world", "helloworld"},
		{"special chars", "a!@#b$%c", "abc"},
		{"numbers", "12345", "12345"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeKey(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPathHash(t *testing.T) {
	path := "/music/song.mp3"
	hash1 := pathHash(path)
	hash2 := pathHash(path)
	if hash1 != hash2 {
		t.Errorf("pathHash() should be deterministic")
	}
	if len(hash1) != 64 {
		t.Errorf("pathHash() should return 64-character hex string (SHA-256)")
	}
}
