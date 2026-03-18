package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
	"github.com/p-society/raag/internal/playlist"
)

const playlistsPrefix = "/playlists/"

type PlaylistData struct {
	Name  string          `json:"name"`
	Songs []metadata.Song `json:"songs"`
}

func SavePlaylists(ctx context.Context, store *Store, pm *playlist.Manager) error {
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

	for _, pl := range playlists {
		data, err := json.Marshal(pl)
		if err != nil {
			return fmt.Errorf("failed to marshal playlist %s: %w", pl.Name, err)
		}
		key := playlistsPrefix + pl.Name
		if err := store.Put(ctx, key, data); err != nil {
			return fmt.Errorf("failed to save playlist %s: %w", pl.Name, err)
		}
	}

	allPlaylists, err := store.GetAll(ctx, playlistsPrefix)
	if err != nil {
		return err
	}

	savedNames := make(map[string]bool)
	for _, pl := range playlists {
		savedNames[pl.Name] = true
	}

	for key := range allPlaylists {
		name := key[len(playlistsPrefix):]
		if !savedNames[name] {
			if err := store.Delete(ctx, key); err != nil {
				logger.Warnf("failed to delete playlist %s: %v", name, err)
			}
		}
	}

	return nil
}

func LoadPlaylists(ctx context.Context, store *Store, pm *playlist.Manager) error {
	allData, err := store.GetAll(ctx, playlistsPrefix)
	if err != nil {
		return err
	}

	for _, data := range allData {
		var pl PlaylistData
		if err := json.Unmarshal(data, &pl); err != nil {
			logger.Warnf("corrupted playlist data, skipping: %v", err)
			continue
		}
		if err := pm.Create(pl.Name); err != nil {
			continue
		}
		for _, song := range pl.Songs {
			if err := pm.AddSong(pl.Name, song); err != nil {
				logger.Warnf("failed to add song %s to playlist %s: %v", song.Title, pl.Name, err)
			}
		}
	}
	return nil
}
