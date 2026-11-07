package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/domain"
)

type mockStream struct {
	id      string
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

func (m *mockStream) SetDeadline(t time.Time) error                     { return nil }
func (m *mockStream) SetReadDeadline(t time.Time) error                 { return nil }
func (m *mockStream) SetWriteDeadline(t time.Time) error                { return nil }
func (m *mockStream) Conn() network.Conn                                { return nil }
func (m *mockStream) ID() string                                        { return m.id }
func (m *mockStream) Protocol() protocol.ID                             { return "/test/1.0.0" }
func (m *mockStream) SetProtocol(id protocol.ID) error                  { return nil }
func (m *mockStream) Read(b []byte) (int, error)                        { return 0, io.EOF }
func (m *mockStream) Write(b []byte) (int, error)                       { return len(b), nil }
func (m *mockStream) ResetWithError(code network.StreamErrorCode) error { return nil }
func (m *mockStream) Scope() network.StreamScope                        { return nil }
func (m *mockStream) Stat() network.Stats                               { return network.Stats{} }

type mockHost struct {
	streams  map[peer.ID][]*mockStream
	mu       sync.RWMutex
	streamID int
	failFor  map[peer.ID]bool
}

type blockingMockHost struct {
	stream  network.Stream
	entered chan struct{}
	release chan struct{}
}

type blockingPeerMockHost struct {
	streams map[peer.ID]network.Stream
	entered chan peer.ID
	release chan struct{}
}

func (h *blockingPeerMockHost) NewStream(_ context.Context, pid peer.ID, _ ...protocol.ID) (network.Stream, error) {
	h.entered <- pid
	<-h.release
	return h.streams[pid], nil
}

func (h *blockingMockHost) NewStream(context.Context, peer.ID, ...protocol.ID) (network.Stream, error) {
	close(h.entered)
	<-h.release
	return h.stream, nil
}

func newMockHost() *mockHost {
	return &mockHost{
		streams: make(map[peer.ID][]*mockStream),
		failFor: make(map[peer.ID]bool),
	}
}

func (h *mockHost) NewStream(ctx context.Context, pid peer.ID, protos ...protocol.ID) (network.Stream, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.failFor[pid] {
		return nil, fmt.Errorf("mock: connection refused to %s", pid)
	}

	h.streamID++
	stream := &mockStream{id: fmt.Sprintf("stream-%d", h.streamID)}
	h.streams[pid] = append(h.streams[pid], stream)
	return stream, nil
}

func TestStreamPool_Acquire(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

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
	defer pool.Close()

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

	if _, err := pool.Acquire(context.Background(), pid); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if pool.GetPoolSize(pid) != 3 {
		t.Errorf("Pool size after acquire = %d, want 3", pool.GetPoolSize(pid))
	}
}

func TestStreamPool_ReleaseReturnsToPool(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

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
	defer pool.Close()

	pid := peer.ID("test-peer")
	stream, _ := pool.Acquire(context.Background(), pid)

	pool.Remove(pid, stream)

	if pool.GetPoolSize(pid) != 0 {
		t.Errorf("Pool size after remove = %d, want 0", pool.GetPoolSize(pid))
	}
}

func TestStreamPool_RemoveNonExistentStream(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	fakeStream := &mockStream{id: "fake"}

	// Should not panic
	pool.Remove(pid, fakeStream)
}

func TestStreamPool_RemoveDecrementsTotal(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	s1, _ := pool.Acquire(context.Background(), pid)
	s2, _ := pool.Acquire(context.Background(), pid)

	if stats := pool.Stats(); stats.TotalStreams != 2 {
		t.Fatalf("TotalStreams = %d, want 2", stats.TotalStreams)
	}

	pool.Remove(pid, s1)

	if stats := pool.Stats(); stats.TotalStreams != 1 {
		t.Fatalf("TotalStreams after remove = %d, want 1", stats.TotalStreams)
	}

	pool.Remove(pid, s2)

	if stats := pool.Stats(); stats.TotalStreams != 0 {
		t.Fatalf("TotalStreams after removing all = %d, want 0", stats.TotalStreams)
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

func TestStreamPool_Close_Idempotent(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	stream, _ := pool.Acquire(context.Background(), pid)
	pool.Release(pid, stream)

	// A failed node Start may close the pool; Stop later closes it again. The
	// second Close must be a no-op, not a double close of the done channel.
	pool.Close()
	pool.Close()

	if pool.Stats().TotalStreams != 0 {
		t.Errorf("TotalStreams after double close = %d, want 0", pool.Stats().TotalStreams)
	}
}

func TestStreamPool_CloseWithPendingWaiters(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")

	pid := peer.ID("test-peer")
	// Fill pool to max
	streams := make([]network.Stream, domain.MaxStreamsPerPeer)
	for i := range domain.MaxStreamsPerPeer {
		s, err := pool.Acquire(context.Background(), pid)
		if err != nil {
			t.Fatalf("Acquire %d error = %v", i, err)
		}
		streams[i] = s
	}

	// Launch a goroutine that will block on Acquire
	acquired := make(chan network.Stream, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, _ := pool.Acquire(ctx, pid)
		acquired <- s
	}()

	// Give it a moment to block
	time.Sleep(10 * time.Millisecond)

	// Close should unblock the waiter
	pool.Close()

	select {
	case s := <-acquired:
		if s != nil {
			t.Error("pending waiter should receive nil stream on Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending waiter was not unblocked by Close")
	}
}

func TestStreamPool_DrainPeer(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("drain-peer")
	s1, _ := pool.Acquire(context.Background(), pid)
	s2, _ := pool.Acquire(context.Background(), pid)
	pool.Release(pid, s1)
	pool.Release(pid, s2)

	pool.DrainPeer(pid)

	if pool.GetPoolSize(pid) != 0 {
		t.Errorf("pool size after drain = %d, want 0", pool.GetPoolSize(pid))
	}
	if pool.Stats().TotalStreams != 0 {
		t.Errorf("total streams after drain = %d, want 0", pool.Stats().TotalStreams)
	}
}

func TestStreamPool_DrainPeer_WithPendingWaiters(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	// Fill pool to max
	for range domain.MaxStreamsPerPeer {
		if _, err := pool.Acquire(context.Background(), pid); err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
	}

	// Launch blocked waiter
	acquired := make(chan network.Stream, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, _ := pool.Acquire(ctx, pid)
		acquired <- s
	}()

	time.Sleep(10 * time.Millisecond)

	pool.DrainPeer(pid)

	select {
	case s := <-acquired:
		if s != nil {
			t.Error("pending waiter should receive nil on drain")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending waiter was not unblocked by drain")
	}
}

func TestStreamPool_DrainPeer_OtherPeersUnaffected(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")

	if _, err := pool.Acquire(context.Background(), p1); err != nil {
		t.Fatalf("Acquire p1 error = %v", err)
	}
	if _, err := pool.Acquire(context.Background(), p2); err != nil {
		t.Fatalf("Acquire p2 error = %v", err)
	}

	pool.DrainPeer(p1)

	if pool.GetPoolSize(p1) != 0 {
		t.Error("p1 should be drained")
	}
	if pool.GetPoolSize(p2) != 1 {
		t.Errorf("p2 pool size = %d, want 1 (unaffected)", pool.GetPoolSize(p2))
	}
}

func TestStreamPool_ReleaseToPendingWaiter(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	// Fill pool to max
	streams := make([]network.Stream, domain.MaxStreamsPerPeer)
	for i := range domain.MaxStreamsPerPeer {
		s, err := pool.Acquire(context.Background(), pid)
		if err != nil {
			t.Fatalf("Acquire %d error = %v", i, err)
		}
		streams[i] = s
	}

	// Blocked waiter
	acquired := make(chan network.Stream, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, _ := pool.Acquire(ctx, pid)
		acquired <- s
	}()

	time.Sleep(10 * time.Millisecond)

	// Release should hand off to the waiter
	pool.Release(pid, streams[0])

	select {
	case s := <-acquired:
		if s == nil {
			t.Error("pending waiter should receive a non-nil stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending waiter was not unblocked by release")
	}
}

func TestStreamPool_AcquireContextCancelled(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	// Fill pool to max
	for range domain.MaxStreamsPerPeer {
		pool.Acquire(context.Background(), pid)
	}

	// Try to acquire with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pool.Acquire(ctx, pid)
	if err == nil {
		t.Fatal("Acquire() should fail with canceled context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestStreamPool_AcquireAlreadyCancelledContext(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	// Fill pool to max
	for range domain.MaxStreamsPerPeer {
		pool.Acquire(context.Background(), pid)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := pool.Acquire(ctx, pid)
	if err == nil {
		t.Fatal("Acquire() should fail with already-canceled context")
	}
}

func TestStreamPool_AcquireHostError(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("fail-peer")
	host.failFor[pid] = true

	_, err := pool.Acquire(context.Background(), pid)
	if err == nil {
		t.Fatal("Acquire() should fail when host.NewStream fails")
	}
}

func TestStreamPool_ConcurrentAcquire(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

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
		t.Logf("Active streams = %d (expected up to %d due to pool limit)", active, domain.MaxStreamsPerPeer)
	}
}

func TestStreamPool_ConcurrentAcquireRespectsPerPeerLimit(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	const acquireCount = 12
	start := make(chan struct{})
	results := make(chan network.Stream, acquireCount)
	var wg sync.WaitGroup
	for range acquireCount {
		wg.Go(func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			stream, _ := pool.Acquire(ctx, pid)
			results <- stream
		})
	}
	close(start)
	wg.Wait()
	close(results)

	var acquired int
	for stream := range results {
		if stream != nil {
			acquired++
			pool.Release(pid, stream)
		}
	}
	if got := pool.GetPoolSize(pid); got > domain.MaxStreamsPerPeer {
		t.Fatalf("per-peer pool size = %d, exceeds limit %d (acquired %d)", got, domain.MaxStreamsPerPeer, acquired)
	}
}

func TestStreamPool_ConcurrentAcquireRelease(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")
	var wg sync.WaitGroup

	for range 50 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s, err := pool.Acquire(ctx, pid)
			if err != nil {
				return
			}
			time.Sleep(time.Millisecond)
			pool.Release(pid, s)
		})
	}
	wg.Wait()

	// Pool should be consistent after concurrent operations
	stats := pool.Stats()
	if stats.TotalStreams < 0 {
		t.Errorf("TotalStreams = %d, should not be negative", stats.TotalStreams)
	}
}

func TestStreamPool_ConcurrentMultiplePeers(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	var wg sync.WaitGroup
	for i := range 10 {
		pid := peer.ID(fmt.Sprintf("peer-%d", i))
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s, err := pool.Acquire(ctx, pid)
			if err != nil {
				return
			}
			time.Sleep(time.Millisecond)
			pool.Release(pid, s)
		})
	}
	wg.Wait()
}

func TestStreamPool_Stats(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	if stats := pool.Stats(); stats.TotalStreams != 0 {
		t.Errorf("Initial TotalStreams = %d, want 0", stats.TotalStreams)
	}

	pid1 := peer.ID("peer-1")
	pid2 := peer.ID("peer-2")

	s1, _ := pool.Acquire(context.Background(), pid1)
	pool.Acquire(context.Background(), pid1)
	pool.Acquire(context.Background(), pid2)

	pool.Release(pid1, s1)

	stats := pool.Stats()
	if stats.PeerCount != 2 {
		t.Errorf("Stats().PeerCount = %d, want 2", stats.PeerCount)
	}
}

func TestStreamPool_StatsAfterRemoveAll(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("p")
	s1, _ := pool.Acquire(context.Background(), pid)
	s2, _ := pool.Acquire(context.Background(), pid)

	pool.Remove(pid, s1)
	pool.Remove(pid, s2)

	stats := pool.Stats()
	if stats.TotalStreams != 0 {
		t.Errorf("TotalStreams = %d, want 0", stats.TotalStreams)
	}
	if stats.PeerCount != 0 {
		t.Errorf("PeerCount = %d, want 0", stats.PeerCount)
	}
}

func TestStreamPool_GetPoolSize(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

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
	defer pool.Close()

	pool.Acquire(context.Background(), peer.ID("peer-1"))
	pool.Acquire(context.Background(), peer.ID("peer-2"))
	pool.Acquire(context.Background(), peer.ID("peer-3"))

	if count := pool.TotalPeers(); count != 3 {
		t.Errorf("TotalPeers = %d, want 3", count)
	}
}

func TestStreamPool_TotalPeers_AfterDrain(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pool.Acquire(context.Background(), peer.ID("peer-1"))
	pool.Acquire(context.Background(), peer.ID("peer-2"))

	pool.DrainPeer(peer.ID("peer-1"))

	if count := pool.TotalPeers(); count != 1 {
		t.Errorf("TotalPeers after drain = %d, want 1", count)
	}
}

func TestStreamPool_PendingCount(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	pid := peer.ID("test-peer")

	if count := pool.PendingCount(pid); count != 0 {
		t.Errorf("Initial pending count = %d, want 0", count)
	}
}

func TestStreamPool_MaxStreamsPerPeer(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

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

func TestStreamPool_Errors(t *testing.T) {
	err := errors.New("connection failed")
	if err.Error() != ErrConnectionFailed.Error() {
		t.Error("ErrConnectionFailed message should match")
	}
}

func TestStreamPool_AcquireAfterClose(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	pool.Close()

	pid := peer.ID("test-peer")
	s, err := pool.Acquire(context.Background(), pid)
	if s != nil {
		t.Fatal("Acquire() returned a stream after Close")
	}
	if !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Acquire() error = %v, want ErrPoolClosed", err)
	}
}

func TestStreamPool_CloseDuringStreamCreationDiscardsStream(t *testing.T) {
	stream := &mockStream{id: "in-flight"}
	host := &blockingMockHost{
		stream:  stream,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	pool := NewStreamPool(host, "/test/1.0.0")

	result := make(chan error, 1)
	go func() {
		got, err := pool.Acquire(context.Background(), peer.ID("test-peer"))
		if got != nil {
			result <- errors.New("Acquire returned stream after concurrent Close")
			return
		}
		result <- err
	}()

	<-host.entered
	pool.Close()
	close(host.release)

	if err := <-result; !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Acquire() error = %v, want ErrPoolClosed", err)
	}
	stream.closeMu.Lock()
	closed := stream.closed
	stream.closeMu.Unlock()
	if !closed {
		t.Fatal("stream created concurrently with Close was not closed")
	}
	if got := pool.Stats().TotalStreams; got != 0 {
		t.Fatalf("TotalStreams after Close = %d, want 0", got)
	}
}

func TestStreamPool_DrainPeerDuringStreamCreationDiscardsStream(t *testing.T) {
	pid := peer.ID("draining-peer")
	stream := &mockStream{id: "in-flight"}
	host := &blockingPeerMockHost{
		streams: map[peer.ID]network.Stream{pid: stream},
		entered: make(chan peer.ID, 1),
		release: make(chan struct{}),
	}
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	result := make(chan error, 1)
	go func() {
		got, err := pool.Acquire(context.Background(), pid)
		if got != nil {
			result <- errors.New("Acquire returned stream after peer was drained")
			return
		}
		result <- err
	}()

	if got := <-host.entered; got != pid {
		t.Fatalf("stream dial peer = %s, want %s", got, pid)
	}
	pool.DrainPeer(pid)
	close(host.release)

	if err := <-result; !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("Acquire() error = %v, want ErrConnectionFailed after peer drain", err)
	}
	stream.closeMu.Lock()
	closed := stream.closed
	stream.closeMu.Unlock()
	if !closed {
		t.Fatal("stream created after peer drain was not closed")
	}
	if got := pool.GetPoolSize(pid); got != 0 {
		t.Fatalf("drained peer pool size = %d, want 0", got)
	}
}
