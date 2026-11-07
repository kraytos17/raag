package client

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/backoff"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const (
	EventPlaybackState    uint32 = 1
	EventTrackChanged     uint32 = 2
	EventProgress         uint32 = 4
	EventVolumeChanged    uint32 = 8
	EventQueueUpdated     uint32 = 16
	EventPeerConnected    uint32 = 32
	EventPeerDisconnected uint32 = 64
	EventLibraryUpdated   uint32 = 128
	EventError            uint32 = 256
)

// ConnState reports the lifecycle of an event subscription's connection.
type ConnState int

const (
	// ConnConnected is emitted after a successful (re)subscribe.
	ConnConnected ConnState = iota
	// ConnReconnecting is emitted when the subscription lost its connection
	// and is backing off before retrying.
	ConnReconnecting
)

// EventClient handles event subscriptions with backpressure. The client's
// single reader goroutine fans events out to registered subscribers; the
// EventClient does not read the connection itself.
type EventClient struct {
	events    chan *pb.Event
	errChan   chan error
	state     chan ConnState
	closeOnce sync.Once
	closed    chan struct{}
	client    *Client
	eventMask uint32
	subID     uint64
}

// Subscribe subscribes to events from the server.
// Returns an EventClient that streams events through the Events channel.
func (c *Client) Subscribe(eventMask uint32) (*EventClient, error) {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		state:     make(chan ConnState, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	c.mu.Lock()
	c.nextSubID++
	ec.subID = c.nextSubID
	c.subs[ec.subID] = ec
	c.mu.Unlock()

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: eventMask}},
	}
	if _, err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.subs, ec.subID)
		c.mu.Unlock()
		return nil, fmt.Errorf("subscribe failed: %w", err)
	}
	return ec, nil
}

// SubscribeWithRetry subscribes with automatic reconnection and retry logic.
// Events are queued on backpressure, with exponential backoff on disconnect.
func (c *Client) SubscribeWithRetry(eventMask uint32) *EventClient {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		state:     make(chan ConnState, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	c.mu.Lock()
	c.nextSubID++
	ec.subID = c.nextSubID
	c.subs[ec.subID] = ec
	c.mu.Unlock()

	go ec.eventLoopWithRetry()
	return ec
}

// eventLoopWithRetry re-subscribes until the client is closed, using
// exponential backoff on failure. The reader goroutine signals a connection
// drop by delivering to errChan (via notifyConnLost).
func (ec *EventClient) eventLoopWithRetry() {
	policy := backoff.NewExponential(100*time.Millisecond, 5*time.Second)
	for {
		select {
		case <-ec.closed:
			return
		default:
		}

		err := ec.resubscribe()
		if err != nil {
			ec.setState(ConnReconnecting)
			if backoff.Wait(ec.closed, policy.Next()) {
				return
			}
			continue
		}

		policy.Reset()
		ec.setState(ConnConnected)
		select {
		case <-ec.closed:
			return
		case <-ec.errChan:
			// Connection dropped. Report reconnecting immediately — the
			// resubscribe below may block in the client's dial backoff.
			ec.setState(ConnReconnecting)
		}
	}
}

// setState emits a connection-state signal to the subscriber (non-blocking).
func (ec *EventClient) setState(state ConnState) {
	select {
	case ec.state <- state:
	default:
	}
}

// resubscribe (re-)issues the Subscribe request on the shared connection.
func (ec *EventClient) resubscribe() error {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: ec.eventMask}},
	}

	_, err := ec.client.send(req)
	return err
}

// sendEvent sends an event to the channel with backpressure handling.
func (ec *EventClient) sendEvent(event *pb.Event) {
	select {
	case ec.events <- event:
	default:
		select {
		case <-ec.events:
		default:
		}

		select {
		case ec.events <- event:
		default:
			slog.Warn("dropping event due to backpressure", "eventType", event.EventType)
		}
	}
}

// notifyConnLost delivers a connection-drop signal to the subscriber.
func (ec *EventClient) notifyConnLost(err error) {
	select {
	case ec.errChan <- err:
	default:
	}
}

// Events returns the channel to receive events
func (ec *EventClient) Events() <-chan *pb.Event {
	return ec.events
}

// State returns the channel that reports connection-state transitions
// (ConnConnected / ConnReconnecting) for this subscription.
func (ec *EventClient) State() <-chan ConnState {
	return ec.state
}

// Err returns the error channel
func (ec *EventClient) Err() <-chan error {
	return ec.errChan
}

// Close unsubscribes and closes the event client.
// The unsubscribe request is written without waiting for a response: the
// server removes the subscription when the connection closes regardless.
func (ec *EventClient) Close() error {
	ec.closeOnce.Do(func() {
		close(ec.closed)
		ec.client.mu.Lock()
		delete(ec.client.subs, ec.subID)

		conn := ec.client.conn
		ec.client.mu.Unlock()
		if conn != nil {
			req := &pb.Request{
				ProtocolVersion: domain.IPCProtocolVersion,
				Payload:         &pb.Request_Unsubscribe{Unsubscribe: &pb.UnsubscribeRequest{}},
			}
			wire.WriteMsg(conn, req)
		}
	})
	return nil
}
