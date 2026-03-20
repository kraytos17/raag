package domain

import (
	"time"

	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

func TrackToProto(t *Track) *pb.Track {
	if t == nil {
		return nil
	}
	return &pb.Track{
		Id:              string(t.ID),
		Path:            t.Path,
		Title:           t.Title,
		Artist:          t.Artist,
		AlbumArtist:     t.AlbumArtist,
		Album:           t.Album,
		TrackNumber:     t.TrackNumber,
		DiscNumber:      t.DiscNumber,
		Year:            t.Year,
		Genres:          t.Genres,
		DurationMs:      t.DurationMs,
		SizeBytes:       t.SizeBytes,
		MimeType:        t.MimeType,
		Codec:           t.Codec,
		Bitrate:         t.Bitrate,
		SampleRate:      t.SampleRate,
		Channels:        t.Channels,
		Lyrics:          t.Lyrics,
		CoverArt:        t.CoverArt,
		AddedAt:         t.AddedAt,
		ModifiedAt:      t.ModifiedAt,
		PlayCount:       t.PlayCount,
		LastPlayed:      t.LastPlayed,
		ReplayGainTrack: t.ReplayGainTrack,
		ReplayGainAlbum: t.ReplayGainAlbum,
		ContentHash:     t.ContentHash,
	}
}

func ProtoToTrack(p *pb.Track) *Track {
	if p == nil {
		return nil
	}
	return &Track{
		ID:              TrackID(p.Id),
		Path:            p.Path,
		Title:           p.Title,
		Artist:          p.Artist,
		AlbumArtist:     p.AlbumArtist,
		Album:           p.Album,
		TrackNumber:     p.TrackNumber,
		DiscNumber:      p.DiscNumber,
		Year:            p.Year,
		Genres:          p.Genres,
		DurationMs:      p.DurationMs,
		SizeBytes:       p.SizeBytes,
		MimeType:        p.MimeType,
		Codec:           p.Codec,
		Bitrate:         p.Bitrate,
		SampleRate:      p.SampleRate,
		Channels:        p.Channels,
		Lyrics:          p.Lyrics,
		CoverArt:        p.CoverArt,
		AddedAt:         p.AddedAt,
		ModifiedAt:      p.ModifiedAt,
		PlayCount:       p.PlayCount,
		LastPlayed:      p.LastPlayed,
		ReplayGainTrack: p.ReplayGainTrack,
		ReplayGainAlbum: p.ReplayGainAlbum,
		ContentHash:     p.ContentHash,
	}
}

func PlaylistToProto(p *Playlist) *pb.Playlist {
	if p == nil {
		return nil
	}

	trackIDs := make([]string, len(p.TrackIDs))
	for i, id := range p.TrackIDs {
		trackIDs[i] = string(id)
	}
	return &pb.Playlist{
		Id:         string(p.ID),
		Name:       p.Name,
		TrackIds:   trackIDs,
		CreatedAt:  p.CreatedAt,
		ModifiedAt: p.ModifiedAt,
	}
}

func ProtoToPlaylist(p *pb.Playlist) *Playlist {
	if p == nil {
		return nil
	}

	trackIDs := make([]TrackID, len(p.TrackIds))
	for i, id := range p.TrackIds {
		trackIDs[i] = TrackID(id)
	}
	return &Playlist{
		ID:         PlaylistID(p.Id),
		Name:       p.Name,
		TrackIDs:   trackIDs,
		CreatedAt:  p.CreatedAt,
		ModifiedAt: p.ModifiedAt,
	}
}

func PeerInfoToProto(p *PeerInfo) *pb.Peer {
	if p == nil {
		return nil
	}

	addrs := make([]string, len(p.Addrs))
	copy(addrs, p.Addrs)

	var caps *pb.PeerCapabilities
	if p.Capabilities != nil {
		caps = &pb.PeerCapabilities{
			SupportedCodecs:   p.Capabilities.SupportedCodecs,
			SupportedBitrates: p.Capabilities.SupportedBitrates,
			CanTranscode:      p.Capabilities.CanTranscode,
			UploadBandwidth:   p.Capabilities.UploadBandwidth,
			ProtocolVersion:   p.Capabilities.ProtocolVersion,
		}
	}

	var score *pb.PeerScore
	if p.LibrarySummary != nil {
		score = &pb.PeerScore{
			PeerId: string(p.ID),
		}
	}
	return &pb.Peer{
		Id:           string(p.ID),
		Addrs:        addrs,
		Capabilities: caps,
		Score:        score,
	}
}

func ProtoToPeerInfo(p *pb.Peer) *PeerInfo {
	if p == nil {
		return nil
	}

	addrs := make([]string, len(p.Addrs))
	copy(addrs, p.Addrs)
	info := &PeerInfo{
		ID:       PeerID(p.Id),
		Addrs:    addrs,
		LastSeen: time.Now(),
	}
	if p.Capabilities != nil {
		info.Capabilities = &PeerCapabilities{
			SupportedCodecs:   p.Capabilities.SupportedCodecs,
			SupportedBitrates: p.Capabilities.SupportedBitrates,
			CanTranscode:      p.Capabilities.CanTranscode,
			UploadBandwidth:   p.Capabilities.UploadBandwidth,
			ProtocolVersion:   p.Capabilities.ProtocolVersion,
		}
	}
	return info
}

func PeerScoreToProto(s *PeerScore) *pb.PeerScore {
	if s == nil {
		return nil
	}
	return &pb.PeerScore{
		PeerId:       string(s.PeerID),
		AvgLatencyMs: float64(s.AvgLatency.Milliseconds()),
		AvgBandwidth: s.AvgBandwidth,
		FailureCount: int32(s.FailureCount),
		SuccessCount: int32(s.SuccessCount),
		LastSeen:     s.LastSeen.Unix(),
	}
}

func ProtoToPeerScore(p *pb.PeerScore) *PeerScore {
	if p == nil {
		return nil
	}
	return &PeerScore{
		PeerID:       PeerID(p.PeerId),
		AvgLatency:   time.Duration(p.AvgLatencyMs) * time.Millisecond,
		AvgBandwidth: p.AvgBandwidth,
		FailureCount: int(p.FailureCount),
		SuccessCount: int(p.SuccessCount),
		LastSeen:     time.Unix(p.LastSeen, 0),
	}
}

func FileStatToProto(f *FileStat) *pb.FileStat {
	if f == nil {
		return nil
	}
	return &pb.FileStat{
		Path:  f.Path,
		Mtime: f.Mtime,
		Size:  f.Size,
	}
}

func ProtoToFileStat(p *pb.FileStat) *FileStat {
	if p == nil {
		return nil
	}
	return &FileStat{
		Path:  p.Path,
		Mtime: p.Mtime,
		Size:  p.Size,
	}
}

func MarshalTrack(t *Track) ([]byte, error) {
	return proto.Marshal(TrackToProto(t))
}

func UnmarshalTrack(data []byte) (*Track, error) {
	p := &pb.Track{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToTrack(p), nil
}

func MarshalPlaylist(p *Playlist) ([]byte, error) {
	return proto.Marshal(PlaylistToProto(p))
}

func UnmarshalPlaylist(data []byte) (*Playlist, error) {
	p := &pb.Playlist{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToPlaylist(p), nil
}

func MarshalPeerInfo(p *PeerInfo) ([]byte, error) {
	return proto.Marshal(PeerInfoToProto(p))
}

func UnmarshalPeerInfo(data []byte) (*PeerInfo, error) {
	p := &pb.Peer{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToPeerInfo(p), nil
}

func MarshalPeerScore(s *PeerScore) ([]byte, error) {
	return proto.Marshal(PeerScoreToProto(s))
}

func UnmarshalPeerScore(data []byte) (*PeerScore, error) {
	p := &pb.PeerScore{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToPeerScore(p), nil
}

func MarshalFileStat(f *FileStat) ([]byte, error) {
	return proto.Marshal(FileStatToProto(f))
}

func UnmarshalFileStat(data []byte) (*FileStat, error) {
	p := &pb.FileStat{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToFileStat(p), nil
}

func LibraryManifestToProto(m *LibraryManifest) *pb.LibraryManifest {
	if m == nil {
		return nil
	}
	
	trackIDs := make([]string, len(m.TrackIDs))
	for i, id := range m.TrackIDs {
		trackIDs[i] = string(id)
	}
	return &pb.LibraryManifest{
		PeerId:     string(m.PeerID),
		Timestamp:  m.LastUpdated.Unix(),
		TrackCount: int32(m.TrackCount()),
		TrackIds:   trackIDs,
	}
}

func ProtoToLibraryManifest(p *pb.LibraryManifest) *LibraryManifest {
	if p == nil {
		return nil
	}
	
	trackIDs := make([]TrackID, len(p.TrackIds))
	for i, id := range p.TrackIds {
		trackIDs[i] = TrackID(id)
	}
	return &LibraryManifest{
		PeerID:      PeerID(p.PeerId),
		TrackIDs:    trackIDs,
		LastUpdated: time.Unix(p.Timestamp, 0),
	}
}

func MarshalLibraryManifest(m *LibraryManifest) ([]byte, error) {
	return proto.Marshal(LibraryManifestToProto(m))
}

func UnmarshalLibraryManifest(data []byte) (*LibraryManifest, error) {
	p := &pb.LibraryManifest{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return ProtoToLibraryManifest(p), nil
}
