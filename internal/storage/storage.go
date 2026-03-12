package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	appconfig "github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
	"github.com/p-society/raag/internal/playlist"
)

type Storage struct {
	configDir string
}

type PlaylistData struct {
	Name  string          `json:"name"`
	Songs []metadata.Song `json:"songs"`
}

type StorageData struct {
	Playlists []PlaylistData `json:"playlists"`
}

func New() (*Storage, error) {
	configDir, err := appconfig.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("error creating config directory: %w", err)
	}
	return &Storage{configDir: configDir}, nil
}

func (s *Storage) SavePlaylists(pm *playlist.Manager) error {
	playlistNames := pm.List()
	playlists := make([]PlaylistData, 0, len(playlistNames))
	for _, name := range playlistNames {
		songs, err := pm.GetSongs(name)
		if err != nil {
			continue
		}

		playlists = append(playlists, PlaylistData{
			Name:  name,
			Songs: songs,
		})
	}

	data := StorageData{Playlists: playlists}
	filePath := filepath.Join(s.configDir, "playlists.json")
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error creating playlists file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("error encoding playlists: %w", err)
	}
	return nil
}

func (s *Storage) LoadPlaylists(pm *playlist.Manager) error {
	filePath := filepath.Join(s.configDir, "playlists.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error reading playlists file: %w", err)
	}
	if len(data) == 0 {
		logger.Warnf("playlists file is empty, starting with no playlists")
		return nil
	}

	var storageData StorageData
	if err := json.Unmarshal(data, &storageData); err != nil {
		logger.Warnf("corrupted playlists file, resetting error=%v", err)
		return nil
	}
	for _, pl := range storageData.Playlists {
		if err := pm.Create(pl.Name); err != nil {
			continue
		}
		for _, song := range pl.Songs {
			pm.AddSong(pl.Name, song)
		}
	}
	return nil
}
