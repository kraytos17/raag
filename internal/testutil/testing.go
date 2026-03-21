package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/p-society/raag/internal/domain"
	db "github.com/p-society/raag/internal/infra/storage"
)

func SetupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir, db.DefaultOptions(dir))
	if err != nil {
		t.Fatalf("SetupTestDB() failed to open: %v", err)
	}

	cleanup := func() {
		_ = database.Close()
		_ = os.RemoveAll(dir)
	}
	return database, cleanup
}

func CreateTrack(t *testing.T, path string) *domain.Track {
	t.Helper()
	track := domain.NewTrack(path)
	track.Title = "Test Track"
	track.Artist = "Test Artist"
	track.Album = "Test Album"
	track.DurationMs = 180000
	return track
}

func CreateTrackWithFields(t *testing.T, path, title, artist, album string) *domain.Track {
	t.Helper()
	track := domain.NewTrack(path)
	track.Title = title
	track.Artist = artist
	track.Album = album
	track.DurationMs = 180000
	return track
}

func CreateTracks(t *testing.T, count int) []*domain.Track {
	t.Helper()
	tracks := make([]*domain.Track, count)
	for i := range count {
		tracks[i] = CreateTrack(t, "/music/track_"+string(rune('a'+i))+".mp3")
	}
	return tracks
}

func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func AssertError(t *testing.T, err error, want error) {
	t.Helper()
	if err == nil {
		t.Errorf("expected error %v, got nil", want)
		return
	}
	if err != want && !errorsIs(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
}

func errorsIs(err, target error) bool {
	if target == nil {
		return err == nil
	}

	isNil := err == nil
	return isNil || err.Error() == target.Error()
}

func AssertEqual[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func AssertNotEqual[T comparable](t *testing.T, got, dontWant T, msg string) {
	t.Helper()
	if got == dontWant {
		t.Errorf("%s: got %v, should not equal %v", msg, got, dontWant)
	}
}

func AssertTrue(t *testing.T, got bool, msg string) {
	t.Helper()
	if !got {
		t.Errorf("%s: got %v, want true", msg, got)
	}
}

func AssertFalse(t *testing.T, got bool, msg string) {
	t.Helper()
	if got {
		t.Errorf("%s: got %v, want false", msg, got)
	}
}

func Context(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
