package db

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/dgraph-io/badger/v4"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

const (
	defaultCacheSize = 1000
	batchSize        = 500
)

type libraryRepo struct {
	db    *DB
	cache *lru.Cache[string, *domain.Track]
}

func NewLibraryRepo(db *DB, paths []string) (app.LibraryRepository, error) {
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
		return r.saveTrack(txn, track)
	})
}

func (r *libraryRepo) saveTrack(txn *badger.Txn, track *domain.Track) error {
	data, err := json.Marshal(track)
	if err != nil {
		return fmt.Errorf("failed to marshal track: %w", err)
	}
	if err := txn.Set(TrackKey(track.ID), data); err != nil {
		return fmt.Errorf("failed to save track: %w", err)
	}
	if err := txn.Set(PathKey(track.Path), []byte(track.ID)); err != nil {
		return fmt.Errorf("failed to save path index: %w", err)
	}
	if err := txn.Set(ArtistIndexKey(track.Artist, track.ID), nil); err != nil {
		return fmt.Errorf("failed to save artist index: %w", err)
	}
	if err := txn.Set(AlbumIndexKey(track.Album, track.ID), nil); err != nil {
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

		track := &domain.Track{}
		if err := json.Unmarshal(data, track); err != nil {
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

func (r *libraryRepo) AllTracksIter(ctx context.Context) iter.Seq[*domain.Track] {
	return func(yield func(*domain.Track) bool) {
		_ = r.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()

			prefix := []byte(PrefixTrackData)
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				item := it.Item()
				var track domain.Track
				err := item.Value(func(val []byte) error {
					return json.Unmarshal(val, &track)
				})
				if err != nil {
					continue
				}
				if !yield(&track) {
					return nil
				}
			}
			return nil
		})
	}
}

func (r *libraryRepo) Search(ctx context.Context, q app.SearchQuery) ([]*domain.Track, error) {
	var results []*domain.Track
	query := strings.ToLower(strings.TrimSpace(q.Query))
	for track := range r.AllTracksIter(ctx) {
		if strings.Contains(strings.ToLower(track.Title), query) ||
			strings.Contains(strings.ToLower(track.Artist), query) {
			results = append(results, track)
			if len(results) >= q.Limit {
				break
			}
		}
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
	wb := r.db.NewWriteBatch()
	for i, track := range tracks {
		data, err := json.Marshal(track)
		if err != nil {
			wb.Cancel()
			return fmt.Errorf("failed to marshal track: %w", err)
		}
		if err := wb.Set(TrackKey(track.ID), data); err != nil {
			wb.Cancel()
			return fmt.Errorf("failed to save track: %w", err)
		}
		if err := wb.Set(PathKey(track.Path), []byte(track.ID)); err != nil {
			wb.Cancel()
			return fmt.Errorf("failed to save path index: %w", err)
		}
		if err := wb.Set(ArtistIndexKey(track.Artist, track.ID), nil); err != nil {
			wb.Cancel()
			return fmt.Errorf("failed to save artist index: %w", err)
		}
		if err := wb.Set(AlbumIndexKey(track.Album, track.ID), nil); err != nil {
			wb.Cancel()
			return fmt.Errorf("failed to save album index: %w", err)
		}

		r.cache.Add(string(track.ID), track)
		if (i+1)%batchSize == 0 {
			if err := wb.Flush(); err != nil {
				wb.Cancel()
				return err
			}
		}
	}
	if err := wb.Flush(); err != nil {
		wb.Cancel()
		return err
	}
	return nil
}

func (r *libraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	var tracks []*domain.Track
	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		iter.Seek([]byte(PrefixTrackData))
		for iter.ValidForPrefix([]byte(PrefixTrackData)) {
			item := iter.Item()
			data, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			track := &domain.Track{}
			if err := json.Unmarshal(data, track); err != nil {
				return err
			}

			tracks = append(tracks, track)
			iter.Next()
		}
		return nil
	})
	return tracks, err
}

func (r *libraryRepo) SaveFileStats(ctx context.Context, stats map[string]*domain.FileStat) error {
	wb := r.db.NewWriteBatch()
	for path, stat := range stats {
		data, err := json.Marshal(stat)
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

			stat := &domain.FileStat{}
			if err := json.Unmarshal(data, stat); err != nil {
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

func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == ' ' {
			result.WriteByte(c)
		}
	}
	return result.String()
}

func matchesQuery(query string, track *domain.Track) bool {
	q := normalize(query)
	return strings.Contains(normalize(track.Title), q) ||
		strings.Contains(normalize(track.Artist), q) ||
		strings.Contains(normalize(track.Album), q)
}
