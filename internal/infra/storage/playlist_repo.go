package db

import (
	"context"
	"errors"
	"iter"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
)

type playlistRepo struct {
	*BaseRepository[*domain.Playlist, domain.PlaylistID]
}

func NewPlaylistRepo(db *DB) app.PlaylistRepository {
	return &playlistRepo{
		BaseRepository: NewBaseRepository(
			db,
			PlaylistKey,
			[]byte(PrefixPlaylist),
			ipc.MarshalPlaylist,
			ipc.UnmarshalPlaylist,
			func() error { return domain.ErrPlaylistNotFound },
		),
	}
}

func (r *playlistRepo) ListAll(ctx context.Context) iter.Seq2[*domain.Playlist, error] {
	return listAll(r.DB, []byte(PrefixPlaylist), ipc.UnmarshalPlaylist)
}

func (r *playlistRepo) FindByID(ctx context.Context, id domain.PlaylistID) (*domain.Playlist, error) {
	var zero *domain.Playlist
	val, err := view(r.DB, PlaylistKey(id), ipc.UnmarshalPlaylist)
	if errors.Is(err, errNotFound) {
		return zero, domain.ErrPlaylistNotFound
	}
	return val, err
}

func (r *playlistRepo) Save(ctx context.Context, playlist *domain.Playlist) error {
	data, err := ipc.MarshalPlaylist(playlist)
	if err != nil {
		return err
	}
	return r.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(PlaylistKey(playlist.ID), data)
	})
}

func (r *playlistRepo) Delete(ctx context.Context, id domain.PlaylistID) error {
	return r.DB.Update(func(txn *badger.Txn) error {
		return txn.Delete(PlaylistKey(id))
	})
}
