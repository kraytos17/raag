package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/p-society/raag/internal/domain"
)

var subscriptionID uint64

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[domain.EventType]map[uint64]domain.EventHandler
}

func New() *EventBus {
	return &EventBus{
		subscribers: make(map[domain.EventType]map[uint64]domain.EventHandler),
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

func (eb *EventBus) Subscribe(eventType domain.EventType, handler domain.EventHandler) domain.Unsubscribe {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.subscribers[eventType] == nil {
		eb.subscribers[eventType] = make(map[uint64]domain.EventHandler)
	}

	id := atomic.AddUint64(&subscriptionID, 1)
	eb.subscribers[eventType][id] = handler
	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		delete(eb.subscribers[eventType], id)
	}
}
