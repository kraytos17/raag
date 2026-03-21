package app

import (
	"context"
	"sync"
	"testing"

	"github.com/p-society/raag/internal/infra/events"
)

func TestFSM_New(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	if fsm.state != StateIdle {
		t.Errorf("NewPlaybackFSM() initial state = %v, want StateIdle", fsm.state)
	}
}

func TestFSM_InitialState(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	tests := []struct {
		name  string
		event PlaybackEvent
	}{
		{"EventPlay", EventPlay},
		{"EventPause", EventPause},
		{"EventResume", EventResume},
		{"EventStop", EventStop},
		{"EventEOF", EventEOF},
		{"EventSeek", EventSeek},
		{"EventBufferReady", EventBufferReady},
		{"EventBufferFail", EventBufferFail},
		{"EventUnderrun", EventUnderrun},
		{"EventSeekDone", EventSeekDone},
		{"EventRetry", EventRetry},
	}

	for _, tt := range tests {
		t.Run("from_Idle_"+tt.name, func(t *testing.T) {
			err := fsm.Send(context.Background(), tt.event)
			if err != nil {
				t.Errorf("Send(%s) error = %v", tt.event, err)
			}
		})
	}
}

func TestFSM_ValidTransitions(t *testing.T) {
	tests := []struct {
		from    PlaybackState
		event   PlaybackEvent
		to      PlaybackState
		wantErr bool
	}{
		{StateIdle, EventPlay, StateBuffering, false},
		{StateBuffering, EventBufferReady, StatePlaying, false},
		{StateBuffering, EventBufferFail, StateError, false},
		{StatePlaying, EventPause, StatePaused, false},
		{StatePlaying, EventEOF, StateIdle, false},
		{StatePlaying, EventSeek, StateSeeking, false},
		{StatePlaying, EventUnderrun, StateBuffering, false},
		{StatePaused, EventResume, StatePlaying, false},
		{StateSeeking, EventSeekDone, StatePlaying, false},
		{StateError, EventRetry, StateBuffering, false},
		{StateError, EventStop, StateIdle, false},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.from, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.state = tt.from

			err := fsm.Send(context.Background(), tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("Send(%s) error = %v, wantErr %v", tt.event, err, tt.wantErr)
			}

			if fsm.state != tt.to {
				t.Errorf("Send(%s) state = %v, want %v", tt.event, fsm.state, tt.to)
			}
		})
	}
}

func TestFSM_InvalidTransitions(t *testing.T) {
	tests := []struct {
		from  PlaybackState
		event PlaybackEvent
	}{
		{StateIdle, EventPause},
		{StateIdle, EventResume},
		{StateIdle, EventEOF},
		{StateIdle, EventSeek},
		{StateIdle, EventBufferReady},
		{StateIdle, EventBufferFail},
		{StateIdle, EventUnderrun},
		{StateIdle, EventSeekDone},
		{StateIdle, EventRetry},
		{StateBuffering, EventPlay},
		{StateBuffering, EventPause},
		{StateBuffering, EventResume},
		{StateBuffering, EventEOF},
		{StateBuffering, EventSeek},
		{StateBuffering, EventUnderrun},
		{StateBuffering, EventStop},
		{StateBuffering, EventSeekDone},
		{StateBuffering, EventRetry},
		{StatePlaying, EventPlay},
		{StatePlaying, EventBufferReady},
		{StatePlaying, EventBufferFail},
		{StatePlaying, EventRetry},
		{StatePaused, EventPlay},
		{StatePaused, EventPause},
		{StatePaused, EventEOF},
		{StatePaused, EventSeek},
		{StatePaused, EventBufferReady},
		{StatePaused, EventBufferFail},
		{StatePaused, EventUnderrun},
		{StatePaused, EventSeekDone},
		{StatePaused, EventRetry},
		{StateSeeking, EventPlay},
		{StateSeeking, EventPause},
		{StateSeeking, EventResume},
		{StateSeeking, EventEOF},
		{StateSeeking, EventBufferReady},
		{StateSeeking, EventBufferFail},
		{StateSeeking, EventUnderrun},
		{StateSeeking, EventRetry},
		{StateError, EventPlay},
		{StateError, EventPause},
		{StateError, EventResume},
		{StateError, EventEOF},
		{StateError, EventSeek},
		{StateError, EventBufferReady},
		{StateError, EventBufferFail},
		{StateError, EventUnderrun},
		{StateError, EventSeekDone},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.from, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.state = tt.from
			initialState := fsm.state

			err := fsm.Send(context.Background(), tt.event)
			if err != nil {
				t.Errorf("Send(%s) error = %v, want nil for invalid transition", tt.event, err)
			}
			if fsm.state != initialState {
				t.Errorf("Send(%s) state changed from %v to %v, want unchanged", tt.event, initialState, fsm.state)
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
		state PlaybackState
	}{
		{"play from idle", EventPlay, StateBuffering},
		{"buffer ready", EventBufferReady, StatePlaying},
		{"pause", EventPause, StatePaused},
		{"resume", EventResume, StatePlaying},
		{"seek", EventSeek, StateSeeking},
		{"seek done", EventSeekDone, StatePlaying},
		{"eof returns to idle", EventEOF, StateIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fsm.Send(context.Background(), tt.event)
			if err != nil {
				t.Errorf("Send(%s) error = %v", tt.event, err)
			}
			if fsm.state != tt.state {
				t.Errorf("Send(%s) state = %v, want %v", tt.event, fsm.state, tt.state)
			}
		})
	}
}

