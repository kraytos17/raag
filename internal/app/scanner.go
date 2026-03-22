package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/hashing"
	"golang.org/x/image/draw"
	"golang.org/x/sync/errgroup"
)

var AudioExtensions = []string{".mp3", ".flac", ".ogg", ".wav", ".m4a", ".aac", ".opus", ".wma"}

type ScanProgress struct {
	Scanned     int
	Total       int
	CurrentFile string
	Phase       ScanPhase
}

type ScanPhase string

const (
	ScanPhaseWalking ScanPhase = "walking"
	ScanPhaseParsing ScanPhase = "parsing"
)

func WalkAudioFiles(ctx context.Context, dirPath string, yield func(string) bool) error {
	return filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !d.IsDir() && isAudioExt(filepath.Ext(path)) {
			if !yield(path) {
				return fs.SkipAll
			}
		}
		return nil
	})
}

func isAudioExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp3", ".flac", ".ogg", ".wav", ".m4a", ".aac", ".opus", ".wma":
		return true
	}
	return false
}

type LibraryScanner struct {
	libraryRepo LibraryRepository
	statStore   FileStatStore
	index       SearchIndex
	bus         domain.EventBus
	paths       []string
	onProgress  func(ScanProgress)
	hashWorker  *hashing.HashWorker
}

func NewLibraryScanner(
	libraryRepo LibraryRepository,
	statStore FileStatStore,
	index SearchIndex,
	bus domain.EventBus,
	paths []string,
) *LibraryScanner {
	return &LibraryScanner{
		libraryRepo: libraryRepo,
		statStore:   statStore,
		index:       index,
		bus:         bus,
		paths:       paths,
	}
}

func (s *LibraryScanner) EnableHashing(workers int) {
	s.hashWorker = hashing.NewHashWorker(workers)
	s.hashWorker.Start()
}

func (s *LibraryScanner) HashContent(path string) string {
	if s.hashWorker == nil {
		return ""
	}
	resultCh := s.hashWorker.Submit(path)
	return <-resultCh
}

func (s *LibraryScanner) Close() error {
	if s.hashWorker != nil {
		s.hashWorker.Close()
	}
	return nil
}

func (s *LibraryScanner) Name() string {
	return "library-scanner"
}

func (s *LibraryScanner) Start(_ context.Context) error {
	return nil
}

func (s *LibraryScanner) Stop(_ context.Context) error {
	return s.Close()
}

func (s *LibraryScanner) OnProgress(fn func(ScanProgress)) {
	s.onProgress = fn
}

func (s *LibraryScanner) LibraryRepo() LibraryRepository {
	return s.libraryRepo
}

func (s *LibraryScanner) Index() SearchIndex {
	return s.index
}

func (s *LibraryScanner) AddFile(ctx context.Context, path string) error {
	track, err := s.parseFile(path)
	if err != nil {
		return err
	}
	return s.indexTrack(ctx, track)
}

func (s *LibraryScanner) RemoveFile(ctx context.Context, path string) error {
	track, err := s.libraryRepo.FindByPath(ctx, path)
	if err != nil {
		if errors.Is(err, domain.ErrTrackNotFound) {
			return nil
		}
		return err
	}
	if err := s.libraryRepo.Delete(ctx, track.ID); err != nil {
		return err
	}
	if err := s.index.Delete(ctx, track.ID); err != nil {
		slog.Warn("failed to delete track from index", "id", track.ID, "error", err)
	}
	return nil
}

