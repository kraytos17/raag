package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

var ErrInvalidTransition = errors.New("invalid state transition")

type FSM[S ~string, E ~string] struct {
	mu          sync.RWMutex
	state       S
	transitions map[fsmKey[S, E]]S
}

type fsmKey[S ~string, E ~string] struct {
	from  S
	event E
}

func NewFSM[S ~string, E ~string](initial S, transitions map[S]map[E]S) *FSM[S, E] {
	flat := make(map[fsmKey[S, E]]S)
	for from, events := range transitions {
		for event, to := range events {
			flat[fsmKey[S, E]{from: from, event: event}] = to
		}
	}
	return &FSM[S, E]{
		state:       initial,
		transitions: flat,
	}
}

func (f *FSM[S, E]) State() S {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

func (f *FSM[S, E]) Send(ctx context.Context, event E) error {
	f.mu.Lock()
	prev := f.state
	next, ok := f.transitions[fsmKey[S, E]{from: prev, event: event}]
	if ok {
		f.state = next
	}

	f.mu.Unlock()
	if !ok {
		slog.Debug("FSM: invalid transition", "event", event, "from", prev)
		return ErrInvalidTransition
	}

	slog.Debug("FSM transition", "from", prev, "to", next, "event", event)
	return nil
}

func (f *FSM[S, E]) CanTransition(event E) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	_, ok := f.transitions[fsmKey[S, E]{from: f.state, event: event}]
	return ok
}

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

var playbackTransitions = map[domain.PlayerState]map[PlaybackEvent]domain.PlayerState{
	domain.PlayerStateIdle: {
		EventPlay: domain.PlayerStateBuffering,
		EventStop: domain.PlayerStateIdle,
	},
	domain.PlayerStateBuffering: {
		EventBufferReady: domain.PlayerStatePlaying,
		EventBufferFail:  domain.PlayerStateError,
		EventStop:        domain.PlayerStateIdle,
	},
	domain.PlayerStatePlaying: {
		EventPause:    domain.PlayerStatePaused,
		EventEOF:      domain.PlayerStateIdle,
		EventSeek:     domain.PlayerStateSeeking,
		EventUnderrun: domain.PlayerStateBuffering,
		EventStop:     domain.PlayerStateIdle,
	},
	domain.PlayerStatePaused: {
		EventResume: domain.PlayerStatePlaying,
		EventSeek:   domain.PlayerStateSeeking,
		EventStop:   domain.PlayerStateIdle,
	},
	domain.PlayerStateSeeking: {
		EventSeekDone: domain.PlayerStatePlaying,
		EventStop:     domain.PlayerStateIdle,
	},
	domain.PlayerStateError: {
		EventRetry: domain.PlayerStateBuffering,
		EventStop:  domain.PlayerStateIdle,
	},
}

type PlaybackFSM struct {
	*FSM[domain.PlayerState, PlaybackEvent]
	bus domain.EventBus
}

func NewPlaybackFSM(bus domain.EventBus) *PlaybackFSM {
	return &PlaybackFSM{
		FSM: NewFSM(domain.PlayerStateIdle, playbackTransitions),
		bus: bus,
	}
}

func (p *PlaybackFSM) State() domain.PlayerState {
	return p.FSM.State()
}
