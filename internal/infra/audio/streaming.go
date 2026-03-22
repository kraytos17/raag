package audio

import (
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
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err == io.EOF {
				ss.mu.Lock()
				if !ss.closed.Load() {
					ss.err = err
				}

				ss.mu.Unlock()
				ss.rb.Close()
				return
			}

			slog.Debug("source read error", "err", err)
			ss.mu.Lock()
			if !ss.closed.Load() {
				ss.err = err
			}

			ss.mu.Unlock()
			ss.rb.Close()
			return
		}
		if n > 0 {
			_, writeErr := ss.rb.Write(buf[:n])
			if writeErr != nil {
				slog.Debug("ring buffer write error", "err", writeErr)
				return
			}
		}
	}
}

func (ss *StreamingSource) setReadDeadline(d time.Duration) {
	if fd, ok := ss.source.(interface {
		SetReadDeadline(time.Time) error
	}); ok {
		fd.SetReadDeadline(time.Now().Add(d))
	}
}

func (ss *StreamingSource) Read(p []byte) (int, error) {
	return ss.rb.Read(p)
}

func (ss *StreamingSource) Close() error {
	ss.mu.Lock()
	if ss.closed.Load() {
		ss.mu.Unlock()
		return nil
	}

	ss.closed.Store(true)
	ss.mu.Unlock()

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
	ss.mu.Lock()
	defer ss.mu.Unlock()
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
