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
		event   domain.PlaybackEvent
		wantErr bool
	}{
		{"domain.EventPlay", domain.EventPlay, false},
		{"domain.EventPause", domain.EventPause, true},
		{"domain.EventResume", domain.EventResume, true},
		{"domain.EventStop", domain.EventStop, false},
		{"domain.EventEOF", domain.EventEOF, true},
		{"domain.EventSeek", domain.EventSeek, true},
		{"domain.EventBufferReady", domain.EventBufferReady, true},
		{"domain.EventBufferFail", domain.EventBufferFail, true},
		{"domain.EventUnderrun", domain.EventUnderrun, true},
		{"domain.EventSeekDone", domain.EventSeekDone, true},
		{"domain.EventRetry", domain.EventRetry, true},
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
		event   domain.PlaybackEvent
		to      domain.PlayerState
		wantErr bool
	}{
		{domain.PlayerStateIdle, domain.EventPlay, domain.PlayerStateBuffering, false},
		{domain.PlayerStateBuffering, domain.EventBufferReady, domain.PlayerStatePlaying, false},
		{domain.PlayerStateBuffering, domain.EventBufferFail, domain.PlayerStateError, false},
		{domain.PlayerStatePlaying, domain.EventPause, domain.PlayerStatePaused, false},
		{domain.PlayerStatePlaying, domain.EventEOF, domain.PlayerStateIdle, false},
		{domain.PlayerStatePlaying, domain.EventSeek, domain.PlayerStateSeeking, false},
		{domain.PlayerStatePlaying, domain.EventUnderrun, domain.PlayerStateBuffering, false},
		{domain.PlayerStatePaused, domain.EventResume, domain.PlayerStatePlaying, false},
		{domain.PlayerStateSeeking, domain.EventSeekDone, domain.PlayerStatePlaying, false},
		{domain.PlayerStateError, domain.EventRetry, domain.PlayerStateBuffering, false},
		{domain.PlayerStateError, domain.EventStop, domain.PlayerStateIdle, false},
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
		event domain.PlaybackEvent
	}{
		{domain.PlayerStateIdle, domain.EventPause},
		{domain.PlayerStateIdle, domain.EventResume},
		{domain.PlayerStateIdle, domain.EventEOF},
		{domain.PlayerStateIdle, domain.EventSeek},
		{domain.PlayerStateIdle, domain.EventBufferReady},
		{domain.PlayerStateIdle, domain.EventBufferFail},
		{domain.PlayerStateIdle, domain.EventUnderrun},
		{domain.PlayerStateIdle, domain.EventSeekDone},
		{domain.PlayerStateIdle, domain.EventRetry},
		{domain.PlayerStateBuffering, domain.EventPlay},
		{domain.PlayerStateBuffering, domain.EventPause},
		{domain.PlayerStateBuffering, domain.EventResume},
		{domain.PlayerStateBuffering, domain.EventEOF},
		{domain.PlayerStateBuffering, domain.EventSeek},
		{domain.PlayerStateBuffering, domain.EventUnderrun},
		{domain.PlayerStateBuffering, domain.EventSeekDone},
		{domain.PlayerStateBuffering, domain.EventRetry},
		{domain.PlayerStatePlaying, domain.EventPlay},
		{domain.PlayerStatePlaying, domain.EventBufferReady},
		{domain.PlayerStatePlaying, domain.EventBufferFail},
		{domain.PlayerStatePlaying, domain.EventRetry},
		{domain.PlayerStatePaused, domain.EventPlay},
		{domain.PlayerStatePaused, domain.EventPause},
		{domain.PlayerStatePaused, domain.EventEOF},
		{domain.PlayerStatePaused, domain.EventBufferReady},
		{domain.PlayerStatePaused, domain.EventBufferFail},
		{domain.PlayerStatePaused, domain.EventUnderrun},
		{domain.PlayerStatePaused, domain.EventSeekDone},
		{domain.PlayerStatePaused, domain.EventRetry},
		{domain.PlayerStateSeeking, domain.EventPlay},
		{domain.PlayerStateSeeking, domain.EventPause},
		{domain.PlayerStateSeeking, domain.EventResume},
		{domain.PlayerStateSeeking, domain.EventEOF},
		{domain.PlayerStateSeeking, domain.EventBufferReady},
		{domain.PlayerStateSeeking, domain.EventBufferFail},
		{domain.PlayerStateSeeking, domain.EventUnderrun},
		{domain.PlayerStateSeeking, domain.EventRetry},
		{domain.PlayerStateError, domain.EventPlay},
		{domain.PlayerStateError, domain.EventPause},
		{domain.PlayerStateError, domain.EventResume},
		{domain.PlayerStateError, domain.EventEOF},
		{domain.PlayerStateError, domain.EventSeek},
		{domain.PlayerStateError, domain.EventBufferReady},
		{domain.PlayerStateError, domain.EventBufferFail},
		{domain.PlayerStateError, domain.EventUnderrun},
		{domain.PlayerStateError, domain.EventSeekDone},
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
		event domain.PlaybackEvent
		state domain.PlayerState
	}{
		{"play from idle", domain.EventPlay, domain.PlayerStateBuffering},
		{"buffer ready", domain.EventBufferReady, domain.PlayerStatePlaying},
		{"pause", domain.EventPause, domain.PlayerStatePaused},
		{"resume", domain.EventResume, domain.PlayerStatePlaying},
		{"seek", domain.EventSeek, domain.PlayerStateSeeking},
		{"seek done", domain.EventSeekDone, domain.PlayerStatePlaying},
		{"eof returns to idle", domain.EventEOF, domain.PlayerStateIdle},
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
	if err := fsm.Send(context.Background(), domain.EventBufferFail); err != nil {
		t.Errorf("Send(domain.EventBufferFail) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateError {
		t.Errorf("Send(domain.EventBufferFail) state = %v, want domain.PlayerStateError", got)
	}
	if err := fsm.Send(context.Background(), domain.EventRetry); err != nil {
		t.Errorf("Send(domain.EventRetry) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateBuffering {
		t.Errorf("Send(domain.EventRetry) state = %v, want domain.PlayerStateBuffering", got)
	}
	if err := fsm.Send(context.Background(), domain.EventBufferReady); err != nil {
		t.Errorf("Send(domain.EventBufferReady) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStatePlaying {
		t.Errorf("Send(domain.EventBufferReady) state = %v, want domain.PlayerStatePlaying", got)
	}
}

func TestFSM_EOFBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	fsm.SetStateForTest(domain.PlayerStatePlaying)
	if err := fsm.Send(context.Background(), domain.EventEOF); err != nil {
		t.Errorf("Send(domain.EventEOF) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateIdle {
		t.Errorf("Send(domain.EventEOF) state = %v, want domain.PlayerStateIdle", got)
	}
}

func TestFSM_UnderrunBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)
	fsm.SetStateForTest(domain.PlayerStatePlaying)
	if err := fsm.Send(context.Background(), domain.EventUnderrun); err != nil {
		t.Errorf("Send(domain.EventUnderrun) error = %v", err)
	}
	if got := fsm.State(); got != domain.PlayerStateBuffering {
		t.Errorf("Send(domain.EventUnderrun) state = %v, want domain.PlayerStateBuffering", got)
	}
}

func TestFSM_CanTransition(t *testing.T) {
	tests := []struct {
		state    domain.PlayerState
		event    domain.PlaybackEvent
		canTrans bool
	}{
		{domain.PlayerStateIdle, domain.EventPlay, true},
		{domain.PlayerStateIdle, domain.EventPause, false},
		{domain.PlayerStateBuffering, domain.EventBufferReady, true},
		{domain.PlayerStateBuffering, domain.EventBufferFail, true},
		{domain.PlayerStateBuffering, domain.EventPlay, false},
		{domain.PlayerStatePlaying, domain.EventPause, true},
		{domain.PlayerStatePlaying, domain.EventEOF, true},
		{domain.PlayerStatePlaying, domain.EventSeek, true},
		{domain.PlayerStatePlaying, domain.EventPlay, false},
		{domain.PlayerStatePaused, domain.EventResume, true},
		{domain.PlayerStatePaused, domain.EventStop, true},
		{domain.PlayerStatePaused, domain.EventSeek, true},
		{domain.PlayerStatePaused, domain.EventPlay, false},
		{domain.PlayerStateSeeking, domain.EventSeekDone, true},
		{domain.PlayerStateSeeking, domain.EventStop, true},
		{domain.PlayerStateError, domain.EventRetry, true},
		{domain.PlayerStateError, domain.EventStop, true},
		{domain.PlayerStateError, domain.EventPlay, false},
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
			_ = fsm.Send(context.Background(), domain.EventPlay)
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
		event domain.PlaybackEvent
		str   string
	}{
		{domain.EventPlay, "play"},
		{domain.EventBufferReady, "buffer_ready"},
		{domain.EventBufferFail, "buffer_fail"},
		{domain.EventPause, "pause"},
		{domain.EventResume, "resume"},
		{domain.EventEOF, "eof"},
		{domain.EventSeek, "seek"},
		{domain.EventSeekDone, "seek_done"},
		{domain.EventUnderrun, "underrun"},
		{domain.EventStop, "stop"},
		{domain.EventRetry, "retry"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			if string(tt.event) != tt.str {
				t.Errorf("Event constant = %q, want %q", tt.event, tt.str)
			}
		})
	}
}

func stateEventName(state domain.PlayerState, event domain.PlaybackEvent) string {
	return string(state) + "_" + string(event)
}
