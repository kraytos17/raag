package p2p

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/domain"
)

type mockStream struct {
	closed  bool
	closeMu sync.Mutex
	readMu  sync.Mutex
	writeMu sync.Mutex
}

func (m *mockStream) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	if !m.closed {
		m.closed = true
	}
	return nil
}

func (m *mockStream) Reset() error {
	return nil
}

func (m *mockStream) CloseRead() error {
	m.readMu.Lock()
	defer m.readMu.Unlock()
	return nil
}

func (m *mockStream) CloseWrite() error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return nil
}

func (m *mockStream) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockStream) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockStream) SetWriteDeadline(t time.Time) error {
	return nil
}

func (m *mockStream) Conn() network.Conn {
	return nil
}

func (m *mockStream) ID() string {
	return "mock-stream"
}

func (m *mockStream) Protocol() protocol.ID {
	return "/test/1.0.0"
}

func (m *mockStream) SetProtocol(id protocol.ID) error {
	return nil
}

func (m *mockStream) Read(b []byte) (int, error) {
	return 0, nil
}

func (m *mockStream) Write(b []byte) (int, error) {
	return len(b), nil
}

func (m *mockStream) ResetWithError(code network.StreamErrorCode) error {
	return nil
}

func (m *mockStream) Scope() network.StreamScope {
	return nil
}

func (m *mockStream) Stat() network.Stats {
	return network.Stats{}
}

type mockHost struct {
	streams map[peer.ID][]*mockStream
	mu      sync.RWMutex
}

func newMockHost() *mockHost {
	return &mockHost{
		streams: make(map[peer.ID][]*mockStream),
	}
}

func (h *mockHost) NewStream(ctx context.Context, pid peer.ID, protos ...protocol.ID) (network.Stream, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	stream := &mockStream{}
	h.streams[pid] = append(h.streams[pid], stream)
	return stream, nil
}

func TestStreamPool_Acquire(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	stream, err := pool.Acquire(context.Background(), pid)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if stream == nil {
		t.Fatal("Acquire() returned nil stream")
	}

	pool.Release(pid, stream)

	if stats := pool.Stats(); stats.TotalStreams != 1 {
		t.Errorf("Stats().TotalStreams = %d, want 1", stats.TotalStreams)
	}
}

func TestStreamPool_Reuse(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	s1, _ := pool.Acquire(context.Background(), pid)
	s2, _ := pool.Acquire(context.Background(), pid)
	s3, _ := pool.Acquire(context.Background(), pid)

	if pool.GetPoolSize(pid) != 3 {
		t.Errorf("Pool size = %d, want 3", pool.GetPoolSize(pid))
	}

	pool.Release(pid, s1)
	pool.Release(pid, s2)
	pool.Release(pid, s3)

	s4, _ := pool.Acquire(context.Background(), pid)
	if pool.GetPoolSize(pid) != 3 {
		t.Errorf("Pool size after acquire = %d, want 3", pool.GetPoolSize(pid))
	}

	_ = s4
}

func TestStreamPool_ReleaseReturnsToPool(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	stream1, _ := pool.Acquire(context.Background(), pid)
	pool.Release(pid, stream1)

	stream2, _ := pool.Acquire(context.Background(), pid)

	if stream1 != stream2 {
		t.Error("Released stream should be reused")
	}
}

func TestStreamPool_Remove(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	stream, _ := pool.Acquire(context.Background(), pid)

	pool.Remove(pid, stream)

	if pool.GetPoolSize(pid) != 0 {
		t.Errorf("Pool size after remove = %d, want 0", pool.GetPoolSize(pid))
	}
}

func TestStreamPool_Close(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	stream, _ := pool.Acquire(context.Background(), pid)
	pool.Release(pid, stream)

	pool.Close()

	if pool.Stats().TotalStreams != 0 {
		t.Errorf("TotalStreams after close = %d, want 0", pool.Stats().TotalStreams)
	}
}

func TestStreamPool_ConcurrentAcquire(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	var wg sync.WaitGroup
	results := make([]network.Stream, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream, _ := pool.Acquire(ctx, pid)
			results[idx] = stream
		}(i)
	}
	wg.Wait()

	active := 0
	for _, s := range results {
		if s != nil {
			active++
		}
	}

	if active < 3 {
		t.Logf("Active streams = %d (expected up to 3 due to pool limit)", active)
	}
}

func TestStreamPool_Stats(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	if stats := pool.Stats(); stats.TotalStreams != 0 {
		t.Errorf("Initial TotalStreams = %d, want 0", stats.TotalStreams)
	}

	pid1 := peer.ID("peer-1")
	pid2 := peer.ID("peer-2")

	s1, _ := pool.Acquire(context.Background(), pid1)
	s2, _ := pool.Acquire(context.Background(), pid1)
	s3, _ := pool.Acquire(context.Background(), pid2)

	_ = s2
	_ = s3

	pool.Release(pid1, s1)

	stats := pool.Stats()
	if stats.PeerCount != 2 {
		t.Errorf("Stats().PeerCount = %d, want 2", stats.PeerCount)
	}
}

func TestStreamPool_GetPoolSize(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")

	if size := pool.GetPoolSize(pid); size != 0 {
		t.Errorf("Initial pool size = %d, want 0", size)
	}

	pool.Acquire(context.Background(), pid)
	pool.Acquire(context.Background(), pid)
	pool.Acquire(context.Background(), pid)

	if size := pool.GetPoolSize(pid); size != 3 {
		t.Errorf("Pool size = %d, want 3", size)
	}
}

func TestStreamPool_TotalPeers(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pool.Acquire(context.Background(), peer.ID("peer-1"))
	pool.Acquire(context.Background(), peer.ID("peer-2"))
	pool.Acquire(context.Background(), peer.ID("peer-3"))

	if count := pool.TotalPeers(); count != 3 {
		t.Errorf("TotalPeers = %d, want 3", count)
	}
}

func TestStreamPool_PendingCount(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")

	if count := pool.PendingCount(pid); count != 0 {
		t.Errorf("Initial pending count = %d, want 0", count)
	}
}

func TestStreamPool_Errors(t *testing.T) {
	err := errors.New("connection failed")
	if err.Error() != ErrConnectionFailed.Error() {
		t.Error("ErrConnectionFailed message should match")
	}
}

func TestStreamPool_MaxStreamsPerPeer(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")

	streams := make([]network.Stream, 10)
	for i := range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		streams[i], _ = pool.Acquire(ctx, pid)
		cancel()
	}

	active := 0
	for _, s := range streams {
		if s != nil {
			active++
		}
	}

	if active != domain.MaxStreamsPerPeer {
		t.Errorf("Active streams = %d, want %d (max per peer)", active, domain.MaxStreamsPerPeer)
	}
}
