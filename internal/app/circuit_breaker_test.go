package app

import (
	"sync"
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
	if cb.state != domain.CBStateClosed {
		t.Errorf("NewCircuitBreaker() initial state = %v, want domain.CBStateClosed", cb.state)
	}
	if cb.threshold != 5 {
		t.Errorf("NewCircuitBreaker() threshold = %d, want 5", cb.threshold)
	}
	if cb.cooldown != 30*time.Second {
		t.Errorf("NewCircuitBreaker() cooldown = %v, want 30s", cb.cooldown)
	}
	if cb.failCount != 0 {
		t.Errorf("NewCircuitBreaker() failCount = %d, want 0", cb.failCount)
	}
	if cb.successCount != 0 {
		t.Errorf("NewCircuitBreaker() successCount = %d, want 0", cb.successCount)
	}
	if cb.halfOpenSuccesses != 0 {
		t.Errorf("NewCircuitBreaker() halfOpenSuccesses = %d, want 0", cb.halfOpenSuccesses)
	}
	if cb.halfOpenProbes != 0 {
		t.Errorf("NewCircuitBreaker() halfOpenProbes = %d, want 0", cb.halfOpenProbes)
	}
}

func TestCircuitBreaker_States(t *testing.T) {
	tests := []struct {
		name  string
		state domain.CBState
	}{
		{"closed", domain.CBStateClosed},
		{"open", domain.CBStateOpen},
		{"half_open", domain.CBStateHalfOpen},
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
		state       domain.CBState
		cooldown    time.Duration
		lastFailAgo time.Duration
		wantAllowed bool
	}{
		{"closed allows", domain.CBStateClosed, 0, 0, true},
		{"half_open allows", domain.CBStateHalfOpen, 0, 0, true},
		{"open blocks during cooldown", domain.CBStateOpen, time.Hour, time.Minute, false},
		{"open allows after cooldown", domain.CBStateOpen, time.Millisecond, time.Hour, true},
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

func TestCircuitBreaker_AllowTransitionsOpenToHalfOpen(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Fatalf("state = %v, want open", cb.GetState())
	}

	time.Sleep(2 * time.Millisecond)

	if !cb.Allow() {
		t.Error("Allow() should return true after cooldown expired")
	}
	if cb.GetState() != domain.CBStateHalfOpen {
		t.Errorf("state after Allow() = %v, want half_open", cb.GetState())
	}

	cb.mu.Lock()
	if cb.halfOpenProbes != 0 {
		t.Errorf("halfOpenProbes = %d, want 0", cb.halfOpenProbes)
	}
	if cb.halfOpenSuccesses != 0 {
		t.Errorf("halfOpenSuccesses = %d, want 0", cb.halfOpenSuccesses)
	}
	cb.mu.Unlock()
}

func TestCircuitBreaker_Threshold(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 3, time.Minute)

	for i := range 2 {
		cb.RecordFailure()
		if cb.GetState() != domain.CBStateClosed {
			t.Errorf("after %d failures, state = %v, want closed", i+1, cb.GetState())
		}
	}

	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("after 3 failures, state = %v, want open", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("after threshold failures, state = %v, want open", cb.GetState())
	}

	time.Sleep(time.Millisecond)
	if !cb.Allow() {
		t.Error("Allow() should return true after cooldown")
	}
	if cb.GetState() != domain.CBStateHalfOpen {
		t.Errorf("after Allow() during cooldown, state = %v, want half_open", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Millisecond)

	cb.state = domain.CBStateHalfOpen
	cb.RecordSuccess()
	if cb.GetState() != domain.CBStateHalfOpen {
		t.Errorf("after 1 success in half_open, state = %v, want half_open", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != domain.CBStateHalfOpen {
		t.Errorf("after 2 successes in half_open, state = %v, want half_open", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != domain.CBStateClosed {
		t.Errorf("after 3 successes in half_open, state = %v, want closed", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)

	cb.state = domain.CBStateHalfOpen
	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("after failure in half_open, state = %v, want open", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenProbes_Limited(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)
	cb.state = domain.CBStateHalfOpen

	// Allow should permit up to CBHalfOpenSuccessesRequired probes
	for i := range domain.CBHalfOpenSuccessesRequired {
		if !cb.Allow() {
			t.Errorf("Allow() probe %d should return true", i)
		}
	}

	// Next probe should be blocked
	if cb.Allow() {
		t.Error("Allow() should return false after max probes in half_open")
	}
}

func TestCircuitBreaker_HalfOpenFailure_DecrementsProbes(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)
	cb.state = domain.CBStateHalfOpen
	cb.halfOpenProbes = 2

	cb.RecordFailure()

	cb.mu.Lock()
	// halfOpenProbes decremented by RecordFailure in half_open
	if cb.halfOpenProbes != 1 {
		t.Errorf("halfOpenProbes after failure = %d, want 1", cb.halfOpenProbes)
	}
	cb.mu.Unlock()

	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("state = %v, want open", cb.GetState())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)

	cb.state = domain.CBStateOpen
	cb.failCount = 10
	cb.successCount = 5
	cb.halfOpenSuccesses = 2
	cb.halfOpenProbes = 3

	cb.Reset()
	if cb.GetState() != domain.CBStateClosed {
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
	if cb.halfOpenProbes != 0 {
		t.Errorf("after Reset(), halfOpenProbes = %d, want 0", cb.halfOpenProbes)
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

func TestCircuitBreaker_SuccessAtZeroFailCount(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 5, time.Minute)

	cb.failCount = 0
	cb.RecordSuccess()
	if cb.failCount != 0 {
		t.Errorf("failCount should not go below 0, got %d", cb.failCount)
	}
}

func TestCircuitBreaker_SuccessInOpenState(t *testing.T) {
	// RecordSuccess in open state does nothing (no state transition)
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, time.Minute)
	cb.state = domain.CBStateOpen

	cb.RecordSuccess()
	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("RecordSuccess in open state should not change state, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_Constants(t *testing.T) {
	tests := []struct {
		name  string
		state domain.CBState
		str   string
	}{
		{"closed", domain.CBStateClosed, "closed"},
		{"open", domain.CBStateOpen, "open"},
		{"half_open", domain.CBStateHalfOpen, "half_open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.str {
				t.Errorf("state constant = %q, want %q", tt.state, tt.str)
			}
		})
	}
}

// ---------- Full lifecycle ----------

func TestCircuitBreaker_FullLifecycle(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 3, 10*time.Millisecond)

	// Closed: allow requests
	if !cb.Allow() {
		t.Fatal("closed: should allow")
	}

	// Record failures to trip
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Fatal("should be open after 3 failures")
	}

	// Open: block requests
	if cb.Allow() {
		t.Fatal("open: should block")
	}

	// Wait for cooldown
	time.Sleep(15 * time.Millisecond)

	// Should transition to half_open
	if !cb.Allow() {
		t.Fatal("should allow after cooldown")
	}
	if cb.GetState() != domain.CBStateHalfOpen {
		t.Fatal("should be half_open")
	}

	// Record successes to close
	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.GetState() != domain.CBStateClosed {
		t.Fatal("should be closed after 3 successes in half_open")
	}

	// Should be fully operational again
	if !cb.Allow() {
		t.Fatal("closed again: should allow")
	}
}

func TestCircuitBreaker_HalfOpenFailureReOpens(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 2, 10*time.Millisecond)

	// Trip to open
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for cooldown
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // transitions to half_open

	// Failure in half_open should reopen
	cb.RecordFailure()
	if cb.GetState() != domain.CBStateOpen {
		t.Errorf("state = %v, want open", cb.GetState())
	}

	// Should block again
	if cb.Allow() {
		t.Error("should block after re-opening")
	}
}

// ---------- Concurrent safety ----------

func TestCircuitBreaker_ConcurrentAllowRecordSuccess(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 100, time.Minute)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			cb.Allow()
		}()
		go func() {
			defer wg.Done()
			cb.RecordSuccess()
		}()
		go func() {
			defer wg.Done()
			cb.GetState()
		}()
	}
	wg.Wait()
}

func TestCircuitBreaker_ConcurrentAllowRecordFailure(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 100, time.Minute)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			cb.Allow()
		}()
		go func() {
			defer wg.Done()
			cb.RecordFailure()
		}()
		go func() {
			defer wg.Done()
			cb.GetState()
		}()
	}
	wg.Wait()
}

func TestCircuitBreaker_ConcurrentReset(t *testing.T) {
	peerID := domain.PeerID("test-peer")
	cb := NewCircuitBreaker(peerID, 5, time.Minute)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cb.RecordFailure()
		}()
		go func() {
			defer wg.Done()
			cb.Reset()
		}()
	}
	wg.Wait()
}
