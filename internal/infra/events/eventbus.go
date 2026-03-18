package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/p-society/raag/internal/domain"
)

var subscriptionID uint64

type subscription struct {
	id        uint64
	eventType domain.EventType
	bus       *EventBus
}

func (s *subscription) Unsubscribe() {
	s.bus.UnsubscribeByID(s.eventType, s.id)
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[domain.EventType]map[uint64]domain.EventHandler
	lockOrder   map[domain.EventType]*sync.Mutex
}

func New() *EventBus {
	return &EventBus{
		subscribers: make(map[domain.EventType]map[uint64]domain.EventHandler),
		lockOrder:   make(map[domain.EventType]*sync.Mutex),
	}
}

func (eb *EventBus) Publish(ctx context.Context, event domain.Event) {
	eb.mu.RLock()
	handlers, ok := eb.subscribers[event.Type]
	eb.mu.RUnlock()

	if !ok || len(handlers) == 0 {
		return
	}

	for id, handler := range handlers {
		go func(h domain.EventHandler, subscriptionID uint64) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("event handler panic", "event", event.Type, "error", r)
				}
			}()
			select {
			case <-ctx.Done():
				return
			default:
				h(event)
			}
		}(handler, id)
	}
}

func (eb *EventBus) Subscribe(eventType domain.EventType, handler domain.EventHandler) domain.Subscription {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.subscribers[eventType] == nil {
		eb.subscribers[eventType] = make(map[uint64]domain.EventHandler)
	}

	id := atomic.AddUint64(&subscriptionID, 1)
	eb.subscribers[eventType][id] = handler

	return &subscription{
		id:        id,
		eventType: eventType,
		bus:       eb,
	}
}

func (eb *EventBus) Unsubscribe(eventType domain.EventType, handler domain.EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if handlers, ok := eb.subscribers[eventType]; ok {
		for id, h := range handlers {
			if &h == &handler {
				delete(handlers, id)
				return
			}
		}
	}
}

func (eb *EventBus) UnsubscribeByID(eventType domain.EventType, id uint64) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if handlers, ok := eb.subscribers[eventType]; ok {
		delete(handlers, id)
	}
}
