package app

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/looplab/fsm"
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
	mu  sync.Mutex
	fsm *fsm.FSM
	bus domain.EventBus
}

func NewPlaybackFSM(bus domain.EventBus) *PlaybackFSM {
	f := &PlaybackFSM{
		bus: bus,
	}

	f.fsm = fsm.NewFSM(
		string(StateIdle),
		[]fsm.EventDesc{
			{Name: string(EventPlay), Src: []string{string(StateIdle)}, Dst: string(StateBuffering)},
			{Name: string(EventBufferReady), Src: []string{string(StateBuffering)}, Dst: string(StatePlaying)},
			{Name: string(EventBufferFail), Src: []string{string(StateBuffering)}, Dst: string(StateError)},
			{Name: string(EventPause), Src: []string{string(StatePlaying)}, Dst: string(StatePaused)},
			{Name: string(EventResume), Src: []string{string(StatePaused)}, Dst: string(StatePlaying)},
			{Name: string(EventEOF), Src: []string{string(StatePlaying)}, Dst: string(StateIdle)},
			{Name: string(EventSeek), Src: []string{string(StatePlaying)}, Dst: string(StateSeeking)},
			{Name: string(EventUnderrun), Src: []string{string(StatePlaying)}, Dst: string(StateBuffering)},
			{Name: string(EventSeekDone), Src: []string{string(StateSeeking)}, Dst: string(StatePlaying)},
			{Name: string(EventStop), Src: []string{string(StateBuffering), string(StatePlaying), string(StatePaused), string(StateSeeking), string(StateError)}, Dst: string(StateIdle)},
			{Name: string(EventRetry), Src: []string{string(StateError)}, Dst: string(StateBuffering)},
		},
		map[string]fsm.Callback{
			"enter_state": f.onStateChange,
		},
	)
	return f
}

func (p *PlaybackFSM) onStateChange(ctx context.Context, e *fsm.Event) {
	currentState := PlaybackState(e.Dst)
	prevState := PlaybackState(e.Src[0])
	event := PlaybackEvent(e.Event)
	slog.Debug("playback FSM state change",
		"from", prevState,
		"to", currentState,
		"event", event,
	)

	innerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch currentState {
	case StatePlaying:
		p.bus.Publish(innerCtx, domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{
			TrackID: "",
			Title:   "",
			Artist:  "",
			Album:   "",
		}))
	case StatePaused:
		p.bus.Publish(innerCtx, domain.NewEvent(domain.EventTrackPaused, domain.TrackPausedPayload{
			TrackID: "",
		}))
	case StateIdle:
		if len(e.Src) > 0 && (prevState == StatePlaying || prevState == StatePaused) {
			p.bus.Publish(innerCtx, domain.NewEvent(domain.EventTrackFinished, domain.TrackFinishedPayload{
				Completed: true,
			}))
		}
	}
}

func (p *PlaybackFSM) CurrentState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PlaybackState(p.fsm.Current())
}

func (p *PlaybackFSM) Send(event PlaybackEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.fsm.Event(context.Background(), string(event)); err != nil {
		slog.Warn("playback FSM event rejected", "event", event, "current", p.fsm.Current(), "error", err)
		return err
	}
	return nil
}

func (p *PlaybackFSM) CanTransition(event PlaybackEvent) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.fsm.AvailableTransitions(), string(event))
}
