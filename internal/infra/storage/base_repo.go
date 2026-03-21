package db

import (
	"context"
	"errors"
	"iter"

	"github.com/dgraph-io/badger/v4"
)

var errNotFound = errors.New("not found")

type SetFunc func(k, v []byte) error

type KeyFunc[T any] func(T) []byte

type KeyByIDFunc[T any, ID comparable] func(ID) []byte

type MarshalFunc[T any] func(T) ([]byte, error)

type UnmarshalFunc[T any] func([]byte) (T, error)

func view[T any](
	db *DB,
	key []byte,
	unmarshal UnmarshalFunc[T],
) (T, error) {
	var zero T
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		val, err := unmarshal(data)
		if err != nil {
			return err
		}

		zero = val
		return nil
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return zero, errNotFound
	}
	return zero, err
}

func listAll[T any](
	db *DB,
	prefix []byte,
	unmarshal func([]byte) (T, error),
) iter.Seq[T] {
	return func(yield func(T) bool) {
		_ = db.View(func(txn *badger.Txn) error {
			iter := txn.NewIterator(badger.DefaultIteratorOptions)
			defer iter.Close()

			iter.Seek(prefix)
			for iter.ValidForPrefix(prefix) {
				item := iter.Item()
				var val T
				if err := item.Value(func(data []byte) error {
					var err error
					val, err = unmarshal(data)
					return err
				}); err != nil {
					iter.Next()
					continue
				}
				if !yield(val) {
					return nil
				}
				iter.Next()
			}
			return nil
		})
	}
}

func collectAll[T any](db *DB, prefix []byte, unmarshal func([]byte) (T, error)) []T {
	var results []T
	for item := range listAll(db, prefix, unmarshal) {
		results = append(results, item)
	}
	return results
}

type BaseRepository[T any, ID comparable] struct {
	DB          *DB
	keyByID     KeyByIDFunc[T, ID]
	prefix      []byte
	marshal     MarshalFunc[T]
	unmarshal   UnmarshalFunc[T]
	notFoundErr func() error
}

func NewBaseRepository[T any, ID comparable](
	db *DB,
	keyByID KeyByIDFunc[T, ID],
	prefix []byte,
	marshal MarshalFunc[T],
	unmarshal UnmarshalFunc[T],
	notFoundErr func() error,
) *BaseRepository[T, ID] {
	return &BaseRepository[T, ID]{
		DB:          db,
		keyByID:     keyByID,
		prefix:      prefix,
		marshal:     marshal,
		unmarshal:   unmarshal,
		notFoundErr: notFoundErr,
	}
}

func (r *BaseRepository[T, ID]) Save(ctx context.Context, id ID, entity T) error {
	data, err := r.marshal(entity)
	if err != nil {
		return err
	}
	return r.DB.Update(func(txn *badger.Txn) error {
		return txn.Set(r.keyByID(id), data)
	})
}

func (r *BaseRepository[T, ID]) FindByID(ctx context.Context, id ID) (T, error) {
	var zero T
	val, err := view(r.DB, r.keyByID(id), r.unmarshal)
	if errors.Is(err, errNotFound) {
		return zero, r.notFoundErr()
	}
	return val, err
}

func (r *BaseRepository[T, ID]) Delete(ctx context.Context, id ID) error {
	return r.DB.Update(func(txn *badger.Txn) error {
		return txn.Delete(r.keyByID(id))
	})
}

func (r *BaseRepository[T, ID]) ListAll(ctx context.Context) iter.Seq[T] {
	return listAll(r.DB, r.prefix, r.unmarshal)
}
