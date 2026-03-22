package app

import (
	"testing"
	"time"

	"github.com/p-society/raag/internal/domain"
)

func TestCircuitBreaker_New(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 5, 30*time.Second)
	if cb.peerID != peerID {
		t.Errorf("NewCircuitBreaker() peerID = %v, want %v", cb.peerID, peerID)
	}
	if cb.state != CBStateClosed {
		t.Errorf("NewCircuitBreaker() initial state = %v, want CBStateClosed", cb.state)
	}
	if cb.threshold != 5 {
		t.Errorf("NewCircuitBreaker() threshold = %d, want 5", cb.threshold)
	}
}

func TestCircuitBreaker_States(t *testing.T) {
	tests := []struct {
		name  string
		state CBState
	}{
		{"closed", CBStateClosed},
		{"open", CBStateOpen},
		{"half_open", CBStateHalfOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerID := domain.PeerID("test-peer")
			cb := NewCircuitBreaker(peerID, 5, 30*time.Second)
			cb.state = tt.state
			if cb.GetState() != tt.state {
				t.Errorf("GetState() = %v, want %v", cb.GetState(), tt.state)
			}
		})
	}
}

func TestCircuitBreaker_Allow(t *testing.T) {
	tests := []struct {
		name        string
		state       CBState
		cooldown    time.Duration
		lastFailAgo time.Duration
		wantAllowed bool
	}{
		{"closed allows", CBStateClosed, 0, 0, true},
		{"half_open allows", CBStateHalfOpen, 0, 0, true},
		{"open blocks during cooldown", CBStateOpen, time.Hour, time.Minute, false},
		{"open allows after cooldown", CBStateOpen, time.Millisecond, time.Hour, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerID := domain.PeerID("test-peer")
			cb := NewCircuitBreaker(peerID, 5, tt.cooldown)
			cb.state = tt.state
			cb.lastFailTime = time.Now().Add(-tt.lastFailAgo)

			if got := cb.Allow(); got != tt.wantAllowed {
				t.Errorf("Allow() = %v, want %v", got, tt.wantAllowed)
			}
		})
	}
}

func TestCircuitBreaker_Threshold(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 3, time.Minute)

	for i := range 2 {
		cb.RecordFailure()
		if cb.GetState() != CBStateClosed {
			t.Errorf("after %d failures, state = %v, want closed", i+1, cb.GetState())
		}
	}

	cb.RecordFailure()
	if cb.GetState() != CBStateOpen {
		t.Errorf("after 3 failures, state = %v, want open", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != CBStateOpen {
		t.Errorf("after threshold failures, state = %v, want open", cb.GetState())
	}

	time.Sleep(time.Millisecond)
	if !cb.Allow() {
		t.Error("Allow() should return true after cooldown")
	}
	if cb.GetState() != CBStateHalfOpen {
		t.Errorf("after Allow() during cooldown, state = %v, want half_open", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Millisecond)

	cb.state = CBStateHalfOpen
	cb.RecordSuccess()
	if cb.GetState() != CBStateHalfOpen {
		t.Errorf("after 1 success in half_open, state = %v, want half_open", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != CBStateHalfOpen {
		t.Errorf("after 2 successes in half_open, state = %v, want half_open", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != CBStateClosed {
		t.Errorf("after 3 successes in half_open, state = %v, want closed", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)

	cb.state = CBStateHalfOpen
	cb.RecordFailure()
	if cb.GetState() != CBStateOpen {
		t.Errorf("after failure in half_open, state = %v, want open", cb.GetState())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)

	cb.state = CBStateOpen
	cb.failCount = 10
	cb.successCount = 5
	cb.halfOpenSuccesses = 2

	cb.Reset()
	if cb.GetState() != CBStateClosed {
		t.Errorf("after Reset(), state = %v, want closed", cb.GetState())
	}
	if cb.failCount != 0 {
		t.Errorf("after Reset(), failCount = %d, want 0", cb.failCount)
	}
	if cb.successCount != 0 {
		t.Errorf("after Reset(), successCount = %d, want 0", cb.successCount)
	}
	if cb.halfOpenSuccesses != 0 {
		t.Errorf("after Reset(), halfOpenSuccesses = %d, want 0", cb.halfOpenSuccesses)
	}
}

func TestCircuitBreaker_SuccessDecrementFailure(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 5, time.Minute)

	cb.failCount = 3
	cb.RecordSuccess()
	if cb.failCount != 2 {
		t.Errorf("after RecordSuccess(), failCount = %d, want 2", cb.failCount)
	}
}

func TestCircuitBreaker_Constants(t *testing.T) {
	tests := []struct {
		name  string
		state CBState
		str   string
	}{
		{"closed", CBStateClosed, "closed"},
		{"open", CBStateOpen, "open"},
		{"half_open", CBStateHalfOpen, "half_open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.str {
				t.Errorf("state constant = %q, want %q", tt.state, tt.str)
			}
		})
	}
}

func TestCBRegistry_New(t *testing.T) {
	registry := NewCBRegistry(5, time.Minute)
	if registry.threshold != 5 {
		t.Errorf("NewCBRegistry() threshold = %d, want 5", registry.threshold)
	}
	if registry.cooldown != time.Minute {
		t.Errorf("NewCBRegistry() cooldown = %v, want 1m", registry.cooldown)
	}
}

func TestCBRegistry_Get(t *testing.T) {
	registry := NewCBRegistry(5, time.Minute)
	peerID := domain.PeerID("test-peer")

	cb1 := registry.Get(peerID)
	cb2 := registry.Get(peerID)
	if cb1 != cb2 {
		t.Error("Get() should return same circuit breaker for same peer")
	}
}

func TestCBRegistry_GetNew(t *testing.T) {
	registry := NewCBRegistry(5, time.Minute)
	peerID := domain.PeerID("test-peer")

	cb := registry.Get(peerID)
	if cb.GetState() != CBStateClosed {
		t.Errorf("Get() new breaker state = %v, want closed", cb.GetState())
	}
}

func TestCBRegistry_Remove(t *testing.T) {
	registry := NewCBRegistry(5, time.Minute)
	peerID := domain.PeerID("test-peer")

	cb1 := registry.Get(peerID)
	registry.Remove(peerID)
	cb2 := registry.Get(peerID)
	if cb1 == cb2 {
		t.Error("Remove() should create new breaker after removal")
	}
}

func TestCBRegistry_ResetAll(t *testing.T) {
	registry := NewCBRegistry(5, time.Minute)
	peer1 := domain.PeerID("peer1")
	peer2 := domain.PeerID("peer2")

	cb1 := registry.Get(peer1)
	cb2 := registry.Get(peer2)
	cb1.state = CBStateOpen
	cb1.failCount = 10

	registry.ResetAll()
	if cb1.GetState() != CBStateClosed {
		t.Errorf("after ResetAll(), cb1 state = %v, want closed", cb1.GetState())
	}
	if cb2.GetState() != CBStateClosed {
		t.Errorf("after ResetAll(), cb2 state = %v, want closed", cb2.GetState())
	}
}
