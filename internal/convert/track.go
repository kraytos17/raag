package convert

import (
	"time"

	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

func TrackToProto(t *domain.Track) *pb.Track {
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

func ProtoToTrack(p *pb.Track) *domain.Track {
	if p == nil {
		return nil
	}
	return &domain.Track{
		ID:              domain.TrackID(p.Id),
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

func PlaylistToProto(p *domain.Playlist) *pb.Playlist {
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

func ProtoToPlaylist(p *pb.Playlist) *domain.Playlist {
	if p == nil {
		return nil
	}

	trackIDs := make([]domain.TrackID, len(p.TrackIds))
	for i, id := range p.TrackIds {
		trackIDs[i] = domain.TrackID(id)
	}
	return &domain.Playlist{
		ID:         domain.PlaylistID(p.Id),
		Name:       p.Name,
		TrackIDs:   trackIDs,
		CreatedAt:  p.CreatedAt,
		ModifiedAt: p.ModifiedAt,
	}
}

func PeerInfoToProto(p *domain.PeerInfo) *pb.Peer {
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
	if p.Score != nil {
		score = &pb.PeerScore{
			PeerId:       string(p.ID),
			AvgLatencyMs: float64(p.Score.AvgLatency.Milliseconds()),
			AvgBandwidth: p.Score.AvgBandwidth,
			FailureCount: int32(p.Score.FailureCount),
			SuccessCount: int32(p.Score.SuccessCount),
			LastSeen:     p.Score.LastSeen.Unix(),
			Score:        p.Score.Score(),
		}
	}
	return &pb.Peer{
		Id:           string(p.ID),
		Addrs:        addrs,
		Capabilities: caps,
		Score:        score,
	}
}

func ProtoToPeerInfo(p *pb.Peer) *domain.PeerInfo {
	if p == nil {
		return nil
	}

	addrs := make([]string, len(p.Addrs))
	copy(addrs, p.Addrs)
	info := &domain.PeerInfo{
		ID:       domain.PeerID(p.Id),
		Addrs:    addrs,
		LastSeen: time.Now(),
	}
	if p.Capabilities != nil {
		info.Capabilities = &domain.PeerCapabilities{
			SupportedCodecs:   p.Capabilities.SupportedCodecs,
			SupportedBitrates: p.Capabilities.SupportedBitrates,
			CanTranscode:      p.Capabilities.CanTranscode,
			UploadBandwidth:   p.Capabilities.UploadBandwidth,
			ProtocolVersion:   p.Capabilities.ProtocolVersion,
		}
	}
	return info
}

func PeerScoreToProto(s *domain.PeerScore) *pb.PeerScore {
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

func ProtoToPeerScore(p *pb.PeerScore) *domain.PeerScore {
	if p == nil {
		return nil
	}
	return &domain.PeerScore{
		PeerID:       domain.PeerID(p.PeerId),
		AvgLatency:   time.Duration(p.AvgLatencyMs) * time.Millisecond,
		AvgBandwidth: p.AvgBandwidth,
		FailureCount: int(p.FailureCount),
		SuccessCount: int(p.SuccessCount),
		LastSeen:     time.Unix(p.LastSeen, 0),
	}
}

func FileStatToProto(f *domain.FileStat) *pb.FileStat {
	if f == nil {
		return nil
	}
	return &pb.FileStat{
		Path:  f.Path,
		Mtime: f.Mtime,
		Size:  f.Size,
	}
}

func ProtoToFileStat(p *pb.FileStat) *domain.FileStat {
	if p == nil {
		return nil
	}
	return &domain.FileStat{
		Path:  p.Path,
		Mtime: p.Mtime,
		Size:  p.Size,
	}
}

func LibraryManifestToProto(m *domain.LibraryManifest) *pb.LibraryManifest {
	if m == nil {
		return nil
	}

	trackIDs := make([]string, len(m.TrackIDs))
	for i, id := range m.TrackIDs {
		trackIDs[i] = string(id)
	}
	return &pb.LibraryManifest{
		PeerId:    string(m.PeerID),
		Timestamp: m.LastUpdated.Unix(),
		TrackIds:  trackIDs,
	}
}

func ProtoToLibraryManifest(p *pb.LibraryManifest) *domain.LibraryManifest {
	if p == nil {
		return nil
	}

	trackIDs := make([]domain.TrackID, len(p.TrackIds))
	for i, id := range p.TrackIds {
		trackIDs[i] = domain.TrackID(id)
	}
	return &domain.LibraryManifest{
		PeerID:      domain.PeerID(p.PeerId),
		TrackIDs:    trackIDs,
		LastUpdated: time.Unix(p.Timestamp, 0),
	}
}
