package domain

import (
	"math"
	"testing"
	"time"
)

func TestScorePeer(t *testing.T) {
	// No history: only the idle latency term contributes.
	if got := ScorePeer(0, 0, 0, 0); got != 0.5 {
		t.Errorf("ScorePeer() with no data = %v, want 0.5", got)
	}

	// Spot-check the canonical weights: 0.5 latency + 0.4 reliability + 0.1 bandwidth.
	got := ScorePeer(100*time.Millisecond, 1_000_000, 9, 1)
	want := 1.0/1.1*0.5 + 0.9*0.4 + 0.1*0.1
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("ScorePeer() = %v, want %v", got, want)
	}

	// Latency hurts, bandwidth and successes help.
	if slow, fast := ScorePeer(10*time.Second, 0, 1, 0), ScorePeer(time.Millisecond, 0, 1, 0); slow >= fast {
		t.Errorf("higher latency should reduce score: slow=%v, fast=%v", slow, fast)
	}
	if low, high := ScorePeer(0, 10_000_000, 1, 0), ScorePeer(0, 100_000_000, 1, 0); high <= low {
		t.Errorf("higher bandwidth should increase score: low=%v, high=%v", low, high)
	}
	if failures, successes := ScorePeer(0, 0, 0, 3), ScorePeer(0, 0, 3, 0); successes <= failures {
		t.Errorf("successes should outrank failures: %v vs %v", successes, failures)
	}
}

func TestPeerScore_ScoreMatchesScorePeer(t *testing.T) {
	s := NewPeerScore("peer-1")
	s.AvgLatency = 50 * time.Millisecond
	s.AvgBandwidth = 2_000_000
	s.SuccessCount = 3
	s.FailureCount = 1
	if got, want := s.Score(), ScorePeer(50*time.Millisecond, 2_000_000, 3, 1); got != want {
		t.Errorf("PeerScore.Score() = %v, want ScorePeer() = %v", got, want)
	}
}
