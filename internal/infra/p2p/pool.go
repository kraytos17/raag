package p2p

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/domain"
)

var (
	ErrConnectionFailed = errors.New("connection failed")
	ErrPoolClosed       = errors.New("stream pool is closed")
)

type StreamOpener interface {
	NewStream(ctx context.Context, pid peer.ID, protos ...protocol.ID) (network.Stream, error)
}

type StreamPool struct {
	host           StreamOpener
	protocolID     protocol.ID
	mu             sync.Mutex
	pools          map[peer.ID][]*PooledStream
	pending        map[peer.ID][]chan *PooledStream
	creating       map[peer.ID]int
	peerGeneration map[peer.ID]uint64
	creatingTotal  int
	totalStreams   int
	done           chan struct{}
	closeOnce      sync.Once
	closed         bool
}

type PooledStream struct {
	stream   network.Stream
	peerID   peer.ID
	lastUsed time.Time
	refCnt   int
}

func NewStreamPool(h StreamOpener, protoID protocol.ID) *StreamPool {
	sp := &StreamPool{
		host:           h,
		protocolID:     protoID,
		pools:          make(map[peer.ID][]*PooledStream),
		pending:        make(map[peer.ID][]chan *PooledStream),
		creating:       make(map[peer.ID]int),
		peerGeneration: make(map[peer.ID]uint64),
		done:           make(chan struct{}),
	}

	go sp.reaper()
	return sp
}

func (p *StreamPool) Acquire(ctx context.Context, pid peer.ID) (network.Stream, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}

	pool := p.pools[pid]
	now := time.Now()
	for i, s := range slices.Backward(pool) {
		if s.refCnt > 0 {
			continue
		}
		if now.Sub(s.lastUsed) > domain.StreamIdleTimeout {
			if err := s.stream.Reset(); err != nil {
				slog.Debug("idle stream reset error", "peer", pid, "err", err)
			}

			p.removeStreamLocked(pid, i)
			continue
		}

		s.refCnt++
		p.mu.Unlock()
		return s.stream, nil
	}
	if len(pool)+p.creating[pid] < domain.MaxStreamsPerPeer &&
		p.totalStreams+p.creatingTotal < domain.MaxPoolSize {
		p.creating[pid]++
		p.creatingTotal++
		generation := p.peerGeneration[pid]
		p.mu.Unlock()
		return p.createStream(ctx, pid, generation)
	}

	ch := make(chan *PooledStream, 1)
	p.pending[pid] = append(p.pending[pid], ch)
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		p.mu.Lock()
		select {
		case s := <-ch:
			p.mu.Unlock()
			if s != nil {
				p.Release(pid, s.stream)
			}
		default:
			p.removePendingLocked(pid, ch)
			p.mu.Unlock()
		}
		return nil, ctx.Err()
	case s := <-ch:
		if s != nil {
			return s.stream, nil
		}

		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return nil, ErrPoolClosed
		}
		return nil, ErrConnectionFailed
	}
}

func (p *StreamPool) createStream(ctx context.Context, pid peer.ID, generation uint64) (network.Stream, error) {
	p.mu.Lock()
	if p.closed {
		p.releaseCreationLocked(pid)
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	if p.peerGeneration[pid] != generation {
		p.releaseCreationLocked(pid)
		p.mu.Unlock()
		return nil, ErrConnectionFailed
	}

	p.mu.Unlock()
	stream, err := p.host.NewStream(ctx, pid, p.protocolID)
	if err != nil {
		p.mu.Lock()
		p.releaseCreationLocked(pid)
		p.mu.Unlock()
		return nil, err
	}
	ps := &PooledStream{
		stream:   stream,
		peerID:   pid,
		lastUsed: time.Now(),
		refCnt:   1,
	}

	p.mu.Lock()
	p.releaseCreationLocked(pid)
	closed := p.closed
	stale := p.peerGeneration[pid] != generation
	if closed || stale {
		if err := stream.Close(); err != nil {
			slog.Debug("stream close error for rejected stream", "peer", pid, "err", err)
		}

		p.mu.Unlock()
		if closed {
			return nil, ErrPoolClosed
		}
		return nil, ErrConnectionFailed
	}
	if p.totalStreams >= domain.MaxPoolSize {
		stream.Close()
		p.mu.Unlock()
		return nil, ErrConnectionFailed
	}

	p.pools[pid] = append(p.pools[pid], ps)
	p.totalStreams++
	p.mu.Unlock()
	return stream, nil
}

// releaseCreationLocked releases a per-peer slot reserved for an in-flight
// NewStream dial. The caller must hold p.mu.
func (p *StreamPool) releaseCreationLocked(pid peer.ID) {
	p.creatingTotal--
	if p.creating[pid] <= 1 {
		delete(p.creating, pid)
		return
	}
	p.creating[pid]--
}

func (p *StreamPool) Release(pid peer.ID, stream network.Stream) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pool := p.pools[pid]
	for _, ps := range pool {
		if ps.stream == stream {
			ps.lastUsed = time.Now()
			if chans := p.pending[pid]; len(chans) > 0 {
				ch := chans[0]
				p.pending[pid] = chans[1:]
				ch <- ps
				return
			}

			ps.refCnt--
			return
		}
	}
}

