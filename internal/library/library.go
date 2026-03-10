package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/p-society/raag/internal/metadata"
)

type Library struct {
	Songs    map[string]metadata.Song
	musicDir string
	mutex    sync.RWMutex
}

func NewLibrary(musicDir string) (*Library, error) {
	lib := &Library{
		Songs:    make(map[string]metadata.Song),
		musicDir: musicDir,
	}

	if err := os.MkdirAll(musicDir, 0755); err != nil {
		return nil, fmt.Errorf("error creating music directory: %w", err)
	}
	if err := lib.ScanMusicLibrary(musicDir); err != nil {
		return nil, fmt.Errorf("error scanning music library: %w", err)
	}
	return lib, nil
}

func (l *Library) ScanMusicLibrary(musicDir string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.Songs = make(map[string]metadata.Song)
	info, err := os.Stat(musicDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", musicDir)
	}

	return filepath.Walk(musicDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && isAudioFile(info.Name()) {
			song, err := metadata.ExtractMetadata(path)
			if err != nil {
				return fmt.Errorf("error extracting metadata from %s: %w", path, err)
			}
			l.Songs[strings.ToLower(song.Title)] = song
		}
		return nil
	})
}

func isAudioFile(filename string) bool {
	lower := strings.ToLower(filename)
	exts := []string{".mp3", ".flac", ".wav", ".ogg", ".ogv"}
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func (l *Library) ListSongs() []metadata.Song {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	songs := make([]metadata.Song, 0, len(l.Songs))
	for _, song := range l.Songs {
		songs = append(songs, song)
	}
	return songs
}

func (l *Library) FindSong(title string) (metadata.Song, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	song, exists := l.Songs[strings.ToLower(title)]
	if !exists {
		return metadata.Song{}, fmt.Errorf("song not found: %s", title)
	}
	return song, nil
}

func (l *Library) AddSong(path string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if !isAudioFile(path) {
		return fmt.Errorf("unsupported audio file: %s", path)
	}

	song, err := metadata.ExtractMetadata(path)
	if err != nil {
		return fmt.Errorf("error extracting metadata: %w", err)
	}

	l.Songs[strings.ToLower(song.Title)] = song
	return nil
}

func (l *Library) RemoveSong(title string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	key := strings.ToLower(title)
	if _, exists := l.Songs[key]; !exists {
		return fmt.Errorf("song not found: %s", title)
	}

	delete(l.Songs, key)
	return nil
}

func (l *Library) GetByArtist(artist string) []metadata.Song {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	var songs []metadata.Song
	artistLower := strings.ToLower(artist)
	for _, song := range l.Songs {
		if strings.Contains(strings.ToLower(song.Artist), artistLower) {
			songs = append(songs, song)
		}
	}
	return songs
}

func (l *Library) GetByAlbum(album string) []metadata.Song {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	var songs []metadata.Song
	albumLower := strings.ToLower(album)
	for _, song := range l.Songs {
		if strings.Contains(strings.ToLower(song.Album), albumLower) {
			songs = append(songs, song)
		}
	}
	return songs
}

func (l *Library) GetMusicDir() string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.musicDir
}
