package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
)

type PlaybackController struct {
	mu            sync.RWMutex
	libraryRepo   LibraryRepository
	searchHandler SearchHandler
	player        Player
	resolver      Resolver
	bus           domain.EventBus
	fsm           *PlaybackFSM
	currentTrack  *domain.Track
	preparedTrack *domain.Track
	volume        int
	queue         Queue
	sessionCancel context.CancelFunc
	progressCb    func(positionMs, durationMs int64)
}

// NewPlaybackController creates a playback controller. An optional initial
// volume (0-100) may be passed as a trailing argument; it defaults to 80.
func NewPlaybackController(
	libraryRepo LibraryRepository,
	searchHandler SearchHandler,
	player Player,
	resolver Resolver,
	bus domain.EventBus,
	initialVolume ...int,
) *PlaybackController {
	vol := 80
	if len(initialVolume) > 0 {
		vol = initialVolume[0]
	}
	return &PlaybackController{
		libraryRepo:   libraryRepo,
		searchHandler: searchHandler,
		player:        player,
		resolver:      resolver,
		bus:           bus,
		fsm:           NewPlaybackFSM(bus),
		volume:        vol,
	}
}

// startSession creates the context that all per-playback goroutines (advance
// watcher, buffer monitor, progress ticker) share for this playback session.
// It cancels any previous session so a new Play supersedes the old one, and
// Stop cancels it. The generation counters previously used to detect stale
// watchers are unnecessary: a canceled context stays canceled, so no old
// goroutine can mistake itself for current.
func (c *PlaybackController) startSession(ctx context.Context) context.Context {
	c.mu.Lock()
	if c.sessionCancel != nil {
		c.sessionCancel()
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	c.sessionCancel = cancel
	c.mu.Unlock()
	return sessionCtx
}

func (c *PlaybackController) Play(ctx context.Context, trackID domain.TrackID) error {
	sessionCtx := c.startSession(ctx)
	resolved, err := c.resolver.Resolve(ctx, trackID)
	if err != nil {
		return err
	}

	track := resolved.Track
	if track == nil {
		track, err = c.libraryRepo.FindByID(ctx, trackID)
		if err != nil {
			_ = resolved.Reader.Close()
			return err
		}
	}
	if err := c.fsm.Send(ctx, domain.EventPlay); err != nil {
		return err
	}

	c.mu.Lock()
	c.currentTrack = track
	c.mu.Unlock()

	var playErr error
	c.player.SetLoudness(float64(track.LoudnessDB))
	if resolved.Source == SourceP2P {
		src := audio.NewStreamingSource(resolved.Reader, audio.DefaultBufferSize)
		// Pre-roll: wait until the buffer has enough data before decoding.
		// Otherwise the decoder's first read races an empty buffer and the
		// track fails on the very first play over the network
		if err := src.WaitReady(ctx, audio.PreRollBytes); err != nil {
			_ = src.Close()
			_ = c.fsm.Send(ctx, domain.EventBufferFail)

			c.mu.Lock()
			c.currentTrack = nil
			c.mu.Unlock()
			return err
		}
		playErr = c.player.PlayStreaming(ctx, src, track.MimeType)
	} else {
		playErr = c.player.Play(ctx, resolved.Reader, track.MimeType)
	}

	if playErr != nil {
		_ = c.fsm.Send(ctx, domain.EventBufferFail)
		c.mu.Lock()
		c.currentTrack = nil
		c.mu.Unlock()
		return playErr
	}

	// Best-effort gapless preload of the next local queue item.
	if resolved.Source == SourceLocal {
		c.preloadNext(ctx)
	}
	// EventBufferReady leaves the Buffering state entered on EventPlay above.
	// For P2P this transition is real: WaitReady blocked until PreRollBytes of
	// pre-roll data buffered. For local sources there is no buffering
	// phase, so the hop is instant. All later underrun/refill transitions are
	// driven by the buffer monitor (startBufferMonitor/checkBufferLevel), which
	// is the source of truth once playback is underway.
	if err := c.fsm.Send(ctx, domain.EventBufferReady); err != nil {
		slog.Error("FSM transition to playing failed", "error", err)
	}

	c.startAdvanceWatcher(ctx, sessionCtx)
	c.startProgressTicker(sessionCtx)
	c.startBufferMonitor(sessionCtx)
	c.bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{
		TrackID:  track.ID,
		Title:    track.Title,
		Artist:   track.Artist,
		Album:    track.Album,
		Duration: track.Duration(),
	}))
	return nil
}

func (c *PlaybackController) PlayQuery(ctx context.Context, query string) error {
	results, err := c.search(ctx, query, 1)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return domain.ErrTrackNotFound
	}
	return c.Play(ctx, results[0].ID)
}

