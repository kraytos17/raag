package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
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

func (r *testLibraryRepo) ListAllPaths(ctx context.Context) ([]string, error) { return nil, nil }

type testSearchHandler struct{}

func (testSearchHandler) Search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return nil, nil
}

type testResolver struct {
	resolveFunc func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error)
}

func (r *testResolver) Resolve(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
	if r.resolveFunc != nil {
		return r.resolveFunc(ctx, trackID)
	}
	return &ResolvedTrack{Reader: io.NopCloser(strings.NewReader("")), Source: SourceLocal}, nil
}

func (r *testResolver) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	return nil
}

type testPlayer struct {
	seekErr           error
	pos               time.Duration
	done              chan struct{}
	playStreamingHook func()
	state             domain.PlayerState
	fillLevel         float64
}

func (p *testPlayer) Play(ctx context.Context, reader io.Reader, mimeType string) error {
	p.state = domain.PlayerStatePlaying
	return nil
}

func (p *testPlayer) PlayStreaming(ctx context.Context, source audio.AudioSource, mimeType string) error {
	p.state = domain.PlayerStatePlaying
	if p.playStreamingHook != nil {
		p.playStreamingHook()
	}
	return nil
}

func (p *testPlayer) Pause(ctx context.Context) error {
	p.state = domain.PlayerStatePaused
	return nil
}

func (p *testPlayer) Resume(ctx context.Context) error {
	p.state = domain.PlayerStatePlaying
	return nil
}

func (p *testPlayer) Stop(ctx context.Context) error {
	p.state = domain.PlayerStateIdle
	return nil
}

func (p *testPlayer) Seek(ctx context.Context, position time.Duration) error {
	return p.seekErr
}

func (p *testPlayer) SetVolume(ctx context.Context, volume int) error { return nil }
func (p *testPlayer) GetState() domain.PlayerState                    { return p.state }
func (p *testPlayer) GetPosition() time.Duration                      { return p.pos }
func (p *testPlayer) GetBufferFillLevel() float64                     { return p.fillLevel }
func (p *testPlayer) Done() <-chan struct{} {
	if p.done == nil {
		p.done = make(chan struct{})
	}
	return p.done
}

func TestPlaybackController_SeekFailure_RollsBackToPaused(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path}
	repo := &testLibraryRepo{track: track}
	seekErr := errors.New("seek failed")
	player := &testPlayer{seekErr: seekErr, pos: 2 * time.Second}
	resolver := &testResolver{}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
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

	track := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path}
	repo := &testLibraryRepo{track: track}
	seekErr := errors.New("seek failed")
	player := &testPlayer{seekErr: seekErr, pos: 2 * time.Second}
	resolver := &testResolver{}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
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

type testQueue struct {
	tracks []*domain.Track
	pos    int
}

func (q *testQueue) Peek() *domain.Track {
	if q.pos >= len(q.tracks) {
		return nil
	}
	return q.tracks[q.pos]
}

func (q *testQueue) Next() *domain.Track {
	if q.pos >= len(q.tracks) {
		return nil
	}
	track := q.tracks[q.pos]
	q.pos++
	return track
}

func (q *testQueue) Previous() *domain.Track {
	if q.pos <= 0 {
		return nil
	}
	q.pos--
	return q.tracks[q.pos]
}

func (q *testQueue) Current() *domain.Track {
	if q.pos >= len(q.tracks) {
		return nil
	}
	return q.tracks[q.pos]
}

func (q *testQueue) Length() int {
	return len(q.tracks)
}

func TestPlaybackController_Play_LocalSource_UsesPlay(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: track}
	player := &testPlayer{}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			return &ResolvedTrack{
				Reader: io.NopCloser(strings.NewReader("")),
				Source: SourceLocal,
				Track:  track,
			}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)

	if err := c.Play(ctx, track.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if c.GetState() != domain.PlayerStatePlaying {
		t.Fatalf("state = %v, want playing", c.GetState())
	}
}

func TestPlaybackController_Play_P2PSource_UsesPlayStreaming(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: track}
	p2pPlayed := false

	player := &testPlayer{
		playStreamingHook: func() { p2pPlayed = true },
	}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			return &ResolvedTrack{
				Reader: io.NopCloser(bytes.NewReader(make([]byte, audio.PreRollBytes))),
				Source: SourceP2P,
				Track:  track,
			}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	if err := c.Play(ctx, track.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if !p2pPlayed {
		t.Fatal("PlayStreaming was not called for P2P source")
	}
}

