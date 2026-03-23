package app

import (
	"context"
	"sync"
	"testing"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/events"
)

// SetStateForTest force-sets the FSM state.
// This exists for tests that need to arrange state without racing concurrent Send().
func (f *FSM[S, E]) SetStateForTest(state S) {
	f.mu.Lock()
	f.state = state
	f.mu.Unlock()
}

func TestFSM_New(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	if got := fsm.State(); got != domain.PlayerStateIdle {
		t.Errorf("NewPlaybackFSM() initial state = %v, want domain.PlayerStateIdle", got)
	}
}

func TestFSM_InitialState(t *testing.T) {
	tests := []struct {
		name    string
		event   PlaybackEvent
		wantErr bool
	}{
		{"EventPlay", EventPlay, false},
		{"EventPause", EventPause, true},
		{"EventResume", EventResume, true},
		{"EventStop", EventStop, false},
		{"EventEOF", EventEOF, true},
		{"EventSeek", EventSeek, true},
		{"EventBufferReady", EventBufferReady, true},
		{"EventBufferFail", EventBufferFail, true},
		{"EventUnderrun", EventUnderrun, true},
		{"EventSeekDone", EventSeekDone, true},
		{"EventRetry", EventRetry, true},
	}

	for _, tt := range tests {
		t.Run("from_Idle_"+tt.name, func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			err := fsm.Send(context.Background(), tt.event)
			if tt.wantErr && err == nil {
				t.Errorf("Send(%s) error = nil, want ErrInvalidTransition", tt.event)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Send(%s) error = %v, want nil", tt.event, err)
			}
		})
	}
}

func TestFSM_ValidTransitions(t *testing.T) {
	tests := []struct {
		from    domain.PlayerState
		event   PlaybackEvent
		to      domain.PlayerState
		wantErr bool
	}{
		{domain.PlayerStateIdle, EventPlay, domain.PlayerStateBuffering, false},
		{domain.PlayerStateBuffering, EventBufferReady, domain.PlayerStatePlaying, false},
		{domain.PlayerStateBuffering, EventBufferFail, domain.PlayerStateError, false},
		{domain.PlayerStatePlaying, EventPause, domain.PlayerStatePaused, false},
		{domain.PlayerStatePlaying, EventEOF, domain.PlayerStateIdle, false},
		{domain.PlayerStatePlaying, EventSeek, domain.PlayerStateSeeking, false},
		{domain.PlayerStatePlaying, EventUnderrun, domain.PlayerStateBuffering, false},
		{domain.PlayerStatePaused, EventResume, domain.PlayerStatePlaying, false},
		{domain.PlayerStateSeeking, EventSeekDone, domain.PlayerStatePlaying, false},
		{domain.PlayerStateError, EventRetry, domain.PlayerStateBuffering, false},
		{domain.PlayerStateError, EventStop, domain.PlayerStateIdle, false},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.from, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.SetStateForTest(tt.from)
			err := fsm.Send(context.Background(), tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("Send(%s) error = %v, wantErr %v", tt.event, err, tt.wantErr)
			}
			if got := fsm.State(); got != tt.to {
				t.Errorf("Send(%s) state = %v, want %v", tt.event, got, tt.to)
			}
		})
	}
}

