package audio

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrClosed     = errors.New("ring buffer closed")
	ErrWouldBlock = errors.New("operation would block")
	ErrEmpty      = errors.New("ring buffer is empty")
	ErrFull       = errors.New("ring buffer is full")
)

const (
	DefaultBufferSize = 1024 * 1024 // 1 MiB = 4 × MaxChunkSize
	LowWatermark      = 0.20
	HighWatermark     = 0.80
)

type RingBuffer struct {
	buf      []byte
	size     int
	readPos  int
	writePos int
	count    int
	mu       sync.Mutex
	closed   bool
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	rb := &RingBuffer{
		buf:      make([]byte, size),
		size:     size,
		readPos:  0,
		writePos: 0,
		count:    0,
	}
	return rb
}

func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.closed {
		return 0, ErrClosed
	}
	if rb.count == rb.size {
		return 0, ErrFull
	}

	n := min(len(p), rb.size-rb.count)
	end := rb.writePos + n
	if end <= rb.size {
		copy(rb.buf[rb.writePos:], p[:n])
	} else {
		first := rb.size - rb.writePos
		copy(rb.buf[rb.writePos:], p[:first])
		copy(rb.buf[:n-first], p[first:])
	}

	rb.writePos = (rb.writePos + n) % rb.size
	rb.count += n
	return n, nil
}

func (rb *RingBuffer) Read(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		if rb.closed {
			return 0, io.EOF
		}
		return 0, ErrEmpty
	}

	n := min(len(p), rb.count)
	end := rb.readPos + n
	if end <= rb.size {
		copy(p, rb.buf[rb.readPos:rb.readPos+n])
	} else {
		first := rb.size - rb.readPos
		copy(p, rb.buf[rb.readPos:])
		copy(p[first:], rb.buf[:n-first])
	}

	rb.readPos = (rb.readPos + n) % rb.size
	rb.count -= n
	return n, nil
}

func (rb *RingBuffer) Available() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size - rb.count
}

func (rb *RingBuffer) Used() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

func (rb *RingBuffer) FillLevel() float64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.size == 0 {
		return 0
	}
	return float64(rb.count) / float64(rb.size)
}

func (rb *RingBuffer) Close() error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.closed = true
	return nil
}

func (rb *RingBuffer) IsClosed() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.closed
}

func (rb *RingBuffer) Size() int {
	return rb.size
}

func (rb *RingBuffer) Peek(p []byte, offset int) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if offset < 0 || offset >= rb.count {
		return 0, ErrEmpty
	}

	n := len(p)
	remaining := rb.count - offset
	if n > remaining {
		n = remaining
	}

	pos := (rb.readPos + offset) % rb.size
	end := pos + n
	if end <= rb.size {
		copy(p, rb.buf[pos:pos+n])
	} else {
		first := rb.size - pos
		copy(p, rb.buf[pos:])
		copy(p[first:], rb.buf[:n-first])
	}
	return n, nil
}

func (rb *RingBuffer) Discard(n int) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if n >= rb.count {
		discarded := rb.count
		rb.count = 0
		rb.readPos = rb.writePos
		return discarded
	}

	rb.readPos = (rb.readPos + n) % rb.size
	rb.count -= n
	return n
}

// Reset clears the buffer and un-closes it, so a closed (EOF) ring can be
// reused after a seek repositions the source.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.count = 0
	rb.readPos = 0
	rb.writePos = 0
	rb.closed = false
}
