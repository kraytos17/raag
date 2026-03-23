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
	data, err := ipc.MarshalTrack(track)
	if err != nil {
		return fmt.Errorf("failed to marshal track: %w", err)
	}
	if err := set(TrackKey(track.ID), data); err != nil {
		return fmt.Errorf("failed to save track: %w", err)
	}
	if err := set(PathKey(track.Path), []byte(track.ID)); err != nil {
		return fmt.Errorf("failed to save path index: %w", err)
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
		return track.Copy(), nil
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
			results = append(results, track.Copy())
			delete(idSet, id)
		}
	}
	if len(idSet) == 0 {
		return results, nil
	}

	err := r.db.View(func(txn *badger.Txn) error {
		for id := range idSet {
			item, err := txn.Get(TrackKey(id))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					continue
				}
				return err
			}

			data, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			track, err := ipc.UnmarshalTrack(data)
			if err != nil {
				return fmt.Errorf("failed to unmarshal track %s: %w", id, err)
			}

			results = append(results, track)
			r.cache.Add(string(id), track)
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
	return collectAll(r.db, []byte(PrefixTrackData), ipc.UnmarshalTrack), nil
}

func (r *libraryRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	var paths []string
	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		iter.Seek([]byte(PrefixTrackPath))
		for iter.ValidForPrefix([]byte(PrefixTrackPath)) {
			item := iter.Item()
			key := string(item.Key())
			if len(key) <= len(PrefixTrackPath) {
				iter.Next()
				continue
			}

			paths = append(paths, key[len(PrefixTrackPath):])
			iter.Next()
		}
		return nil
	})
	return paths, err
}

func (r *libraryRepo) SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error {
	wb := r.db.NewWriteBatch()
	defer wb.Cancel()

	for path, stat := range stats {
		data, err := ipc.MarshalFileStat(stat)
		if err != nil {
			return err
		}
		if err := wb.Set(FileStatKey(path), data); err != nil {
			return err
		}
	}
	return wb.Flush()
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
	title := track.NormalizedTitle
	if title == "" {
		title = domain.Normalize(track.Title)
	}
	artist := track.NormalizedArtist
	if artist == "" {
		artist = domain.Normalize(track.Artist)
	}
	album := track.NormalizedAlbum
	if album == "" {
		album = domain.Normalize(track.Album)
	}

	return strings.Contains(title, q) ||
		strings.Contains(artist, q) ||
		strings.Contains(album, q)
}
