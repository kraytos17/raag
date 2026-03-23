package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/p-society/raag/internal/domain"
)

const (
	eventChannelSize = 64
)

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
	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
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
		sub.rb.Push(event)
	}
}

func (eb *EventBus) Subscribe(eventType domain.EventType, handler domain.EventHandler) domain.Unsubscribe {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		return func() {}
	}

	id := eb.nextSubID.Add(1)
	stop := make(chan struct{})
	sub := &subscription{
		id:        id,
		eventType: eventType,
		rb:        newRingBuffer(eventChannelSize),
		handler:   handler,
		stop:      stop,
	}

	eb.subs[id] = sub
	sub.wg.Go(func() {
		eb.dispatch(sub, stop)
	})
	return func() {
		sub.closeOnce.Do(func() { close(stop) })
		eb.mu.Lock()
		delete(eb.subs, id)
		eb.mu.Unlock()
		sub.wg.Wait()
	}
}

func (eb *EventBus) dispatch(sub *subscription, stop chan struct{}) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		event, ok := sub.rb.Pop()
		if !ok {
			if !timer.Reset(5 * time.Second) {
				timer = time.NewTimer(5 * time.Second)
			}
			select {
			case <-sub.rb.notify:
			case <-timer.C:
			case <-stop:
				return
			}
			continue
		}
		if !timer.Reset(5 * time.Second) {
			timer = time.NewTimer(5 * time.Second)
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
