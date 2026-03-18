package storage

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	badger4 "github.com/ipfs/go-ds-badger4"
)

type Store struct {
	ds *badger4.Datastore
}

func NewStore(dataDir string) (*Store, error) {
	absPath, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("invalid data directory path: %w", err)
	}

	opts := badger4.DefaultOptions
	opts.Dir = absPath
	opts.ValueDir = absPath

	store, err := badger4.NewDatastore(absPath, &opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger datastore: %w", err)
	}

	return &Store{ds: store}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if s.ds == nil {
		return nil, fmt.Errorf("datastore not initialized")
	}
	return s.ds.Get(ctx, datastore.NewKey(key))
}

func (s *Store) Put(ctx context.Context, key string, value []byte) error {
	if s.ds == nil {
		return fmt.Errorf("datastore not initialized")
	}
	return s.ds.Put(ctx, datastore.NewKey(key), value)
}

func (s *Store) GetBool(ctx context.Context, key string) (bool, error) {
	data, err := s.Get(ctx, key)
	if err != nil {
		if err == datastore.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}
	return data[0] == 1, nil
}

func (s *Store) PutBool(ctx context.Context, key string, value bool) error {
	var b byte
	if value {
		b = 1
	}
	return s.Put(ctx, key, []byte{b})
}

func (s *Store) GetString(ctx context.Context, key string) (string, error) {
	data, err := s.Get(ctx, key)
	if err != nil {
		if err == datastore.ErrNotFound {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (s *Store) PutString(ctx context.Context, key, value string) error {
	return s.Put(ctx, key, []byte(value))
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if s.ds == nil {
		return fmt.Errorf("datastore not initialized")
	}
	return s.ds.Delete(ctx, datastore.NewKey(key))
}

func (s *Store) Has(ctx context.Context, key string) (bool, error) {
	if s.ds == nil {
		return false, fmt.Errorf("datastore not initialized")
	}
	return s.ds.Has(ctx, datastore.NewKey(key))
}

func (s *Store) GetAll(ctx context.Context, prefix string) (map[string][]byte, error) {
	if s.ds == nil {
		return nil, fmt.Errorf("datastore not initialized")
	}

	q := query.Query{Prefix: prefix}
	results, err := s.ds.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer results.Close()

	m := make(map[string][]byte)
	for result := range results.Next() {
		if result.Error != nil {
			return m, result.Error
		}
		m[result.Key] = result.Value
	}
	return m, nil
}

func (s *Store) Close() error {
	if s.ds != nil {
		return s.ds.Close()
	}
	return nil
}
