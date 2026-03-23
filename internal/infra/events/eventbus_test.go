package events

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/p-society/raag/internal/domain"
)

func TestEventBus_New(t *testing.T) {
	bus := New()
	if bus.subs == nil {
		t.Error("New() should initialize subs map")
	}
	if bus.closed {
		t.Error("New() should not be closed")
	}
}

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := New()
	ctx := context.Background()

	done := make(chan struct{}, 1)

	unsub := bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		done <- struct{}{}
	})
	defer unsub()

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, domain.TrackStartedPayload{
		TrackID: domain.GenerateTrackID("/music/song.mp3"),
		Title:   "Test Song",
	}))

	select {
	case <-time.After(time.Second):
		t.Error("Subscribe() timeout - event not received")
	case <-done:
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := New()
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(5)

	for range 5 {
		bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
			wg.Done()
		})
	}

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-time.After(time.Second):
		t.Error("Subscribe() timeout - handlers not called")
	case <-done:
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := New()
	ctx := context.Background()

	var count int64
	var wg sync.WaitGroup
	wg.Add(1)

	unsub := bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		atomic.AddInt64(&count, 1)
		wg.Done()
	})

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-time.After(time.Second):
		t.Fatal("first publish timeout")
	case <-done:
	}

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("before unsubscribe, count = %d, want 1", atomic.LoadInt64(&count))
	}

	unsub()

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	// Ensure any in-flight dispatch has drained.
	bus.Close()

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("after unsubscribe, count = %d, want 1", atomic.LoadInt64(&count))
	}
}

func TestEventBus_ClosedBus(t *testing.T) {
	bus := New()
	ctx := context.Background()
	bus.Close()

	var called bool
	bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		called = true
	})

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	if called {
		t.Error("handler on closed bus should not be called")
	}
}

func TestEventBus_DuplicateUnsubscribe(t *testing.T) {
	bus := New()
	ctx := context.Background()

	var count int64
	var wg sync.WaitGroup
	wg.Add(1)

	unsub := bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		atomic.AddInt64(&count, 1)
		wg.Done()
	})

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-time.After(time.Second):
		t.Fatal("first publish timeout")
	case <-done:
	}

	unsub()
	unsub()

	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	bus.Close()

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("duplicate unsubscribe count = %d, want 1", atomic.LoadInt64(&count))
	}
}

func TestEventBus_PublishManyHandlers(t *testing.T) {
	bus := New()
	ctx := context.Background()

	var count int64
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		newCount := atomic.AddInt64(&count, 1)
		if newCount == 50 {
			wg.Done()
		}
	})

	for range 50 {
		bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-time.After(time.Second):
		t.Error("PublishManyHandlers() timeout - events not processed")
	case <-done:
	}

	finalCount := atomic.LoadInt64(&count)
	if finalCount != 50 {
		t.Errorf("expected 50 handlers called, got %d", finalCount)
	}
}

func TestRingBuffer_PushPop(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		pushData []domain.EventType
	}{
		{
			name:     "single element",
			size:     4,
			pushData: []domain.EventType{domain.EventTrackStarted},
		},
		{
			name:     "multiple elements",
			size:     4,
			pushData: []domain.EventType{domain.EventTrackStarted, domain.EventTrackPaused, domain.EventTrackResumed},
		},
		{
			name:     "full buffer",
			size:     2,
			pushData: []domain.EventType{domain.EventTrackStarted, domain.EventTrackPaused},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := newRingBuffer(tt.size)
			for i, et := range tt.pushData {
				rb.Push(domain.NewEvent(et, nil))
				if i < tt.size {
					if rb.count != i+1 {
						t.Errorf("after push %d, count = %d, want %d", i, rb.count, i+1)
					}
				}
			}
		})
	}
}

func TestRingBuffer_Pop(t *testing.T) {
	rb := newRingBuffer(4)
	event, ok := rb.Pop()
	if ok {
		t.Error("Pop() on empty buffer should return ok=false")
	}

	rb.Push(domain.NewEvent(domain.EventTrackStarted, nil))
	event, ok = rb.Pop()
	if !ok {
		t.Error("Pop() on non-empty buffer should return ok=true")
	}
	if event.Type != domain.EventTrackStarted {
		t.Errorf("Pop() event.Type = %v, want EventTrackStarted", event.Type)
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := newRingBuffer(2)
	rb.Push(domain.NewEvent(domain.EventTrackStarted, nil))
	rb.Push(domain.NewEvent(domain.EventTrackPaused, nil))

	if rb.count != 2 {
		t.Errorf("after 2 pushes, count = %d, want 2", rb.count)
	}

	rb.Push(domain.NewEvent(domain.EventTrackResumed, nil))
	if rb.count != 2 {
		t.Errorf("after overflow push, count = %d, want 2", rb.count)
	}
}

func TestRingBuffer_Notify(t *testing.T) {
	rb := newRingBuffer(4)
	rb.Push(domain.NewEvent(domain.EventTrackStarted, nil))
	select {
	case <-rb.notify:
	default:
		t.Error("Push() should notify when buffer has events")
	}
}

func TestEventBus_PublishClosedBus(t *testing.T) {
	bus := New()
	bus.Close()
	bus.Publish(context.Background(), domain.NewEvent(domain.EventTrackStarted, nil))
}

func TestEventBus_CloseIdempotent(t *testing.T) {
	bus := New()
	bus.Close()
	bus.Close()
	bus.Publish(context.Background(), domain.NewEvent(domain.EventTrackStarted, nil))
}
