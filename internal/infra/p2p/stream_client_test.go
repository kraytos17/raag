package p2p

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/p-society/raag/proto/gen"
)

func TestChunkedReader_CloseSetsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	r.mu.Lock()
	if !r.closed {
		t.Error("closed should be true after Close()")
	}
	r.mu.Unlock()
}

func TestChunkedReader_ReadAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	r.Close()

	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("Read after Close: n=%d, err=%v; want 0, io.EOF", n, err)
	}
}

func TestChunkedReader_DoubleClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	if err := r.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	// Second close should not panic (context cancel is idempotent)
	if err := r.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestChunkedReader_EOFWhenTotalSizeReached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		totalSize:   10,
		offset:      10, // already at end
		ctx:         ctx,
		fetchCancel: cancel,
	}

	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("Read at EOF: n=%d, err=%v; want 0, io.EOF", n, err)
	}
}

func TestChunkedReader_ReadFromCurrentBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		current:     []byte("hello world"),
		pos:         0,
		totalSize:   11,
		offset:      11,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("Read() = %d, %q; want 5, %q", n, buf, "hello")
	}

	// Read the rest
	buf2 := make([]byte, 20)
	n, err = r.Read(buf2)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 6 || string(buf2[:n]) != " world" {
		t.Fatalf("Read() = %d, %q; want 6, %q", n, buf2[:n], " world")
	}
}

func TestChunkedReader_ReadSmallBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		current:     []byte("abcdef"),
		pos:         0,
		totalSize:   6,
		offset:      6,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	// Read one byte at a time
	expected := "abcdef"
	for i := range len(expected) {
		buf := make([]byte, 1)
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("Read() at %d error = %v", i, err)
		}
		if n != 1 || buf[0] != expected[i] {
			t.Fatalf("Read() at %d = %d, %q; want 1, %q", i, n, buf[0], expected[i])
		}
	}

	// Next read should be EOF
	buf := make([]byte, 1)
	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("Read at end: n=%d, err=%v; want 0, io.EOF", n, err)
	}
}

func TestChunkedReader_ReadLargerThanCurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		current:     []byte("short"),
		pos:         0,
		totalSize:   5,
		offset:      5,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	// Buffer larger than data
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 || string(buf[:n]) != "short" {
		t.Fatalf("Read() = %d, %q; want 5, %q", n, buf[:n], "short")
	}
}

func TestChunkedReader_StartPrefetch_NothingLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		totalSize:   100,
		offset:      100, // at end
		ctx:         ctx,
		fetchCancel: cancel,
	}

	r.startPrefetch()
	if r.ahead != nil {
		t.Fatal("startPrefetch should not create ahead channel when offset >= totalSize")
	}
}

func TestChunkedReader_StartPrefetch_PartialRemaining(t *testing.T) {
	// This tests the internal logic without actually creating a client
	chunkSize := int64(1024)
	totalSize := int64(100)
	offset := int64(50)

	length := chunkSize
	if totalSize > 0 && offset+length > totalSize {
		length = totalSize - offset
	}

	if length != 50 {
		t.Fatalf("adjusted length = %d, want 50", length)
	}
}

func TestPrefetchResult_Success(t *testing.T) {
	resp := &pb.ChunkResponse{
		Data:      []byte("prefetched"),
		TotalSize: 100,
		LastChunk: false,
	}

	result := prefetchResult{resp: resp, err: nil}
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if string(result.resp.Data) != "prefetched" {
		t.Fatalf("data = %q, want %q", result.resp.Data, "prefetched")
	}
}

func TestPrefetchResult_Error(t *testing.T) {
	result := prefetchResult{resp: nil, err: context.Canceled}
	if result.err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", result.err)
	}
}

func TestPrefetchChannel_BufferedDelivery(t *testing.T) {
	ch := make(chan prefetchResult, 1)

	resp := &pb.ChunkResponse{
		Data:      []byte("prefetched-data"),
		TotalSize: 100,
		LastChunk: false,
	}

	go func() {
		ch <- prefetchResult{resp: resp, err: nil}
	}()

	result := <-ch
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if string(result.resp.Data) != "prefetched-data" {
		t.Fatalf("data = %q, want %q", result.resp.Data, "prefetched-data")
	}
}

