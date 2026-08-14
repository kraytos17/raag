package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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
	volume        int
	queue         Queue
	advanceCancel context.CancelFunc
	advanceGen    atomic.Int64

	progressCb     func(positionMs, durationMs int64)
	progressTicker *time.Ticker
}

func NewPlaybackController(
	libraryRepo LibraryRepository,
	searchHandler SearchHandler,
	player Player,
	resolver Resolver,
	bus domain.EventBus,
) *PlaybackController {
	return &PlaybackController{
		libraryRepo:   libraryRepo,
		searchHandler: searchHandler,
		player:        player,
		resolver:      resolver,
		bus:           bus,
		fsm:           NewPlaybackFSM(bus),
		volume:        80,
	}
}

func (c *PlaybackController) Play(ctx context.Context, trackID domain.TrackID) error {
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
	if resolved.Source == SourceP2P {
		src := audio.NewStreamingSource(resolved.Reader, audio.DefaultBufferSize)
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
	if err := c.fsm.Send(ctx, domain.EventBufferReady); err != nil {
		slog.Error("FSM transition to playing failed", "error", err)
	}

	c.startAdvanceWatcher(ctx)
	c.startProgressTicker(ctx)
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
	if c.advanceCancel != nil {
		c.advanceCancel()
		c.advanceCancel = nil
	}
	if c.progressTicker != nil {
		c.progressTicker.Stop()
		c.progressTicker = nil
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

func (c *PlaybackController) GetState() domain.PlayerState {
	return c.fsm.State()
}

func (c *PlaybackController) GetCurrentTrack() *domain.Track {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentTrack
}

func (c *PlaybackController) SetQueue(q Queue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = q
}

func (c *PlaybackController) startAdvanceWatcher(ctx context.Context) {
	if c.advanceCancel != nil {
		c.advanceCancel()
	}

	gen := c.advanceGen.Add(1)
	watchCtx, cancel := context.WithCancel(ctx)
	c.advanceCancel = cancel
	go func() {
		select {
		case <-c.player.Done():
		case <-watchCtx.Done():
			return
		}

		// Check if this watcher is stale
		if gen != c.advanceGen.Load() {
			return
		}

		_ = c.fsm.Send(watchCtx, domain.EventEOF)

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
		_ = c.Play(watchCtx, next.ID)
	}()
}

func (c *PlaybackController) search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return c.searchHandler.Search(ctx, query, limit)
}

func (c *PlaybackController) OnProgress(callback func(positionMs, durationMs int64)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressCb = callback
}

func (c *PlaybackController) startProgressTicker(ctx context.Context) {
	c.mu.Lock()
	if c.progressTicker != nil {
		c.progressTicker.Stop()
	}

	c.progressTicker = time.NewTicker(100 * time.Millisecond)
	c.mu.Unlock()
	go func() {
		for {
			select {
			case <-c.progressTicker.C:
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
			case <-ctx.Done():
				c.mu.Lock()
				if c.progressTicker != nil {
					c.progressTicker.Stop()
					c.progressTicker = nil
				}
				c.mu.Unlock()
				return
			}
		}
	}()
}
