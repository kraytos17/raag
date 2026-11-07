package p2p

import (
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
)

type PeerScorer struct {
	mu          sync.RWMutex
	failures    map[peer.ID]int
	successes   map[peer.ID]int
	failTotals  map[peer.ID]int
	bandwidths  map[peer.ID][]int64
	lastFailure map[peer.ID]time.Time
	banned      map[peer.ID]bool
	latencies   map[peer.ID][]time.Duration
	failLimit   int
	cooldown    time.Duration
}

func NewPeerScorer() *PeerScorer {
	return NewPeerScorerWithConfig(3, 5*time.Minute)
}

func NewPeerScorerWithConfig(failLimit int, cooldown time.Duration) *PeerScorer {
	return &PeerScorer{
		failures:    make(map[peer.ID]int),
		successes:   make(map[peer.ID]int),
		failTotals:  make(map[peer.ID]int),
		bandwidths:  make(map[peer.ID][]int64),
		lastFailure: make(map[peer.ID]time.Time),
		banned:      make(map[peer.ID]bool),
		latencies:   make(map[peer.ID][]time.Duration),
		failLimit:   failLimit,
		cooldown:    cooldown,
	}
}

func (s *PeerScorer) RecordLatency(pid peer.ID, rtt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.latencies[pid] = append(s.latencies[pid], rtt)
	if len(s.latencies[pid]) > 5 {
		s.latencies[pid] = s.latencies[pid][1:]
	}
}

func (s *PeerScorer) AvgLatency(pid peer.ID) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lats := s.latencies[pid]
	if len(lats) == 0 {
		return 0
	}

	var sum time.Duration
	for _, l := range lats {
		sum += l
	}
	return sum / time.Duration(len(lats))
}

func (s *PeerScorer) RecordBandwidth(pid peer.ID, bytesPerSecond int64) {
	if bytesPerSecond <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bandwidths[pid] = append(s.bandwidths[pid], bytesPerSecond)
	if len(s.bandwidths[pid]) > 5 {
		s.bandwidths[pid] = s.bandwidths[pid][1:]
	}
}

func (s *PeerScorer) AvgBandwidth(pid peer.ID) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bws := s.bandwidths[pid]
	if len(bws) == 0 {
		return 0
	}

	var sum int64
	for _, bw := range bws {
		sum += bw
	}
	return sum / int64(len(bws))
}

func (s *PeerScorer) RecordSuccess(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.successes[pid]++
	delete(s.failures, pid)
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
	slog.Debug("peer success recorded", "peer", pid)
}

func (s *PeerScorer) RecordFailure(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failTotals[pid]++
	s.failures[pid]++
	s.lastFailure[pid] = time.Now()
	if s.failures[pid] >= s.failLimit {
		s.banned[pid] = true
		slog.Warn("peer banned due to failures", "peer", pid, "failures", s.failures[pid])
		// Deliberately no gater escalation: the scorer's ban is cooldown-based
		// (IsBanned auto-clears after the cooldown and RecordSuccess clears it),
		// while a gater ban is permanent for the session and can't be undone by
		// success. Manual/admin bans go through the gater separately.
	} else {
		slog.Debug("peer failure recorded", "peer", pid, "failures", s.failures[pid])
	}
}

func (s *PeerScorer) Snapshot(pid peer.ID) (avgLatency time.Duration, avgBandwidth int64, successes int, failures int, banned bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lats := s.latencies[pid]
	if len(lats) > 0 {
		var sum time.Duration
		for _, l := range lats {
			sum += l
		}
		avgLatency = sum / time.Duration(len(lats))
	}

	successes = s.successes[pid]
	failures = s.failTotals[pid]
	bws := s.bandwidths[pid]
	if len(bws) > 0 {
		var sum int64
		for _, bw := range bws {
			sum += bw
		}
		avgBandwidth = sum / int64(len(bws))
	}
	if s.banned[pid] {
		if time.Since(s.lastFailure[pid]) > s.cooldown {
			delete(s.banned, pid)
			banned = false
		} else {
			banned = true
		}
	}
	return avgLatency, avgBandwidth, successes, failures, banned
}

func (s *PeerScorer) ClearLatency(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.latencies, pid)
}

func (s *PeerScorer) IsBanned(pid peer.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.banned[pid] {
		return false
	}
	if time.Since(s.lastFailure[pid]) > s.cooldown {
		delete(s.banned, pid)
		return false
	}
	return true
}

func (s *PeerScorer) Reset(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, pid)
	delete(s.successes, pid)
	delete(s.failTotals, pid)
	delete(s.bandwidths, pid)
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
	delete(s.latencies, pid)
}

func (s *PeerScorer) Score(pid peer.ID) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lats := s.latencies[pid]
	var totalLatency time.Duration
	for _, l := range lats {
		totalLatency += l
	}

	var avgLatency time.Duration
	if len(lats) > 0 {
		avgLatency = totalLatency / time.Duration(len(lats))
	}

	bws := s.bandwidths[pid]
	var totalBW int64
	for _, bw := range bws {
		totalBW += bw
	}

	var avgBandwidth int64
	if len(bws) > 0 {
		avgBandwidth = totalBW / int64(len(bws))
	}
	return domain.ScorePeer(avgLatency, avgBandwidth, s.successes[pid], s.failTotals[pid])
}
