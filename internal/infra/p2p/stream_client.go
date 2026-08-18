package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/infra/backoff"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type StreamClient struct {
	peerID     peer.ID
	pool       *StreamPool
	scorer     *PeerScorer
	timeout    time.Duration
	maxRetries int
}

func NewStreamClient(peerID peer.ID, pool *StreamPool, scorer *PeerScorer) *StreamClient {
	return &StreamClient{
		peerID:     peerID,
		pool:       pool,
		scorer:     scorer,
		timeout:    30 * time.Second,
		maxRetries: 3,
	}
}

func (c *StreamClient) GetChunk(ctx context.Context, req *pb.ChunkRequest) (*pb.ChunkResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if backoff.Wait(ctx.Done(), backoff.Linear(attempt, 100*time.Millisecond)) {
				return nil, ctx.Err()
			}
		}

		stream, err := c.pool.Acquire(ctx, c.peerID)
		if err != nil {
			lastErr = err
			continue
		}

		if err := c.writeRequest(stream, req); err != nil {
			c.pool.Remove(c.peerID, stream)
			lastErr = err
			continue
		}

		// Time only the response read: the write is a tiny fixed-size request
		// frame, so including it (or connection setup) would skew the bandwidth
		// estimate downward.
		readStart := time.Now()
		var resp pb.ChunkResponse
		if err := c.readResponse(stream, &resp); err != nil {
			c.pool.Remove(c.peerID, stream)
			lastErr = err
			continue
		}

		elapsed := time.Since(readStart)
		if c.scorer != nil {
			if elapsed > 0 {
				bps := int64(float64(len(resp.Data)) / elapsed.Seconds())
				c.scorer.RecordBandwidth(c.peerID, bps)
			}
		}

		c.pool.Release(c.peerID, stream)
		return &resp, nil
	}
	return nil, lastErr
}

func (c *StreamClient) writeRequest(stream network.Stream, req *pb.ChunkRequest) error {
	stream.SetWriteDeadline(time.Now().Add(c.timeout))
	defer stream.SetWriteDeadline(time.Time{})
	return wire.WriteMsg(stream, req)
}

func (c *StreamClient) readResponse(stream network.Stream, resp *pb.ChunkResponse) error {
	stream.SetReadDeadline(time.Now().Add(c.timeout))
	defer stream.SetReadDeadline(time.Time{})
	return wire.ReadMsg(stream, resp)
}

func (c *StreamClient) GetTrack(ctx context.Context, trackID string, codec string, bitrate int32) (io.ReadCloser, error) {
	fetchCtx, fetchCancel := context.WithCancel(ctx)
	r := &chunkedReader{
		client:      c,
		trackID:     trackID,
		codec:       codec,
		bitrate:     bitrate,
		offset:      0,
		chunkSize:   int64(protocols.MaxChunkSize),
		ctx:         fetchCtx,
		fetchCancel: fetchCancel,
	}
	return r, nil
}

type prefetchResult struct {
	resp *pb.ChunkResponse
	err  error
	gen  uint64
}

// timeoutError reports a read that exceeded its deadline. It satisfies
// net.Error so the streaming fill goroutine treats it as a retryable timeout
// rather than a terminal error.
type timeoutError struct{}

func (timeoutError) Error() string   { return "chunk read timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// deadlineWait blocks until the channel delivers, the read deadline elapses, or
// the reader is closed. Returns (true, result, nil) when a value is delivered,
// (false, {}, timeoutError) on deadline, and (false, {}, ctxErr) if closed.
func (r *chunkedReader) deadlineWait(ch <-chan prefetchResult) (bool, prefetchResult, error) {
	remaining := r.remaining()
	if remaining == 0 {
		// No deadline set: block until delivery or close.
		select {
		case result := <-ch:
			return true, result, nil
		case <-r.ctx.Done():
			return false, prefetchResult{}, r.ctx.Err()
		}
	}
	if remaining < 0 {
		return false, prefetchResult{}, timeoutError{}
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case result := <-ch:
		return true, result, nil
	case <-timer.C:
		return false, prefetchResult{}, timeoutError{}
	case <-r.ctx.Done():
		return false, prefetchResult{}, r.ctx.Err()
	}
}

type chunkedReader struct {
	client      *StreamClient
	trackID     string
	codec       string
	bitrate     int32
	offset      int64
	chunkSize   int64
	current     []byte
	pos         int
	mu          sync.Mutex
	closed      bool
	totalSize   int64
	ctx         context.Context
	fetchCancel context.CancelFunc
	ahead       chan prefetchResult
	seekGen     uint64
	deadline    time.Time
}

// SetReadDeadline sets an absolute time after which Read should return a
// timeout error even if no data has arrived, so a stalled peer can't block the
// fill goroutine indefinitely. A zero time clears the deadline.
func (r *chunkedReader) SetReadDeadline(t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deadline = t
	return nil
}

// Deadline returns the current read deadline, or the zero time if unset.
func (r *chunkedReader) Deadline() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deadline
}