func TestFSM_InvalidTransitions(t *testing.T) {
	tests := []struct {
		from  domain.PlayerState
		event PlaybackEvent
	}{
		{domain.PlayerStateIdle, EventPause},
		{domain.PlayerStateIdle, EventResume},
		{domain.PlayerStateIdle, EventEOF},
		{domain.PlayerStateIdle, EventSeek},
		{domain.PlayerStateIdle, EventBufferReady},
		{domain.PlayerStateIdle, EventBufferFail},
		{domain.PlayerStateIdle, EventUnderrun},
		{domain.PlayerStateIdle, EventSeekDone},
		{domain.PlayerStateIdle, EventRetry},
		{domain.PlayerStateBuffering, EventPlay},
		{domain.PlayerStateBuffering, EventPause},
		{domain.PlayerStateBuffering, EventResume},
		{domain.PlayerStateBuffering, EventEOF},
		{domain.PlayerStateBuffering, EventSeek},
		{domain.PlayerStateBuffering, EventUnderrun},
		{domain.PlayerStateBuffering, EventSeekDone},
		{domain.PlayerStateBuffering, EventRetry},
		{domain.PlayerStatePlaying, EventPlay},
		{domain.PlayerStatePlaying, EventBufferReady},
		{domain.PlayerStatePlaying, EventBufferFail},
		{domain.PlayerStatePlaying, EventRetry},
		{domain.PlayerStatePaused, EventPlay},
		{domain.PlayerStatePaused, EventPause},
		{domain.PlayerStatePaused, EventEOF},
		{domain.PlayerStatePaused, EventBufferReady},
		{domain.PlayerStatePaused, EventBufferFail},
		{domain.PlayerStatePaused, EventUnderrun},
		{domain.PlayerStatePaused, EventSeekDone},
		{domain.PlayerStatePaused, EventRetry},
		{domain.PlayerStateSeeking, EventPlay},
		{domain.PlayerStateSeeking, EventPause},
		{domain.PlayerStateSeeking, EventResume},
		{domain.PlayerStateSeeking, EventEOF},
		{domain.PlayerStateSeeking, EventBufferReady},
		{domain.PlayerStateSeeking, EventBufferFail},
		{domain.PlayerStateSeeking, EventUnderrun},
		{domain.PlayerStateSeeking, EventRetry},
		{domain.PlayerStateError, EventPlay},
		{domain.PlayerStateError, EventPause},
		{domain.PlayerStateError, EventResume},
		{domain.PlayerStateError, EventEOF},
		{domain.PlayerStateError, EventSeek},
		{domain.PlayerStateError, EventBufferReady},
		{domain.PlayerStateError, EventBufferFail},
		{domain.PlayerStateError, EventUnderrun},
		{domain.PlayerStateError, EventSeekDone},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.from, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.SetStateForTest(tt.from)
			initialState := fsm.State()
			err := fsm.Send(context.Background(), tt.event)
			if err == nil {
				t.Errorf("Send(%s) error = nil, want ErrInvalidTransition for invalid transition", tt.event)
			}
			if got := fsm.State(); got != initialState {
				t.Errorf("Send(%s) state changed from %v to %v, want unchanged", tt.event, initialState, got)
			}
		})
	}
}

func TestFSM_FullPlaybackCycle(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	tests := []struct {
		name  string
		event PlaybackEvent
		state domain.PlayerState
	}{
		{"play from idle", EventPlay, domain.PlayerStateBuffering},
		{"buffer ready", EventBufferReady, domain.PlayerStatePlaying},
		{"pause", EventPause, domain.PlayerStatePaused},
		{"resume", EventResume, domain.PlayerStatePlaying},
		{"seek", EventSeek, domain.PlayerStateSeeking},
		{"seek done", EventSeekDone, domain.PlayerStatePlaying},
		{"eof returns to idle", EventEOF, domain.PlayerStateIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fsm.Send(context.Background(), tt.event)
			if err != nil {
				t.Errorf("Send(%s) error = %v", tt.event, err)
			}
			if got := fsm.State(); got != tt.state {
				t.Errorf("Send(%s) state = %v, want %v", tt.event, got, tt.state)
			}
		})
	}
}

