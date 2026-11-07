package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	peerpkg "github.com/p-society/raag/internal/infra/p2p/peer"
	pb "github.com/p-society/raag/proto/gen"
)

type (
	PeerScorer        = peerpkg.Scorer
	SyncError         = peerpkg.SyncError
	PeerLifecycle     = peerpkg.Lifecycle
	PeerManager       = peerpkg.PeerManager
	LocalCapabilities = peerpkg.LocalCapabilities
)

const (
	CodecMP3    = peerpkg.CodecMP3
	CodecFlac   = peerpkg.CodecFlac
	CodecVorbis = peerpkg.CodecVorbis
	CodecPCM    = peerpkg.CodecPCM
	CodecAAC    = peerpkg.CodecAAC
	CodecOpus   = peerpkg.CodecOpus
	CodecWMA    = peerpkg.CodecWMA

	codecMP3    = CodecMP3
	codecFlac   = CodecFlac
	codecVorbis = CodecVorbis
	codecPCM    = CodecPCM
	codecAAC    = CodecAAC
	codecOpus   = CodecOpus
	codecWMA    = CodecWMA
)

func NewPeerScorer() *PeerScorer { return peerpkg.NewScorer() }
func NewPeerScorerWithConfig(failLimit int, cooldown time.Duration) *PeerScorer {
	return peerpkg.NewScorerWithConfig(failLimit, cooldown)
}

func NewPeerLifecycle(h host.Host, cache *discovery.PeerCache, library app.LibraryRepository, scorer *PeerScorer, ttl time.Duration, repo app.PeerRepository) PeerLifecycle {
	return peerpkg.NewManager(h, cache, library, scorer, ttl, repo)
}

func SyncErrorCode(err error) (pb.ErrorCode, bool) { return peerpkg.ErrorCode(err) }
