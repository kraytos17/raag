package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/p-society/raag/internal/domain"
)

var audioExtensions = []string{".mp3", ".flac", ".ogg", ".wav", ".m4a", ".aac", ".opus", ".wma"}

func isAudioExt(ext string) bool {
	return slices.Contains(audioExtensions, ext)
}

type Scanner struct {
	paths         []string
	supportedExts map[string]bool
	onProgress    func(scanProgress)
	onTrack       func(*domain.Track)
	onError       func(error)
}

func NewScanner(paths []string) *Scanner {
	exts := make(map[string]bool)
	for _, ext := range audioExtensions {
		exts[ext] = true
	}
	return &Scanner{
		paths:         paths,
		supportedExts: exts,
	}
}

type scanProgress struct {
	Phase       string
	TotalFound  int
	CurrentFile string
}

func (s *Scanner) OnProgress(fn func(scanProgress)) {
	s.onProgress = fn
}

func (s *Scanner) OnTrack(fn func(*domain.Track)) {
	s.onTrack = fn
}

func (s *Scanner) OnError(fn func(error)) {
	s.onError = fn
}

func (s *Scanner) Scan(ctx context.Context) ([]string, error) {
	var files []string
	for _, path := range s.paths {
		if err := filepath.WalkDir(path, func(walkPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if d.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(walkPath))
			if s.supportedExts[ext] {
				files = append(files, walkPath)
				if s.onProgress != nil {
					s.onProgress(scanProgress{
						Phase:      "discovery",
						TotalFound: len(files),
					})
				}
			}
			return nil
		}); err != nil {
			if s.onError != nil {
				s.onError(err)
			}
		}
	}
	return files, nil
}

type LibraryScanner struct {
	libraryRepo LibraryRepository
	index       SearchIndex
	bus         domain.EventBus
	paths       []string
}

func NewLibraryScanner(
	libraryRepo LibraryRepository,
	index SearchIndex,
	bus domain.EventBus,
	paths []string,
) *LibraryScanner {
	return &LibraryScanner{
		libraryRepo: libraryRepo,
		index:       index,
		bus:         bus,
		paths:       paths,
	}
}

func (s *LibraryScanner) Scan(ctx context.Context) (int, error) {
	startTime := time.Now()
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanStarted, domain.ScanStartedPayload{
		Paths:     s.paths,
		StartTime: startTime,
	}))

	var totalScanned int
	var totalAdded int
	for _, scanPath := range s.paths {
		scanned, added, err := s.scanDirectory(ctx, scanPath)
		if err != nil {
			slog.Error("scan directory failed", "path", scanPath, "error", err)
			continue
		}

		totalScanned += scanned
		totalAdded += added
	}

	duration := time.Since(startTime)
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanComplete, domain.ScanCompletePayload{
		Scanned:  totalScanned,
		Added:    totalAdded,
		Removed:  0,
		Duration: duration,
		Errors:   []string{},
	}))
	return totalScanned, nil
}

