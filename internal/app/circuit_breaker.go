package app

import (
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type cbState string

const (
	cbStateClosed   cbState = "closed"
	cbStateOpen     cbState = "open"
	cbStateHalfOpen cbState = "half_open"
)

type circuitBreaker struct {
	mu                sync.Mutex
	peerID            domain.PeerID
	state             cbState
	failCount         int
	successCount      int
	lastFailTime      time.Time
	threshold         int
	cooldown          time.Duration
	halfOpenSuccesses int
}

func newCircuitBreaker(peerID domain.PeerID, threshold int, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{
		peerID:    peerID,
		state:     cbStateClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbStateClosed:
		return true
	case cbStateOpen:
		if time.Since(cb.lastFailTime) > cb.cooldown {
			cb.state = cbStateHalfOpen
			cb.halfOpenSuccesses = 0
			return true
		}
		return false
	case cbStateHalfOpen:
		return true
	}
	return false
}

func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbStateClosed:
		cb.successCount++
		if cb.failCount > 0 {
			cb.failCount--
		}
	case cbStateHalfOpen:
		cb.halfOpenSuccesses++
		cb.successCount++
		if cb.halfOpenSuccesses >= 3 {
			cb.state = cbStateClosed
			cb.failCount = 0
		}
	}
}

func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failCount++
	cb.lastFailTime = time.Now()

	switch cb.state {
	case cbStateClosed:
		if cb.failCount >= cb.threshold {
			cb.state = cbStateOpen
		}
	case cbStateHalfOpen:
		cb.state = cbStateOpen
	}
}

func (cb *circuitBreaker) GetState() cbState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *circuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = cbStateClosed
	cb.failCount = 0
	cb.successCount = 0
	cb.halfOpenSuccesses = 0
}

type cbRegistry struct {
	mu        sync.RWMutex
	breakers  map[domain.PeerID]*circuitBreaker
	threshold int
	cooldown  time.Duration
}

func newCBRegistry(threshold int, cooldown time.Duration) *cbRegistry {
	return &cbRegistry{
		breakers:  make(map[domain.PeerID]*circuitBreaker),
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (r *cbRegistry) Get(peerID domain.PeerID) *circuitBreaker {
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

	cb = newCircuitBreaker(peerID, r.threshold, r.cooldown)
	r.breakers[peerID] = cb
	return cb
}

func (r *cbRegistry) Remove(peerID domain.PeerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, peerID)
}

func (r *cbRegistry) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}
