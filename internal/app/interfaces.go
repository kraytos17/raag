package app

import (
	"context"
	"io"
	"iter"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type SearchQuery struct {
	Query  string
	Limit  int
	Offset int
}

type IndexStats struct {
	TotalTracks   int
	TotalTerms    int
	TotalTrigrams int
	LastUpdated   int64
}

type LibraryRepository interface {
	Save(ctx context.Context, track *domain.Track) error
	FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error)
	FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error)
	FindByPath(ctx context.Context, path string) (*domain.Track, error)
	GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error)
	Search(ctx context.Context, query SearchQuery) ([]*domain.Track, error)
	Delete(ctx context.Context, id domain.TrackID) error
	BulkSave(ctx context.Context, tracks []*domain.Track) error
	ListAll(ctx context.Context) ([]*domain.Track, error)
	ListAllPaths(ctx context.Context) ([]string, error)
	SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error
	LoadFileStats(ctx context.Context) (map[string]*domain.FileStat, error)
}

type PlaylistRepository interface {
	Save(ctx context.Context, playlist *domain.Playlist) error
	FindByID(ctx context.Context, id domain.PlaylistID) (*domain.Playlist, error)
	ListAll(ctx context.Context) iter.Seq[*domain.Playlist]
	Delete(ctx context.Context, id domain.PlaylistID) error
}

type PeerRepository interface {
	SavePeerInfo(ctx context.Context, info *domain.PeerInfo) error
	GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error)
	SavePeerScore(ctx context.Context, id domain.PeerID, score *domain.PeerScore) error
	GetPeerScore(ctx context.Context, id domain.PeerID) (*domain.PeerScore, error)
	SaveLibraryManifest(ctx context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error
	GetLibraryManifest(ctx context.Context, id domain.PeerID) (*domain.LibraryManifest, error)
	ListAll(ctx context.Context) iter.Seq[*domain.PeerInfo]
}

type SearchIndex interface {
	Index(ctx context.Context, track *domain.Track) error
	IndexBatch(ctx context.Context, tracks []*domain.Track) error
	Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error)
	SearchFuzzy(ctx context.Context, query string, limit int) ([]domain.TrackID, error)
	Delete(ctx context.Context, id domain.TrackID) error
	Stats(ctx context.Context) (IndexStats, error)
	Rebuild(ctx context.Context, repo LibraryRepository) error
}

type ChunkRequest struct {
	TrackID domain.TrackID
	Offset  int64
	Length  int32
}

type ChunkResponse struct {
	TrackID   domain.TrackID
	Offset    int64
	Data      []byte
	LastChunk bool
}

type StreamTransport interface {
	Open(ctx context.Context, peerID domain.PeerID, req ChunkRequest) (io.ReadCloser, error)
	GetCapabilities(ctx context.Context, peerID domain.PeerID) (*domain.PeerCapabilities, error)
	Close() error
}

type PeerTransport interface {
	Connect(ctx context.Context, peerID domain.PeerID) error
	Disconnect(ctx context.Context, peerID domain.PeerID) error
	IsConnected(peerID domain.PeerID) bool
	GetPeers() []domain.PeerID
}

type SyncTransport interface {
	RequestManifest(ctx context.Context, peerID domain.PeerID) (*domain.LibraryManifest, error)
	RequestTrack(ctx context.Context, peerID domain.PeerID, trackID domain.TrackID) (*domain.Track, error)
}

type Player interface {
	Play(ctx context.Context, reader io.Reader, mimeType string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, position time.Duration) error
	SetVolume(ctx context.Context, volume int) error
	GetState() PlayerState
	GetPosition() time.Duration
}

type PlayerState string

const (
	PlayerStateIdle      PlayerState = "idle"
	PlayerStatePlaying   PlayerState = "playing"
	PlayerStatePaused    PlayerState = "paused"
	PlayerStateBuffering PlayerState = "buffering"
	PlayerStateError     PlayerState = "error"
)

type Queue interface {
	Peek() *domain.Track
	Next() *domain.Track
	Previous() *domain.Track
	Current() *domain.Track
	Length() int
}

type RepeatMode string

const (
	RepeatModeNone RepeatMode = "none"
	RepeatModeOne  RepeatMode = "one"
	RepeatModeAll  RepeatMode = "all"
)
