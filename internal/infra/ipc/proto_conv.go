package ipc

import (
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

func MarshalTrack(t *domain.Track) ([]byte, error) {
	return proto.Marshal(convert.TrackToProto(t))
}

func UnmarshalTrack(data []byte) (*domain.Track, error) {
	p := &pb.Track{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToTrack(p), nil
}

func MarshalPlaylist(p *domain.Playlist) ([]byte, error) {
	return proto.Marshal(convert.PlaylistToProto(p))
}

func UnmarshalPlaylist(data []byte) (*domain.Playlist, error) {
	p := &pb.Playlist{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToPlaylist(p), nil
}

func MarshalPeerInfo(p *domain.PeerInfo) ([]byte, error) {
	return proto.Marshal(convert.PeerInfoToProto(p))
}

func UnmarshalPeerInfo(data []byte) (*domain.PeerInfo, error) {
	p := &pb.Peer{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToPeerInfo(p), nil
}

func MarshalPeerScore(s *domain.PeerScore) ([]byte, error) {
	return proto.Marshal(convert.PeerScoreToProto(s))
}

func UnmarshalPeerScore(data []byte) (*domain.PeerScore, error) {
	p := &pb.PeerScore{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToPeerScore(p), nil
}

func MarshalFileStat(f *domain.FileStat) ([]byte, error) {
	return proto.Marshal(convert.FileStatToProto(f))
}

func UnmarshalFileStat(data []byte) (*domain.FileStat, error) {
	p := &pb.FileStat{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToFileStat(p), nil
}

func MarshalLibraryManifest(m *domain.LibraryManifest) ([]byte, error) {
	return proto.Marshal(convert.LibraryManifestToProto(m))
}

func UnmarshalLibraryManifest(data []byte) (*domain.LibraryManifest, error) {
	p := &pb.LibraryManifest{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return convert.ProtoToLibraryManifest(p), nil
}
