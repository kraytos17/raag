package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

type PlaybackEvent string

const (
	EventPlay        PlaybackEvent = "play"
	EventBufferReady PlaybackEvent = "buffer_ready"
	EventBufferFail  PlaybackEvent = "buffer_fail"
	EventPause       PlaybackEvent = "pause"
	EventResume      PlaybackEvent = "resume"
	EventEOF         PlaybackEvent = "eof"
	EventSeek        PlaybackEvent = "seek"
	EventSeekDone    PlaybackEvent = "seek_done"
	EventUnderrun    PlaybackEvent = "underrun"
	EventStop        PlaybackEvent = "stop"
	EventRetry       PlaybackEvent = "retry"
)

type PlaybackState string

const (
	StateIdle      PlaybackState = "idle"
	StateBuffering PlaybackState = "buffering"
	StatePlaying   PlaybackState = "playing"
	StatePaused    PlaybackState = "paused"
	StateSeeking   PlaybackState = "seeking"
	StateError     PlaybackState = "error"
)

type PlaybackFSM struct {
	mu    sync.Mutex
	state PlaybackState
	bus   domain.EventBus
}

func NewPlaybackFSM(bus domain.EventBus) *PlaybackFSM {
	return &PlaybackFSM{
		state: StateIdle,
		bus:   bus,
	}
}

func (p *PlaybackFSM) CurrentState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *PlaybackFSM) Send(event PlaybackEvent) error {
	p.mu.Lock()
	prev := p.state
	next, ok := transitions[prev][event]
	if !ok {
		p.mu.Unlock()
		slog.Warn("playback FSM: invalid transition", "event", event, "from", prev)
		return nil
	}

	p.state = next
	slog.Debug("playback FSM transition", "from", prev, "to", next, "event", event)
	p.mu.Unlock()

	switch next {
	case StatePlaying:
		p.bus.Publish(context.Background(), domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{}))
	case StatePaused:
		p.bus.Publish(context.Background(), domain.NewEvent(domain.EventTrackPaused, domain.TrackPausedPayload{}))
	case StateIdle:
		if prev == StatePlaying || prev == StatePaused {
			p.bus.Publish(context.Background(), domain.NewEvent(domain.EventTrackFinished, domain.TrackFinishedPayload{Completed: true}))
		}
	}
	return nil
}

var transitions = map[PlaybackState]map[PlaybackEvent]PlaybackState{
	StateIdle:      {EventPlay: StateBuffering},
	StateBuffering: {EventBufferReady: StatePlaying, EventBufferFail: StateError},
	StatePlaying:   {EventPause: StatePaused, EventEOF: StateIdle, EventSeek: StateSeeking, EventUnderrun: StateBuffering},
	StatePaused:    {EventResume: StatePlaying},
	StateSeeking:   {EventSeekDone: StatePlaying},
	StateError:     {EventRetry: StateBuffering, EventStop: StateIdle},
}

func (p *PlaybackFSM) CanTransition(event PlaybackEvent) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := transitions[p.state][event]
	return ok
}
