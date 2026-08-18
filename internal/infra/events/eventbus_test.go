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
	defer bus.Close()

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
	defer bus.Close()
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

	var count atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	unsub := bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		count.Add(1)
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

	if count.Load() != 1 {
		t.Errorf("before unsubscribe, count = %d, want 1", count.Load())
	}

	unsub()
	bus.Publish(ctx, domain.NewEvent(domain.EventTrackStarted, nil))
	bus.Close()
	if count.Load() != 1 {
		t.Errorf("after unsubscribe, count = %d, want 1", count.Load())
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

	var count atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)

	unsub := bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		count.Add(1)
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

	if count.Load() != 1 {
		t.Errorf("duplicate unsubscribe count = %d, want 1", count.Load())
	}
}

func TestEventBus_PublishManyHandlers(t *testing.T) {
	bus := New()
	defer bus.Close()
	ctx := context.Background()

	var count atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(domain.EventTrackStarted, func(e domain.Event) {
		newCount := count.Add(1)
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

	finalCount := count.Load()
	if finalCount != 50 {
		t.Errorf("expected 50 handlers called, got %d", finalCount)
	}
}

func TestSubscription_BackpressureDropOldest(t *testing.T) {
	sub := &subscription{
		id:   1,
		ch:   make(chan domain.Event, 2),
		stop: make(chan struct{}),
	}

	a := domain.NewEvent(domain.EventTrackStarted, nil)
	b := domain.NewEvent(domain.EventTrackPaused, nil)
	c := domain.NewEvent(domain.EventTrackResumed, nil)

	sub.push(a)
	sub.push(b)
	// Buffer is now full. push must drop the oldest (a) and deliver c.
	sub.push(c)
	got := make([]domain.EventType, 0, 2)
	for range 2 {
		select {
		case e := <-sub.ch:
			got = append(got, e.Type)
		case <-time.After(time.Second):
			t.Fatal("timed out draining subscription channel")
		}
	}

	want := []domain.EventType{domain.EventTrackPaused, domain.EventTrackResumed}
	if len(got) != len(want) {
		t.Fatalf("drained %d events, want %d", len(got), len(want))
	}
	for i, et := range want {
		if got[i] != et {
			t.Errorf("drained event %d = %v, want %v", i, got[i], et)
		}
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