func (p *StreamPool) Remove(pid peer.ID, stream network.Stream) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pool := p.pools[pid]
	for i, ps := range pool {
		if ps.stream == stream {
			if err := ps.stream.Reset(); err != nil {
				slog.Debug("stream reset error on remove", "peer", pid, "err", err)
			}
			p.removeStreamLocked(pid, i)
			return
		}
	}
}

func (p *StreamPool) removeStreamLocked(pid peer.ID, idx int) {
	pool := p.pools[pid]
	ps := pool[idx]

	ps.stream.Close()
	p.pools[pid] = append(pool[:idx], pool[idx+1:]...)
	p.totalStreams--
	if len(p.pools[pid]) == 0 {
		delete(p.pools, pid)
	}
}

func (p *StreamPool) removePendingLocked(pid peer.ID, ch chan *PooledStream) {
	pending := p.pending[pid]
	for i, c := range pending {
		if c == ch {
			p.pending[pid] = append(pending[:i], pending[i+1:]...)
			return
		}
	}
}

func (p *StreamPool) DrainPeer(pid peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peerGeneration[pid]++

	pool := p.pools[pid]
	for _, ps := range pool {
		if err := ps.stream.Reset(); err != nil {
			slog.Debug("stream reset error on drain", "peer", pid, "err", err)
		}
	}

	delete(p.pools, pid)
	p.totalStreams -= len(pool)
	for _, ch := range p.pending[pid] {
		ch <- nil
	}
	delete(p.pending, pid)
}

// Close shuts the pool down, closing all pooled streams and unblocking any
// pending waiters. It is idempotent: a second call is a no-op (a failed node
// Start may close the pool, and Stop later closes it again).
func (p *StreamPool) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.done)

		for pid, pool := range p.pools {
			for _, ps := range pool {
				if err := ps.stream.Close(); err != nil {
					slog.Debug("stream close error on pool close", "peer", pid, "err", err)
				}
			}
			delete(p.pools, pid)
		}

		p.totalStreams = 0
		for pid, chans := range p.pending {
			for _, ch := range chans {
				ch <- nil
			}
			delete(p.pending, pid)
		}
		p.mu.Unlock()
	})
}

func (p *StreamPool) reaper() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for pid, pool := range p.pools {
				for i, ps := range slices.Backward(pool) {
					if ps.refCnt == 0 && now.Sub(ps.lastUsed) > domain.StreamIdleTimeout {
						if err := ps.stream.Reset(); err != nil {
							slog.Debug("reaper stream reset error", "peer", pid, "err", err)
						}
						p.removeStreamLocked(pid, i)
					}
				}
			}
			p.mu.Unlock()
		case <-p.done:
			return
		}
	}
}

func (p *StreamPool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolStats{
		TotalStreams: p.totalStreams,
		PeerCount:    len(p.pools),
	}
}

type PoolStats struct {
	TotalStreams int
	PeerCount    int
}

func (p *StreamPool) GetPoolSize(pid peer.ID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pools[pid])
}

func (p *StreamPool) TotalPeers() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pools)
}

func (p *StreamPool) PendingCount(pid peer.ID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending[pid])
}