func (s *LibraryScanner) ScanIncremental(ctx context.Context) (added int, modified int, removed int, err error) {
	currentStats := make(map[string]*domain.FileStat)
	for _, scanPath := range s.paths {
		files := s.walkDirectory(scanPath)
		for _, file := range files {
			stat, err := s.getFileStat(file)
			if err != nil {
				continue
			}
			currentStats[file] = stat
		}
	}

	prevStats, err := s.libraryRepo.LoadFileStats(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	for path, current := range currentStats {
		previous, exists := prevStats[path]
		if !exists {
			added++
		} else if current.Changed(previous) {
			modified++
		}
	}
	for path := range prevStats {
		if _, exists := currentStats[path]; !exists {
			removed++
		}
	}
	if added > 0 || modified > 0 {
		if _, err := s.Scan(ctx); err != nil {
			return 0, 0, 0, err
		}
		if err := s.libraryRepo.SaveFileStats(ctx, currentStats); err != nil {
			return 0, 0, 0, err
		}
	}
	return added, modified, removed, nil
}

func (s *LibraryScanner) scanDirectory(ctx context.Context, dirPath string) (int, int, error) {
	var scanned int
	var added int
	files := s.walkDirectory(dirPath)
	for i, file := range files {
		select {
		case <-ctx.Done():
			return scanned, added, ctx.Err()
		default:
		}

		s.bus.Publish(ctx, domain.NewEvent(domain.EventScanProgress, ScanProgressPayload{
			Scanned:     i + 1,
			Total:       len(files),
			CurrentFile: file,
			Phase:       ScanPhaseParsing,
		}))

		track, err := s.parseFile(file)
		if err != nil {
			slog.Warn("failed to parse file", "file", file, "error", err)
			continue
		}
		if err := s.libraryRepo.Save(ctx, track); err != nil {
			slog.Warn("failed to save track", "track", track.ID, "error", err)
			continue
		}
		if err := s.index.Index(ctx, track); err != nil {
			slog.Warn("failed to index track", "track", track.ID, "error", err)
		}

		scanned++
		added++
	}
	return scanned, added, nil
}

func (s *LibraryScanner) walkDirectory(dirPath string) []string {
	var files []string
	if err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if isAudioExt(ext) {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		_ = err
	}
	return files
}

func (s *LibraryScanner) parseFile(path string) (*domain.Track, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	metadata, err := tag.ReadFrom(file)
	if err != nil {
		return nil, err
	}

	track := domain.NewTrack(path)
	track.Title = metadata.Title()
	track.Artist = metadata.Artist()
	track.AlbumArtist = metadata.AlbumArtist()
	track.Album = metadata.Album()
	trackNum, _ := metadata.Track()
	track.TrackNumber = uint32(trackNum)
	discNum, _ := metadata.Disc()
	track.DiscNumber = uint32(discNum)
	track.Year = uint32(metadata.Year())

	if genre := metadata.Genre(); genre != "" {
		track.Genres = []string{genre}
	}

	track.Lyrics = metadata.Lyrics()
	track.SizeBytes = uint64(stat.Size())
	track.MimeType = mimeType(filepath.Ext(path))
	track.Codec = codecFromExtension(filepath.Ext(path))
	track.ModifiedAt = stat.ModTime().Unix()
	hash, err := computeContentHash(path)
	if err != nil {
		slog.Warn("failed to compute content hash", "path", path, "error", err)
	} else {
		track.ContentHash = hash
	}

	if cover := extractCoverArt(metadata); len(cover) > 0 {
		track.CoverArt = cover
	}
	return track, nil
}

func mimeType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".opus":
		return "audio/opus"
	case ".wma":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}

func codecFromExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "mp3"
	case ".flac":
		return "flac"
	case ".ogg":
		return "vorbis"
	case ".wav":
		return "pcm"
	case ".m4a", ".aac":
		return "aac"
	case ".opus":
		return "opus"
	case ".wma":
		return "wma"
	default:
		return "unknown"
	}
}

func computeContentHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractCoverArt(m tag.Metadata) []byte {
	if m == nil {
		return nil
	}

	picture := m.Picture()
	if picture == nil {
		return nil
	}

	data := picture.Data
	if len(data) > 256*1024 {
		data = resizeCoverArt(data)
	}
	return data
}

func resizeCoverArt(data []byte) []byte {
	// TODO: Implement proper image resizing using an image processing library
	// For now, truncate to max size to prevent memory bloat
	// This is not ideal as it corrupts the image, but prevents unbounded memory usage
	maxSize := 256 * 1024
	if len(data) > maxSize {
		return data[:maxSize]
	}
	return data
}

func (s *LibraryScanner) getFileStat(path string) (*domain.FileStat, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	hash, err := computeContentHash(path)
	if err != nil {
		return nil, err
	}

	return &domain.FileStat{
		Path:  path,
		Mtime: stat.ModTime().Unix(),
		Size:  stat.Size(),
		Hash:  hash,
	}, nil
}
