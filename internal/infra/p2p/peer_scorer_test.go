package p2p

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPeerScorer_RecordSuccess(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)

	if !scorer.IsBanned(pid) {
		t.Error("IsBanned() = false, want true after 3 failures")
	}

	scorer.RecordSuccess(pid)
	if scorer.IsBanned(pid) {
		t.Error("IsBanned() = true, want false after success")
	}
}

func TestPeerScorer_RecordFailure(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)

	scorer.mu.RLock()
	count := scorer.failures[pid]
	scorer.mu.RUnlock()
	if count != 2 {
		t.Errorf("failures[%s] = %d, want 2", pid, count)
	}
}

func TestPeerScorer_Cooldown(t *testing.T) {
	scorer := NewPeerScorerWithConfig(2, 100*time.Millisecond)
	pid := peer.ID("test-peer")
	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)
	if !scorer.IsBanned(pid) {
		t.Error("IsBanned() = false, want true after 2 failures")
	}

	time.Sleep(150 * time.Millisecond)
	if scorer.IsBanned(pid) {
		t.Error("IsBanned() = true, want false after cooldown")
	}
}

func TestPeerScorer_RecordFailure_NoGaterEscalation(t *testing.T) {
	// Failure-based bans must stay cooldown-bounded and never escalate to a
	// permanent gater ban, which RecordSuccess cannot undo.
	scorer := NewPeerScorerWithConfig(3, 5*time.Minute)
	gater := NewPeerGater()
	pid := peer.ID("fail-peer")

	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)
	if gater.IsBanned(pid) {
		t.Fatal("gater banned peer before failLimit reached")
	}

	scorer.RecordFailure(pid) // reaches failLimit -> scorer ban only
	if !scorer.IsBanned(pid) {
		t.Error("IsBanned() = false, want true after failLimit failures")
	}
	if gater.IsBanned(pid) {
		t.Error("gater.IsBanned() = true, want false (no permanent ban on failure)")
	}

	// A success must clear the scorer ban.
	scorer.RecordSuccess(pid)
	if scorer.IsBanned(pid) {
		t.Error("IsBanned() = true, want false after RecordSuccess")
	}
}

func TestPeerScorer_Score(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")
	score := scorer.Score(pid, 100*time.Millisecond, 1000000, 0.9)
	if score <= 0 {
		t.Errorf("Score() = %f, want > 0", score)
	}
}

func TestPeerScorer_ScoreComponents(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")
	// Zero latency, zero bandwidth, zero success
	s1 := scorer.Score(pid, 0, 0, 0)
	// High latency should reduce score
	s2 := scorer.Score(pid, 10*time.Second, 0, 0)
	if s2 >= s1 {
		t.Errorf("higher latency should reduce score: s1=%f, s2=%f", s1, s2)
	}

	// Higher bandwidth should increase score
	s3 := scorer.Score(pid, 0, 10_000_000, 0.9)
	s4 := scorer.Score(pid, 0, 100_000_000, 0.9)
	if s4 <= s3 {
		t.Errorf("higher bandwidth should increase score: s3=%f, s4=%f", s3, s4)
	}
}

func TestPeerScorer_RecordLatency(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")
	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Fatalf("AvgLatency() = %v, want 0 for unknown peer", lat)
	}

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordLatency(pid, 200*time.Millisecond)
	avg := scorer.AvgLatency(pid)
	if avg != 150*time.Millisecond {
		t.Fatalf("AvgLatency() = %v, want 150ms", avg)
	}
}

func TestPeerScorer_RecordLatency_SlidingWindow(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")
	// Record 7 latencies, window is 5
	for i := range 7 {
		scorer.RecordLatency(pid, time.Duration(i+1)*time.Millisecond)
	}

	scorer.mu.RLock()
	count := len(scorer.latencies[pid])
	scorer.mu.RUnlock()
	if count != 5 {
		t.Fatalf("latency window = %d, want 5", count)
	}

	// avg of 3,4,5,6,7 = 5
	avg := scorer.AvgLatency(pid)
	if avg != 5*time.Millisecond {
		t.Fatalf("AvgLatency() = %v, want 5ms", avg)
	}
}

func TestPeerScorer_ClearLatency(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.ClearLatency(pid)
	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Fatalf("AvgLatency() = %v after clear, want 0", lat)
	}
}

