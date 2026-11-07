package peer

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

const (
	CodecMP3    = "mp3"
	CodecFlac   = "flac"
	CodecVorbis = "vorbis"
	CodecPCM    = "pcm"
	CodecAAC    = "aac"
	CodecOpus   = "opus"
	CodecWMA    = "wma"
)

// LocalCapabilities describes this node's advertised protocol capabilities.
type LocalCapabilities struct {
	LibraryRepo     app.LibraryRepository
	PeerID          peer.ID
	CanTranscode    bool
	UploadBandwidth int64
}

func (lc *LocalCapabilities) GetLocalCapabilities() *pb.PeerCapabilities {
	return &pb.PeerCapabilities{
		SupportedCodecs:   domain.PlayableCodecs,
		SupportedBitrates: domain.SupportedBitrates,
		ProtocolVersion:   domain.PeerProtocolVersion,
		CanTranscode:      lc.CanTranscode,
		UploadBandwidth:   lc.UploadBandwidth,
	}
}

func (lc *LocalCapabilities) PeerIDValue() peer.ID { return lc.PeerID }