func TestChunkedReader_ConcurrentCloseAndRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	// Set up an ahead channel that will block
	ch := make(chan prefetchResult, 1)
	r.ahead = ch

	var wg sync.WaitGroup
	wg.Add(2)

	// Reader goroutine
	go func() {
		defer wg.Done()
		buf := make([]byte, 100)
		r.Read(buf) // will block on r.ahead, then see closed
	}()

	// Closer goroutine
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		r.Close()
		// Send something on channel to unblock the reader
		ch <- prefetchResult{resp: &pb.ChunkResponse{Data: []byte("x")}, err: nil}
	}()

	wg.Wait()
}

func TestChunkedReader_ReadFromAheadChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
		pos:         0,
		current:     nil, // empty, will trigger fetch
	}

	// Pre-populate the ahead channel as if startPrefetch had run
	ch := make(chan prefetchResult, 1)
	ch <- prefetchResult{
		resp: &pb.ChunkResponse{
			Data:      []byte("from-prefetch"),
			TotalSize: 13,
			LastChunk: true,
		},
		err: nil,
	}
	r.ahead = ch

	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 13 || string(buf[:n]) != "from-prefetch" {
		t.Fatalf("Read() = %d, %q; want 13, %q", n, buf[:n], "from-prefetch")
	}
}

func TestChunkedReader_ReadFromAheadChannel_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
		pos:         0,
		current:     nil,
	}

	ch := make(chan prefetchResult, 1)
	ch <- prefetchResult{
		resp: nil,
		err:  io.ErrUnexpectedEOF,
	}
	r.ahead = ch

	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if n != 0 {
		t.Fatalf("Read() n = %d, want 0", n)
	}
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("Read() err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestChunkedReader_ReadMultipleChunksFromAhead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")
	client := NewStreamClient(pid, pool, scorer)

	r := &chunkedReader{
		client:      client,
		trackID:     testTrackID,
		chunkSize:   5,
		ctx:         ctx,
		fetchCancel: cancel,
		pos:         0,
		current:     nil,
	}

	// First read: from ahead channel (simulate first prefetch completing)
	ch1 := make(chan prefetchResult, 1)
	ch1 <- prefetchResult{
		resp: &pb.ChunkResponse{
			Data:      []byte("AAAAA"),
			TotalSize: 10,
			Offset:    0,
			LastChunk: false,
		},
		err: nil,
	}
	r.ahead = ch1

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("first Read() error = %v", err)
	}
	if n != 5 || string(buf) != "AAAAA" {
		t.Fatalf("first Read() = %d, %q; want 5, AAAAA", n, buf)
	}

	// After first Read, startPrefetch was called with the real client.
	// That goroutine will fail (mockHost doesn't speak protobuf) but won't
	// panic. Override ahead with our second chunk before Read sees it.
	r.mu.Lock()
	r.ahead = nil // discard the real prefetch
	ch2 := make(chan prefetchResult, 1)
	ch2 <- prefetchResult{
		resp: &pb.ChunkResponse{
			Data:      []byte("BBBBB"),
			TotalSize: 10,
			Offset:    5,
			LastChunk: true,
		},
		err: nil,
	}
	r.ahead = ch2
	r.mu.Unlock()

	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if n != 5 || string(buf) != "BBBBB" {
		t.Fatalf("second Read() = %d, %q; want 5, BBBBB", n, buf)
	}
}

func TestNewStreamClient(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	client := NewStreamClient(pid, pool, scorer)
	if client == nil {
		t.Fatal("NewStreamClient returned nil")
	}
	if client.peerID != pid {
		t.Errorf("peerID = %q, want %q", client.peerID, pid)
	}
	if client.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", client.timeout)
	}
	if client.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", client.maxRetries)
	}
}

func TestNewStreamClient_GetTrack_ReturnschunkedReader(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	client := NewStreamClient(pid, pool, scorer)
	reader, err := client.GetTrack(context.Background(), track1ID, codecMP3, 320)
	if err != nil {
		t.Fatalf("GetTrack() error = %v", err)
	}
	if reader == nil {
		t.Fatal("GetTrack() returned nil reader")
	}

	cr, ok := reader.(*chunkedReader)
	if !ok {
		t.Fatal("GetTrack() reader is not *chunkedReader")
	}
	if cr.trackID != track1ID {
		t.Errorf("trackID = %q, want %q", cr.trackID, track1ID)
	}
	if cr.codec != codecMP3 {
		t.Errorf("codec = %q, want %q", cr.codec, codecMP3)
	}
	if cr.bitrate != 320 {
		t.Errorf("bitrate = %d, want 320", cr.bitrate)
	}
	if cr.fetchCancel == nil {
		t.Error("fetchCancel should not be nil")
	}
	reader.Close()
}

