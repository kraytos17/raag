package p2p

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestRunTickerLoop_FiresOnSchedule verifies the ticker loop calls its work
// function once per interval and exits when the context is canceled, using
// synctest's virtual clock so no real 30s sleep is needed.
func TestRunTickerLoop_FiresOnSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var calls atomic.Int32
		go runTickerLoop(ctx, 30*time.Second, func() { calls.Add(1) })

		// Advance the virtual clock past two intervals.
		time.Sleep(61 * time.Second)
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("calls after 61s = %d, want 2", got)
		}

		// Cancellation must stop the loop; no further ticks fire.
		cancel()
		synctest.Wait()
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("calls after cancel = %d, want still 2", got)
		}
	})
}

// TestRunTickerLoop_ImmediateExit verifies a pre-canceled context exits the
// loop without firing any work.
func TestRunTickerLoop_ImmediateExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var calls atomic.Int32
		go runTickerLoop(ctx, time.Second, func() { calls.Add(1) })

		synctest.Wait()
		if got := calls.Load(); got != 0 {
			t.Fatalf("calls = %d, want 0 for pre-canceled context", got)
		}
	})
}
