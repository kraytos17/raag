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
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerKey(info.ID), data)
	})
}

func (r *peerRepo) GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	info, err := view(r.db, PeerKey(id), ipc.UnmarshalPeerInfo)
	if err == errNotFound {
		return nil, domain.ErrPeerUnavailable
	}
	return info, err
}

func (r *peerRepo) SavePeerScore(ctx context.Context, id domain.PeerID, score *domain.PeerScore) error {
	data, err := ipc.MarshalPeerScore(score)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerScoreKey(id), data)
	})
}

func (r *peerRepo) GetPeerScore(ctx context.Context, id domain.PeerID) (*domain.PeerScore, error) {
	score, err := view(r.db, PeerScoreKey(id), ipc.UnmarshalPeerScore)
	if err == errNotFound {
		return nil, nil
	}
	return score, err
}

func (r *peerRepo) SaveLibraryManifest(ctx context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error {
	data, err := ipc.MarshalLibraryManifest(manifest)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerLibraryKey(id), data)
	})
}

func (r *peerRepo) GetLibraryManifest(ctx context.Context, id domain.PeerID) (*domain.LibraryManifest, error) {
	manifest, err := view(r.db, PeerLibraryKey(id), ipc.UnmarshalLibraryManifest)
	if err == errNotFound {
		return nil, nil
	}
	return manifest, err
}

func (r *peerRepo) ListAll(ctx context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {
		err := r.db.View(func(txn *badger.Txn) error {
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			prefix := []byte(PrefixPeerInfo)
			iter.Seek(prefix)
			for iter.ValidForPrefix(prefix) {
				item := iter.Item()
				key := item.Key()
				keyStr := string(key)

				data, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				p, err := ipc.UnmarshalPeerInfo(data)
				if err != nil {
					iter.Next()
					continue
				}

				peerID := domain.PeerID(keyStr[len(PrefixPeerInfo):])
				scoreItem, err := txn.Get(PeerScoreKey(peerID))
				if err == nil && scoreItem != nil {
					scoreData, _ := scoreItem.ValueCopy(nil)
					score, _ := ipc.UnmarshalPeerScore(scoreData)
					if score != nil {
						p.Score = score
					}
				}
				if !yield(p, nil) {
					return nil
				}
				iter.Next()
			}
			return nil
		})
		if err != nil {
			yield(nil, err)
		}
	}
}
