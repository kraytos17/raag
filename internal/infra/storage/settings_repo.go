package db

import (
	"context"
	"errors"
	"strconv"

	"github.com/dgraph-io/badger/v4"
	"github.com/p-society/raag/internal/app"
)

const settingsKeyVolume = "volume"

// settingsRepo persists runtime user settings in the database. Config.toml
// stays the source of defaults; this is the mutable, restart-surviving layer.
type settingsRepo struct {
	db *DB
}

// NewSettingsRepo returns a SettingsRepository backed by the given database.
func NewSettingsRepo(db *DB) app.SettingsRepository {
	return &settingsRepo{db: db}
}

func (r *settingsRepo) GetVolume(ctx context.Context) (int, bool, error) {
	var volume int
	found := false
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(SettingsKey(settingsKeyVolume))
		if err != nil {
			return err
		}
		return item.Value(func(data []byte) error {
			v, err := strconv.Atoi(string(data))
			if err != nil {
				return err
			}
			volume = v
			found = true
			return nil
		})
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return 0, false, nil
	}
	return volume, found, err
}

func (r *settingsRepo) SetVolume(ctx context.Context, volume int) error {
	return r.db.Update(func(txn *badger.Txn) error {
		return txn.Set(SettingsKey(settingsKeyVolume), []byte(strconv.Itoa(volume)))
	})
}
