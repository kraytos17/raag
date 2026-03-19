package db

import (
	"context"
	"encoding/json"

	"github.com/dgraph-io/badger/v4"

	"github.com/p-society/raag/internal/domain"
)

type peerRepo struct {
	db *DB
}

func newPeerRepo(db *DB) *peerRepo {
	return &peerRepo{db: db}
}

func (r *peerRepo) SavePeerInfo(ctx context.Context, info *domain.PeerInfo) error {
	data, err := json.Marshal(info)
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

		p := &domain.PeerInfo{}
		if err := json.Unmarshal(data, p); err != nil {
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
	data, err := json.Marshal(score)
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

		s := &domain.PeerScore{}
		if err := json.Unmarshal(data, s); err != nil {
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
	data, err := json.Marshal(manifest)
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

		m := &domain.LibraryManifest{}
		if err := json.Unmarshal(data, m); err != nil {
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

func (r *peerRepo) ListAllPeers(ctx context.Context) ([]*domain.PeerInfo, error) {
	var peers []*domain.PeerInfo
	err := r.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		iter.Seek([]byte(PrefixPeer))
		for iter.ValidForPrefix([]byte(PrefixPeer)) {
			item := iter.Item()
			data, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			p := &domain.PeerInfo{}
			if err := json.Unmarshal(data, p); err != nil {
				return err
			}

			peers = append(peers, p)
			iter.Next()
		}
		return nil
	})
	return peers, err
}
