package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/ipfs/go-cid"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

// prefetchAhead is the number of blocks fetched speculatively ahead of the
// current read position.
const prefetchAhead = constants.TransferConcurrency

// P2PReader implements io.ReadSeeker over a remote song whose blocks are
// available via the BitswapClient.  It is designed to be passed to audio
// decoders (mp3, flac, wav, ogg) so playback can begin while the remainder of
// the file is still downloading.
//
// Blocks are cached in memory once fetched; they are never evicted during the
// lifetime of a single playback session.
type P2PReader struct {
	client    *BitswapClient
	cids      []cid.Cid      // ordered block CIDs
	blockSize int            // nominal block size in bytes
	totalSize int64          // total file size in bytes
	pos       int64          // current read position in the virtual file
	cache     map[int][]byte // blockIndex → data
	mu        sync.Mutex
	ctx       context.Context
}

// NewP2PReader creates a P2PReader.
// blockSize must match the Chunker.BlockSize() used to create the blocks.
// totalSize is the original file size.
func NewP2PReader(
	ctx context.Context,
	client *BitswapClient,
	cids []cid.Cid,
	blockSize int,
	totalSize int64,
) *P2PReader {
	r := &P2PReader{
		client:    client,
		cids:      cids,
		blockSize: blockSize,
		totalSize: totalSize,
		cache:     make(map[int][]byte),
		ctx:       ctx,
	}
	// Speculatively prefetch the first few blocks so the decoder can start
	// immediately without waiting for the DHT round-trip.
	r.triggerPrefetch(0)
	return r
}

// Read implements io.Reader.  It fetches whichever block(s) cover [pos, pos+len(p))
// and copies the relevant bytes.
func (r *P2PReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pos >= r.totalSize {
		return 0, io.EOF
	}

	want := int64(len(p))
	if r.pos+want > r.totalSize {
		want = r.totalSize - r.pos
	}

	copied := 0
	for copied < int(want) {
		blockIdx := int(r.pos / int64(r.blockSize))
		if blockIdx >= len(r.cids) {
			return copied, io.EOF
		}

		data, err := r.getBlock(blockIdx)
		if err != nil {
			return copied, fmt.Errorf("read block %d: %w", blockIdx, err)
		}

		offsetInBlock := int(r.pos % int64(r.blockSize))
		available := len(data) - offsetInBlock
		if available <= 0 {
			return copied, io.EOF
		}

		n := min(int(want)-copied, available)
		copy(p[copied:copied+n], data[offsetInBlock:offsetInBlock+n])
		copied += n
		r.pos += int64(n)
	}

	// Kick off prefetch for upcoming blocks (non-blocking)
	nextBlock := int(r.pos / int64(r.blockSize))
	go r.triggerPrefetch(nextBlock)

	return copied, nil
}

// Seek implements io.Seeker.
func (r *P2PReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = r.pos + offset
	case io.SeekEnd:
		newPos = r.totalSize + offset
	default:
		return r.pos, fmt.Errorf("invalid whence: %d", whence)
	}

	if newPos < 0 {
		return r.pos, fmt.Errorf("seek before start of file")
	}
	if newPos > r.totalSize {
		newPos = r.totalSize
	}

	r.pos = newPos
	targetBlock := int(newPos / int64(r.blockSize))
	go r.triggerPrefetch(targetBlock)

	return r.pos, nil
}

// getBlock returns the bytes for blockIdx, fetching via bitswap if not cached.
// Must be called with r.mu held.
func (r *P2PReader) getBlock(blockIdx int) ([]byte, error) {
	if data, ok := r.cache[blockIdx]; ok {
		return data, nil
	}

	// Release the lock while fetching so other goroutines can read the cache.
	r.mu.Unlock()
	data, err := r.client.FetchBlock(r.ctx, r.cids[blockIdx])
	r.mu.Lock()

	if err != nil {
		return nil, err
	}
	r.cache[blockIdx] = data
	return data, nil
}

// triggerPrefetch speculatively fetches the next prefetchAhead blocks starting
// from startBlock.  It is safe to call concurrently and never blocks the
// caller.
func (r *P2PReader) triggerPrefetch(startBlock int) {
	end := min(startBlock+prefetchAhead, len(r.cids))
	for i := startBlock; i < end; i++ {
		idx := i
		go func() {
			r.mu.Lock()
			_, cached := r.cache[idx]
			r.mu.Unlock()
			if cached {
				return
			}

			data, err := r.client.FetchBlock(r.ctx, r.cids[idx])
			if err != nil {
				logger.Debugf("Prefetch block %d failed: %v", idx, err)
				return
			}

			r.mu.Lock()
			r.cache[idx] = data
			r.mu.Unlock()
		}()
	}
}

// Size returns the total size of the file this reader represents.
func (r *P2PReader) Size() int64 {
	return r.totalSize
}
