package db

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/dgraph-io/badger/v4"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
)

const (
	defaultCacheSize = 10000
)

type libraryRepo struct {
	db    *DB
	cache *lru.Cache[string, *domain.Track]
}

type LibraryRepo interface {
	app.LibraryRepository
	app.FileStatStore
}

func NewLibraryRepo(db *DB, paths []string) (LibraryRepo, error) {
	cache, err := lru.New[string, *domain.Track](defaultCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}
	return &libraryRepo{
		db:    db,
		cache: cache,
	}, nil
}

func (r *libraryRepo) Save(ctx context.Context, track *domain.Track) error {
	return r.db.Update(func(txn *badger.Txn) error {
		return r.setTrackEntry(txn.Set, track)
	})
}

func (r *libraryRepo) setTrackEntry(set func([]byte, []byte) error, track *domain.Track) error {
	if len(track.CoverArt) > 0 {
		if err := set(CoverArtKey(track.ID), track.CoverArt); err != nil {
			return fmt.Errorf("failed to save cover art: %w", err)
		}

		trackCopy := *track
		trackCopy.CoverArt = nil
		data, err := ipc.MarshalTrack(&trackCopy)
		if err != nil {
			return fmt.Errorf("failed to marshal track: %w", err)
		}
		if err := set(TrackKey(track.ID), data); err != nil {
			return fmt.Errorf("failed to save track: %w", err)
		}
	} else {
		data, err := ipc.MarshalTrack(track)
		if err != nil {
			return fmt.Errorf("failed to marshal track: %w", err)
		}
		if err := set(TrackKey(track.ID), data); err != nil {
			return fmt.Errorf("failed to save track: %w", err)
		}
	}
	if err := set(PathKey(track.Path), []byte(track.ID)); err != nil {
		return fmt.Errorf("failed to save path index: %w", err)
	}
	if err := set(PathStrKey(track.Path), []byte(track.ID)); err != nil {
		return fmt.Errorf("failed to save path str index: %w", err)
	}
	if err := set(ArtistIndexKey(track.Artist, track.ID), nil); err != nil {
		return fmt.Errorf("failed to save artist index: %w", err)
	}
	if err := set(AlbumIndexKey(track.Album, track.ID), nil); err != nil {
		return fmt.Errorf("failed to save album index: %w", err)
	}
	r.cache.Add(string(track.ID), track)
	return nil
}

func (r *libraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := r.cache.Get(string(id)); ok {
		return track, nil
	}

	var result *domain.Track
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(TrackKey(id))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		track, err := ipc.UnmarshalTrack(data)
		if err != nil {
			return fmt.Errorf("failed to unmarshal track: %w", err)
		}

		result = track
		r.cache.Add(string(id), track)
		return nil
	})

	if err == badger.ErrKeyNotFound {
		return nil, domain.ErrTrackNotFound
	}
	return result, err
}

func (r *libraryRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var results []*domain.Track
	idSet := make(map[domain.TrackID]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
		if track, ok := r.cache.Get(string(id)); ok {
			results = append(results, track)
			delete(idSet, id)
		}
	}
	if len(idSet) == 0 {
		return results, nil
	}

	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		prefix := []byte(PrefixTrackData)
		for iter.Seek(prefix); iter.ValidForPrefix(prefix); iter.Next() {
			item := iter.Item()
			key := string(item.Key())
			trackID := domain.TrackID(key[len(PrefixTrackData):])
			if !idSet[trackID] {
				continue
			}

			data, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			track, err := ipc.UnmarshalTrack(data)
			if err != nil {
				continue
			}

			results = append(results, track)
			r.cache.Add(string(trackID), track)
		}
		return nil
	})
	return results, err
}

func (r *libraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	var trackID domain.TrackID
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(PathKey(path))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		trackID = domain.TrackID(data)
		return nil
	})

	if err == badger.ErrKeyNotFound {
		return nil, domain.ErrTrackNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, trackID)
}

func (r *libraryRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	var coverArt []byte
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(CoverArtKey(id))
		if err != nil {
			return err
		}
		coverArt, err = item.ValueCopy(nil)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return nil, nil
	}
	return coverArt, err
}

