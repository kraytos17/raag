package app

import (
	"context"
	"io"
	"iter"
	"time"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
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
	Delete(ctx context.Context, id domain.TrackID) error
	BulkSave(ctx context.Context, tracks []*domain.Track) error
	ListAll(ctx context.Context) ([]*domain.Track, error)
	ListAllPaths(ctx context.Context) ([]string, error)
}

type FileStatStore interface {
	SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error
	LoadFileStats(ctx context.Context) (map[string]*domain.FileStat, error)
}

type PlaylistRepository interface {
	Save(ctx context.Context, playlist *domain.Playlist) error
	FindByID(ctx context.Context, id domain.PlaylistID) (*domain.Playlist, error)
	ListAll(ctx context.Context) iter.Seq2[*domain.Playlist, error]
	Delete(ctx context.Context, id domain.PlaylistID) error
}

type PeerRepository interface {
	SavePeerInfo(ctx context.Context, info *domain.PeerInfo) error
	GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error)
	SavePeerScore(ctx context.Context, id domain.PeerID, score *domain.PeerScore) error
	GetPeerScore(ctx context.Context, id domain.PeerID) (*domain.PeerScore, error)
	SaveLibraryManifest(ctx context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error
	GetLibraryManifest(ctx context.Context, id domain.PeerID) (*domain.LibraryManifest, error)
	ListAll(ctx context.Context) iter.Seq2[*domain.PeerInfo, error]
}

// SettingsRepository persists runtime user settings (e.g. playback volume) in
// the database. Config.toml remains the source of defaults; this is the
// mutable, restart-surviving layer on top of it.
type SettingsRepository interface {
	GetVolume(ctx context.Context) (int, bool, error)
	SetVolume(ctx context.Context, volume int) error
}

type SearchIndex interface {
	Index(ctx context.Context, track *domain.Track) error
	IndexBatch(ctx context.Context, tracks []*domain.Track) error
	Search(ctx context.Context, query string, limit int) ([]domain.TrackID, error)
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
	PlayStreaming(ctx context.Context, source audio.AudioSource, mimeType string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, position time.Duration) error
	SetVolume(ctx context.Context, volume int) error
	SetLoudness(db float64)
	SetEqualizer(settings domain.EqualizerSettings)
	GetEqualizer() domain.EqualizerSettings
	Prepare(reader io.Reader, mimeType string) error
	PrepareStreaming(source audio.AudioSource, mimeType string) error
	HasNext() bool
	CommitNext() error
	GetState() domain.PlayerState
	GetPosition() time.Duration
	GetBufferFillLevel() float64
	Done() <-chan struct{}
}

type Queue interface {
	Peek() *domain.Track
	Next() *domain.Track
	Previous() *domain.Track
	Current() *domain.Track
	Length() int
}

type SearchHandler interface {
	Search(ctx context.Context, query string, limit int) ([]*domain.Track, error)
}
