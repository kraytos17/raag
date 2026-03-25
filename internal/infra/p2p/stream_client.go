package p2p

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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
			backoff := time.Duration(attempt*100) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		stream, err := c.pool.Acquire(ctx, c.peerID)
		if err != nil {
			lastErr = err
			continue
		}

		start := time.Now()
		if err := c.writeRequest(stream, req); err != nil {
			c.pool.Remove(c.peerID, stream)
			lastErr = err
			continue
		}

		var resp pb.ChunkResponse
		if err := c.readResponse(stream, &resp); err != nil {
			c.pool.Remove(c.peerID, stream)
			lastErr = err
			continue
		}

		elapsed := time.Since(start)
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

	go func() {
		resp, err := r.client.GetChunk(r.ctx, req)
		ch <- prefetchResult{resp: resp, err: err}
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
			r.mu.Unlock()
			result := <-r.ahead
			r.mu.Lock()
			r.ahead = nil
			if r.closed {
				return 0, io.EOF
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

			r.mu.Unlock()
			resp, err = r.client.GetChunk(r.ctx, req)
			r.mu.Lock()
			if r.closed {
				return 0, io.EOF
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
