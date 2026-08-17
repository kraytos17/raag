package audio

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// PreRollBytes is the minimum number of bytes buffered before playback decode
// begins, so the first read never races an empty buffer over the network.
const PreRollBytes = 64 * 1024

type StreamingSource struct {
	rb     *RingBuffer
	source io.ReadCloser
	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed atomic.Bool
	err    error

	// dataCh is signaled (non-blocking, buffered 1) whenever the ring buffer
	// gains data or the source reaches a terminal state (EOF/error/close).
	// Read and WaitReady select on it instead of returning ErrEmpty, which
	// prevents first-play races and silent mid-play track death on underrun.
	dataCh chan struct{}
}

func NewStreamingSource(source io.ReadCloser, bufferSize int) *StreamingSource {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}

	ss := &StreamingSource{
		rb:     NewRingBuffer(bufferSize),
		source: source,
		done:   make(chan struct{}),
		dataCh: make(chan struct{}, 1),
	}

	ss.wg.Add(1)
	go ss.fillBuffer()
	return ss
}

// signal wakes any goroutine blocked in Read or WaitReady. The buffered-1
// channel plus predicate re-checks in the waiters makes a dropped signal
// harmless: a waiter that misses a signal re-checks the buffer on the next
// wake and, if it finds data, consumes it directly.
func (ss *StreamingSource) signal() {
	select {
	case ss.dataCh <- struct{}{}:
	default:
	}
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
			// Ring-buffer full is transient here (the consumer drains while
			// playing), wait for space instead of aborting the stream.
			if !ss.writeAll(buf[:n]) {
				return
			}
		}
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			ss.setTerminal(err)
			return
		}
	}
}

// writeAll writes data into the ring buffer, waiting (on done/space) if it is
// full. Returns false if the source was closed mid-write.
func (ss *StreamingSource) writeAll(data []byte) bool {
	for len(data) > 0 {
		select {
		case <-ss.done:
			return false
		default:
		}

		n, writeErr := ss.rb.Write(data)
		if n > 0 {
			ss.signal()
		}
		if writeErr != nil {
			if errors.Is(writeErr, ErrFull) {
				// Wait for the consumer to drain, then retry.
				select {
				case <-ss.done:
					return false
				case <-time.After(5 * time.Millisecond):
				}
				continue
			}

			slog.Debug("ring buffer write error", "err", writeErr)
			ss.setTerminal(writeErr)
			return false
		}
		data = data[n:]
	}
	return true
}

// setTerminal records the source's terminal error, closes the ring buffer, and
// wakes any blocked readers.
func (ss *StreamingSource) setTerminal(err error) {
	ss.mu.Lock()
	if !ss.closed.Load() {
		ss.err = err
	}

	ss.mu.Unlock()
	_ = ss.rb.Close()
	ss.signal()
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

// Read returns buffered audio data, blocking until data is available or the
// source reaches a terminal state. It never returns ErrEmpty to callers, so a
// stalled network peer pauses playback instead of killing the track.
func (ss *StreamingSource) Read(p []byte) (int, error) {
	for {
		n, err := ss.rb.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != ErrEmpty {
			return 0, ss.terminalErr()
		}

		// Buffer empty and not closed: wait for data or a terminal state.
		select {
		case <-ss.dataCh:
		case <-ss.done:
			return 0, ss.terminalErr()
		}
	}
}

// terminalErr returns the source's recorded error (or io.EOF) once the ring
// buffer is closed; nil if the stream is still live.
func (ss *StreamingSource) terminalErr() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if !ss.rb.IsClosed() && !ss.closed.Load() {
		return nil
	}
	if ss.err != nil {
		return ss.err
	}
	return io.EOF
}

// WaitReady blocks until at least minBytes are buffered, the source reaches a
// terminal state, or ctx is done. It is the pre-roll gate: PlayStreaming must
// not begin decoding until data has arrived, otherwise the very first read
// over the network races an empty buffer.
func (ss *StreamingSource) WaitReady(ctx context.Context, minBytes int) error {
	if minBytes <= 0 {
		return nil
	}

	for {
		if ss.rb.Used() >= minBytes {
			return nil
		}
		if err := ss.terminalErr(); err != nil {
			// The source ended before reaching the pre-roll threshold. If any
			// data was buffered (e.g. a track smaller than PreRollBytes), it is
			// still playable; otherwise surface the terminal error.
			if ss.rb.Used() > 0 {
				return nil
			}
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ss.dataCh:
		case <-ss.done:
			return ss.terminalErr()
		}
	}
}

func (ss *StreamingSource) Close() error {
	if !ss.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close the source first so an in-flight Read unblocks, then wait for the
	// fill goroutine to exit. Closing in the other order would hang Close() on
	// a stalled peer whose read never returns.
	close(ss.done)
	srcErr := ss.source.Close()
	ss.wg.Wait()
	if err := ss.rb.Close(); err != nil {
		return err
	}
	ss.signal()
	return srcErr
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

// IsBuffering reports whether the buffer is below the low-watermark, i.e. the
// stream cannot sustain playback and needs to buffer more before resuming.
func (ss *StreamingSource) IsBuffering() bool {
	if ss.closed.Load() {
		return false
	}
	return ss.rb.FillLevel() < LowWatermark
}

type AudioSource interface {
	io.ReadCloser
	FillLevel() float64
	BufferUsed() int
	BufferSize() int
	IsBuffering() bool
	WaitReady(ctx context.Context, minBytes int) error
}

var _ AudioSource = (*StreamingSource)(nil)
