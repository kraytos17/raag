package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PlayerState struct {
	LastSong   string   `json:"last_song"`
	Position   int      `json:"position"`
	Volume     int      `json:"volume"`
	Queue      []string `json:"queue"`
	CurrentIdx int      `json:"current_idx"`
}

func (s *Storage) LoadState() (*PlayerState, error) {
	filePath := filepath.Join(s.configDir, "state.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &PlayerState{
				Volume: 50,
			}, nil
		}
		return nil, fmt.Errorf("error reading state file: %w", err)
	}

	var state PlayerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("error decoding state: %w", err)
	}
	return &state, nil
}

func (s *Storage) SaveState(state *PlayerState) error {
	filePath := filepath.Join(s.configDir, "state.json")
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error creating state file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		return fmt.Errorf("error encoding state: %w", err)
	}
	return nil
}
