package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/p-society/raag/internal/config"
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

func LoadState() (*PlayerState, error) {
	filePath, err := config.StatePath()
	if err != nil {
		return nil, err
	}

	cleanPath := filepath.Clean(filePath)
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}

	relPath, err := filepath.Rel(dir, cleanPath)
	if err != nil {
		return nil, fmt.Errorf("error computing relative path: %w", err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &PlayerState{
				Volume:     50,
				Shuffle:    false,
				Repeat:     false,
				LastPlayed: time.Time{},
			}, nil
		}
		return nil, fmt.Errorf("error opening config root: %w", err)
	}

	stat, err := root.Stat(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &PlayerState{
				Volume:     50,
				Shuffle:    false,
				Repeat:     false,
				LastPlayed: time.Time{},
			}, nil
		}
		return nil, fmt.Errorf("error checking file: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	data, err := root.ReadFile(relPath)
	if err != nil {
		return nil, fmt.Errorf("error reading state file: %w", err)
	}

	var state PlayerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("error decoding state: %w", err)
	}
	return &state, nil
}

func SaveState(state *PlayerState) error {
	filePath, err := config.StatePath()
	if err != nil {
		return err
	}
	return WriteJSONAtomic(filePath, state)
}
