package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"
)

type PlaylistID string

func (p PlaylistID) String() string {
	return string(p)
}

func (p PlaylistID) Validate() bool {
	return p != ""
}

func GeneratePlaylistID() (PlaylistID, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate playlist ID: %w", err)
	}
	return PlaylistID(hex.EncodeToString(b)), nil
}

type Playlist struct {
	ID          PlaylistID
	Name        string
	TrackIDs    []TrackID
	CreatedAt   int64
	ModifiedAt  int64
	Description string
}

func NewPlaylist(name string) (*Playlist, error) {
	if name == "" {
		return nil, ErrInvalidPlaylistName
	}

	id, err := GeneratePlaylistID()
	if err != nil {
		return nil, err
	}
	return &Playlist{
		ID:         id,
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
	p.TrackIDs = slices.DeleteFunc(p.TrackIDs, func(id TrackID) bool { return id == trackID })
	p.ModifiedAt = time.Now().Unix()
}

func (p *Playlist) MoveTrack(from, to int) error {
	if from < 0 || from >= len(p.TrackIDs) {
		return errors.New("invalid from index")
	}
	if to < 0 || to >= len(p.TrackIDs) {
		return errors.New("invalid to index")
	}
	if from == to {
		return nil
	}

	track := p.TrackIDs[from]
	p.TrackIDs = slices.Delete(p.TrackIDs, from, from+1)
	p.TrackIDs = slices.Insert(p.TrackIDs, to, track)
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
