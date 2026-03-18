package p2p

import (
	"context"
	"log/slog"
	"sync"

	"github.com/looplab/fsm"
)

type StreamEvent string

const (
	StreamEventRequest  StreamEvent = "request"
	StreamEventDataRecv StreamEvent = "data_received"
	StreamEventStall    StreamEvent = "stall"
	StreamEventResume   StreamEvent = "resume"
	StreamEventComplete StreamEvent = "complete"
	StreamEventFail     StreamEvent = "fail"
	StreamEventCancel   StreamEvent = "cancel"
)

type StreamState string

const (
	StreamStateIdle       StreamState = "idle"
	StreamStateRequesting StreamState = "requesting"
	StreamStateStreaming  StreamState = "streaming"
	StreamStateStalled    StreamState = "stalled"
	StreamStateComplete   StreamState = "complete"
	StreamStateError      StreamState = "error"
)

type StreamingFSM struct {
	mu  sync.Mutex
	fsm *fsm.FSM
}

func NewStreamingFSM() *StreamingFSM {
	s := &StreamingFSM{}

	s.fsm = fsm.NewFSM(
		string(StreamStateIdle),
		[]fsm.EventDesc{
			{Name: string(StreamEventRequest), Src: []string{string(StreamStateIdle)}, Dst: string(StreamStateRequesting)},
			{Name: string(StreamEventDataRecv), Src: []string{string(StreamStateRequesting)}, Dst: string(StreamStateStreaming)},
			{Name: string(StreamEventStall), Src: []string{string(StreamStateStreaming)}, Dst: string(StreamStateStalled)},
			{Name: string(StreamEventResume), Src: []string{string(StreamStateStalled)}, Dst: string(StreamStateStreaming)},
			{Name: string(StreamEventDataRecv), Src: []string{string(StreamStateStalled)}, Dst: string(StreamStateStreaming)},
			{Name: string(StreamEventComplete), Src: []string{string(StreamStateStreaming), string(StreamStateRequesting)}, Dst: string(StreamStateComplete)},
			{Name: string(StreamEventFail), Src: []string{string(StreamStateRequesting), string(StreamStateStreaming), string(StreamStateStalled)}, Dst: string(StreamStateError)},
			{Name: string(StreamEventCancel), Src: []string{string(StreamStateRequesting), string(StreamStateStreaming), string(StreamStateStalled)}, Dst: string(StreamStateIdle)},
		},
		map[string]fsm.Callback{
			"enter_state": s.onStateChange,
		},
	)

	return s
}

func (s *StreamingFSM) onStateChange(ctx context.Context, e *fsm.Event) {
	currentState := StreamState(e.Dst)
	event := StreamEvent(e.Event)

	slog.Debug("streaming FSM state change",
		"to", currentState,
		"event", event,
	)
}

func (s *StreamingFSM) CurrentState() StreamState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StreamState(s.fsm.Current())
}

func (s *StreamingFSM) Send(event StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fsm.Event(context.Background(), string(event)); err != nil {
		slog.Warn("streaming FSM event rejected", "event", event, "current", s.fsm.Current(), "error", err)
		return err
	}
	return nil
}
