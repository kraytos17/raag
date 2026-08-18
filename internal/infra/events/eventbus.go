package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/p-society/raag/internal/domain"
)

type subscription struct {
	id        uint64
	eventType domain.EventType
	ch        chan domain.Event
	handler   domain.EventHandler
	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// push delivers an event to the subscription's channel. A slow handler never
// blocks the publisher: when the buffer is full the oldest queued event is
// dropped in favor of the newest, mirroring the IPC EventClient's backpressure
// handling.
func (s *subscription) push(event domain.Event) {
	select {
	case s.ch <- event:
		return
	default:
	}

	select {
	case <-s.ch:
	default:
	}

	select {
	case s.ch <- event:
	default:
		slog.Warn("dropping event due to backpressure", "eventType", event.Type, "subscription", s.id)
	}
}

type EventBus struct {
	mu        sync.RWMutex
	subs      map[uint64]*subscription
	closed    bool
	nextSubID atomic.Uint64
}

func New() *EventBus {
	return &EventBus{
		subs: make(map[uint64]*subscription),
	}
}

func (eb *EventBus) Publish(ctx context.Context, event domain.Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}
	for _, sub := range eb.subs {
		if sub.eventType != event.Type {
			continue
		}
		sub.push(event)
	}
}

func (eb *EventBus) Subscribe(eventType domain.EventType, handler domain.EventHandler) domain.Unsubscribe {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		return func() {}
	}

	id := eb.nextSubID.Add(1)
	sub := &subscription{
		id:        id,
		eventType: eventType,
		ch:        make(chan domain.Event, domain.EventChannelSize),
		handler:   handler,
		stop:      make(chan struct{}),
	}

	eb.subs[id] = sub
	sub.wg.Go(func() {
		eb.dispatch(sub)
	})
	return func() {
		sub.closeOnce.Do(func() { close(sub.stop) })
		eb.mu.Lock()
		delete(eb.subs, id)
		eb.mu.Unlock()
		sub.wg.Wait()
	}
}

func (eb *EventBus) dispatch(sub *subscription) {
	for {
		select {
		case event := <-sub.ch:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("event handler panic", "event", event.Type, "subscription", sub.id, "error", r)
					}
				}()
				sub.handler(event)
			}()
		case <-sub.stop:
			return
		}
	}
}

func (eb *EventBus) Close() {
	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		return
	}

	subs := make([]*subscription, 0, len(eb.subs))
	for _, sub := range eb.subs {
		sub.closeOnce.Do(func() { close(sub.stop) })
		subs = append(subs, sub)
	}

	eb.closed = true
	eb.subs = make(map[uint64]*subscription)
	eb.mu.Unlock()

	for _, sub := range subs {
		sub.wg.Wait()
	}
}
