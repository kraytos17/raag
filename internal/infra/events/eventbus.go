package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/p-society/raag/internal/domain"
)

const (
	eventChannelSize = 64
)

var subscriptionID atomic.Uint64

type ringBuffer struct {
	buf    []domain.Event
	size   int
	head   int
	count  int
	mu     sync.Mutex
	notify chan struct{}
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		buf:    make([]domain.Event, size),
		size:   size,
		notify: make(chan struct{}, 1),
	}
}

func (rb *ringBuffer) Push(event domain.Event) {
	rb.mu.Lock()
	rb.buf[rb.head] = event
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
	select {
	case rb.notify <- struct{}{}:
	default:
	}
	rb.mu.Unlock()
}

func (rb *ringBuffer) Pop() (domain.Event, bool) {
	rb.mu.Lock()
	if rb.count == 0 {
		rb.mu.Unlock()
		return domain.Event{}, false
	}

	idx := (rb.head - rb.count + rb.size) % rb.size
	event := rb.buf[idx]
	rb.count--
	rb.mu.Unlock()
	return event, true
}

type subscription struct {
	id        uint64
	eventType domain.EventType
	rb        *ringBuffer
	handler   domain.EventHandler
}

type EventBus struct {
	mu     sync.RWMutex
	subs   map[uint64]*subscription
	closed bool
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
		sub.rb.Push(event)
	}
}

func (eb *EventBus) Subscribe(eventType domain.EventType, handler domain.EventHandler) domain.Unsubscribe {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		return func() {}
	}

	id := subscriptionID.Add(1)
	sub := &subscription{
		id:        id,
		eventType: eventType,
		rb:        newRingBuffer(eventChannelSize),
		handler:   handler,
	}

	eb.subs[id] = sub
	go eb.dispatch(sub)
	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		delete(eb.subs, id)
	}
}

func (eb *EventBus) dispatch(sub *subscription) {
	for {
		event, ok := sub.rb.Pop()
		if !ok {
			select {
			case <-sub.rb.notify:
				continue
			default:
				return
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("event handler panic", "event", event.Type, "subscription", sub.id, "error", r)
				}
			}()
			sub.handler(event)
		}()
	}
}

func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		return
	}

	eb.closed = true
	eb.subs = make(map[uint64]*subscription)
}
