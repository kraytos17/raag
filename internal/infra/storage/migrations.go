package db

import (
	"encoding/binary"
	"fmt"

	"github.com/dgraph-io/badger/v4"
)

type Migration struct {
	Version     int
	Description string
	Up          func(txn *badger.Txn) error
	Down        func(txn *badger.Txn) error
}

var migrations = []Migration{
	{
		Version:     1,
		Description: "initial schema",
		Up:          migration1Up,
		Down:        migration1Down,
	},
	{
		Version:     2,
		Description: "add play_count field",
		Up:          migration2Up,
		Down:        migration2Down,
	},
	{
		Version:     3,
		Description: "add gain values",
		Up:          migration3Up,
		Down:        migration3Down,
	},
	{
		Version:     4,
		Description: "add inverted index",
		Up:          migration4Up,
		Down:        migration4Down,
	},
	{
		Version:     5,
		Description: "add peer score cache",
		Up:          migration5Up,
		Down:        migration5Down,
	},
}

func RunMigrations(db *DB) error {
	currentVersion, err := getSchemaVersion(db)
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}
	for _, m := range migrations {
		if m.Version > currentVersion {
			if err := db.Update(m.Up); err != nil {
				return fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Description, err)
			}
			if err := setSchemaVersion(db, m.Version); err != nil {
				return fmt.Errorf("failed to set schema version: %w", err)
			}
			currentVersion = m.Version
		}
	}
	return nil
}

func getSchemaVersion(db *DB) (int, error) {
	var version int
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(KeySchemaVersion))
		if err == badger.ErrKeyNotFound {
			version = 0
			return nil
		}
		if err != nil {
			return err
		}

		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		version = int(binary.BigEndian.Uint64(val))
		return nil
	})
	return version, err
}

func setSchemaVersion(db *DB, version int) error {
	return db.Update(func(txn *badger.Txn) error {
		val := make([]byte, 8)
		binary.BigEndian.PutUint64(val, uint64(version))
		return txn.Set([]byte(KeySchemaVersion), val)
	})
}

func migration1Up(txn *badger.Txn) error {
	return nil
}

func migration1Down(txn *badger.Txn) error {
	return nil
}

func migration2Up(txn *badger.Txn) error {
	return nil
}

func migration2Down(txn *badger.Txn) error {
	return nil
}

func migration3Up(txn *badger.Txn) error {
	return nil
}

func migration3Down(txn *badger.Txn) error {
	return nil
}

func migration4Up(txn *badger.Txn) error {
	return nil
}

func migration4Down(txn *badger.Txn) error {
	return nil
}

func migration5Up(txn *badger.Txn) error {
	return nil
}

func migration5Down(txn *badger.Txn) error {
	return nil
}