func (c *PlaybackController) Pause(ctx context.Context) error {
	if err := c.fsm.Send(ctx, domain.EventPause); err != nil {
		return err
	}
	if err := c.player.Pause(ctx); err != nil {
		_ = c.fsm.Send(ctx, domain.EventResume)
		return err
	}

	c.mu.Lock()
	var trackID domain.TrackID
	if c.currentTrack != nil {
		trackID = c.currentTrack.ID
	}

	c.mu.Unlock()
	c.bus.Publish(ctx, domain.NewEvent(domain.EventTrackPaused, domain.TrackPausedPayload{
		TrackID:  trackID,
		Position: c.player.GetPosition(),
	}))
	return nil
}

func (c *PlaybackController) Resume(ctx context.Context) error {
	if err := c.fsm.Send(ctx, domain.EventResume); err != nil {
		return err
	}
	if err := c.player.Resume(ctx); err != nil {
		_ = c.fsm.Send(ctx, domain.EventPause)
		return err
	}

	c.mu.Lock()
	var trackID domain.TrackID
	if c.currentTrack != nil {
		trackID = c.currentTrack.ID
	}

	c.mu.Unlock()
	c.bus.Publish(ctx, domain.NewEvent(domain.EventTrackResumed, domain.TrackResumedPayload{
		TrackID:  trackID,
		Position: c.player.GetPosition(),
	}))
	return nil
}

func (c *PlaybackController) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.sessionCancel != nil {
		c.sessionCancel()
		c.sessionCancel = nil
	}

	state := c.fsm.State()
	if state == domain.PlayerStateIdle {
		c.currentTrack = nil
		c.mu.Unlock()
		return nil
	}

	c.mu.Unlock()
	if err := c.fsm.Send(ctx, domain.EventStop); err != nil {
		return err
	}
	if err := c.player.Stop(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	c.currentTrack = nil
	c.mu.Unlock()
	return nil
}

func (c *PlaybackController) Seek(ctx context.Context, position time.Duration) error {
	prevState := c.fsm.State()
	prevPos := c.player.GetPosition()
	if err := c.fsm.Send(ctx, domain.EventSeek); err != nil {
		return err
	}
	if err := c.player.Seek(ctx, position); err != nil {
		// Roll back to the previous stable state.
		// Currently in Seeking; SeekDone returns to Playing.
		_ = c.fsm.Send(ctx, domain.EventSeekDone)
		if prevState == domain.PlayerStatePaused {
			_ = c.fsm.Send(ctx, domain.EventPause)
		}
		return err
	}
	if err := c.fsm.Send(ctx, domain.EventSeekDone); err != nil {
		return err
	}
	c.mu.Lock()

	var trackID domain.TrackID
	if c.currentTrack != nil {
		trackID = c.currentTrack.ID
	}

	c.mu.Unlock()
	c.bus.Publish(ctx, domain.NewEvent(domain.EventTrackSeeked, domain.TrackSeekedPayload{
		TrackID: trackID,
		From:    prevPos,
		To:      position,
	}))
	return nil
}

func (c *PlaybackController) SetVolume(ctx context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("volume must be between 0 and 100, got %d", volume)
	}
	if err := c.player.SetVolume(ctx, volume); err != nil {
		return err
	}

	c.mu.Lock()
	prevVolume := c.volume
	c.volume = volume
	c.mu.Unlock()

	c.bus.Publish(ctx, domain.NewEvent(domain.EventVolumeChanged, domain.VolumeChangedPayload{
		Volume:   volume,
		Previous: prevVolume,
	}))
	return nil
}

func (c *PlaybackController) GetVolume() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.volume
}

// SetEqualizer forwards the EQ settings to the engine (live rebuild).
func (c *PlaybackController) SetEqualizer(settings domain.EqualizerSettings) {
	c.player.SetEqualizer(settings)
}

// GetEqualizer returns the engine's current EQ settings.
func (c *PlaybackController) GetEqualizer() domain.EqualizerSettings {
	return c.player.GetEqualizer()
}

func (c *PlaybackController) GetState() domain.PlayerState {
	return c.fsm.State()
}

func (c *PlaybackController) GetCurrentTrack() *domain.Track {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentTrack
}

// GetPosition returns the current playback position from the engine.
func (c *PlaybackController) GetPosition() time.Duration {
	return c.player.GetPosition()
}

// GetBufferFillLevel returns the audio buffer fill ratio (0-1) from the
// engine, surfaced to the daemon so the UI can show streaming health.
func (c *PlaybackController) GetBufferFillLevel() float64 {
	return c.player.GetBufferFillLevel()
}

func (c *PlaybackController) SetQueue(q Queue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = q
}

