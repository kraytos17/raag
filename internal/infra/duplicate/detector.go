package duplicate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/domain"
	storage "github.com/p-society/raag/internal/infra/storage"
)

type Handler int

const (
	Skip Handler = iota
	Warn
	Keep
)

func ConfigToHandler(h config.DuplicateHandling) Handler {
	switch h {
	case config.DuplicateSkip:
		return Skip
	case config.DuplicateKeep:
		return Keep
	default:
		return Warn
	}
}

type Info struct {
	Hash           string
	OriginalID     domain.TrackID
	DuplicateIDs   []domain.TrackID
	OriginalPath   string
	DuplicatePaths []string
}

type Detector struct {
	store      *storage.DB
	handler    Handler
	mu         sync.Mutex
	duplicates map[string][]domain.TrackID
	paths      map[domain.TrackID]string
}

func NewDetector(store *storage.DB, handler Handler) *Detector {
	return &Detector{
		store:      store,
		handler:    handler,
		duplicates: make(map[string][]domain.TrackID),
		paths:      make(map[domain.TrackID]string),
	}
}

func (d *Detector) CheckDuplicate(ctx context.Context, hash string, trackID domain.TrackID, path string) (domain.TrackID, bool, bool, error) {
	if hash == "" {
		return "", false, false, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.paths[trackID] = path

	var existingID string
	var shouldSkip bool
	err := d.store.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(storage.ContentHashKey(hash))
		if err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		if err == badger.ErrKeyNotFound {
			d.duplicates[hash] = append(d.duplicates[hash], trackID)
			if err := txn.Set(storage.ContentHashKey(hash), []byte(trackID)); err != nil {
				return fmt.Errorf("failed to register content hash: %w", err)
			}
			return nil
		}

		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		existingID = string(val)
		d.duplicates[hash] = append(d.duplicates[hash], trackID)
		switch d.handler {
		case Skip:
			shouldSkip = true
		case Warn:
			slog.Warn("duplicate track detected",
				"current_path", path,
				"current_id", trackID,
				"original_id", existingID,
			)
		case Keep:
			if err := txn.Set(storage.ContentHashKey(hash), []byte(trackID)); err != nil {
				return fmt.Errorf("failed to update content hash: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return "", false, false, err
	}
	if existingID != "" {
		return domain.TrackID(existingID), true, shouldSkip, nil
	}
	return "", false, false, nil
}

func (d *Detector) GetDuplicates() []Info {
	d.mu.Lock()
	defer d.mu.Unlock()

	var result []Info
	for hash, trackIDs := range d.duplicates {
		if len(trackIDs) >= 2 {
			originalID := trackIDs[0]
			duplicateIDs := trackIDs[1:]
			duplicatePaths := make([]string, 0, len(duplicateIDs))
			for _, id := range duplicateIDs {
				if p := d.paths[id]; p != "" {
					duplicatePaths = append(duplicatePaths, p)
				}
			}
			result = append(result, Info{
				Hash:           hash,
				OriginalID:     originalID,
				DuplicateIDs:   duplicateIDs,
				OriginalPath:   d.paths[originalID],
				DuplicatePaths: duplicatePaths,
			})
		}
	}
	return result
}

func (d *Detector) CountGroups() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := 0
	for _, ids := range d.duplicates {
		if len(ids) >= 2 {
			count++
		}
	}
	return count
}

func (d *Detector) CountDuplicateTracks() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := 0
	for _, ids := range d.duplicates {
		if len(ids) >= 2 {
			count += len(ids)
		}
	}
	return count
}
