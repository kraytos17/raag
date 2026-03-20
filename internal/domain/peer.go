package domain

import (
	"slices"
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
		SupportedCodecs:   []string{"mp3", "flac", "ogg", "wav", "aac"},
		SupportedBitrates: []int32{128, 192, 256, 320},
		CanTranscode:      false,
		UploadBandwidth:   0,
		ProtocolVersion:   "1.0.0",
	}
}

type PeerScore struct {
	PeerID       PeerID
	AvgLatency   time.Duration
	AvgBandwidth int64
	FailureCount int
	SuccessCount int
	LastSeen     time.Time
	TotalLatency int64
	SampleCount  int
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
	s.TotalLatency += latency.Nanoseconds()
	s.SampleCount++
	s.AvgLatency = time.Duration(s.TotalLatency / int64(s.SampleCount))
}

func (s *PeerScore) RecordBandwidth(bytes int64) {
	if s.AvgBandwidth == 0 {
		s.AvgBandwidth = bytes
	} else {
		s.AvgBandwidth = (s.AvgBandwidth + bytes) / 2
	}
}

func (s *PeerScore) RecordSuccess() {
	s.SuccessCount++
	s.LastSeen = time.Now()
}

func (s *PeerScore) RecordFailure() {
	s.FailureCount++
	s.LastSeen = time.Now()
}

func (s *PeerScore) SuccessRate() float64 {
	total := s.SuccessCount + s.FailureCount
	if total == 0 {
		return 1.0
	}
	return float64(s.SuccessCount) / float64(total)
}

func (s *PeerScore) IsHealthy() bool {
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
