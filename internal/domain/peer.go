package domain

import (
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerID = peer.ID

type PeerInfo struct {
	ID             PeerID
	Addrs          []string
	LastSeen       time.Time
	LibrarySummary *LibraryManifest
	Capabilities   *PeerCapabilities
	Score          *PeerScore
}

func NewPeerInfo(id PeerID, addrs []string) *PeerInfo {
	return &PeerInfo{
		ID:       id,
		Addrs:    addrs,
		LastSeen: time.Now(),
	}
}

func (p *PeerInfo) UpdateLastSeen() {
	p.LastSeen = time.Now()
}

type LibraryManifest struct {
	PeerID        PeerID
	TrackIDs      []TrackID
	TotalDuration uint64
	TotalSize     uint64
	LastUpdated   time.Time
}

func NewLibraryManifest(peerID PeerID) *LibraryManifest {
	return &LibraryManifest{
		PeerID:      peerID,
		TrackIDs:    []TrackID{},
		LastUpdated: time.Now(),
	}
}

func (m *LibraryManifest) TrackCount() int {
	return len(m.TrackIDs)
}

func (m *LibraryManifest) HasTrack(trackID TrackID) bool {
	return slices.Contains(m.TrackIDs, trackID)
}

type PeerCapabilities struct {
	SupportedCodecs   []string
	SupportedBitrates []int32
	CanTranscode      bool
	UploadBandwidth   int64
	ProtocolVersion   string
}

func NewPeerCapabilities() *PeerCapabilities {
	return &PeerCapabilities{
		SupportedCodecs:   PlayableCodecs,
		SupportedBitrates: SupportedBitrates,
		CanTranscode:      false,
		UploadBandwidth:   0,
		ProtocolVersion:   "1.0.0",
	}
}

type PeerScore struct {
	mu           sync.Mutex
	PeerID       PeerID
	AvgLatency   time.Duration
	AvgBandwidth int64
	FailureCount int
	SuccessCount int
	LastSeen     time.Time
}

func NewPeerScore(peerID PeerID) *PeerScore {
	return &PeerScore{
		PeerID:       peerID,
		LastSeen:     time.Now(),
		AvgLatency:   0,
		AvgBandwidth: 0,
	}
}

func (s *PeerScore) Score() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	latencyScore := 1.0 / (1.0 + s.AvgLatency.Seconds())
	totalAttempts := s.SuccessCount + s.FailureCount
	if totalAttempts == 0 {
		totalAttempts = 1
	}

	reliabilityScore := float64(s.SuccessCount) / float64(totalAttempts)
	bandwidthScore := float64(s.AvgBandwidth) / 1e6 * 0.1
	return latencyScore*0.5 + reliabilityScore*0.5 + bandwidthScore
}

func (s *PeerScore) RecordLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	const latencyAlpha = 0.2
	if s.AvgLatency == 0 {
		s.AvgLatency = latency
	} else {
		s.AvgLatency = time.Duration(float64(s.AvgLatency)*(1-latencyAlpha) + float64(latency)*latencyAlpha)
	}
}

func (s *PeerScore) RecordBandwidth(bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	const bandwidthAlpha = 0.2
	if s.AvgBandwidth == 0 {
		s.AvgBandwidth = bytes
	} else {
		s.AvgBandwidth = int64(float64(s.AvgBandwidth)*(1-bandwidthAlpha) + float64(bytes)*bandwidthAlpha)
	}
}

func (s *PeerScore) RecordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SuccessCount++
	s.LastSeen = time.Now()
}

func (s *PeerScore) RecordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.FailureCount++
	s.LastSeen = time.Now()
}

func (s *PeerScore) SuccessRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := s.SuccessCount + s.FailureCount
	if total == 0 {
		return 1.0
	}
	return float64(s.SuccessCount) / float64(total)
}

func (s *PeerScore) IsHealthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SuccessRate() > 0.5 && s.AvgLatency < 5*time.Second
}

type FileStat struct {
	Path  string
	Mtime int64
	Size  int64
	Hash  string
}

func (s *FileStat) Changed(other *FileStat) bool {
	return s.Mtime != other.Mtime || s.Size != other.Size
}