func (r *libraryRepo) AllTracksIter(ctx context.Context) iter.Seq2[*domain.Track, error] {
	return func(yield func(*domain.Track, error) bool) {
		err := r.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()

			prefix := []byte(PrefixTrackData)
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				item := it.Item()
				var track *domain.Track
				e := item.Value(func(val []byte) error {
					var err error
					track, err = ipc.UnmarshalTrack(val)
					return err
				})
				if e != nil {
					if !yield(nil, e) {
						return nil
					}
					continue
				}
				if !yield(track, nil) {
					return nil
				}
			}
			return nil
		})
		if err != nil {
			yield(nil, err)
		}
	}
}

func (r *libraryRepo) Search(ctx context.Context, q app.SearchQuery) ([]*domain.Track, error) {
	var results []*domain.Track
	var iterErr error
	query := strings.ToLower(strings.TrimSpace(q.Query))
	for track, err := range r.AllTracksIter(ctx) {
		if err != nil {
			iterErr = err
			continue
		}
		if strings.Contains(strings.ToLower(track.Title), query) ||
			strings.Contains(strings.ToLower(track.Artist), query) {
			results = append(results, track)
			if len(results) >= q.Limit {
				break
			}
		}
	}
	if iterErr != nil {
		return results, iterErr
	}
	return results, nil
}

func (r *libraryRepo) Delete(ctx context.Context, id domain.TrackID) error {
	track, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(TrackKey(id)); err != nil {
			return err
		}
		if err := txn.Delete(CoverArtKey(id)); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		if err := txn.Delete(PathKey(track.Path)); err != nil {
			return err
		}
		if err := txn.Delete(PathStrKey(track.Path)); err != nil {
			return err
		}
		if err := txn.Delete(ArtistIndexKey(track.Artist, id)); err != nil {
			return err
		}
		if err := txn.Delete(AlbumIndexKey(track.Album, id)); err != nil {
			return err
		}

		r.cache.Remove(string(id))
		return nil
	})
}

func (r *libraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	// BulkSave uses BadgerDB WriteBatch for bulk writes.
	// WriteBatch handles its own internal chunking when transactions grow too large
	// (ErrTxnTooBig triggers auto-commit of sub-transactions). Earlier committed
	// sub-transactions cannot be rolled back, so partial completion on error is
	// possible. Callers should handle this by either:
	// 1. Using unique IDs to detect duplicates on retry, or
	// 2. Accepting that BulkSave may partially complete on error.
	wb := r.db.NewWriteBatch()
	defer wb.Cancel()

	for _, track := range tracks {
		if err := r.setTrackEntry(wb.Set, track); err != nil {
			return err
		}
	}
	return wb.Flush()
}

func (r *libraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	return collectAll(r.db, []byte(PrefixTrackData), ipc.UnmarshalTrack)
}

func (r *libraryRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	var paths []string
	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		iter.Seek([]byte(PrefixTrackPathStr))
		for iter.ValidForPrefix([]byte(PrefixTrackPathStr)) {
			item := iter.Item()
			key := string(item.Key())
			if len(key) <= len(PrefixTrackPathStr) {
				iter.Next()
				continue
			}

			paths = append(paths, key[len(PrefixTrackPathStr):])
			iter.Next()
		}
		return nil
	})
	return paths, err
}

func (r *libraryRepo) SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error {
	wb := r.db.NewWriteBatch()
	for path, stat := range stats {
		data, err := ipc.MarshalFileStat(stat)
		if err != nil {
			wb.Cancel()
			return err
		}
		if err := wb.Set(FileStatKey(path), data); err != nil {
			wb.Cancel()
			return err
		}
	}
	if err := wb.Flush(); err != nil {
		wb.Cancel()
		return err
	}
	return nil
}

func (r *libraryRepo) LoadFileStats(ctx context.Context) (map[string]*domain.FileStat, error) {
	stats := make(map[string]*domain.FileStat)
	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		iter.Seek([]byte(PrefixFileStat))
		for iter.ValidForPrefix([]byte(PrefixFileStat)) {
			item := iter.Item()
			data, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			stat, err := ipc.UnmarshalFileStat(data)
			if err != nil {
				return err
			}

			stats[stat.Path] = stat
			iter.Next()
		}
		return nil
	})
	return stats, err
}

func (r *libraryRepo) InvalidateCache() {
	r.cache.Purge()
}

func matchesQuery(query string, track *domain.Track) bool {
	q := domain.Normalize(query)
	return strings.Contains(domain.Normalize(track.Title), q) ||
		strings.Contains(domain.Normalize(track.Artist), q) ||
		strings.Contains(domain.Normalize(track.Album), q)
}