// remaining returns the time until the read deadline, or 0 if no deadline is
// set. The caller must hold r.mu.
func (r *chunkedReader) remaining() time.Duration {
	if r.deadline.IsZero() {
		return 0
	}
	return time.Until(r.deadline)
}

// streamPos returns the current absolute byte position in the stream, i.e. the
// offset the next Read would serve data from.
func (r *chunkedReader) streamPos() int64 {
	return r.offset - int64(len(r.current)) + int64(r.pos)
}

// Seek repositions the stream to a byte offset so subsequent Reads fetch
// chunks from the new position. The peer serves byte ranges, so a seek is just
// a reposition of the next request offset. seekGen invalidates any in-flight
// prefetch issued before the seek so a stale response is never consumed.
func (r *chunkedReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, io.ErrClosedPipe
	}

	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.streamPos() + offset
	case io.SeekEnd:
		if r.totalSize <= 0 {
			return 0, errors.New("chunked reader: unknown total size, cannot seek from end")
		}
		target = r.totalSize + offset
	default:
		return 0, errors.New("chunked reader: invalid whence")
	}
	if target < 0 {
		return 0, errors.New("chunked reader: negative seek position")
	}

	r.offset = target
	r.current = nil
	r.pos = 0
	r.ahead = nil
	r.seekGen++
	return target, nil
}

func (r *chunkedReader) startPrefetch() {
	if r.totalSize > 0 && r.offset >= r.totalSize {
		return // nothing left to prefetch
	}

	length := r.chunkSize
	if r.totalSize > 0 && r.offset+length > r.totalSize {
		length = r.totalSize - r.offset
	}

	req := &pb.ChunkRequest{
		TrackId: r.trackID,
		Offset:  r.offset,
		Length:  int32(length),
		Codec:   r.codec,
		Bitrate: r.bitrate,
	}

	ch := make(chan prefetchResult, 1)
	r.ahead = ch
	gen := r.seekGen

	go func() {
		resp, err := r.client.GetChunk(r.ctx, req)
		ch <- prefetchResult{resp: resp, err: err, gen: gen}
	}()
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, io.EOF
	}
	for r.pos >= len(r.current) {
		if r.totalSize > 0 && r.offset >= r.totalSize {
			return 0, io.EOF
		}

		var resp *pb.ChunkResponse
		var err error
		if r.ahead != nil {
			ch := r.ahead
			r.ahead = nil
			r.mu.Unlock()
			delivered, result, waitErr := r.deadlineWait(ch)
			r.mu.Lock()
			if !delivered {
				if errors.Is(waitErr, context.Canceled) {
					return 0, io.EOF
				}
				return 0, waitErr
			}
			if r.closed {
				return 0, io.EOF
			}
			// A seek happened while this prefetch was in flight; the chunk was
			// fetched from a stale offset. Drop it and loop to refetch from the
			// new position.
			if result.gen != r.seekGen {
				continue
			}
			resp, err = result.resp, result.err
		} else {
			length := r.chunkSize
			if r.totalSize > 0 && r.offset+length > r.totalSize {
				length = r.totalSize - r.offset
			}

			req := &pb.ChunkRequest{
				TrackId: r.trackID,
				Offset:  r.offset,
				Length:  int32(length),
				Codec:   r.codec,
				Bitrate: r.bitrate,
			}
			gen := r.seekGen

			// If the deadline has already expired, fail fast without spawning a
			// fetch that can never be consumed. remaining()==0 means no deadline.
			if remaining := r.remaining(); remaining < 0 {
				return 0, timeoutError{}
			}

			// Run the fetch in a goroutine so the read deadline can bound it:
			// a stalled peer must not block the fill goroutine for GetChunk's
			// full 30s timeout. The late result is discarded on timeout.
			ch := make(chan prefetchResult, 1)
			r.mu.Unlock()
			go func() {
				rresp, rerr := r.client.GetChunk(r.ctx, req)
				ch <- prefetchResult{resp: rresp, err: rerr, gen: gen}
			}()

			delivered, result, waitErr := r.deadlineWait(ch)
			r.mu.Lock()
			if !delivered {
				if errors.Is(waitErr, context.Canceled) {
					return 0, io.EOF
				}
				return 0, waitErr
			}

			resp, err = result.resp, result.err
			if r.closed {
				return 0, io.EOF
			}
			// Same staleness guard for the synchronous fetch path.
			if gen != r.seekGen {
				continue
			}
		}

		if err != nil {
			return 0, err
		}
		if r.totalSize == 0 {
			r.totalSize = resp.TotalSize
		}

		r.current = resp.Data
		r.pos = 0
		if resp.LastChunk {
			r.totalSize = r.offset + int64(len(resp.Data))
		}
		if len(resp.Data) == 0 && !resp.LastChunk {
			return 0, fmt.Errorf("p2p: empty chunk from peer %s at offset %d", r.client.peerID, r.offset)
		}

		r.offset += int64(len(resp.Data))
		r.startPrefetch()
	}

	n := copy(p, r.current[r.pos:])
	r.pos += n
	return n, nil
}

func (r *chunkedReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.fetchCancel()
	return nil
}
