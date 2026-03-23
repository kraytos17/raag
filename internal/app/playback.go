package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type PlaybackController struct {
	mu            sync.Mutex
	libraryRepo   LibraryRepository
	searchHandler SearchHandler
	player        Player
	resolve       ResolveFunc
	bus           domain.EventBus
	fsm           *PlaybackFSM
	currentTrack  *domain.Track
	volume        int
}

func NewPlaybackController(
	libraryRepo LibraryRepository,
	searchHandler SearchHandler,
	player Player,
	resolve ResolveFunc,
	bus domain.EventBus,
) *PlaybackController {
	return &PlaybackController{
		libraryRepo:   libraryRepo,
		searchHandler: searchHandler,
		player:        player,
		resolve:       resolve,
		bus:           bus,
		fsm:           NewPlaybackFSM(bus),
		volume:        80,
	}
}

func (c *PlaybackController) Play(ctx context.Context, trackID domain.TrackID) error {
	track, err := c.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return err
	}
	if err := c.fsm.Send(ctx, EventPlay); err != nil {
		return err
	}

	c.mu.Lock()
	c.currentTrack = track
	c.mu.Unlock()

	reader, err := c.resolve(ctx, trackID)
	if err != nil {
		_ = c.fsm.Send(ctx, EventBufferFail)
		c.mu.Lock()
		c.currentTrack = nil
		c.mu.Unlock()
		return err
	}
	if err := c.player.Play(ctx, reader, track.MimeType); err != nil {
		_ = c.fsm.Send(ctx, EventBufferFail)
		c.mu.Lock()
		c.currentTrack = nil
		c.mu.Unlock()
		return err
	}
	if err := c.fsm.Send(ctx, EventBufferReady); err != nil {
		slog.Error("FSM transition to playing failed", "error", err)
	}

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
	if err := c.fsm.Send(ctx, EventPause); err != nil {
		return err
	}
	if err := c.player.Pause(ctx); err != nil {
		_ = c.fsm.Send(ctx, EventResume)
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
	if err := c.fsm.Send(ctx, EventResume); err != nil {
		return err
	}
	if err := c.player.Resume(ctx); err != nil {
		_ = c.fsm.Send(ctx, EventPause)
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
	if c.fsm.State() == domain.PlayerStateIdle {
		c.mu.Lock()
		c.currentTrack = nil
		c.mu.Unlock()
		return nil
	}
	if err := c.fsm.Send(ctx, EventStop); err != nil {
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
	if err := c.fsm.Send(ctx, EventSeek); err != nil {
		return err
	}
	if err := c.player.Seek(ctx, position); err != nil {
		// Roll back to the previous stable state.
		// We are currently in Seeking; SeekDone returns to Playing.
		_ = c.fsm.Send(ctx, EventSeekDone)
		if prevState == domain.PlayerStatePaused {
			_ = c.fsm.Send(ctx, EventPause)
		}
		return err
	}
	if err := c.fsm.Send(ctx, EventSeekDone); err != nil {
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

func (c *PlaybackController) search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return c.searchHandler.Search(ctx, query, limit)
}
