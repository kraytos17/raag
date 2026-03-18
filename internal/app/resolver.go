package app

import (
	"context"
	"io"
	"os"

	"github.com/p-society/raag/internal/domain"
)

type Resolver struct {
	libraryRepo     LibraryRepository
	peerTransport   PeerTransport
	streamTransport StreamTransport
}

func NewResolver(
	libraryRepo LibraryRepository,
	peerTransport PeerTransport,
	streamTransport StreamTransport,
) *Resolver {
	return &Resolver{
		libraryRepo:     libraryRepo,
		peerTransport:   peerTransport,
		streamTransport: streamTransport,
	}
}

func (s *Resolver) Resolve(ctx context.Context, trackID domain.TrackID) (io.Reader, error) {
	track, err := s.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if file, err := os.Open(track.Path); err == nil {
		return file, nil
	}

	peerWithTrack := s.findPeerWithTrack(ctx, trackID)
	if peerWithTrack == "" {
		return nil, domain.ErrTrackNotFound
	}

	req := ChunkRequest{
		TrackID: trackID,
		Offset:  0,
		Length:  256 * 1024,
	}

	reader, err := s.streamTransport.Open(ctx, peerWithTrack, req)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (s *Resolver) findPeerWithTrack(ctx context.Context, trackID domain.TrackID) domain.PeerID {
	peers := s.peerTransport.GetPeers()
	for _, peerID := range peers {
		manifest, err := s.getPeerManifest(ctx, peerID)
		if err != nil {
			continue
		}
		if manifest.HasTrack(trackID) {
			return peerID
		}
	}
	return ""
}

func (s *Resolver) getPeerManifest(ctx context.Context, peerID domain.PeerID) (*domain.LibraryManifest, error) {
	return nil, nil
}
