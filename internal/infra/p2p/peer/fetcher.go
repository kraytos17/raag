package peer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

// fetchEntry identifies one in-flight FetchPeerData run. Cleanup removes the
// cancellation handle only while its own entry is still current, so an older
// fetch finishing late cannot strand a newer fetch.
type fetchEntry struct {
	cancel context.CancelFunc
}

// peerFetcher owns the peer data-fetch lifecycle: stream setup, the
// per-peer cancellation map, and wire (de)serialization. Result storage,
// scoring, and persistence stay with peerManager.
type peerFetcher struct {
	host host.Host

	mu     sync.RWMutex
	cancel map[peer.ID]*fetchEntry
}

func newPeerFetcher(h host.Host) *peerFetcher {
	return &peerFetcher{
		host:   h,
		cancel: make(map[peer.ID]*fetchEntry),
	}
}

// start preempts any in-flight fetch for pid and registers the new run.
func (f *peerFetcher) start(ctx context.Context, pid peer.ID) (context.Context, *fetchEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.cancel[pid]; ok {
		existing.cancel()
	}

	baseCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	entry := &fetchEntry{cancel: cancel}
	f.cancel[pid] = entry
	return baseCtx, entry
}

// finish releases pid's cancellation handle, but only if entry is still the
// current run.
func (f *peerFetcher) finish(pid peer.ID, entry *fetchEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if current, ok := f.cancel[pid]; ok && current == entry {
		delete(f.cancel, pid)
	}
}

// cancelFetch aborts pid's current run, if any, and releases its handle.
func (f *peerFetcher) cancelFetch(pid peer.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry, ok := f.cancel[pid]; ok {
		entry.cancel()
		delete(f.cancel, pid)
	}
}

func resetStream(s network.Stream) {
	if err := s.Reset(); err != nil {
		slog.Debug("stream reset error", "err", err)
	}
}

func closeStream(s network.Stream) {
	if err := s.Close(); err != nil {
		slog.Debug("stream close error", "err", err)
	}
}

// fetchManifest requests the peer's library manifest without storing it.
func (f *peerFetcher) fetchManifest(ctx context.Context, pid peer.ID) (*pb.LibraryManifest, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := f.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return nil, err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return nil, err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return nil, err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Manifest:
		closeStream(stream)
		return payload.Manifest, nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return nil, &SyncError{Code: payload.Error.Code, Message: payload.Error.Message}
	default:
		resetStream(stream)
		return nil, errors.New("unexpected sync response")
	}
}

// fetchCapabilities requests the peer's capabilities without storing them.
func (f *peerFetcher) fetchCapabilities(ctx context.Context, pid peer.ID) (*pb.PeerCapabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := f.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return nil, err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_CapabilitiesRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return nil, err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return nil, err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Capabilities:
		slog.Debug("capabilities received", "peer", pid,
			"codecs", payload.Capabilities.SupportedCodecs,
			"transcode", payload.Capabilities.CanTranscode)
		closeStream(stream)
		return payload.Capabilities, nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return nil, &SyncError{Code: payload.Error.Code, Message: payload.Error.Message}
	default:
		resetStream(stream)
		return nil, errors.New("unexpected sync response")
	}
}
