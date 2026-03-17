package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/multiformats/go-multihash"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

// CIDFromData computes a SHA2-256 multihash CID (CIDv1, raw codec) from arbitrary bytes.
func CIDFromData(data []byte) (cid.Cid, error) {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to hash data: %w", err)
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

// CIDFromHash wraps an existing SHA2-256 raw hash (32 bytes) into a CIDv1.
// The hash must be the raw digest (not a multihash-encoded value).
func CIDFromHash(rawHash []byte) (cid.Cid, error) {
	mh, err := multihash.Encode(rawHash, multihash.SHA2_256)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to encode multihash: %w", err)
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

// BitswapClient performs DHT provider lookups and fetches individual blocks
// from peers over the /raag/blocks/1.0.0 stream protocol.
type BitswapClient struct {
	host    host.Host
	routing routing.Routing
}

// NewBitswapClient creates a BitswapClient.
// r must implement routing.Routing.
func NewBitswapClient(h host.Host, r routing.Routing) *BitswapClient {
	return &BitswapClient{
		host:    h,
		routing: r,
	}
}

// FindProviders queries the DHT for peers that provide the given CID.
// Returns up to 10 providers within constants.BlockFindProvidersTimeout.
func (b *BitswapClient) FindProviders(ctx context.Context, c cid.Cid) ([]peer.AddrInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, constants.BlockFindProvidersTimeout)
	defer cancel()
	ch := b.routing.FindProvidersAsync(ctx, c, 10)

	var providers []peer.AddrInfo
	for {
		select {
		case <-ctx.Done():
			return providers, nil
		case pi, ok := <-ch:
			if !ok {
				return providers, nil
			}
			providers = append(providers, pi)
		}
	}
}

// Provide announces to the DHT that this node holds the given CID.
func (b *BitswapClient) Provide(ctx context.Context, c cid.Cid) error {
	return b.routing.Provide(ctx, c, true)
}

// FetchBlock locates providers for c in the DHT, then fetches the raw block
// bytes from the first responding peer via /raag/blocks/1.0.0.
func (b *BitswapClient) FetchBlock(ctx context.Context, c cid.Cid) ([]byte, error) {
	providers, err := b.FindProviders(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers found for CID %s", c)
	}

	var lastErr error
	for _, pi := range providers {
		data, err := b.fetchBlockFromPeer(ctx, pi, c)
		if err != nil {
			logger.Debugf("Failed to fetch block from peer=%s cid=%s: %v", pi.ID, c, err)
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("all providers failed for CID %s: %w", c, lastErr)
}

// fetchBlockFromPeer opens a /raag/blocks/1.0.0 stream to pi and sends the CID
// as a binary key, then reads back the block bytes.
func (b *BitswapClient) fetchBlockFromPeer(ctx context.Context, pi peer.AddrInfo, c cid.Cid) ([]byte, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, constants.BlockFetchTimeout)
	defer cancel()

	if err := b.host.Connect(fetchCtx, pi); err != nil {
		return nil, fmt.Errorf("connect to peer=%s: %w", pi.ID, err)
	}

	stream, err := b.host.NewStream(fetchCtx, pi.ID, constants.BlockProtocolID)
	if err != nil {
		return nil, fmt.Errorf("open stream to peer=%s: %w", pi.ID, err)
	}
	defer stream.Close()

	cidBytes := c.Bytes()
	if _, err := stream.Write(cidBytes); err != nil {
		_ = stream.Reset()
		return nil, fmt.Errorf("send CID to peer=%s: %w", pi.ID, err)
	}
	// Signal end-of-write so the peer knows the request is complete
	if err := stream.CloseWrite(); err != nil {
		_ = stream.Reset()
		return nil, fmt.Errorf("close-write to peer=%s: %w", pi.ID, err)
	}

	data, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read block from peer=%s: %w", pi.ID, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("peer=%s returned empty block for CID %s", pi.ID, c)
	}
	return data, nil
}

// ProviderManager wraps BitswapClient and keeps an in-memory map of locally
// provided blocks so the block stream handler can serve them.
type ProviderManager struct {
	client   *BitswapClient
	mu       sync.RWMutex
	provided map[cid.Cid][]byte
}

// NewProviderManager creates a ProviderManager backed by the given host and routing.
func NewProviderManager(h host.Host, r routing.Routing) *ProviderManager {
	return &ProviderManager{
		client:   NewBitswapClient(h, r),
		provided: make(map[cid.Cid][]byte),
	}
}

// Provide registers data locally and announces the CID to the DHT.
// Returns the CID for the caller to record.
func (p *ProviderManager) Provide(ctx context.Context, data []byte) (cid.Cid, error) {
	c, err := CIDFromData(data)
	if err != nil {
		return cid.Undef, err
	}

	p.mu.Lock()
	p.provided[c] = data
	p.mu.Unlock()

	if err := p.client.Provide(ctx, c); err != nil {
		logger.Warnf("DHT Provide failed for CID %s: %v", c, err)
	}
	return c, nil
}

// Get retrieves a locally held block by CID (used by the block stream handler).
func (p *ProviderManager) Get(c cid.Cid) ([]byte, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	data, ok := p.provided[c]
	return data, ok
}

// Remove deletes a locally held block.
func (p *ProviderManager) Remove(c cid.Cid) {
	p.mu.Lock()
	delete(p.provided, c)
	p.mu.Unlock()
}

// FetchBlock fetches a block from the network.
func (p *ProviderManager) FetchBlock(ctx context.Context, c cid.Cid) ([]byte, error) {
	return p.client.FetchBlock(ctx, c)
}