func TestPlaybackController_Play_ResolvesTrackFromResolved(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	resolvedTrack := &domain.Track{ID: domain.TrackID("remote1"), Title: "Remote Song", MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{}
	player := &testPlayer{}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			return &ResolvedTrack{
				Reader: io.NopCloser(bytes.NewReader(make([]byte, audio.PreRollBytes))),
				Source: SourceP2P,
				Track:  resolvedTrack,
			}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	if err := c.Play(ctx, domain.TrackID("remote1")); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if c.GetCurrentTrack() == nil || c.GetCurrentTrack().Title != "Remote Song" {
		t.Fatalf("currentTrack title = %v, want 'Remote Song'", c.GetCurrentTrack().Title)
	}
}

func TestPlaybackController_SetQueue_GetQueue(t *testing.T) {
	bus := events.New()
	defer bus.Close()

	repo := &testLibraryRepo{}
	player := &testPlayer{}
	resolver := &testResolver{}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	q := &testQueue{
		tracks: []*domain.Track{
			{ID: domain.TrackID("t1")},
			{ID: domain.TrackID("t2")},
		},
	}

	c.SetQueue(q)
	c.mu.Lock()
	got := c.queue
	c.mu.Unlock()

	if got != q {
		t.Fatal("queue not set correctly")
	}
}

func TestPlaybackController_Underrun_BufferingTransitions(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	player := &testPlayer{state: domain.PlayerStatePlaying}
	c := NewPlaybackController(&testLibraryRepo{}, testSearchHandler{}, player, &testResolver{}, bus)
	c.fsm.SetStateForTest(domain.PlayerStatePlaying)

	// In Playing with fill below LowWatermark → underrun → Buffering.
	player.fillLevel = audio.LowWatermark - 0.05
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStateBuffering {
		t.Fatalf("state after underrun = %v, want buffering", got)
	}

	// Still below threshold while Buffering → no change.
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStateBuffering {
		t.Fatalf("state after second low-fill check = %v, want buffering", got)
	}

	// Refilled above HighWatermark → ready → Playing.
	player.fillLevel = audio.HighWatermark + 0.05
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStatePlaying {
		t.Fatalf("state after refill = %v, want playing", got)
	}

	// Above watermark while Playing → no spurious transition.
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStatePlaying {
		t.Fatalf("state after high-fill check = %v, want playing", got)
	}
}

func TestPlaybackController_Underrun_PublishesEvents(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	player := &testPlayer{state: domain.PlayerStatePlaying}
	c := NewPlaybackController(&testLibraryRepo{}, testSearchHandler{}, player, &testResolver{}, bus)
	c.fsm.SetStateForTest(domain.PlayerStatePlaying)

	eventsCh := make(chan domain.EventType, 4)
	unsub := bus.Subscribe(domain.EventPlaybackBuffering, func(e domain.Event) {
		eventsCh <- e.Type
	})
	unsub2 := bus.Subscribe(domain.EventPlaybackReady, func(e domain.Event) {
		eventsCh <- e.Type
	})
	defer unsub()
	defer unsub2()

	player.fillLevel = audio.LowWatermark - 0.05
	c.checkBufferLevel(ctx)

	player.fillLevel = audio.HighWatermark + 0.05
	c.checkBufferLevel(ctx)

	got := map[domain.EventType]bool{}
	for range 2 {
		select {
		case et := <-eventsCh:
			got[et] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for buffering/ready events")
		}
	}
	if !got[domain.EventPlaybackBuffering] || !got[domain.EventPlaybackReady] {
		t.Fatalf("events = %v, want both buffering and ready", got)
	}
}

func TestPlaybackController_InitialVolume(t *testing.T) {
	bus := events.New()
	defer bus.Close()

	c := NewPlaybackController(&testLibraryRepo{}, testSearchHandler{}, &testPlayer{}, &testResolver{}, bus, 35)
	if got := c.GetVolume(); got != 35 {
		t.Fatalf("GetVolume() with configured initial = %d, want 35", got)
	}

	c2 := NewPlaybackController(&testLibraryRepo{}, testSearchHandler{}, &testPlayer{}, &testResolver{}, bus)
	if got := c2.GetVolume(); got != 80 {
		t.Fatalf("GetVolume() default = %d, want 80", got)
	}
}
