package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

type FSM[S ~string, E ~string] struct {
	mu          sync.Mutex
	state       S
	transitions map[S]map[E]S
}

func NewFSM[S ~string, E ~string](initial S, transitions map[S]map[E]S) *FSM[S, E] {
	return &FSM[S, E]{
		state:       initial,
		transitions: transitions,
	}
}

func (f *FSM[S, E]) State() S {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *FSM[S, E]) Send(ctx context.Context, event E) error {
	f.mu.Lock()
	prev := f.state
	next, ok := f.transitions[prev][event]
	if !ok {
		f.mu.Unlock()
		slog.Debug("FSM: invalid transition", "event", event, "from", prev)
		return nil
	}

	f.state = next
	slog.Debug("FSM transition", "from", prev, "to", next, "event", event)
	f.mu.Unlock()

	return nil
}

func (f *FSM[S, E]) CanTransition(event E) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.transitions[f.state][event]
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

type PlaybackState string

const (
	StateIdle      PlaybackState = "idle"
	StateBuffering PlaybackState = "buffering"
	StatePlaying   PlaybackState = "playing"
	StatePaused    PlaybackState = "paused"
	StateSeeking   PlaybackState = "seeking"
	StateError     PlaybackState = "error"
)

var playbackTransitions = map[PlaybackState]map[PlaybackEvent]PlaybackState{
	StateIdle:      {EventPlay: StateBuffering},
	StateBuffering: {EventBufferReady: StatePlaying, EventBufferFail: StateError},
	StatePlaying:   {EventPause: StatePaused, EventEOF: StateIdle, EventSeek: StateSeeking, EventUnderrun: StateBuffering},
	StatePaused:    {EventResume: StatePlaying},
	StateSeeking:   {EventSeekDone: StatePlaying},
	StateError:     {EventRetry: StateBuffering, EventStop: StateIdle},
}

type PlaybackFSM struct {
	*FSM[PlaybackState, PlaybackEvent]
	bus domain.EventBus
}

func NewPlaybackFSM(bus domain.EventBus) *PlaybackFSM {
	return &PlaybackFSM{
		FSM: NewFSM(StateIdle, playbackTransitions),
		bus: bus,
	}
}

func (p *PlaybackFSM) CurrentState() PlaybackState {
	return p.FSM.State()
}

func (p *PlaybackFSM) State() PlayerState {
	return PlayerState(p.FSM.State())
}