func TestNewStreamClient_GetTrack_CloseCancelsContext(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	client := NewStreamClient(pid, pool, scorer)
	reader, _ := client.GetTrack(context.Background(), track1ID, "", 0)

	cr := reader.(*chunkedReader)
	reader.Close()

	// The context should be canceled after Close
	select {
	case <-cr.ctx.Done():
		// expected
	default:
		t.Error("context should be canceled after Close")
	}
}

func TestChunkedReader_ConcurrentIndependentReaders(t *testing.T) {
	var wg sync.WaitGroup
	var completed atomic.Int32

	for range 10 {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			r := &chunkedReader{
				trackID:     testTrackID,
				chunkSize:   1024,
				current:     []byte("data for reader"),
				pos:         0,
				totalSize:   15,
				offset:      15,
				ctx:         ctx,
				fetchCancel: cancel,
			}

			buf := make([]byte, 100)
			_, err := r.Read(buf)
			if err != nil {
				return
			}
			r.Close()
			completed.Add(1)
		})
	}
	wg.Wait()

	if c := completed.Load(); c != 10 {
		t.Fatalf("completed = %d, want 10", c)
	}
}

func newSeekTestReader() *chunkedReader {
	ctx, cancel := context.WithCancel(context.Background())
	return &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		current:     []byte("0123456789abcdef"),
		pos:         4, // 4 bytes consumed: stream position is 4
		offset:      16,
		totalSize:   100,
		ctx:         ctx,
		fetchCancel: cancel,
	}
}

