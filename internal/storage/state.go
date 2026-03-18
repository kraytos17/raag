package storage

import (
	"context"
	"encoding/json"
	"time"
)

type PlayerState struct {
	LastSong   string    `json:"last_song"`
	Position   int       `json:"position"`
	Volume     int       `json:"volume"`
	Queue      []string  `json:"queue"`
	CurrentIdx int       `json:"current_idx"`
	Shuffle    bool      `json:"shuffle"`
	Repeat     bool      `json:"repeat"`
	LastPlayed time.Time `json:"last_played"`
}

const stateKey = "/state/player"

func LoadState(ctx context.Context, store *Store) (*PlayerState, error) {
	data, err := store.Get(ctx, stateKey)
	if err != nil {
		return defaultPlayerState(), nil
	}

	var state PlayerState
	if err := json.Unmarshal(data, &state); err != nil {
		return defaultPlayerState(), nil
	}
	return &state, nil
}

func SaveState(ctx context.Context, store *Store, state *PlayerState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.Put(ctx, stateKey, data)
}

func defaultPlayerState() *PlayerState {
	return &PlayerState{
		Volume:     50,
		Shuffle:    false,
		Repeat:     false,
		LastPlayed: time.Time{},
	}
}
