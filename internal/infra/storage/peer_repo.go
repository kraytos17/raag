package db

import (
	"context"
	"iter"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
)

type peerRepo struct {
	db *DB
}

func NewPeerRepo(db *DB) app.PeerRepository {
	return &peerRepo{db: db}
}

func (r *peerRepo) SavePeerInfo(ctx context.Context, info *domain.PeerInfo) error {
	data, err := ipc.MarshalPeerInfo(info)
	if err != nil {
		return err
	}
	return update(r.db, func(txn *badger.Txn) error {
		return txn.Set(PeerKey(info.ID), data)
	})
}

func (r *peerRepo) GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	info, err := view(r.db, PeerKey(id), ipc.UnmarshalPeerInfo)
	if err == domain.ErrNotFound {
		return nil, domain.ErrPeerUnavailable
	}
	return info, err
}

func (r *peerRepo) SavePeerScore(ctx context.Context, id domain.PeerID, score *domain.PeerScore) error {
	data, err := ipc.MarshalPeerScore(score)
	if err != nil {
		return err
	}
	return update(r.db, func(txn *badger.Txn) error {
		return txn.Set(PeerScoreKey(id), data)
	})
}

func (r *peerRepo) GetPeerScore(ctx context.Context, id domain.PeerID) (*domain.PeerScore, error) {
	score, err := view(r.db, PeerScoreKey(id), ipc.UnmarshalPeerScore)
	if err == domain.ErrNotFound {
		return nil, nil
	}
	return score, err
}

func (r *peerRepo) SaveLibraryManifest(ctx context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error {
	data, err := ipc.MarshalLibraryManifest(manifest)
	if err != nil {
		return err
	}
	return update(r.db, func(txn *badger.Txn) error {
		return txn.Set(PeerLibraryKey(id), data)
	})
}

func (r *peerRepo) GetLibraryManifest(ctx context.Context, id domain.PeerID) (*domain.LibraryManifest, error) {
	manifest, err := view(r.db, PeerLibraryKey(id), ipc.UnmarshalLibraryManifest)
	if err == domain.ErrNotFound {
		return nil, nil
	}
	return manifest, err
}

func (r *peerRepo) ListAll(ctx context.Context) iter.Seq[*domain.PeerInfo] {
	return func(yield func(*domain.PeerInfo) bool) {
		_ = r.db.View(func(txn *badger.Txn) error {
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			prefix := []byte(PrefixPeer)
			iter.Seek(prefix)
			for iter.ValidForPrefix(prefix) {
				item := iter.Item()
				key := item.Key()
				keyStr := string(key)
				if len(keyStr) >= len(PrefixPeerLib) && keyStr[:len(PrefixPeerLib)] == PrefixPeerLib {
					iter.Next()
					continue
				}
				if len(keyStr) >= len(PrefixPeerScore) && keyStr[:len(PrefixPeerScore)] == PrefixPeerScore {
					iter.Next()
					continue
				}

				data, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				p, err := ipc.UnmarshalPeerInfo(data)
				if err != nil {
					iter.Next()
					continue
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
