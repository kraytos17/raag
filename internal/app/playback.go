package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type PlaybackController struct {
	libraryRepo  LibraryRepository
	index        SearchIndex
	player       Player
	resolver     *Resolver
	bus          domain.EventBus
	fsm          *PlaybackFSM
	currentTrack *domain.Track
	volume       int
}

func NewPlaybackController(
	libraryRepo LibraryRepository,
	index SearchIndex,
	player Player,
	resolver *Resolver,
	bus domain.EventBus,
) *PlaybackController {
	return &PlaybackController{
		libraryRepo: libraryRepo,
		index:       index,
		player:      player,
		resolver:    resolver,
		bus:         bus,
		fsm:         NewPlaybackFSM(bus),
		volume:      80,
	}
}

func (u *PlaybackController) Play(ctx context.Context, trackID domain.TrackID) error {
	track, err := u.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return err
	}

	u.currentTrack = track
	if err := u.fsm.Send(EventPlay); err != nil {
		return err
	}

	reader, err := u.resolver.Resolve(ctx, trackID)
	if err != nil {
		_ = u.fsm.Send(EventBufferFail)
		return err
	}
	if err := u.player.Play(ctx, reader, track.MimeType); err != nil {
		_ = u.fsm.Send(EventBufferFail)
		return err
	}

	_ = u.fsm.Send(EventBufferReady)
	_ = u.libraryRepo.Save(ctx, track)
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
	if err := u.fsm.Send(EventPause); err != nil {
		return err
	}
	if err := u.player.Pause(ctx); err != nil {
		return err
	}

	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackPaused, domain.TrackPausedPayload{
		TrackID:  u.currentTrack.ID,
		Position: u.player.GetPosition(),
	}))
	return nil
}

func (u *PlaybackController) Resume(ctx context.Context) error {
	if err := u.fsm.Send(EventResume); err != nil {
		return err
	}
	if err := u.player.Resume(ctx); err != nil {
		return err
	}

	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackResumed, domain.TrackResumedPayload{
		TrackID:  u.currentTrack.ID,
		Position: u.player.GetPosition(),
	}))
	return nil
}

func (u *PlaybackController) Stop(ctx context.Context) error {
	if err := u.fsm.Send(EventStop); err != nil {
		return err
	}
	if err := u.player.Stop(ctx); err != nil {
		return err
	}

	u.currentTrack = nil
	return nil
}

func (u *PlaybackController) Seek(ctx context.Context, position time.Duration) error {
	prevPos := u.player.GetPosition()
	if err := u.fsm.Send(EventSeek); err != nil {
		return err
	}
	if err := u.player.Seek(ctx, position); err != nil {
		return err
	}
	if err := u.fsm.Send(EventSeekDone); err != nil {
		return err
	}

	u.bus.Publish(ctx, domain.NewEvent(domain.EventTrackSeeked, domain.TrackSeekedPayload{
		TrackID: u.currentTrack.ID,
		From:    prevPos,
		To:      position,
	}))
	return nil
}

func (u *PlaybackController) SetVolume(ctx context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		volume = 80
	}

	prevVolume := u.volume
	u.volume = volume
	if err := u.player.SetVolume(ctx, volume); err != nil {
		return err
	}

	u.bus.Publish(ctx, domain.NewEvent(domain.EventVolumeChanged, domain.VolumeChangedPayload{
		Volume:   volume,
		Previous: prevVolume,
	}))
	return nil
}

func (u *PlaybackController) GetState() PlayerState {
	return u.player.GetState()
}

func (u *PlaybackController) GetCurrentTrack() *domain.Track {
	return u.currentTrack
}

func (u *PlaybackController) search(ctx context.Context, query string, limit int) ([]*domain.Track, error) {
	trackIDs, err := u.index.Search(ctx, query, limit)
	if err != nil {
		slog.Warn("search failed, falling back to library search", "error", err)
		return u.libraryRepo.Search(ctx, SearchQuery{Query: query, Limit: limit})
	}

	tracks := make([]*domain.Track, 0, len(trackIDs))
	for _, id := range trackIDs {
		track, err := u.libraryRepo.FindByID(ctx, id)
		if err != nil {
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}
