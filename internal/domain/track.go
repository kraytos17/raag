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
	ID              TrackID  `json:"id"`
	Path            string   `json:"path"`
	Title           string   `json:"title"`
	Artist          string   `json:"artist"`
	AlbumArtist     string   `json:"album_artist"`
	Album           string   `json:"album"`
	TrackNumber     uint32   `json:"track_number"`
	DiscNumber      uint32   `json:"disc_number"`
	Year            uint32   `json:"year"`
	Genres          []string `json:"genres"`
	DurationMs      uint64   `json:"duration_ms"`
	SizeBytes       uint64   `json:"size_bytes"`
	MimeType        string   `json:"mime_type"`
	Codec           string   `json:"codec"`
	Bitrate         uint32   `json:"bitrate"`
	SampleRate      uint32   `json:"sample_rate"`
	Channels        uint32   `json:"channels"`
	Lyrics          string   `json:"lyrics"`
	CoverArt        []byte   `json:"cover_art"`
	AddedAt         int64    `json:"added_at"`
	ModifiedAt      int64    `json:"modified_at"`
	PlayCount       uint64   `json:"play_count"`
	LastPlayed      int64    `json:"last_played"`
	ReplayGainTrack float32  `json:"replay_gain_track"`
	ReplayGainAlbum float32  `json:"replay_gain_album"`
	ContentHash     string   `json:"content_hash"`
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
		return ErrFileNotAccessible
	}
	if t.DurationMs == 0 {
		return ErrFileNotAccessible
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
