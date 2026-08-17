// Package backoff provides reusable retry delay policies and a stop-aware wait.
package backoff

import "time"

// Linear returns the delay for the nth attempt (1-based): attempt * step.
func Linear(attempt int, step time.Duration) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return time.Duration(attempt) * step
}

// Exponential is a capped exponential backoff policy.
type Exponential struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

// NewExponential returns a policy starting at base and doubling until max.
func NewExponential(base, max time.Duration) *Exponential {
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	if max < base {
		max = base
	}
	return &Exponential{base: base, max: max, current: base}
}

// Next returns the current delay and advances the policy.
func (b *Exponential) Next() time.Duration {
	d := b.current
	if d < b.max {
		b.current = min(d*2, b.max)
	}
	return d
}

// Reset restarts the policy at its base delay.
func (b *Exponential) Reset() {
	b.current = b.base
}

// Wait sleeps for d, returning true if stop fires first.
// A nil stop channel waits the full duration.
func Wait(stop <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		return false
	}
	if stop == nil {
		time.Sleep(d)
		return false
	}

	select {
	case <-stop:
		return true
	case <-time.After(d):
		return false
	}
}