// preloadNext resolves the next queue item and prepares it for gapless commit.
// Local sources only (v1); P2P sources are skipped because their buffering
// semantics conflict with pre-decode. Best-effort: failures leave the fallback
// `Play(next)` path intact.
func (c *PlaybackController) preloadNext(ctx context.Context) {
	c.mu.RLock()
	queue := c.queue
	c.mu.RUnlock()
	if queue == nil {
		return
	}

	next := queue.Peek()
	if next == nil {
		return
	}

	resolved, err := c.resolver.Resolve(ctx, next.ID)
	if err != nil {
		return
	}
	defer func() { _ = resolved.Reader.Close() }()

	if resolved.Source != SourceLocal {
		return
	}
	if err := c.player.Prepare(resolved.Reader, next.MimeType); err != nil {
		return
	}

	c.mu.Lock()
	c.preparedTrack = next
	c.mu.Unlock()
}

// startAdvanceWatcher waits for the current track's natural end and advances
// the queue. It is bound to the playback session: when sessionCtx is canceled
// (a newer Play superseded this one, or Stop ran), the watcher exits. playCtx
// is the caller's original context, used only for the fallback re-Play path.
func (c *PlaybackController) startAdvanceWatcher(playCtx, sessionCtx context.Context) {
	go func() {
		select {
		case <-c.player.Done():
		case <-sessionCtx.Done():
			return
		}

		// Natural end of the current track: notify the UI before advancing.
		// The watcher only fires on Done() (canceled on Stop or a new track),
		// so this is the "track finished" signal.
		c.mu.RLock()
		track := c.currentTrack
		c.mu.RUnlock()
		if track != nil {
			c.bus.Publish(sessionCtx, domain.NewEvent(domain.EventTrackFinished, domain.TrackFinishedPayload{
				TrackID:   track.ID,
				PlayCount: track.PlayCount,
				Duration:  track.Duration(),
				Completed: true,
			}))
		}
		if c.player.HasNext() {
			if err := c.player.CommitNext(); err == nil {
				c.mu.Lock()
				next := c.preparedTrack
				c.preparedTrack = nil
				c.currentTrack = next
				c.mu.Unlock()
				if next != nil {
					c.bus.Publish(sessionCtx, domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{
						TrackID:  next.ID,
						Duration: next.Duration(),
					}))
				}
				c.startAdvanceWatcher(playCtx, sessionCtx)
				return
			}
		}
		// Fallback: nothing was prepared (or commit failed). Hard-stop and
		// play the next queue item as before.
		_ = c.player.CommitNext() // clear any partially prepared state
		c.mu.Lock()
		c.preparedTrack = nil
		c.mu.Unlock()

		_ = c.fsm.Send(sessionCtx, domain.EventEOF)
		c.mu.Lock()
		state := c.fsm.State()
		queue := c.queue
		c.mu.Unlock()

		if state != domain.PlayerStateIdle {
			return
		}
		if queue == nil {
			return
		}

		next := queue.Next()
		if next == nil {
			return
		}
		_ = c.Play(playCtx, next.ID)
	}()
}

func (c *PlaybackController) search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return c.searchHandler.Search(ctx, query, limit)
}

// startBufferMonitor watches the player's buffer fill level and drives the
// FSM Playing↔Buffering transitions on underrun and refill.
// Without this, a drained buffer would silently stop the track.
func (c *PlaybackController) startBufferMonitor(sessionCtx context.Context) {
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				c.checkBufferLevel(sessionCtx)
			}
		}
	}()
}

// checkBufferLevel emits underrun/refill events based on the live buffer fill.
func (c *PlaybackController) checkBufferLevel(ctx context.Context) {
	state := c.fsm.State()
	fill := c.player.GetBufferFillLevel()
	switch state {
	case domain.PlayerStatePlaying:
		if fill < audio.LowWatermark {
			if err := c.fsm.Send(ctx, domain.EventUnderrun); err != nil {
				return
			}
			slog.Debug("playback underrun — buffering", "fill", fill)
			c.bus.Publish(ctx, domain.NewEvent(domain.EventPlaybackBuffering, domain.PlaybackBufferingPayload{
				FillLevel: fill,
			}))
		}
	case domain.PlayerStateBuffering:
		if fill >= audio.HighWatermark {
			if err := c.fsm.Send(ctx, domain.EventBufferReady); err != nil {
				return
			}

			slog.Debug("playback buffered — resuming", "fill", fill)
			c.bus.Publish(ctx, domain.NewEvent(domain.EventPlaybackReady, domain.PlaybackReadyPayload{
				FillLevel: fill,
			}))
		}
	}
}

func (c *PlaybackController) OnProgress(callback func(positionMs, durationMs int64)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressCb = callback
}

func (c *PlaybackController) startProgressTicker(sessionCtx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state := c.fsm.State()
				if state != domain.PlayerStatePlaying {
					continue
				}

				pos := c.player.GetPosition()
				var durMs int64
				c.mu.RLock()
				if c.currentTrack != nil {
					durMs = c.currentTrack.Duration().Milliseconds()
				}

				cb := c.progressCb
				c.mu.RUnlock()
				if cb != nil {
					cb(pos.Milliseconds(), durMs)
				}
			case <-sessionCtx.Done():
				return
			}
		}
	}()
}
