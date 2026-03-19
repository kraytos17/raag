package p2p

import (
	"log/slog"
	"sync"
)

type streamEvent string

const (
	streamEventRequest  = "request"
	streamEventDataRecv = "data_received"
	streamEventStall    = "stall"
	streamEventResume   = "resume"
	streamEventComplete = "complete"
	streamEventFail     = "fail"
	streamEventCancel   = "cancel"
)

type streamState string

const (
	streamStateIdle       = "idle"
	streamStateRequesting = "requesting"
	streamStateStreaming  = "streaming"
	streamStateStalled    = "stalled"
	streamStateComplete   = "complete"
	streamStateError      = "error"
)

type streamingFSM struct {
	mu    sync.Mutex
	state streamState
}

func newStreamingFSM() *streamingFSM {
	return &streamingFSM{state: streamStateIdle}
}

var streamTransitions = map[streamState]map[streamEvent]streamState{
	streamStateIdle:       {streamEventRequest: streamStateRequesting},
	streamStateRequesting: {streamEventDataRecv: streamStateStreaming, streamEventComplete: streamStateComplete, streamEventFail: streamStateError},
	streamStateStreaming:  {streamEventStall: streamStateStalled, streamEventComplete: streamStateComplete, streamEventFail: streamStateError},
	streamStateStalled:    {streamEventResume: streamStateStreaming, streamEventDataRecv: streamStateStreaming, streamEventFail: streamStateError},
}

func (s *streamingFSM) Send(event streamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.state
	next, ok := streamTransitions[prev][event]
	if !ok {
		slog.Debug("stream FSM: no transition", "event", event, "from", prev)
		return nil
	}

	s.state = next
	slog.Debug("stream FSM transition", "from", prev, "to", next, "event", event)
	return nil
}
