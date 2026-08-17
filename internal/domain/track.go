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
	ID               TrackID
	Path             string
	Title            string
	NormalizedTitle  string
	Artist           string
	NormalizedArtist string
	AlbumArtist      string
	Album            string
	NormalizedAlbum  string
	TrackNumber      uint32
	DiscNumber       uint32
	Year             uint32
	Genres           []string
	DurationMs       uint64
	SizeBytes        uint64
	MimeType         string
	Codec            string
	Bitrate          uint32
	SampleRate       uint32
	Channels         uint32
	Lyrics           string
	AddedAt          int64
	ModifiedAt       int64
	PlayCount        uint64
	LastPlayed       int64
	ReplayGainTrack  float32
	ReplayGainAlbum  float32
	ContentHash      string
	IsDuplicate      bool
	DuplicateOf      TrackID
	// PeerID is set only for tracks resolved from a remote peer (transient,
	// never persisted)
	PeerID string
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

func (t *Track) Copy() *Track {
	genres := make([]string, len(t.Genres))
	copy(genres, t.Genres)

	return &Track{
		ID:               t.ID,
		Path:             t.Path,
		Title:            t.Title,
		NormalizedTitle:  t.NormalizedTitle,
		Artist:           t.Artist,
		NormalizedArtist: t.NormalizedArtist,
		AlbumArtist:      t.AlbumArtist,
		Album:            t.Album,
		NormalizedAlbum:  t.NormalizedAlbum,
		TrackNumber:      t.TrackNumber,
		DiscNumber:       t.DiscNumber,
		Year:             t.Year,
		Genres:           genres,
		DurationMs:       t.DurationMs,
		SizeBytes:        t.SizeBytes,
		MimeType:         t.MimeType,
		Codec:            t.Codec,
		Bitrate:          t.Bitrate,
		SampleRate:       t.SampleRate,
		Channels:         t.Channels,
		Lyrics:           t.Lyrics,
		AddedAt:          t.AddedAt,
		ModifiedAt:       t.ModifiedAt,
		PlayCount:        t.PlayCount,
		LastPlayed:       t.LastPlayed,
		ReplayGainTrack:  t.ReplayGainTrack,
		ReplayGainAlbum:  t.ReplayGainAlbum,
		ContentHash:      t.ContentHash,
		IsDuplicate:      t.IsDuplicate,
		DuplicateOf:      t.DuplicateOf,
	}
}

func (t *Track) Duration() time.Duration {
	return time.Duration(t.DurationMs) * time.Millisecond
}

func (t *Track) IncrementPlayCount() {
	t.PlayCount++
	t.LastPlayed = time.Now().Unix()
}
