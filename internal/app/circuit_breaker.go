package app

import (
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type CircuitBreaker struct {
	mu                sync.Mutex
	peerID            domain.PeerID
	state             domain.CBState
	failCount         int
	successCount      int
	lastFailTime      time.Time
	threshold         int
	cooldown          time.Duration
	halfOpenSuccesses int
}

func NewCircuitBreaker(peerID domain.PeerID, threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		peerID:    peerID,
		state:     domain.CBStateClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case domain.CBStateClosed:
		return true
	case domain.CBStateOpen:
		if time.Since(cb.lastFailTime) > cb.cooldown {
			cb.state = domain.CBStateHalfOpen
			cb.halfOpenSuccesses = 0
			return true
		}
		return false
	case domain.CBStateHalfOpen:
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case domain.CBStateClosed:
		cb.successCount++
		if cb.failCount > 0 {
			cb.failCount--
		}
	case domain.CBStateHalfOpen:
		cb.halfOpenSuccesses++
		cb.successCount++
		if cb.halfOpenSuccesses >= 3 {
			cb.state = domain.CBStateClosed
			cb.failCount = 0
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failCount++
	cb.lastFailTime = time.Now()
	switch cb.state {
	case domain.CBStateClosed:
		if cb.failCount >= cb.threshold {
			cb.state = domain.CBStateOpen
		}
	case domain.CBStateHalfOpen:
		cb.state = domain.CBStateOpen
	}
}

func (cb *CircuitBreaker) GetState() domain.CBState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = domain.CBStateClosed
	cb.failCount = 0
	cb.successCount = 0
	cb.halfOpenSuccesses = 0
}

type CBRegistry struct {
	m         sync.Map
	threshold int
	cooldown  time.Duration
}

func NewCBRegistry(threshold int, cooldown time.Duration) *CBRegistry {
	return &CBRegistry{
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (r *CBRegistry) Get(peerID domain.PeerID) *CircuitBreaker {
	if val, ok := r.m.Load(peerID); ok {
		return val.(*CircuitBreaker)
	}

	val, _ := r.m.LoadOrStore(peerID, NewCircuitBreaker(peerID, r.threshold, r.cooldown))
	return val.(*CircuitBreaker)
}

func (r *CBRegistry) Remove(peerID domain.PeerID) {
	r.m.Delete(peerID)
}

func (r *CBRegistry) ResetAll() {
	r.m.Range(func(key, value any) bool {
		value.(*CircuitBreaker).Reset()
		return true
	})
}
