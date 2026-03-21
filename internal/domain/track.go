package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type TrackID string

func (t TrackID) String() string {
	return string(t)
}

func (t TrackID) Validate() bool {
	if t == "" {
		return false
	}
	return len(t) == 64
}

func GenerateTrackID(path string) TrackID {
	hash := sha256.Sum256([]byte(path))
	return TrackID(hex.EncodeToString(hash[:]))
}

type Track struct {
	ID              TrackID
	Path            string
	Title           string
	Artist          string
	AlbumArtist     string
	Album           string
	TrackNumber     uint32
	DiscNumber      uint32
	Year            uint32
	Genres          []string
	DurationMs      uint64
	SizeBytes       uint64
	MimeType        string
	Codec           string
	Bitrate         uint32
	SampleRate      uint32
	Channels        uint32
	Lyrics          string
	CoverArt        []byte
	AddedAt         int64
	ModifiedAt      int64
	PlayCount       uint64
	LastPlayed      int64
	ReplayGainTrack float32
	ReplayGainAlbum float32
	ContentHash     string
}

func NewTrack(path string) *Track {
	return &Track{
		ID:         GenerateTrackID(path),
		Path:       path,
		AddedAt:    time.Now().Unix(),
		ModifiedAt: time.Now().Unix(),
		Genres:     []string{},
	}
}

func (t *Track) Validate() error {
	if t.ID == "" {
		return ErrInvalidTrackID
	}
	if t.Path == "" {
		return ErrInvalidTrack
	}
	if t.DurationMs == 0 {
		return ErrZeroDuration
	}
	return nil
}

func (t *Track) Duration() time.Duration {
	return time.Duration(t.DurationMs) * time.Millisecond
}

func (t *Track) IncrementPlayCount() {
	t.PlayCount++
	t.LastPlayed = time.Now().Unix()
}

func (t *Track) HasCoverArt() bool {
	return len(t.CoverArt) > 0
}
