package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type slowReader struct {
	data  []byte
	pos   int
	mu    sync.Mutex
	delay time.Duration
}

func (r *slowReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *slowReader) Close() error {
	return nil
}

func waitForData(ss *StreamingSource, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ss.BufferUsed() > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestStreamingSource_BasicRead(t *testing.T) {
	data := bytes.Repeat([]byte("test data "), 1000)
	source := &slowReader{data: data, delay: 10 * time.Millisecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	if !waitForData(ss, 500*time.Millisecond) {
		t.Fatal("timeout waiting for data")
	}

	buf := make([]byte, 1024)
	n, err := ss.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read error: %v", err)
	}
	if n == 0 {
		t.Fatal("Read returned 0 bytes")
	}
}

func TestStreamingSource_FillLevel(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 50*1024)
	source := &slowReader{data: data, delay: 10 * time.Millisecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	time.Sleep(100 * time.Millisecond)

	fill := ss.FillLevel()
	if fill <= 0 {
		t.Errorf("FillLevel = %f, want > 0", fill)
	}
	if fill > 1.0 {
		t.Errorf("FillLevel = %f, want <= 1.0", fill)
	}
}

func TestStreamingSource_BufferStats(t *testing.T) {
	data := bytes.Repeat([]byte("y"), 32*1024)
	source := &slowReader{data: data, delay: 5 * time.Millisecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	time.Sleep(50 * time.Millisecond)
	used := ss.BufferUsed()
	size := ss.BufferSize()
	if used <= 0 {
		t.Errorf("BufferUsed = %d, want > 0", used)
	}
	if size != 64*1024 {
		t.Errorf("BufferSize = %d, want %d", size, 64*1024)
	}
}

func TestStreamingSource_Close(t *testing.T) {
	data := bytes.Repeat([]byte("z"), 1024)
	source := &slowReader{data: data}
	ss := NewStreamingSource(source, 16*1024)

	if err := ss.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}

	buf := make([]byte, 1024)
	_, err := ss.Read(buf)
	if err != io.EOF {
		t.Errorf("Read after close = %v, want io.EOF", err)
	}
}

func TestStreamingSource_IsBuffering(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 10*1024)
	source := &slowReader{data: data, delay: 50 * time.Millisecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	if !ss.IsBuffering() {
		t.Error("IsBuffering = false, want true during fill")
	}

	time.Sleep(500 * time.Millisecond)
	if ss.IsBuffering() {
		t.Log("Still buffering after 500ms (might be ok depending on timing)")
	}
}

func TestStreamingSource_ReadAll(t *testing.T) {
	data := bytes.Repeat([]byte("b"), 10*1024)
	source := &slowReader{data: data, delay: 500 * time.Microsecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	var total int
	buf := make([]byte, 1024)
	attempts := 0
	maxAttempts := 100
	for {
		n, err := ss.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			if err == ErrEmpty {
				attempts++
				if attempts >= maxAttempts {
					t.Fatalf("Too many empty reads, got %d bytes", total)
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatalf("Read error: %v", err)
		}

		total += n
		attempts = 0
	}
	if total != len(data) {
		t.Errorf("Read %d bytes, want %d", total, len(data))
	}
}

func TestStreamingSource_ImplementsAudioSource(t *testing.T) {
	var _ AudioSource = (*StreamingSource)(nil)
}

func TestStreamingSource_ConcurrentReadWrite(t *testing.T) {
	data := bytes.Repeat([]byte("c"), 100*1024)
	source := &slowReader{data: data, delay: 50 * time.Microsecond}
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	readDone := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := ss.Read(buf)
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = ss.FillLevel()
			case <-readDone:
				return
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(readDone)
}

func TestStreamingSource_EmptySource(t *testing.T) {
	source := &slowReader{data: []byte{}}
	ss := NewStreamingSource(source, 16*1024)
	defer func() { _ = ss.Close() }()

	time.Sleep(50 * time.Millisecond)
	buf := make([]byte, 1024)
	n, err := ss.Read(buf)
	if err != io.EOF {
		t.Errorf("Read empty source = %v, want io.EOF", err)
	}
	if n != 0 {
		t.Errorf("Read empty source = %d, want 0", n)
	}
}

func TestStreamingSource_CancelContext(t *testing.T) {
	data := bytes.Repeat([]byte("d"), 1024)
	source := &slowReader{data: data}
	done := make(chan struct{})

	ss := &StreamingSource{
		rb:     NewRingBuffer(16 * 1024),
		source: source,
		done:   done,
		dataCh: make(chan struct{}, 1),
	}

	ss.wg.Add(1)
	go ss.fillBuffer()

	time.Sleep(50 * time.Millisecond)
	close(done)
	ss.wg.Wait()
}

func TestStreamingSource_DefaultBufferSize(t *testing.T) {
	source := &slowReader{data: []byte("e")}
	ss := NewStreamingSource(source, 0)
	if ss.BufferSize() != DefaultBufferSize {
		t.Errorf("Default buffer size = %d, want %d", ss.BufferSize(), DefaultBufferSize)
	}
	if err := ss.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestStreamingSource_LargeData(t *testing.T) {
	data := bytes.Repeat([]byte("f"), 1*1024*1024)
	source := &slowReader{data: data, delay: 10 * time.Microsecond}
	ss := NewStreamingSource(source, 256*1024)
	defer func() { _ = ss.Close() }()

	time.Sleep(200 * time.Millisecond)
	fill := ss.FillLevel()
	t.Logf("Buffer fill level: %.2f", fill)
}

func TestStreamingSource_MultipleClose(t *testing.T) {
	source := &slowReader{data: []byte("g")}
	ss := NewStreamingSource(source, 16*1024)

	err1 := ss.Close()
	err2 := ss.Close()
	if err1 != nil {
		t.Errorf("First close returned error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second close returned error: %v", err2)
	}
}

// TestStreamingSource_ReadBlocksUntilData verifies Read blocks (rather than
// returning ErrEmpty) when the buffer is empty, and returns data once the
// source delivers it (§12.2.2 pre-roll / §12.2.3 underrun).
func TestStreamingSource_ReadBlocksUntilData(t *testing.T) {
	source := newBlockingSource()
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	readDone := make(chan struct{})
	var got []byte
	go func() {
		defer close(readDone)
		buf := make([]byte, 1024)
		n, err := ss.Read(buf)
		if err != nil && err != io.EOF {
			t.Errorf("Read() error = %v", err)
			return
		}
		got = buf[:n]
	}()

	// Give the reader time to block on the empty buffer.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-readDone:
		t.Fatal("Read returned before data was available")
	default:
	}

	source.ch <- []byte("hello")
	select {
	case <-readDone:
		if string(got) != "hello" {
			t.Errorf("Read() = %q, want %q", got, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock after data arrived")
	}
}

// TestStreamingSource_WaitReady verifies the pre-roll gate: it blocks until the
// minimum byte threshold is buffered and returns nil.
func TestStreamingSource_WaitReady(t *testing.T) {
	source := newBlockingSource()
	ss := NewStreamingSource(source, 256*1024)
	defer func() { _ = ss.Close() }()

	ready := make(chan error, 1)
	go func() {
		ready <- ss.WaitReady(context.Background(), PreRollBytes)
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-ready:
		t.Fatalf("WaitReady returned early with %v", err)
	default:
	}

	source.ch <- make([]byte, 32*1024)
	source.ch <- make([]byte, 32*1024)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("WaitReady() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady did not unblock after data arrived")
	}
}

// TestStreamingSource_WaitReady_EmptySource verifies WaitReady surfaces a
// terminal error when the source ends with no data.
func TestStreamingSource_WaitReady_EmptySource(t *testing.T) {
	source := &slowReader{data: []byte{}}
	ss := NewStreamingSource(source, 16*1024)
	defer func() { _ = ss.Close() }()

	if err := ss.WaitReady(context.Background(), PreRollBytes); err == nil {
		t.Fatal("WaitReady on empty source: expected error, got nil")
	}
}

// TestStreamingSource_WaitReady_ContextCancel verifies WaitReady honors ctx.
func TestStreamingSource_WaitReady_ContextCancel(t *testing.T) {
	source := newBlockingSource()
	ss := NewStreamingSource(source, 64*1024)
	defer func() { _ = ss.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	go func() {
		ready <- ss.WaitReady(ctx, PreRollBytes)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-ready:
		if err == nil {
			t.Fatal("WaitReady after cancel: expected error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady did not honor context cancellation")
	}
}

// blockingSource blocks on Read until data is pushed to its channel, and
// returns EOF once closed.
type blockingSource struct {
	ch   chan []byte
	done chan struct{}
}

func (s *blockingSource) Read(p []byte) (int, error) {
	select {
	case <-s.done:
		return 0, io.EOF
	case data, ok := <-s.ch:
		if !ok {
			return 0, io.EOF
		}
		return copy(p, data), nil
	}
}

func (s *blockingSource) Close() error {
	close(s.done)
	return nil
}

func newBlockingSource() *blockingSource {
	return &blockingSource{
		ch:   make(chan []byte),
		done: make(chan struct{}),
	}
}

// seekableSource is an in-memory io.ReadSeekCloser that records the last seek.
type seekableSource struct {
	mu   sync.Mutex
	data []byte
	pos  int
}

func (s *seekableSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}

	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

func (s *seekableSource) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = int64(s.pos) + offset
	case io.SeekEnd:
		target = int64(len(s.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if target < 0 {
		return 0, errors.New("negative seek position")
	}
	s.pos = int(target)
	return target, nil
}

func (s *seekableSource) Close() error {
	return nil
}

func TestStreamingSource_Seek(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 1000) // 10k bytes
	src := &seekableSource{data: data}
	ss := NewStreamingSource(src, 64*1024)
	defer func() { _ = ss.Close() }()

	if !waitForData(ss, 500*time.Millisecond) {
		t.Fatal("timeout waiting for initial data")
	}

	// Seek to a mid-stream byte offset and read; the data must match the
	// underlying source at that position.
	pos, err := ss.Seek(5000, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	if pos != 5000 {
		t.Errorf("Seek() pos = %d, want 5000", pos)
	}
	if !waitForData(ss, 500*time.Millisecond) {
		t.Fatal("timeout waiting for data after seek")
	}

	buf := make([]byte, 64)
	n, err := ss.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}
	if n == 0 {
		t.Fatal("Read() returned 0 bytes after seek")
	}
	if !bytes.Equal(buf[:n], data[5000:5000+n]) {
		t.Errorf("Read() after seek = %q, want %q", buf[:n], data[5000:5000+n])
	}
}

func TestStreamingSource_Seek_FromCurrent(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 2000) // 16k bytes
	src := &seekableSource{data: data}
	ss := NewStreamingSource(src, 64*1024)
	defer func() { _ = ss.Close() }()

	if !waitForData(ss, 500*time.Millisecond) {
		t.Fatal("timeout waiting for initial data")
	}

	// Advance the read position by consuming some bytes, then seek relative to it.
	consume := make([]byte, 1024)
	if n, err := io.ReadFull(ss, consume); err != nil || n != 1024 {
		t.Fatalf("initial read: n=%d err=%v", n, err)
	}

	pos, err := ss.Seek(2048, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek(current) error = %v", err)
	}

	want := int64(1024) + 2048
	if pos != want {
		t.Errorf("Seek(current) pos = %d, want %d", pos, want)
	}
	if !waitForData(ss, 500*time.Millisecond) {
		t.Fatal("timeout waiting for data after seek")
	}

	buf := make([]byte, 64)
	n, err := ss.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}
	if n == 0 {
		t.Fatal("Read() returned 0 bytes after seek")
	}
	if !bytes.Equal(buf[:n], data[want:want+int64(n)]) {
		t.Errorf("Read() after relative seek = %q, want %q", buf[:n], data[want:want+int64(n)])
	}
}

func TestStreamingSource_Seek_NonSeekableSource(t *testing.T) {
	src := &slowReader{data: bytes.Repeat([]byte("x"), 1024)}
	ss := NewStreamingSource(src, 64*1024)
	defer func() { _ = ss.Close() }()

	if _, err := ss.Seek(10, io.SeekStart); err == nil {
		t.Fatal("Seek() on non-seekable source: expected error")
	}
}

func TestStreamingSource_Seek_AfterClose(t *testing.T) {
	src := &seekableSource{data: bytes.Repeat([]byte("x"), 1024)}
	ss := NewStreamingSource(src, 64*1024)
	_ = ss.Close()

	if _, err := ss.Seek(10, io.SeekStart); err != io.ErrClosedPipe {
		t.Errorf("Seek() after Close = %v, want io.ErrClosedPipe", err)
	}
}
