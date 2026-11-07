package p2p

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type P2PResolverAdapter struct {
	resolver      *P2PResolver
	peersMu       sync.RWMutex
	peersProvider func() []peer.ID
}

func NewP2PResolverAdapter(resolver *P2PResolver) *P2PResolverAdapter {
	return &P2PResolverAdapter{resolver: resolver}
}

// SetPeersProvider wires a function that returns currently connected peers
// (e.g. the P2P node's Peers). Required for SearchRemote fan-out.
func (a *P2PResolverAdapter) SetPeersProvider(fn func() []peer.ID) {
	a.peersMu.Lock()
	defer a.peersMu.Unlock()
	a.peersProvider = fn
}

func (a *P2PResolverAdapter) Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	return a.resolver.Resolve(ctx, trackID)
}

func (a *P2PResolverAdapter) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	return a.resolver.FindPeersWithTrack(ctx, trackID)
}

func (a *P2PResolverAdapter) LastPeerID() string {
	return a.resolver.LastPeerIDString()
}

func (a *P2PResolverAdapter) FetchTrackMetadata(ctx context.Context, trackID domain.TrackID, peerID string) (*domain.Track, error) {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return nil, err
	}
	return a.resolver.fetchTrackMetadata(ctx, trackID, pid)
}

// SearchPeer asks a single peer to search its own library and returns the
// matching tracks (with metadata). Best-effort: any error propagates so the
// caller can skip the peer.
func (a *P2PResolverAdapter) SearchPeer(ctx context.Context, pid peer.ID, query string, limit int) ([]*domain.Track, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := a.resolver.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return nil, err
	}

	defer stream.Close()
	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_RemoteSearchRequest{
			RemoteSearchRequest: &pb.RemoteSearchRequest{
				Query: query,
				Limit: int32(limit),
			},
		},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		return nil, err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		return nil, err
	}
	if sr, ok := resp.GetPayload().(*pb.SyncResponse_Search); ok {
		tracks := make([]*domain.Track, 0, len(sr.Search.Tracks))
		for _, t := range sr.Search.Tracks {
			tracks = append(tracks, convert.ProtoToTrack(t))
		}
		return tracks, nil
	}
	if er, ok := resp.GetPayload().(*pb.SyncResponse_Error); ok {
		return nil, errors.New(er.Error.Message)
	}
	return nil, errors.New("unexpected response type")
}

// Peers returns the currently connected peer IDs.
func (a *P2PResolverAdapter) Peers() []peer.ID {
	a.peersMu.RLock()
	provider := a.peersProvider
	a.peersMu.RUnlock()
	if provider != nil {
		return provider()
	}
	return nil
}
