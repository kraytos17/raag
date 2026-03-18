package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

type PlaylistID string

func (p PlaylistID) String() string {
	return string(p)
}

func (p PlaylistID) Validate() bool {
	return p != ""
}

func GeneratePlaylistID() PlaylistID {
	b := make([]byte, 16)
	rand.Read(b)
	return PlaylistID(hex.EncodeToString(b))
}

type Playlist struct {
	ID          PlaylistID `json:"id"`
	Name        string     `json:"name"`
	TrackIDs    []TrackID  `json:"track_ids"`
	CreatedAt   int64      `json:"created_at"`
	ModifiedAt  int64      `json:"modified_at"`
	Description string     `json:"description"`
}

func NewPlaylist(name string) (*Playlist, error) {
	if name == "" {
		return nil, ErrInvalidPlaylistName
	}
	return &Playlist{
		ID:         GeneratePlaylistID(),
		Name:       name,
		TrackIDs:   []TrackID{},
		CreatedAt:  time.Now().Unix(),
		ModifiedAt: time.Now().Unix(),
	}, nil
}

func (p *Playlist) Validate() error {
	if !p.ID.Validate() {
		return errors.New("invalid playlist ID")
	}
	if p.Name == "" {
		return ErrInvalidPlaylistName
	}
	return nil
}

func (p *Playlist) AddTrack(trackID TrackID) {
	p.TrackIDs = append(p.TrackIDs, trackID)
	p.ModifiedAt = time.Now().Unix()
}

func (p *Playlist) RemoveTrack(trackID TrackID) {
	newTracks := make([]TrackID, 0, len(p.TrackIDs))
	for _, id := range p.TrackIDs {
		if id != trackID {
			newTracks = append(newTracks, id)
		}
	}

	p.TrackIDs = newTracks
	p.ModifiedAt = time.Now().Unix()
}

func (p *Playlist) MoveTrack(from, to int) error {
	if from < 0 || from >= len(p.TrackIDs) {
		return errors.New("invalid from index")
	}
	if to < 0 || to >= len(p.TrackIDs) {
		return errors.New("invalid to index")
	}

	track := p.TrackIDs[from]
	p.TrackIDs = append(p.TrackIDs[:from], p.TrackIDs[from+1:]...)
	p.TrackIDs = append(p.TrackIDs[:to], append([]TrackID{track}, p.TrackIDs[to:]...)...)
	p.ModifiedAt = time.Now().Unix()
	return nil
}

func (p *Playlist) TrackCount() int {
	return len(p.TrackIDs)
}

func (p *Playlist) Clear() {
	p.TrackIDs = []TrackID{}
	p.ModifiedAt = time.Now().Unix()
}
