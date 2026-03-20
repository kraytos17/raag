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
	queue           Queue
	bus             domain.EventBus
	currentTrack    *domain.Track
	progress        float64
	prefetchStarted bool
	prefetchedData  map[domain.TrackID][]byte
	prefetchCancel  context.CancelFunc
}

func NewPrefetcher(resolver *Resolver, queue Queue, bus domain.EventBus) *Prefetcher {
	return &Prefetcher{
		resolver:       resolver,
		queue:          queue,
		bus:            bus,
		prefetchedData: make(map[domain.TrackID][]byte),
	}
}

func (p *Prefetcher) SetQueue(queue Queue) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = queue
}

func (p *Prefetcher) SetCurrentTrack(track *domain.Track) {
	p.mu.Lock()
	if p.prefetchCancel != nil {
		p.prefetchCancel()
		p.prefetchCancel = nil
	}
	if p.currentTrack != nil && p.currentTrack.ID != track.ID {
		delete(p.prefetchedData, p.currentTrack.ID)
	}

	p.currentTrack = track
	p.progress = 0
	p.prefetchStarted = false
	p.mu.Unlock()
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
	p.mu.Lock()
	p.prefetchCancel = cancel
	p.mu.Unlock()

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
	if n == 0 {
		if err != nil {
			slog.Warn("prefetch read failed", "track", nextTrackID, "error", err)
		}
		return
	}

	p.mu.Lock()
	p.prefetchedData[nextTrackID] = data[:n]
	p.mu.Unlock()
	slog.Debug("prefetch completed", "track", nextTrackID, "bytes", n)
}

func (p *Prefetcher) peekNext() domain.TrackID {
	if p.queue == nil {
		return ""
	}

	next := p.queue.Next()
	if next == nil {
		return ""
	}
	return next.ID
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

	if p.prefetchCancel != nil {
		p.prefetchCancel()
		p.prefetchCancel = nil
	}

	p.prefetchedData = make(map[domain.TrackID][]byte)
	p.currentTrack = nil
	p.progress = 0
	p.prefetchStarted = false
}
