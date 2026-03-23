package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/events"
)

type testLibraryRepo struct {
	track *domain.Track
}

func (r *testLibraryRepo) Save(ctx context.Context, track *domain.Track) error { return nil }
func (r *testLibraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if r.track != nil && r.track.ID == id {
		return r.track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (r *testLibraryRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (r *testLibraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (r *testLibraryRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}
func (r *testLibraryRepo) Delete(ctx context.Context, id domain.TrackID) error        { return nil }
func (r *testLibraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error { return nil }
func (r *testLibraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error)       { return nil, nil }
func (r *testLibraryRepo) ListAllPaths(ctx context.Context) ([]string, error)         { return nil, nil }

type testSearchHandler struct{}

func (testSearchHandler) Search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return nil, nil
}

type testPlayer struct {
	seekErr error
	pos     time.Duration
}

func (p *testPlayer) Play(ctx context.Context, reader io.Reader, mimeType string) error { return nil }
func (p *testPlayer) Pause(ctx context.Context) error                                   { return nil }
func (p *testPlayer) Resume(ctx context.Context) error                                  { return nil }
func (p *testPlayer) Stop(ctx context.Context) error                                    { return nil }
func (p *testPlayer) Seek(ctx context.Context, position time.Duration) error {
	return p.seekErr
}
func (p *testPlayer) SetVolume(ctx context.Context, volume int) error { return nil }
func (p *testPlayer) GetState() domain.PlayerState                    { return domain.PlayerStateIdle }
func (p *testPlayer) GetPosition() time.Duration                      { return p.pos }

func TestPlaybackController_SeekFailure_RollsBackToPaused(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("t1"), Path: "/tmp/t1.mp3"}
	repo := &testLibraryRepo{track: track}
	seekErr := errors.New("seek failed")
	player := &testPlayer{seekErr: seekErr, pos: 2 * time.Second}
	resolve := func(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolve, bus)
	c.fsm.SetStateForTest(domain.PlayerStatePaused)
	c.currentTrack = track

	if err := c.Seek(ctx, 10*time.Second); !errors.Is(err, seekErr) {
		t.Fatalf("Seek() error = %v, want %v", err, seekErr)
	}
	if got := c.GetState(); got != domain.PlayerStatePaused {
		t.Fatalf("state after seek failure = %v, want paused", got)
	}
	if got := c.GetCurrentTrack(); got == nil || got.ID != track.ID {
		t.Fatalf("currentTrack after seek failure = %v, want %v", got, track.ID)
	}
}

func TestPlaybackController_SeekFailure_RollsBackToPlaying(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("t1"), Path: "/tmp/t1.mp3"}
	repo := &testLibraryRepo{track: track}
	seekErr := errors.New("seek failed")
	player := &testPlayer{seekErr: seekErr, pos: 2 * time.Second}
	resolve := func(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolve, bus)
	c.fsm.SetStateForTest(domain.PlayerStatePlaying)
	c.currentTrack = track

	if err := c.Seek(ctx, 10*time.Second); !errors.Is(err, seekErr) {
		t.Fatalf("Seek() error = %v, want %v", err, seekErr)
	}
	if got := c.GetState(); got != domain.PlayerStatePlaying {
		t.Fatalf("state after seek failure = %v, want playing", got)
	}
	if got := c.GetCurrentTrack(); got == nil || got.ID != track.ID {
		t.Fatalf("currentTrack after seek failure = %v, want %v", got, track.ID)
	}
}
