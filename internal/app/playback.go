package app

import (
	"context"
	"fmt"
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

func (u *PlaybackController) Play(ctx context.Context, trackID domain.TrackID) error {
	track, err := u.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return err
	}
	if err := u.fsm.Send(ctx, EventPlay); err != nil {
		return err
	}

	u.mu.Lock()
	u.currentTrack = track
	u.mu.Unlock()

	reader, err := u.resolve(ctx, trackID)
	if err != nil {
		_ = u.fsm.Send(ctx, EventBufferFail)
		return err
	}
	if err := u.player.Play(ctx, reader, track.MimeType); err != nil {
		_ = u.fsm.Send(ctx, EventBufferFail)
		return err
	}

	_ = u.fsm.Send(ctx, EventBufferReady)
	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{
		TrackID:  track.ID,
		Title:    track.Title,
		Artist:   track.Artist,
		Album:    track.Album,
		Duration: track.Duration(),
	}))
	return nil
}

func (u *PlaybackController) PlayQuery(ctx context.Context, query string) error {
	results, err := u.search(ctx, query, 1)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return domain.ErrTrackNotFound
	}
	return u.Play(ctx, results[0].ID)
}

func (u *PlaybackController) Pause(ctx context.Context) error {
	if err := u.fsm.Send(ctx, EventPause); err != nil {
		return err
	}
	if err := u.player.Pause(ctx); err != nil {
		return err
	}

	u.mu.Lock()
	var trackID domain.TrackID
	if u.currentTrack != nil {
		trackID = u.currentTrack.ID
	}

	u.mu.Unlock()
	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackPaused, domain.TrackPausedPayload{
		TrackID:  trackID,
		Position: u.player.GetPosition(),
	}))
	return nil
}

func (u *PlaybackController) Resume(ctx context.Context) error {
	if err := u.fsm.Send(ctx, EventResume); err != nil {
		return err
	}
	if err := u.player.Resume(ctx); err != nil {
		return err
	}

	u.mu.Lock()
	var trackID domain.TrackID
	if u.currentTrack != nil {
		trackID = u.currentTrack.ID
	}

	u.mu.Unlock()
	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackResumed, domain.TrackResumedPayload{
		TrackID:  trackID,
		Position: u.player.GetPosition(),
	}))
	return nil
}

func (u *PlaybackController) Stop(ctx context.Context) error {
	if err := u.fsm.Send(ctx, EventStop); err != nil {
		return err
	}
	if err := u.player.Stop(ctx); err != nil {
		return err
	}

	u.mu.Lock()
	u.currentTrack = nil
	u.mu.Unlock()
	return nil
}

func (u *PlaybackController) Seek(ctx context.Context, position time.Duration) error {
	prevPos := u.player.GetPosition()
	if err := u.fsm.Send(ctx, EventSeek); err != nil {
		return err
	}
	if err := u.player.Seek(ctx, position); err != nil {
		return err
	}
	if err := u.fsm.Send(ctx, EventSeekDone); err != nil {
		return err
	}

	u.mu.Lock()
	var trackID domain.TrackID
	if u.currentTrack != nil {
		trackID = u.currentTrack.ID
	}

	u.mu.Unlock()
	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackSeeked, domain.TrackSeekedPayload{
		TrackID: trackID,
		From:    prevPos,
		To:      position,
	}))
	return nil
}

func (u *PlaybackController) SetVolume(ctx context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("volume must be between 0 and 100, got %d", volume)
	}

	u.mu.Lock()
	prevVolume := u.volume
	u.volume = volume
	u.mu.Unlock()
	if err := u.player.SetVolume(ctx, volume); err != nil {
		return err
	}

	u.bus.Publish(ctx, domain.NewEvent(domain.EventVolumeChanged, domain.VolumeChangedPayload{
		Volume:   volume,
		Previous: prevVolume,
	}))
	return nil
}

func (u *PlaybackController) GetVolume() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.volume
}

func (u *PlaybackController) GetState() PlayerState {
	return u.fsm.State()
}

func (u *PlaybackController) GetCurrentTrack() *domain.Track {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.currentTrack
}

func (u *PlaybackController) search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	return u.searchHandler.Search(ctx, query, limit)
}
