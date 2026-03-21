package app

import (
	"time"

	"github.com/p-society/raag/internal/domain"
)

type ScanProgress struct {
	Scanned     int
	Total       int
	CurrentFile string
	Phase       ScanPhase
}

type ScanPhase string

const (
	ScanPhaseWalking ScanPhase = "walking"
	ScanPhaseParsing ScanPhase = "parsing"
)

type BufferStatusPayload struct {
	PeerID      domain.PeerID
	FillPercent float64
	Capacity    int64
	Used        int64
}

type BufferLowPayload struct {
	PeerID    domain.PeerID
	Threshold float64
	Current   float64
}

type BufferReadyPayload struct {
	PeerID  domain.PeerID
	Ready   bool
	Latency time.Duration
}
