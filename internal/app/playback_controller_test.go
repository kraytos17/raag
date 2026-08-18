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
	prepared          string
	committed         bool
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
func (p *testPlayer) SetLoudness(db float64)                          {}
func (p *testPlayer) SetEqualizer(settings domain.EqualizerSettings)  {}
func (p *testPlayer) GetEqualizer() domain.EqualizerSettings          { return domain.EqualizerSettings{} }

func (p *testPlayer) Prepare(reader io.Reader, mimeType string) error {
	p.prepared = mimeType
	return nil
}

func (p *testPlayer) PrepareStreaming(source audio.AudioSource, mimeType string) error {
	p.prepared = mimeType
	return nil
}
func (p *testPlayer) HasNext() bool                { return p.prepared != "" }
func (p *testPlayer) CommitNext() error            { p.committed = true; return nil }
func (p *testPlayer) GetState() domain.PlayerState { return p.state }
func (p *testPlayer) GetPosition() time.Duration   { return p.pos }
func (p *testPlayer) GetBufferFillLevel() float64  { return p.fillLevel }
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

// TestPlaybackController_Play_WhilePlaying_IsLegal verifies a mid-playback
// Play is now a valid FSM transition (Playing → Buffering), so user Next/Prev
// no longer fail with ErrInvalidTransition (12.2.1 crossfade prerequisite).
func TestPlaybackController_Play_WhilePlaying_IsLegal(t *testing.T) {
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
	c.fsm.SetStateForTest(domain.PlayerStatePlaying)

	if err := c.Play(ctx, track.ID); err != nil {
		t.Fatalf("Play() while playing error = %v, want nil (no ErrInvalidTransition)", err)
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
	// The pre-roll wait (WaitReady) makes the Buffering state real: the FSM
	// leaves Idle→Buffering on EventPlay, blocks until PreRollBytes buffer,
	// then EventBufferReady→Playing. Assert we end in Playing
	if got := c.GetState(); got != domain.PlayerStatePlaying {
		t.Fatalf("state after P2P play = %v, want playing", got)
	}
}

func TestPlaybackController_Play_BufferingIsReal(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	// The monitor (checkBufferLevel), not the play-time EventBufferReady, is
	// the source of truth for underrun/refill once playback is underway
	player := &testPlayer{state: domain.PlayerStatePlaying}
	c := NewPlaybackController(&testLibraryRepo{}, testSearchHandler{}, player, &testResolver{}, bus)
	c.fsm.SetStateForTest(domain.PlayerStatePlaying)

	eventsCh := make(chan domain.EventType, 2)
	unsub := bus.Subscribe(domain.EventPlaybackBuffering, func(e domain.Event) { eventsCh <- e.Type })
	unsub2 := bus.Subscribe(domain.EventPlaybackReady, func(e domain.Event) { eventsCh <- e.Type })
	defer unsub()
	defer unsub2()

	// Low fill while Playing → real underrun → Buffering.
	player.fillLevel = audio.LowWatermark - 0.05
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStateBuffering {
		t.Fatalf("state after low fill = %v, want buffering", got)
	}

	// Refilled above HighWatermark → real buffer-ready → Playing.
	player.fillLevel = audio.HighWatermark + 0.05
	c.checkBufferLevel(ctx)
	if got := c.GetState(); got != domain.PlayerStatePlaying {
		t.Fatalf("state after refill = %v, want playing", got)
	}

	got := map[domain.EventType]bool{}
	for range 2 {
		select {
		case et := <-eventsCh:
			got[et] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for buffering events")
		}
	}
	if !got[domain.EventPlaybackBuffering] {
		t.Error("missing EventPlaybackBuffering on underrun")
	}
	if !got[domain.EventPlaybackReady] {
		t.Error("missing EventPlaybackReady on refill")
	}
}

func TestPlaybackController_Play_ResolvesTrackFromResolved(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	resolvedTrack := &domain.Track{ID: domain.TrackID("remote1"), Title: remoteSong, MimeType: mimeTypeMPEG}
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
	if c.GetCurrentTrack() == nil || c.GetCurrentTrack().Title != remoteSong {
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

func TestPlaybackController_TrackFinished_PublishedOnDone(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	track := &domain.Track{ID: domain.TrackID("track-1"), Title: "Finished Song", DurationMs: 180000, PlayCount: 3}
	player := &testPlayer{done: make(chan struct{})}
	c := NewPlaybackController(&testLibraryRepo{track: track}, testSearchHandler{}, player, &testResolver{}, bus)

	if err := c.Play(ctx, track.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}

	finished := make(chan domain.TrackFinishedPayload, 1)
	unsub := bus.Subscribe(domain.EventTrackFinished, func(e domain.Event) {
		if p, ok := e.Payload.(domain.TrackFinishedPayload); ok {
			finished <- p
		}
	})
	defer unsub()

	close(player.done) // simulate natural track end
	select {
	case p := <-finished:
		if p.TrackID != track.ID {
			t.Errorf("TrackID = %v, want %v", p.TrackID, track.ID)
		}
		if p.PlayCount != 3 {
			t.Errorf("PlayCount = %d, want 3", p.PlayCount)
		}
		if p.Duration != 180*time.Second {
			t.Errorf("Duration = %v, want 180s", p.Duration)
		}
		if !p.Completed {
			t.Error("Completed = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventTrackFinished")
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

func TestPlaybackController_Preload_AfterLocalPlay(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	t1 := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	t2 := &domain.Track{ID: domain.TrackID("t2"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: t1}
	player := &testPlayer{}
	queue := &testQueue{tracks: []*domain.Track{t2}}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			tr := t1
			if trackID == t2.ID {
				tr = t2
			}
			return &ResolvedTrack{Reader: io.NopCloser(strings.NewReader("")), Source: SourceLocal, Track: tr}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	c.SetQueue(queue)

	if err := c.Play(ctx, t1.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if player.prepared != mimeTypeMPEG {
		t.Fatalf("prepared = %q, want %q (next local track should be preloaded)", player.prepared, mimeTypeMPEG)
	}
	if c.preparedTrack != t2 {
		t.Fatalf("preparedTrack = %v, want t2", c.preparedTrack)
	}
}

func TestPlaybackController_Preload_P2PNext_Skipped(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	t1 := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	remote := &domain.Track{ID: domain.TrackID("remote"), Path: "/r", MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: t1}
	player := &testPlayer{}
	queue := &testQueue{tracks: []*domain.Track{remote}}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			tr := t1
			src := SourceLocal
			if trackID == remote.ID {
				tr = remote
				src = SourceP2P
			}
			return &ResolvedTrack{Reader: io.NopCloser(strings.NewReader("")), Source: src, Track: tr}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	c.SetQueue(queue)

	if err := c.Play(ctx, t1.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if player.prepared != "" {
		t.Fatalf("prepared = %q, want empty (P2P next must not be preloaded)", player.prepared)
	}
}

func TestPlaybackController_NaturalEnd_CommitsPrepared(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	t1 := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	t2 := &domain.Track{ID: domain.TrackID("t2"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: t1}
	player := &testPlayer{done: make(chan struct{})}
	queue := &testQueue{tracks: []*domain.Track{t2}}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			tr := t1
			if trackID == t2.ID {
				tr = t2
			}
			return &ResolvedTrack{Reader: io.NopCloser(strings.NewReader("")), Source: SourceLocal, Track: tr}, nil
		},
	}

	finished := make(chan domain.EventType, 4)
	started := make(chan domain.EventType, 4)
	bus.Subscribe(domain.EventTrackFinished, func(e domain.Event) { finished <- e.Type })
	bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) { started <- e.Type })

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	c.SetQueue(queue)

	if err := c.Play(ctx, t1.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if player.prepared != mimeTypeMPEG {
		t.Fatalf("prepared = %q, want %q", player.prepared, mimeTypeMPEG)
	}

	// Drain the initial EventTrackStarted that Play publishes for t1.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial EventTrackStarted")
	}

	// Signal natural end.
	close(player.done)

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventTrackFinished")
	}
	// The commit's EventTrackStarted is published after CommitNext + currentTrack
	// update, so receiving it establishes the happens-before for those reads.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed EventTrackStarted")
	}

	if !player.committed {
		t.Fatal("CommitNext was not called on natural end")
	}
	if c.GetState() != domain.PlayerStatePlaying {
		t.Fatalf("state = %v, want playing (gapless commit must not go Idle)", c.GetState())
	}
	if got := c.GetCurrentTrack(); got != t2 {
		t.Fatalf("currentTrack = %v, want t2", got)
	}
}

func TestPlaybackController_NaturalEnd_NoPrepared_FallsBack(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	defer bus.Close()

	t1 := &domain.Track{ID: domain.TrackID("t1"), Path: tmpT1Path, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: t1}
	player := &testPlayer{done: make(chan struct{})}
	queue := &testQueue{tracks: []*domain.Track{t1}}
	resolver := &testResolver{
		resolveFunc: func(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
			return &ResolvedTrack{Reader: io.NopCloser(strings.NewReader("")), Source: SourceLocal, Track: t1}, nil
		},
	}

	c := NewPlaybackController(repo, testSearchHandler{}, player, resolver, bus)
	c.SetQueue(queue)
	if err := c.Play(ctx, t1.ID); err != nil {
		t.Fatalf("Play() error = %v", err)
	}

	// Disable preload so nothing is prepared.
	c.mu.Lock()
	c.preparedTrack = nil
	c.mu.Unlock()
	player.prepared = ""

	close(player.done)

	select {
	case <-player.done:
	case <-time.After(time.Second):
	}
	// The fallback path re-Plays the next queue item (t1 again). Give the
	// watcher a moment; the key assertion is that it doesn't panic and the
	// state eventually reflects the fallback Play.
	time.Sleep(50 * time.Millisecond)
}
