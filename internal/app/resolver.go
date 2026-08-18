package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/transcoder"
)

type TrackSource int

const (
	SourceLocal TrackSource = iota
	SourceP2P
)

type ResolvedTrack struct {
	Reader io.ReadCloser
	Source TrackSource
	PeerID string
	Track  *domain.Track
}

type Resolver interface {
	Resolve(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error)
	FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string
}

type LocalResolver struct {
	libraryRepo LibraryRepository
	transcoder  *transcoder.Transcoder
}

// defaultLocalTranscodeBitrate is used when transcoding a non-decodable local
// file (e.g. .m4a) to mp3 for playback. Matches the P2P transcode path.
const defaultLocalTranscodeBitrate = "128k"

func NewLocalResolver(libraryRepo LibraryRepository, tr *transcoder.Transcoder) *LocalResolver {
	return &LocalResolver{libraryRepo: libraryRepo, transcoder: tr}
}

func (r *LocalResolver) Resolve(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
	track, err := r.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(track.Path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			return nil, domain.ErrFileNotAccessible
		}
		return nil, err
	}

	// Local files whose codec the engine can't decode (e.g. .m4a/.aac/.opus/.wma)
	// are transcoded to mp3 via ffmpeg so they play locally, mirroring the P2P
	// transcode path. Without a transcoder, the raw file is returned (the
	// engine will fail to decode it).
	if r.transcoder != nil && !domain.CodecDecodable(track.Codec) {
		tmpPath, err := r.transcoder.TranscodeToFile(ctx, track.Path, codecMP3, defaultLocalTranscodeBitrate)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("local transcode failed: %w", err)
		}
		_ = file.Close()

		tmp, err := os.Open(tmpPath)
		if err != nil {
			return nil, fmt.Errorf("open transcoded file: %w", err)
		}
		track.MimeType = mimeTypeMPEG
		track.Codec = codecMP3
		return &ResolvedTrack{
			Reader: tmp,
			Source: SourceLocal,
			Track:  track,
		}, nil
	}

	return &ResolvedTrack{
		Reader: file,
		Source: SourceLocal,
		Track:  track,
	}, nil
}

func (r *LocalResolver) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	return nil
}

type P2PResolverAdapter interface {
	Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error)
	FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string
	LastPeerID() string
	FetchTrackMetadata(ctx context.Context, trackID domain.TrackID, peerID string) (*domain.Track, error)
	Peers() []domain.PeerID
	SearchPeer(ctx context.Context, peerID domain.PeerID, query string, limit int) ([]*domain.Track, error)
}

// RemoteSearchHit is a peer's search results tagged with the peer it came from.
type RemoteSearchHit struct {
	PeerID string
	Tracks []*domain.Track
}

// RemoteSearcher searches remote peers' libraries in parallel.
type RemoteSearcher interface {
	SearchRemote(ctx context.Context, query string, limit int) []RemoteSearchHit
}

type MultiSourceResolver struct {
	local *LocalResolver
	p2p   P2PResolverAdapter
}

func NewMultiSourceResolver(libraryRepo LibraryRepository, p2pResolver P2PResolverAdapter, tr *transcoder.Transcoder) Resolver {
	return &MultiSourceResolver{
		local: NewLocalResolver(libraryRepo, tr),
		p2p:   p2pResolver,
	}
}

func (r *MultiSourceResolver) Resolve(ctx context.Context, trackID domain.TrackID) (*ResolvedTrack, error) {
	resolved, err := r.local.Resolve(ctx, trackID)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, domain.ErrTrackNotFound) {
		return nil, err
	}
	if r.p2p != nil {
		reader, err := r.p2p.Resolve(ctx, trackID)
		if err == nil {
			peerID := r.p2p.LastPeerID()
			track, _ := r.p2p.FetchTrackMetadata(ctx, trackID, peerID)
			return &ResolvedTrack{
				Reader: reader,
				Source: SourceP2P,
				PeerID: peerID,
				Track:  track,
			}, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (r *MultiSourceResolver) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	if r.p2p == nil {
		return nil
	}
	return r.p2p.FindPeersWithTrack(ctx, trackID)
}

// SearchRemote queries every connected peer's library in parallel with a
// per-peer timeout. Failing peers are skipped best-effort.
func (r *MultiSourceResolver) SearchRemote(ctx context.Context, query string, limit int) []RemoteSearchHit {
	if r.p2p == nil {
		return nil
	}

	peers := r.p2p.Peers()
	if len(peers) == 0 {
		return nil
	}

	hits := make([]RemoteSearchHit, 0, len(peers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, pid := range peers {
		wg.Add(1)
		go func(pid domain.PeerID) {
			defer wg.Done()
			tracks, err := r.p2p.SearchPeer(ctx, pid, query, limit)
			if err != nil || len(tracks) == 0 {
				return
			}

			mu.Lock()
			hits = append(hits, RemoteSearchHit{PeerID: string(pid), Tracks: tracks})
			mu.Unlock()
		}(pid)
	}
	wg.Wait()
	return hits
}