func (s *LibraryScanner) Scan(ctx context.Context) (int, error) {
	startTime := time.Now()
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanStarted, domain.ScanStartedPayload{
		Paths:     s.paths,
		StartTime: startTime,
	}))

	var totalScanned int
	var totalAdded int
	var totalRemoved int

	existingPathsList, err := s.libraryRepo.ListAllPaths(ctx)
	if err != nil {
		slog.Warn("failed to list existing paths", "error", err)
	}

	existingPaths := make(map[string]bool, len(existingPathsList))
	for _, path := range existingPathsList {
		existingPaths[path] = true
	}
	for _, scanPath := range s.paths {
		scanned, added, removed, err := s.scanDirectory(ctx, scanPath, existingPaths)
		if err != nil {
			slog.Error("scan directory failed", "path", scanPath, "error", err)
			continue
		}

		totalScanned += scanned
		totalAdded += added
		totalRemoved += removed
	}

	duration := time.Since(startTime)
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanComplete, domain.ScanCompletePayload{
		Scanned:  totalScanned,
		Added:    totalAdded,
		Removed:  totalRemoved,
		Duration: duration,
		Errors:   []string{},
	}))
	return totalScanned, nil
}

func (s *LibraryScanner) ScanIncremental(ctx context.Context) (added int, modified int, removed int, err error) {
	currentStats := make(map[string]*domain.FileStat)
	for _, scanPath := range s.paths {
		var files []string
		if err := WalkAudioFiles(ctx, scanPath, func(path string) bool {
			files = append(files, path)
			return true
		}); err != nil {
			slog.Warn("failed to walk directory", "path", scanPath, "error", err)
			continue
		}
		for _, file := range files {
			stat, err := s.getFileStat(file)
			if err != nil {
				continue
			}
			currentStats[file] = stat
		}
	}

	prevStats, err := s.statStore.LoadFileStats(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	existingTracks, err := s.libraryRepo.ListAll(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	trackByPath := make(map[string]*domain.Track)
	for _, track := range existingTracks {
		trackByPath[track.Path] = track
	}
	for path, current := range currentStats {
		previous, exists := prevStats[path]
		if !exists {
			track, err := s.parseFile(path)
			if err != nil {
				slog.Warn("failed to parse new file", "path", path, "error", err)
				continue
			}
			if err := s.indexTrack(ctx, track); err != nil {
				slog.Warn("failed to save new track", "path", path, "error", err)
				continue
			}
			added++
		} else if current.Changed(previous) {
			track, err := s.parseFile(path)
			if err != nil {
				slog.Warn("failed to parse modified file", "path", path, "error", err)
				continue
			}

			existing, _ := s.libraryRepo.FindByPath(ctx, path)
			if existing != nil {
				track.ID = existing.ID
			}
			if err := s.updateTrack(ctx, track); err != nil {
				slog.Warn("failed to save modified track", "path", path, "error", err)
				continue
			}
			modified++
		}
	}
	for path, track := range trackByPath {
		if _, exists := currentStats[path]; !exists {
			removed++
			if err := s.libraryRepo.Delete(ctx, track.ID); err != nil {
				slog.Warn("failed to delete removed track", "path", path, "error", err)
			}
			if err := s.index.Delete(ctx, track.ID); err != nil {
				slog.Warn("failed to delete track from index", "path", path, "error", err)
			}
		}
	}
	if err := s.statStore.SaveFileStats(ctx, currentStats); err != nil {
		return 0, 0, 0, err
	}
	return added, modified, removed, nil
}

func (s *LibraryScanner) scanDirectory(ctx context.Context, dirPath string, existingPaths map[string]bool) (int, int, int, error) {
	var scanned int
	var added int
	var removed int

	var files []string
	if err := WalkAudioFiles(ctx, dirPath, func(path string) bool {
		files = append(files, path)
		return true
	}); err != nil {
		return 0, 0, 0, fmt.Errorf("walk directory: %w", err)
	}

	foundPaths := make(map[string]bool)
	newFiles := make([]string, 0, len(files))
	for _, file := range files {
		foundPaths[file] = true
		if !existingPaths[file] {
			newFiles = append(newFiles, file)
		} else {
			scanned++
		}
	}

	const maxConcurrency = 8
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	parsedTracks := make([]*domain.Track, 0, len(newFiles))
	for i, file := range newFiles {
		g.Go(func() error {
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}

			if s.onProgress != nil {
				s.onProgress(ScanProgress{
					Scanned:     i + 1,
					Total:       len(newFiles),
					CurrentFile: file,
					Phase:       ScanPhaseParsing,
				})
			}

			track, err := s.parseFile(file)
			if err != nil {
				slog.Warn("failed to parse file", "file", file, "error", err)
				return nil
			}

			mu.Lock()
			parsedTracks = append(parsedTracks, track)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		slog.Warn("scan directory errgroup failed", "path", dirPath, "error", err)
	}
	for _, track := range parsedTracks {
		if err := s.indexTrack(ctx, track); err != nil {
			slog.Warn("failed to save track", "track", track.ID, "error", err)
			continue
		}

		scanned++
		added++
	}
	for path := range existingPaths {
		if !strings.HasPrefix(path, dirPath) || foundPaths[path] {
			continue
		}

		track, err := s.libraryRepo.FindByPath(ctx, path)
		if err != nil {
			slog.Warn("scan: orphaned path not in repo", "path", path, "error", err)
			continue
		}
		if err := s.libraryRepo.Delete(ctx, track.ID); err != nil {
			slog.Warn("scan: failed to delete orphaned track", "path", path, "error", err)
			continue
		}
		if err := s.index.Delete(ctx, track.ID); err != nil {
			slog.Warn("scan: failed to remove from index", "id", track.ID, "error", err)
		}
		removed++
	}
	return scanned, added, removed, nil
}

func computeSyncHash(file *os.File) string {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		slog.Warn("failed to hash file content", "error", err)
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
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

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		slog.Warn("failed to seek file for hashing", "path", path, "error", err)
	} else if s.hashWorker != nil {
		hashTimer := time.NewTimer(5 * time.Second)
		defer hashTimer.Stop()

		resultCh := s.hashWorker.Submit(path)
		select {
		case hash := <-resultCh:
			if !hashTimer.Stop() {
				<-hashTimer.C
			}
			track.ContentHash = hash
		case <-hashTimer.C:
			slog.Warn("hash worker timed out, using sync fallback", "path", path)
			if _, err := file.Seek(0, io.SeekStart); err == nil {
				track.ContentHash = computeSyncHash(file)
			}
		}
	} else {
		track.ContentHash = computeSyncHash(file)
	}
	if cover := extractCoverArt(metadata); len(cover) > 0 {
		track.CoverArt = cover
	}
	return track, nil
}

func (s *LibraryScanner) indexTrack(ctx context.Context, track *domain.Track) error {
	if err := s.libraryRepo.Save(ctx, track); err != nil {
		return err
	}
	if err := s.index.Index(ctx, track); err != nil {
		slog.Warn("failed to index track", "track", track.ID, "error", err)
	}
	return nil
}

func (s *LibraryScanner) updateTrack(ctx context.Context, track *domain.Track) error {
	if err := s.libraryRepo.Save(ctx, track); err != nil {
		return err
	}
	if err := s.index.Delete(ctx, track.ID); err != nil {
		slog.Warn("failed to delete old index entry", "id", track.ID, "error", err)
	}
	if err := s.index.Index(ctx, track); err != nil {
		slog.Warn("failed to re-index track", "id", track.ID, "error", err)
	}
	return nil
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
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Warn("cover art decode failed, discarding", "error", err)
		return nil
	}

	bounds := img.Bounds()
	const maxDim = 500
	scale := 1.0
	if bounds.Dx() > maxDim || bounds.Dy() > maxDim {
		sx := float64(maxDim) / float64(bounds.Dx())
		sy := float64(maxDim) / float64(bounds.Dy())
		if sx < sy {
			scale = sx
		} else {
			scale = sy
		}
	}

	newW := int(float64(bounds.Dx()) * scale)
	newH := int(float64(bounds.Dy()) * scale)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		slog.Warn("cover art encode failed", "error", err)
		return nil
	}
	return buf.Bytes()
}

func (s *LibraryScanner) getFileStat(path string) (*domain.FileStat, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &domain.FileStat{
		Path:  path,
		Mtime: stat.ModTime().Unix(),
		Size:  stat.Size(),
	}, nil
}
