package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type Prefetcher struct {
	mu              sync.Mutex
	resolver        *Resolver
	bus             domain.EventBus
	currentTrack    *domain.Track
	progress        float64
	prefetchStarted bool
	prefetchedData  map[domain.TrackID][]byte
}

func NewPrefetcher(resolver *Resolver, bus domain.EventBus) *Prefetcher {
	return &Prefetcher{
		resolver:       resolver,
		bus:            bus,
		prefetchedData: make(map[domain.TrackID][]byte),
	}
}

func (p *Prefetcher) SetCurrentTrack(track *domain.Track) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentTrack != nil && p.currentTrack.ID != track.ID {
		delete(p.prefetchedData, p.currentTrack.ID)
	}

	p.currentTrack = track
	p.progress = 0
	p.prefetchStarted = false
}

func (p *Prefetcher) UpdateProgress(progress float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.progress = progress

	if progress > 0.8 && !p.prefetchStarted {
		p.prefetchStarted = true
		go p.prefetch()
	}
}

func (p *Prefetcher) prefetch() {
	p.mu.Lock()
	if p.currentTrack == nil {
		p.mu.Unlock()
		return
	}

	nextTrackID := p.peekNext()
	if nextTrackID == "" {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reader, err := p.resolver.Resolve(ctx, nextTrackID)
	if err != nil {
		slog.Warn("prefetch failed", "track", nextTrackID, "error", err)
		return
	}

	if closer, ok := reader.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	data := make([]byte, 256*1024)
	n, err := reader.Read(data)
	if err != nil && n == 0 {
		slog.Warn("prefetch read failed", "track", nextTrackID, "error", err)
		return
	}

	p.mu.Lock()
	p.prefetchedData[nextTrackID] = data[:n]
	p.mu.Unlock()

	slog.Debug("prefetch completed", "track", nextTrackID, "bytes", n)
}

func (p *Prefetcher) peekNext() domain.TrackID {
	return ""
}

func (p *Prefetcher) GetPrefetched(trackID domain.TrackID) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, ok := p.prefetchedData[trackID]
	return data, ok
}

func (p *Prefetcher) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.prefetchedData = make(map[domain.TrackID][]byte)
	p.currentTrack = nil
	p.progress = 0
	p.prefetchStarted = false
}