func TestFSM_ErrorRecovery(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	fsm.state = StateBuffering
	if err := fsm.Send(context.Background(), EventBufferFail); err != nil {
		t.Errorf("Send(EventBufferFail) error = %v", err)
	}
	if fsm.state != StateError {
		t.Errorf("Send(EventBufferFail) state = %v, want StateError", fsm.state)
	}
	if err := fsm.Send(context.Background(), EventRetry); err != nil {
		t.Errorf("Send(EventRetry) error = %v", err)
	}
	if fsm.state != StateBuffering {
		t.Errorf("Send(EventRetry) state = %v, want StateBuffering", fsm.state)
	}
	if err := fsm.Send(context.Background(), EventBufferReady); err != nil {
		t.Errorf("Send(EventBufferReady) error = %v", err)
	}
	if fsm.state != StatePlaying {
		t.Errorf("Send(EventBufferReady) state = %v, want StatePlaying", fsm.state)
	}
}

func TestFSM_EOFBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	fsm.state = StatePlaying
	if err := fsm.Send(context.Background(), EventEOF); err != nil {
		t.Errorf("Send(EventEOF) error = %v", err)
	}
	if fsm.state != StateIdle {
		t.Errorf("Send(EventEOF) state = %v, want StateIdle", fsm.state)
	}
}

func TestFSM_UnderrunBehavior(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	fsm.state = StatePlaying
	if err := fsm.Send(context.Background(), EventUnderrun); err != nil {
		t.Errorf("Send(EventUnderrun) error = %v", err)
	}
	if fsm.state != StateBuffering {
		t.Errorf("Send(EventUnderrun) state = %v, want StateBuffering", fsm.state)
	}
}

func TestFSM_CanTransition(t *testing.T) {
	tests := []struct {
		state    PlaybackState
		event    PlaybackEvent
		canTrans bool
	}{
		{StateIdle, EventPlay, true},
		{StateIdle, EventPause, false},
		{StateBuffering, EventBufferReady, true},
		{StateBuffering, EventBufferFail, true},
		{StateBuffering, EventPlay, false},
		{StatePlaying, EventPause, true},
		{StatePlaying, EventEOF, true},
		{StatePlaying, EventSeek, true},
		{StatePlaying, EventPlay, false},
		{StatePaused, EventResume, true},
		{StatePaused, EventStop, false},
		{StatePaused, EventPlay, false},
		{StateSeeking, EventSeekDone, true},
		{StateSeeking, EventStop, false},
		{StateError, EventRetry, true},
		{StateError, EventStop, true},
		{StateError, EventPlay, false},
	}

	for _, tt := range tests {
		t.Run(stateEventName(tt.state, tt.event), func(t *testing.T) {
			bus := events.New()
			fsm := NewPlaybackFSM(bus)
			fsm.state = tt.state

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
		state PlaybackState
	}{
		{StateIdle},
		{StateBuffering},
		{StatePlaying},
		{StatePaused},
		{StateSeeking},
		{StateError},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			fsm.state = tt.state
			if got := fsm.State(); got != PlayerState(tt.state) {
				t.Errorf("State() = %v, want %v", got, PlayerState(tt.state))
			}
		})
	}
}

func TestFSM_CurrentState(t *testing.T) {
	bus := events.New()
	fsm := NewPlaybackFSM(bus)

	tests := []struct {
		state PlaybackState
	}{
		{StateIdle},
		{StateBuffering},
		{StatePlaying},
		{StatePaused},
		{StateSeeking},
		{StateError},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			fsm.state = tt.state
			if got := fsm.CurrentState(); got != tt.state {
				t.Errorf("CurrentState() = %v, want %v", got, tt.state)
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
	if fsm.state != StateBuffering {
		t.Errorf("after concurrent sends, state = %v, want StateBuffering", fsm.state)
	}
}

func TestFSM_StateConstants(t *testing.T) {
	tests := []struct {
		state PlaybackState
		str   string
	}{
		{StateIdle, "idle"},
		{StateBuffering, "buffering"},
		{StatePlaying, "playing"},
		{StatePaused, "paused"},
		{StateSeeking, "seeking"},
		{StateError, "error"},
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

func stateEventName(state PlaybackState, event PlaybackEvent) string {
	return string(state) + "_" + string(event)
}
