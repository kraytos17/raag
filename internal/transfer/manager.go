package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
)

// TransferStatus represents the lifecycle state of a single song transfer.
type TransferStatus int

const (
	TransferPending    TransferStatus = iota // queued, not yet started
	TransferActive                           // blocks being fetched
	TransferAssembling                       // all blocks received, writing final file
	TransferDone                             // file available on disk
	TransferFailed                           // unrecoverable error
)

func (s TransferStatus) String() string {
	switch s {
	case TransferPending:
		return "pending"
	case TransferActive:
		return "active"
	case TransferAssembling:
		return "assembling"
	case TransferDone:
		return "done"
	case TransferFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Transfer tracks the download of a single song via chunked block exchange.
type Transfer struct {
	ID          string
	Song        metadata.Song
	Status      TransferStatus
	TotalBlocks int
	DoneBlocks  int
	Err         error
	StartedAt   time.Time
	FinishedAt  time.Time
	OutputPath  string
	// CIDs holds the per-block CID indexed by block index.
	CIDs []cid.Cid

	mu sync.RWMutex
}

func (t *Transfer) setStatus(s TransferStatus) {
	t.mu.Lock()
	t.Status = s
	t.mu.Unlock()
}

func (t *Transfer) progress() (done, total int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.DoneBlocks, t.TotalBlocks
}

// Manager coordinates chunked block transfers for multiple songs concurrently.
// It owns the ProviderManager (DHT announcements + local block serving) and
// the Chunker.
type Manager struct {
	host     host.Host
	routing  routing.Routing
	provider *ProviderManager
	chunker  *Chunker
	musicDir string

	mu        sync.RWMutex
	transfers map[string]*Transfer // keyed by Transfer.ID
}

// NewManager creates a TransferManager.
// musicDir is the directory where completed downloads are saved.
// r may be nil initially; call SetRouting once the DHT is ready.
func NewManager(h host.Host, r routing.Routing, musicDir string) *Manager {
	return &Manager{
		host:      h,
		routing:   r,
		provider:  NewProviderManager(h, r),
		chunker:   NewChunker(DefaultBlockSize),
		musicDir:  musicDir,
		transfers: make(map[string]*Transfer),
	}
}

// SetRouting updates the routing backend (e.g. after DHT initialization).
// It is safe to call after construction and before any Provide/Download calls.
func (m *Manager) SetRouting(r routing.Routing) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routing = r
	m.provider = NewProviderManager(m.host, r)
}

// Provide announces all blocks of a local file to the DHT so remote peers
// can fetch them. Call this when a new song is added to the library.
func (m *Manager) Provide(ctx context.Context, song metadata.Song) error {
	f, err := os.Open(song.Path)
	if err != nil {
		return fmt.Errorf("open file for providing: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	numBlocks := m.chunker.CalculateBlocks(info.Size())
	if numBlocks == 0 {
		return nil
	}

	announced := 0
	for i := range numBlocks {
		block, err := m.chunker.ReadBlock(f, i)
		if err != nil {
			return fmt.Errorf("read block %d: %w", i, err)
		}
		if _, err := m.provider.Provide(ctx, block.Data); err != nil {
			logger.Warnf("Failed to announce block %d for song=%s: %v", i, song.Title, err)
			continue
		}
		announced++
	}

	logger.Infof("Announced %d/%d blocks for song=%s", announced, numBlocks, song.Title)
	return nil
}

// ServeBlock looks up and returns the raw bytes for a block identified by its
// CID bytes. Returns nil, false if the block is not locally held.
func (m *Manager) ServeBlock(cidBytes []byte) ([]byte, bool) {
	c, err := cid.Cast(cidBytes)
	if err != nil {
		return nil, false
	}
	return m.provider.Get(c)
}

// Download fetches a remote song via chunked block exchange and writes it to
// musicDir. It returns the local file path on success.
// blockCIDs is the ordered list of CIDs for each block (obtained out-of-band,
// e.g. via GossipSub library announce or the share protocol metadata).
func (m *Manager) Download(ctx context.Context, song metadata.Song, blockCIDs []cid.Cid) (*Transfer, error) {
	id := fmt.Sprintf("%s-%d", song.Hash, time.Now().UnixNano())
	ext := filepath.Ext(song.Path)
	if ext == "" {
		ext = ".bin"
	}

	safeTitle := sanitizeName(song.Title)
	outputPath := filepath.Join(m.musicDir, fmt.Sprintf("%s%s", safeTitle, ext))
	t := &Transfer{
		ID:          id,
		Song:        song,
		Status:      TransferPending,
		TotalBlocks: len(blockCIDs),
		CIDs:        blockCIDs,
		StartedAt:   time.Now(),
		OutputPath:  outputPath,
	}

	m.mu.Lock()
	m.transfers[id] = t
	m.mu.Unlock()

	go m.runDownload(ctx, t)
	return t, nil
}

// runDownload orchestrates parallel block fetching and final assembly.
func (m *Manager) runDownload(ctx context.Context, t *Transfer) {
	t.setStatus(TransferActive)

	blocks := make([][]byte, t.TotalBlocks)
	sem := make(chan struct{}, constants.TransferConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, c := range t.CIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, blockCID cid.Cid) {
			defer wg.Done()
			defer func() { <-sem }()

			data, err := m.provider.FetchBlock(ctx, blockCID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				logger.Warnf("Block %d fetch failed cid=%s: %v", idx, blockCID, err)
				if firstErr == nil {
					firstErr = fmt.Errorf("block %d: %w", idx, err)
				}
				return
			}

			blocks[idx] = data
			t.mu.Lock()
			t.DoneBlocks++
			t.mu.Unlock()
		}(i, c)
	}

	wg.Wait()
	if firstErr != nil {
		t.mu.Lock()
		t.Status = TransferFailed
		t.Err = firstErr
		t.FinishedAt = time.Now()
		t.mu.Unlock()
		logger.Errorf("Transfer failed for song=%s: %v", t.Song.Title, firstErr)
		return
	}

	t.setStatus(TransferAssembling)
	if err := m.assembleFile(t, blocks); err != nil {
		t.mu.Lock()
		t.Status = TransferFailed
		t.Err = err
		t.FinishedAt = time.Now()
		t.mu.Unlock()
		logger.Errorf("Assembly failed for song=%s: %v", t.Song.Title, err)
		return
	}

	t.mu.Lock()
	t.Status = TransferDone
	t.FinishedAt = time.Now()
	t.mu.Unlock()
	logger.Infof("Transfer complete song=%s path=%s", t.Song.Title, t.OutputPath)
}

func (m *Manager) assembleFile(t *Transfer, blocks [][]byte) error {
	if err := os.MkdirAll(m.musicDir, 0o750); err != nil {
		return fmt.Errorf("create music dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(m.musicDir, "*.part")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		// clean up temp file if rename didn't happen
		if _, err := os.Stat(tmpPath); err == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	for i, data := range blocks {
		if data == nil {
			return fmt.Errorf("missing block %d during assembly", i)
		}

		offset := int64(i * m.chunker.BlockSize())
		if _, err := tmpFile.WriteAt(data, offset); err != nil {
			return fmt.Errorf("write block %d: %w", i, err)
		}
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, t.OutputPath); err != nil {
		return fmt.Errorf("rename to output: %w", err)
	}
	return nil
}

// GetTransfer returns a transfer by ID.
func (m *Manager) GetTransfer(id string) (*Transfer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.transfers[id]
	return t, ok
}

// ActiveTransfers returns all transfers that are not yet done/failed.
func (m *Manager) ActiveTransfers() []*Transfer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Transfer
	for _, t := range m.transfers {
		t.mu.RLock()
		s := t.Status
		t.mu.RUnlock()
		if s != TransferDone && s != TransferFailed {
			out = append(out, t)
		}
	}
	return out
}

// Progress returns (blocksDownloaded, totalBlocks) for a transfer.
func (m *Manager) Progress(id string) (int, int, error) {
	t, ok := m.GetTransfer(id)
	if !ok {
		return 0, 0, fmt.Errorf("transfer %s not found", id)
	}

	done, total := t.progress()
	return done, total, nil
}

// sanitizeName strips characters that are unsafe in filenames.
func sanitizeName(name string) string {
	safe := make([]byte, 0, len(name))
	for i := range len(name) {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			safe = append(safe, c)
		} else {
			safe = append(safe, '_')
		}
	}
	if len(safe) == 0 {
		return "track"
	}
	return string(safe)
}
