package p2p

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
)

var ErrConnectionFailed = errors.New("connection failed")

const (
	MaxStreamsPerPeer = 3
	StreamIdleTimeout = 5 * time.Minute
	MaxPoolSize       = 100
)

type StreamOpener interface {
	NewStream(ctx context.Context, pid peer.ID, protos ...protocol.ID) (network.Stream, error)
}

type StreamPool struct {
	host         StreamOpener
	protocolID   protocol.ID
	mu           sync.Mutex
	pools        map[peer.ID][]*PooledStream
	pending      map[peer.ID][]chan *PooledStream
	totalStreams int
}

type PooledStream struct {
	stream   network.Stream
	peerID   peer.ID
	lastUsed time.Time
	refCnt   int
}

func NewStreamPool(h StreamOpener, protoID protocol.ID) *StreamPool {
	sp := &StreamPool{
		host:       h,
		protocolID: protoID,
		pools:      make(map[peer.ID][]*PooledStream),
		pending:    make(map[peer.ID][]chan *PooledStream),
	}
	go sp.reaper()
	return sp
}

func (p *StreamPool) Acquire(ctx context.Context, pid peer.ID) (network.Stream, error) {
	p.mu.Lock()
	pool := p.pools[pid]
	now := time.Now()
	for i := len(pool) - 1; i >= 0; i-- {
		s := pool[i]
		if s.refCnt > 0 {
			continue
		}
		if now.Sub(s.lastUsed) > StreamIdleTimeout {
			p.removeStreamLocked(pid, i)
			continue
		}

		s.refCnt++
		p.mu.Unlock()
		return s.stream, nil
	}
	if len(pool) < MaxStreamsPerPeer {
		p.mu.Unlock()
		return p.createStream(ctx, pid)
	}

	ch := make(chan *PooledStream, 1)
	p.pending[pid] = append(p.pending[pid], ch)
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		p.mu.Lock()
		p.removePendingLocked(pid, ch)
		p.mu.Unlock()
		return nil, ctx.Err()
	case s := <-ch:
		if s != nil {
			s.refCnt++
			return s.stream, nil
		}
		return nil, ErrConnectionFailed
	}
}

func (p *StreamPool) createStream(ctx context.Context, pid peer.ID) (network.Stream, error) {
	p.mu.Lock()
	if p.totalStreams >= MaxPoolSize {
		p.mu.Unlock()
		return nil, ErrConnectionFailed
	}

	p.mu.Unlock()
	stream, err := p.host.NewStream(ctx, pid, p.protocolID)
	if err != nil {
		return nil, err
	}
	ps := &PooledStream{
		stream:   stream,
		peerID:   pid,
		lastUsed: time.Now(),
		refCnt:   1,
	}

	p.mu.Lock()
	if p.totalStreams >= MaxPoolSize {
		_ = stream.Close()
		p.mu.Unlock()
		return nil, ErrConnectionFailed
	}

	p.pools[pid] = append(p.pools[pid], ps)
	p.totalStreams++
	p.mu.Unlock()
	return stream, nil
}

func (p *StreamPool) Release(pid peer.ID, stream network.Stream) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pool := p.pools[pid]
	for _, ps := range pool {
		if ps.stream == stream {
			ps.refCnt--
			ps.lastUsed = time.Now()
			if chans := p.pending[pid]; len(chans) > 0 {
				ch := chans[0]
				p.pending[pid] = chans[1:]
				ch <- ps
			}
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
			ps.stream.Reset()
			p.removeStreamLocked(pid, i)
			return
		}
	}
}

func (p *StreamPool) removeStreamLocked(pid peer.ID, idx int) {
	pool := p.pools[pid]
	ps := pool[idx]

	_ = ps.stream.Close()
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

func (p *StreamPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for pid, pool := range p.pools {
		for _, ps := range pool {
			ps.stream.Close()
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
}

func (p *StreamPool) reaper() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		now := time.Now()
		for pid, pool := range p.pools {
			for i := len(pool) - 1; i >= 0; i-- {
				ps := pool[i]
				if ps.refCnt == 0 && now.Sub(ps.lastUsed) > StreamIdleTimeout {
					p.removeStreamLocked(pid, i)
				}
			}
		}
		p.mu.Unlock()
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
