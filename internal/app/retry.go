package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"
)

type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}
}

func (r *RetryPolicy) NextDelay(attempt int) time.Duration {
	delay := time.Duration(float64(r.InitialDelay) * math.Pow(r.Multiplier, float64(attempt)))
	if delay > r.MaxDelay {
		return r.MaxDelay
	}
	return delay
}

func DoWithRetry(ctx context.Context, policy *RetryPolicy, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !IsRetryable(lastErr) {
			return lastErr
		}

		delay := policy.NextDelay(attempt)
		slog.Warn("retrying after failure",
			"attempt", attempt+1,
			"max_attempts", policy.MaxAttempts,
			"delay", delay,
			"error", lastErr,
		)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

func IsRetryable(err error) bool {
	_, ok := errors.AsType[*RetryableError](err)
	return ok
}

func MakeRetryable(err error) error {
	return &RetryableError{Err: err}
}