func TestChunkedReader_SeekStart(t *testing.T) {
	r := newSeekTestReader()
	defer r.Close()

	pos, err := r.Seek(50, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	if pos != 50 {
		t.Errorf("Seek() pos = %d, want 50", pos)
	}
	if r.offset != 50 {
		t.Errorf("offset = %d, want 50", r.offset)
	}
	if r.pos != 0 || r.current != nil {
		t.Errorf("buffered data not cleared: pos=%d current=%v", r.pos, r.current)
	}
}

func TestChunkedReader_SeekCurrent(t *testing.T) {
	r := newSeekTestReader()
	defer r.Close()

	// streamPos = offset - len(current) + pos = 16 - 16 + 4 = 4
	pos, err := r.Seek(10, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	if pos != 14 {
		t.Errorf("Seek() pos = %d, want 14 (streamPos 4 + 10)", pos)
	}
	if r.offset != 14 {
		t.Errorf("offset = %d, want 14", r.offset)
	}
}

func TestChunkedReader_SeekEnd(t *testing.T) {
	r := newSeekTestReader()
	defer r.Close()

	pos, err := r.Seek(-20, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	if pos != 80 {
		t.Errorf("Seek() pos = %d, want 80 (totalSize 100 - 20)", pos)
	}
	if r.offset != 80 {
		t.Errorf("offset = %d, want 80", r.offset)
	}
}

func TestChunkedReader_SeekEnd_UnknownTotal(t *testing.T) {
	r := newSeekTestReader()
	r.totalSize = 0
	defer r.Close()

	if _, err := r.Seek(0, io.SeekEnd); err == nil {
		t.Fatal("Seek(End) with unknown totalSize: expected error")
	}
}

func TestChunkedReader_Seek_Negative(t *testing.T) {
	r := newSeekTestReader()
	defer r.Close()

	if _, err := r.Seek(-5, io.SeekStart); err == nil {
		t.Fatal("Seek() negative position: expected error")
	}
}

func TestChunkedReader_Seek_InvalidWhence(t *testing.T) {
	r := newSeekTestReader()
	defer r.Close()

	if _, err := r.Seek(0, 99); err == nil {
		t.Fatal("Seek() invalid whence: expected error")
	}
}

func TestChunkedReader_Seek_AfterClose(t *testing.T) {
	r := newSeekTestReader()
	r.Close()

	if _, err := r.Seek(10, io.SeekStart); err != io.ErrClosedPipe {
		t.Errorf("Seek() after Close = %v, want io.ErrClosedPipe", err)
	}
}

func TestChunkedReader_Seek_InvalidatesPrefetch(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	client := NewStreamClient(peer.ID("test-peer"), pool, scorer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &chunkedReader{
		client:      client,
		trackID:     testTrackID,
		chunkSize:   1024,
		offset:      0,
		totalSize:   0,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	// A prefetch from an older generation must be discarded: Read refetches via
	// the client, which hits the mock host and errors (stream EOF), surfacing
	// the error instead of the stale bytes.
	stale := prefetchResult{
		resp: &pb.ChunkResponse{Data: []byte("stale"), TotalSize: 100},
		gen:  0,
	}

	ch := make(chan prefetchResult, 1)
	ch <- stale
	r.ahead = ch
	r.seekGen = 1

	buf := make([]byte, 16)
	if _, err := r.Read(buf); err == nil {
		t.Fatal("Read() with stale prefetch: expected refetch error, got data")
	}
	if r.current != nil {
		t.Error("current buffer should be empty after stale prefetch discard")
	}
	if r.ahead != nil {
		t.Error("ahead should be cleared after stale prefetch discard")
	}
}

func TestChunkedReader_Seek_CurrentGenPrefetchConsumed(t *testing.T) {
	host := newMockHost()
	pool := NewStreamPool(host, "/test/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	client := NewStreamClient(peer.ID("test-peer"), pool, scorer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &chunkedReader{
		client:      client,
		trackID:     testTrackID,
		chunkSize:   1024,
		offset:      0,
		totalSize:   0,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	r.seekGen = 5
	fresh := prefetchResult{
		resp: &pb.ChunkResponse{Data: []byte("fresh"), TotalSize: 100},
		gen:  5,
	}

	ch := make(chan prefetchResult, 1)
	ch <- fresh
	r.ahead = ch

	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(buf[:n]) != "fresh" {
		t.Errorf("Read() = %q, want %q", buf[:n], "fresh")
	}
}

func TestChunkedReader_SetReadDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		ctx:         ctx,
		fetchCancel: cancel,
	}
	if got := r.Deadline(); !got.IsZero() {
		t.Fatalf("Deadline() initially = %v, want zero", got)
	}

	deadline := time.Now().Add(time.Second)
	if err := r.SetReadDeadline(deadline); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if got := r.Deadline(); got != deadline {
		t.Errorf("Deadline() = %v, want %v", got, deadline)
	}
	if err := r.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("SetReadDeadline(zero) error = %v", err)
	}
	if got := r.Deadline(); !got.IsZero() {
		t.Errorf("Deadline() after clear = %v, want zero", got)
	}
}

func TestChunkedReader_Read_ExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		offset:      0,
		ctx:         ctx,
		fetchCancel: cancel,
	}

	r.deadline = time.Now().Add(-time.Second) // already expired
	buf := make([]byte, 16)
	_, err := r.Read(buf)
	if err == nil {
		t.Fatal("Read() with expired deadline: expected timeout error")
	}
	if _, ok := errors.AsType[timeoutError](err); !ok {
		t.Errorf("Read() error = %v, want timeoutError", err)
	}
}

func TestChunkedReader_Read_StalledPeer_RespectsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &chunkedReader{
		trackID:     testTrackID,
		chunkSize:   1024,
		offset:      0,
		ctx:         ctx,
		fetchCancel: cancel,
	}
	// A prefetch that never delivers: the reader must time out, not block
	// forever on <-r.ahead.
	r.ahead = make(chan prefetchResult)
	r.deadline = time.Now().Add(50 * time.Millisecond)

	start := time.Now()
	buf := make([]byte, 16)
	_, err := r.Read(buf)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Read() on stalled peer: expected timeout error")
	}
	if _, ok := errors.AsType[timeoutError](err); !ok {
		t.Errorf("Read() error = %v, want timeoutError", err)
	}
	if elapsed > time.Second {
		t.Errorf("Read() took %v, want to respect the 50ms deadline", elapsed)
	}
}
