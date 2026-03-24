package audio

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type StreamingSource struct {
	rb     *RingBuffer
	source io.ReadCloser
	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed atomic.Bool
	err    error
}

func NewStreamingSource(source io.ReadCloser, bufferSize int) *StreamingSource {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}

	ss := &StreamingSource{
		rb:     NewRingBuffer(bufferSize),
		source: source,
		done:   make(chan struct{}),
	}

	ss.wg.Add(1)
	go ss.fillBuffer()
	return ss
}

func (ss *StreamingSource) fillBuffer() {
	defer ss.wg.Done()
	buf := make([]byte, 32*1024)
	shortDeadline := 100 * time.Millisecond
	for {
		select {
		case <-ss.done:
			return
		default:
		}

		ss.setReadDeadline(shortDeadline)
		n, err := ss.source.Read(buf)
		if n > 0 {
			_, writeErr := ss.rb.Write(buf[:n])
			if writeErr != nil {
				slog.Debug("ring buffer write error", "err", writeErr)
				ss.mu.Lock()
				if !ss.closed.Load() {
					ss.err = writeErr
				}
				ss.mu.Unlock()
				_ = ss.rb.Close()
				return
			}
		}
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			if errors.Is(err, io.EOF) {
				ss.mu.Lock()
				if !ss.closed.Load() {
					ss.err = err
				}
				
				ss.mu.Unlock()
				_ = ss.rb.Close()
				return
			}

			slog.Debug("source read error", "err", err)
			ss.mu.Lock()
			if !ss.closed.Load() {
				ss.err = err
			}
			
			ss.mu.Unlock()
			_ = ss.rb.Close()
			return
		}
	}
}

func (ss *StreamingSource) setReadDeadline(d time.Duration) {
	if fd, ok := ss.source.(interface {
		SetReadDeadline(time.Time) error
	}); ok {
		if err := fd.SetReadDeadline(time.Now().Add(d)); err != nil {
			slog.Warn("failed to set read deadline", "error", err)
		}
	}
}

func (ss *StreamingSource) Read(p []byte) (int, error) {
	return ss.rb.Read(p)
}

func (ss *StreamingSource) Close() error {
	if !ss.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(ss.done)
	ss.wg.Wait()
	if err := ss.rb.Close(); err != nil {
		return err
	}
	return ss.source.Close()
}

func (ss *StreamingSource) FillLevel() float64 {
	return ss.rb.FillLevel()
}

func (ss *StreamingSource) BufferUsed() int {
	return ss.rb.Used()
}

func (ss *StreamingSource) BufferSize() int {
	return ss.rb.Size()
}

func (ss *StreamingSource) IsBuffering() bool {
	if ss.closed.Load() {
		return false
	}
	return ss.rb.FillLevel() < HighWatermark
}

type AudioSource interface {
	io.ReadCloser
	FillLevel() float64
	BufferUsed() int
	BufferSize() int
	IsBuffering() bool
}

var _ AudioSource = (*StreamingSource)(nil)
