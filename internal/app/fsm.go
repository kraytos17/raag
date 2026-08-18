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

var playbackTransitions = map[domain.PlayerState]map[domain.PlaybackEvent]domain.PlayerState{
	domain.PlayerStateIdle: {
		domain.EventPlay: domain.PlayerStateBuffering,
		domain.EventStop: domain.PlayerStateIdle,
	},
	domain.PlayerStateBuffering: {
		domain.EventBufferReady: domain.PlayerStatePlaying,
		domain.EventBufferFail:  domain.PlayerStateError,
		domain.EventStop:        domain.PlayerStateIdle,
	},
	domain.PlayerStatePlaying: {
		domain.EventPlay:     domain.PlayerStateBuffering,
		domain.EventPause:    domain.PlayerStatePaused,
		domain.EventEOF:      domain.PlayerStateIdle,
		domain.EventSeek:     domain.PlayerStateSeeking,
		domain.EventUnderrun: domain.PlayerStateBuffering,
		domain.EventStop:     domain.PlayerStateIdle,
	},
	domain.PlayerStatePaused: {
		domain.EventResume: domain.PlayerStatePlaying,
		domain.EventSeek:   domain.PlayerStateSeeking,
		domain.EventStop:   domain.PlayerStateIdle,
	},
	domain.PlayerStateSeeking: {
		domain.EventSeekDone: domain.PlayerStatePlaying,
		domain.EventStop:     domain.PlayerStateIdle,
	},
	domain.PlayerStateError: {
		domain.EventRetry: domain.PlayerStateBuffering,
		domain.EventStop:  domain.PlayerStateIdle,
	},
}

type PlaybackFSM struct {
	*FSM[domain.PlayerState, domain.PlaybackEvent]
	bus domain.EventBus
}

func NewPlaybackFSM(bus domain.EventBus) *PlaybackFSM {
	return &PlaybackFSM{
		FSM: NewFSM(domain.PlayerStateIdle, playbackTransitions),
		bus: bus,
	}
}
