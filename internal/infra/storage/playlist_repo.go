package db

import (
	"context"
	"iter"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
)

type playlistRepo struct {
	db *DB
}

func NewPlaylistRepo(db *DB) app.PlaylistRepository {
	return &playlistRepo{db: db}
}

func (r *playlistRepo) Save(ctx context.Context, playlist *domain.Playlist) error {
	data, err := ipc.MarshalPlaylist(playlist)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PlaylistKey(playlist.ID), data)
	})
}

func (r *playlistRepo) FindByID(ctx context.Context, id domain.PlaylistID) (*domain.Playlist, error) {
	var playlist *domain.Playlist
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(PlaylistKey(id))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		p, err := ipc.UnmarshalPlaylist(data)
		if err != nil {
			return err
		}

		playlist = p
		return nil
	})
	if err == badger.ErrKeyNotFound {
		return nil, domain.ErrPlaylistNotFound
	}
	return playlist, err
}

func (r *playlistRepo) ListAll(ctx context.Context) iter.Seq[*domain.Playlist] {
	return func(yield func(*domain.Playlist) bool) {
		_ = r.db.View(func(txn *badger.Txn) error {
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			iter.Seek([]byte(PrefixPlaylist))
			for iter.ValidForPrefix([]byte(PrefixPlaylist)) {
				item := iter.Item()
				data, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				p, err := ipc.UnmarshalPlaylist(data)
				if err != nil {
					return err
				}
				if !yield(p) {
					return nil
				}
				iter.Next()
			}
			return nil
		})
	}
}

func (r *playlistRepo) Delete(ctx context.Context, id domain.PlaylistID) error {
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(PlaylistKey(id))
	})
}
