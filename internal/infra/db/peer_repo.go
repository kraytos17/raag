package db

import (
	"context"
	"iter"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type peerRepo struct {
	db *DB
}

func NewPeerRepo(db *DB) app.PeerRepository {
	return &peerRepo{db: db}
}

func (r *peerRepo) SavePeerInfo(ctx context.Context, info *domain.PeerInfo) error {
	data, err := domain.MarshalPeerInfo(info)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerKey(info.ID), data)
	})
}

func (r *peerRepo) GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	var info *domain.PeerInfo
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(PeerKey(id))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		p, err := domain.UnmarshalPeerInfo(data)
		if err != nil {
			return err
		}

		info = p
		return nil
	})

	if err == badger.ErrKeyNotFound {
		return nil, domain.ErrPeerUnavailable
	}
	return info, err
}

func (r *peerRepo) SavePeerScore(ctx context.Context, id domain.PeerID, score *domain.PeerScore) error {
	data, err := domain.MarshalPeerScore(score)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerScoreKey(id), data)
	})
}

func (r *peerRepo) GetPeerScore(ctx context.Context, id domain.PeerID) (*domain.PeerScore, error) {
	var score *domain.PeerScore
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(PeerScoreKey(id))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		s, err := domain.UnmarshalPeerScore(data)
		if err != nil {
			return err
		}

		score = s
		return nil
	})

	if err == badger.ErrKeyNotFound {
		return nil, nil
	}
	return score, err
}

func (r *peerRepo) SaveLibraryManifest(ctx context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error {
	data, err := domain.MarshalLibraryManifest(manifest)
	if err != nil {
		return err
	}
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(PeerLibraryKey(id), data)
	})
}

func (r *peerRepo) GetLibraryManifest(ctx context.Context, id domain.PeerID) (*domain.LibraryManifest, error) {
	var manifest *domain.LibraryManifest
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(PeerLibraryKey(id))
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		m, err := domain.UnmarshalLibraryManifest(data)
		if err != nil {
			return err
		}

		manifest = m
		return nil
	})
	if err == badger.ErrKeyNotFound {
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

				p, err := domain.UnmarshalPeerInfo(data)
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