func TestPeerScorer_RecordBandwidth(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")
	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Fatalf("AvgBandwidth() = %d, want 0 for unknown peer", bw)
	}

	scorer.RecordBandwidth(pid, 1000)
	scorer.RecordBandwidth(pid, 2000)
	if bw := scorer.AvgBandwidth(pid); bw != 1500 {
		t.Fatalf("AvgBandwidth() = %d, want 1500", bw)
	}
}

func TestPeerScorer_RecordBandwidth_IgnoresNonPositive(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordBandwidth(pid, 0)
	scorer.RecordBandwidth(pid, -100)
	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Fatalf("AvgBandwidth() = %d, want 0 after non-positive recordings", bw)
	}
}

func TestPeerScorer_RecordBandwidth_SlidingWindow(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")
	for i := range 7 {
		scorer.RecordBandwidth(pid, int64((i+1)*1000))
	}

	scorer.mu.RLock()
	count := len(scorer.bandwidths[pid])
	scorer.mu.RUnlock()
	if count != 5 {
		t.Fatalf("bandwidth window = %d, want 5", count)
	}
}

func TestPeerScorer_Snapshot(t *testing.T) {
	scorer := NewPeerScorerWithConfig(5, time.Minute)
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordBandwidth(pid, 5000)
	scorer.RecordSuccess(pid)
	scorer.RecordSuccess(pid)
	scorer.RecordFailure(pid)

	avgLat, avgBw, successes, failures, banned := scorer.Snapshot(pid)
	if avgLat != 100*time.Millisecond {
		t.Errorf("Snapshot avgLat = %v, want 100ms", avgLat)
	}
	if avgBw != 5000 {
		t.Errorf("Snapshot avgBw = %d, want 5000", avgBw)
	}
	if successes != 2 {
		t.Errorf("Snapshot successes = %d, want 2", successes)
	}
	if failures != 1 {
		t.Errorf("Snapshot failures = %d, want 1", failures)
	}
	if banned {
		t.Error("Snapshot banned = true, want false (under threshold)")
	}
}

func TestPeerScorer_Snapshot_BannedExpires(t *testing.T) {
	scorer := NewPeerScorerWithConfig(1, 50*time.Millisecond)
	pid := peer.ID("p")
	scorer.RecordFailure(pid)
	snap := func() bool {
		_, _, _, _, b := scorer.Snapshot(pid)
		return b
	}
	if !snap() {
		t.Error("should be banned immediately after failure")
	}

	time.Sleep(100 * time.Millisecond)
	banned := snap()
	if banned {
		t.Error("should not be banned after cooldown")
	}
}

func TestPeerScorer_Reset(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordBandwidth(pid, 5000)
	scorer.RecordSuccess(pid)
	scorer.RecordFailure(pid)
	scorer.Reset(pid)

	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Errorf("after Reset, AvgLatency = %v, want 0", lat)
	}
	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Errorf("after Reset, AvgBandwidth = %d, want 0", bw)
	}
	if scorer.IsBanned(pid) {
		t.Error("after Reset, IsBanned = true, want false")
	}
}

func TestPeerScorer_ConcurrentAccess(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			switch n % 6 {
			case 0:
				scorer.RecordLatency(pid, time.Duration(n)*time.Millisecond)
			case 1:
				scorer.RecordBandwidth(pid, int64(n*1000))
			case 2:
				scorer.RecordSuccess(pid)
			case 3:
				scorer.RecordFailure(pid)
			case 4:
				scorer.IsBanned(pid)
			case 5:
				scorer.Score(pid, time.Millisecond, 1000, 0.5)
			}
		}(i)
	}
	wg.Wait()
}

func TestNewPeerScorerWithConfig(t *testing.T) {
	scorer := NewPeerScorerWithConfig(10, 2*time.Minute)
	pid := peer.ID("p")
	// Need 10 failures to ban
	for range 9 {
		scorer.RecordFailure(pid)
	}
	if scorer.IsBanned(pid) {
		t.Error("should not be banned after 9 failures with limit 10")
	}

	scorer.RecordFailure(pid)
	if !scorer.IsBanned(pid) {
		t.Error("should be banned after 10 failures with limit 10")
	}
}
