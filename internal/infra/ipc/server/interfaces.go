package server

import (
	"context"
	"iter"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

// DebugStatsProvider exposes storage size for `raag debug db`. Satisfied by
// *db.DB (and a mock in tests).
type DebugStatsProvider interface {
	Size() (lsmBytes, vlogBytes int64)
}

type PlaybackHandler interface {
	Play(ctx context.Context, trackID domain.TrackID) error
	PlayQuery(ctx context.Context, query string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, dur time.Duration) error
	SetVolume(ctx context.Context, vol int) error
	GetState() domain.PlayerState
	GetVolume() int
	GetCurrentTrack() *domain.Track
	GetPosition() time.Duration
	GetBufferFillLevel() float64
	SetEqualizer(settings domain.EqualizerSettings)
	GetEqualizer() domain.EqualizerSettings
	OnProgress(callback func(positionMs, durationMs int64))
}

type ScannerHandler interface {
	Scan(ctx context.Context) (int, error)
	ScanIncremental(ctx context.Context) (added, modified, removed int, err error)
	OnProgress(fn func(app.ScanProgress))
	RemoveProgressHandler(fn func(app.ScanProgress))
}

type SearchHandler interface {
	Search(ctx context.Context, query string, limit int) ([]*domain.Track, error)
}

type LibraryRepoHandler interface {
	FindByID(ctx context.Context, trackID domain.TrackID) (*domain.Track, error)
	FindByPath(ctx context.Context, path string) (*domain.Track, error)
	ListAll(ctx context.Context) ([]*domain.Track, error)
}

type PeerRepoHandler interface {
	ListAll(ctx context.Context) iter.Seq2[*domain.PeerInfo, error]
	GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error)
}

type QueueHandler interface {
	Add(track *domain.Track)
	Insert(pos int, track *domain.Track)
	Remove(pos int)
	Clear()
	Length() int
	Position() int
	MoveTo(track *domain.Track) bool
	Next() *domain.Track
	Previous() *domain.Track
	Tracks() []*domain.Track
	ToggleShuffle()
	SetShuffle(shuffle bool)
	SetRepeat(mode domain.RepeatMode)
	GetShuffle() bool
	GetRepeat() domain.RepeatMode
}
