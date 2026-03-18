package app

import (
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type CBState string

const (
	CBStateClosed   CBState = "closed"
	CBStateOpen     CBState = "open"
	CBStateHalfOpen CBState = "half_open"
)

type CircuitBreaker struct {
	mu                sync.Mutex
	peerID            domain.PeerID
	state             CBState
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
		state:     CBStateClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBStateClosed:
		return true

	case CBStateOpen:
		if time.Since(cb.lastFailTime) > cb.cooldown {
			cb.state = CBStateHalfOpen
			cb.halfOpenSuccesses = 0
			return true
		}
		return false

	case CBStateHalfOpen:
		return true
	}

	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBStateClosed:
		cb.successCount++
		if cb.failCount > 0 {
			cb.failCount--
		}

	case CBStateHalfOpen:
		cb.halfOpenSuccesses++
		cb.successCount++
		if cb.halfOpenSuccesses >= 3 {
			cb.state = CBStateClosed
			cb.failCount = 0
		}

	case CBStateOpen:
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failCount++
	cb.lastFailTime = time.Now()

	switch cb.state {
	case CBStateClosed:
		if cb.failCount >= cb.threshold {
			cb.state = CBStateOpen
		}

	case CBStateHalfOpen:
		cb.state = CBStateOpen

	case CBStateOpen:
	}
}

func (cb *CircuitBreaker) GetState() CBState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CBStateClosed
	cb.failCount = 0
	cb.successCount = 0
	cb.halfOpenSuccesses = 0
}

type CircuitBreakerRegistry struct {
	mu        sync.RWMutex
	breakers  map[domain.PeerID]*CircuitBreaker
	threshold int
	cooldown  time.Duration
}

func NewCircuitBreakerRegistry(threshold int, cooldown time.Duration) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers:  make(map[domain.PeerID]*CircuitBreaker),
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (r *CircuitBreakerRegistry) Get(peerID domain.PeerID) *CircuitBreaker {
	r.mu.RLock()
	cb, exists := r.breakers[peerID]
	r.mu.RUnlock()

	if exists {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, exists = r.breakers[peerID]; exists {
		return cb
	}

	cb = NewCircuitBreaker(peerID, r.threshold, r.cooldown)
	r.breakers[peerID] = cb
	return cb
}

func (r *CircuitBreakerRegistry) Remove(peerID domain.PeerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, peerID)
}

func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}