func TestFSM_ErrorRecovery(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	fsm.SetStateForTest(domain.PlayerStateBuffering)
	if err := fsm.Send(context.Background(), EventBufferFail); err != nil {
		t.Errorf("Send(EventBufferFail) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateError {
		t.Errorf("Send(EventBufferFail) state = %v, want domain.PlayerStateError", got)
	}
	if err := fsm.Send(context.Background(), EventRetry); err != nil {
		t.Errorf("Send(EventRetry) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateBuffering {
		t.Errorf("Send(EventRetry) state = %v, want domain.PlayerStateBuffering", got)
	}
	if err := fsm.Send(context.Background(), EventBufferReady); err != nil {
		t.Errorf("Send(EventBufferReady) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStatePlaying {
		t.Errorf("Send(EventBufferReady) state = %v, want domain.PlayerStatePlaying", got)
	}
}

func TestFSM_EOFBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	fsm.SetStateForTest(domain.PlayerStatePlaying)
	if err := fsm.Send(context.Background(), EventEOF); err != nil {
		t.Errorf("Send(EventEOF) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateIdle {
		t.Errorf("Send(EventEOF) state = %v, want domain.PlayerStateIdle", got)
	}
}

func TestFSM_UnderrunBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	fsm.SetStateForTest(domain.PlayerStatePlaying)
	if err := fsm.Send(context.Background(), EventUnderrun); err != nil {
		t.Errorf("Send(EventUnderrun) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateBuffering {
		t.Errorf("Send(EventUnderrun) state = %v, want domain.PlayerStateBuffering", got)
	}
}

func TestFSM_CanTransition(t *testing.T) {
	tests := []struct {
		state    domain.PlayerState
		event    PlaybackEvent
		canTrans bool
	}{
		{domain.PlayerStateIdle, EventPlay, true},
		{domain.PlayerStateIdle, EventPause, false},
		{domain.PlayerStateBuffering, EventBufferReady, true},
		{domain.PlayerStateBuffering, EventBufferFail, true},
		{domain.PlayerStateBuffering, EventPlay, false},
		{domain.PlayerStatePlaying, EventPause, true},
		{domain.PlayerStatePlaying, EventEOF, true},
		{domain.PlayerStatePlaying, EventSeek, true},
		{domain.PlayerStatePlaying, EventPlay, false},
		{domain.PlayerStatePaused, EventResume, true},
		{domain.PlayerStatePaused, EventStop, true},
		{domain.PlayerStatePaused, EventSeek, true},
		{domain.PlayerStatePaused, EventPlay, false},
		{domain.PlayerStateSeeking, EventSeekDone, true},
		{domain.PlayerStateSeeking, EventStop, true},
		{domain.PlayerStateError, EventRetry, true},
		{domain.PlayerStateError, EventStop, true},
		{domain.PlayerStateError, EventPlay, false},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.state, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.SetStateForTest(tt.state)
			if got := fsm.CanTransition(tt.event); got != tt.canTrans {
				t.Errorf("CanTransition(%s) = %v, want %v", tt.event, got, tt.canTrans)
			}
		})
	}
}

func TestFSM_State(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	tests := []struct {
		state domain.PlayerState
	}{
		{domain.PlayerStateIdle},
		{domain.PlayerStateBuffering},
		{domain.PlayerStatePlaying},
		{domain.PlayerStatePaused},
		{domain.PlayerStateSeeking},
		{domain.PlayerStateError},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			fsm.SetStateForTest(tt.state)
			if got := fsm.State(); got != tt.state {
				t.Errorf("State() = %v, want %v", got, tt.state)
			}
		})
	}
}

func TestFSM_ConcurrentSend(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			_ = fsm.Send(context.Background(), EventPlay)
		})
	}

	wg.Wait()
	if got := fsm.State(); got != domain.PlayerStateBuffering {
		t.Errorf("after concurrent sends, state = %v, want domain.PlayerStateBuffering", got)
	}
}

func TestFSM_StateConstants(t *testing.T) {
	tests := []struct {
		state domain.PlayerState
		str   string
	}{
		{domain.PlayerStateIdle, "idle"},
		{domain.PlayerStateBuffering, "buffering"},
		{domain.PlayerStatePlaying, "playing"},
		{domain.PlayerStatePaused, "paused"},
		{domain.PlayerStateSeeking, "seeking"},
		{domain.PlayerStateError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			if string(tt.state) != tt.str {
				t.Errorf("State constant = %q, want %q", tt.state, tt.str)
			}
		})
	}
}

func TestFSM_EventConstants(t *testing.T) {
	tests := []struct {
		event PlaybackEvent
		str   string
	}{
		{EventPlay, "play"},
		{EventBufferReady, "buffer_ready"},
		{EventBufferFail, "buffer_fail"},
		{EventPause, "pause"},
		{EventResume, "resume"},
		{EventEOF, "eof"},
		{EventSeek, "seek"},
		{EventSeekDone, "seek_done"},
		{EventUnderrun, "underrun"},
		{EventStop, "stop"},
		{EventRetry, "retry"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			if string(tt.event) != tt.str {
				t.Errorf("Event constant = %q, want %q", tt.event, tt.str)
			}
		})
	}
}

func stateEventName(state domain.PlayerState, event PlaybackEvent) string {
	return string(state) + "_" + string(event)
}
