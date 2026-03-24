package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/denisbrodbeck/machineid"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/klauspost/compress/zstd"
)

var (
	zstdEnc, _ = zstd.NewWriter(nil)
	zstdDec, _ = zstd.NewReader(nil)
)

type Options struct {
	GCInterval       time.Duration
	ValueLogFileSize int64
	Compression      bool
}

func DefaultOptions(dir string) Options {
	return Options{
		GCInterval:       10 * time.Minute,
		ValueLogFileSize: 1 << 30,
		Compression:      true,
	}
}

type DB struct {
	db   *badger.DB
	path string
	opts Options
	stop chan struct{}
	wg   sync.WaitGroup
}

func Open(dir string, opts Options) (*DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	encryptionKey, err := getEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	compression := options.ZSTD
	if !opts.Compression {
		compression = options.None
	}

	badgerOpts := badger.DefaultOptions(dir).
		WithValueLogFileSize(opts.ValueLogFileSize).
		WithCompression(compression).
		WithEncryptionKey(encryptionKey)

	if len(encryptionKey) > 0 {
		badgerOpts = badgerOpts.WithIndexCacheSize(64 << 20)
	}

	db, err := badger.Open(badgerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}
	d := &DB{
		db:   db,
		path: dir,
		opts: opts,
		stop: make(chan struct{}),
	}

	d.startCompaction()
	return d, nil
}

func getEncryptionKey() ([]byte, error) {
	if key := os.Getenv("RAAG_DB_KEY"); key != "" {
		if len(key) != 32 {
			return nil, fmt.Errorf("RAAG_DB_KEY must be 32 bytes")
		}
		return []byte(key), nil
	}

	protectedID, err := machineid.ProtectedID("raag-v1")
	if err != nil {
		return nil, fmt.Errorf("failed to get protected machine ID: %w", err)
	}

	h := hmac.New(sha256.New, []byte(protectedID))
	h.Write([]byte("raag-db-v1"))
	key := h.Sum(nil)

	slog.Info("using machine-derived encryption key", "key_hash", hex.EncodeToString(key[:8]))
	return key, nil
}

func (d *DB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return d.CloseWithContext(ctx)
}

func (d *DB) CloseWithContext(ctx context.Context) error {
	close(d.stop)
	done := make(chan error, 1)
	go func() {
		done <- d.db.Close()
	}()

	select {
	case <-ctx.Done():
		slog.Warn("DB close timed out, forcing exit", "error", ctx.Err())
		return ctx.Err()
	case err := <-done:
		if err != nil {
			slog.Warn("DB close error", "error", err)
		}
		return err
	}
}

func (d *DB) Path() string {
	return d.path
}

func (d *DB) Exists() bool {
	_, err := os.Stat(d.path)
	return err == nil
}

func (d *DB) StartHealthCheck(interval time.Duration, onMissing func()) {
	d.wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-d.stop:
				return
			case <-ticker.C:
				if !d.Exists() {
					slog.Error("database directory deleted", "path", d.path)
					if onMissing != nil {
						onMissing()
					}
				}
			}
		}
	})
}

func (d *DB) View(fn func(txn *badger.Txn) error) error {
	return d.db.View(fn)
}

func (d *DB) Update(fn func(txn *badger.Txn) error) error {
	return d.db.Update(fn)
}

func (d *DB) NewWriteBatch() *badger.WriteBatch {
	return d.db.NewWriteBatch()
}

func (d *DB) startCompaction() {
	d.wg.Go(func() {
		ticker := time.NewTicker(d.opts.GCInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.stop:
				return
			case <-ticker.C:
				if err := d.runValueLogGC(0.5); err != nil {
					slog.Warn("value log GC failed", "error", err)
				}
			}
		}
	})
}

func (d *DB) runValueLogGC(discardRatio float64) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("value log GC timed out after 2 minutes")
		}

		err := d.db.RunValueLogGC(discardRatio)
		if err == badger.ErrNoRewrite {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (d *DB) Size() (int64, int64) {
	lsm, vlog := d.db.Size()
	return lsm, vlog
}

func (d *DB) Backup(writeFn func([]byte) error) error {
	return d.db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iter.Close()

		for iter.Rewind(); iter.Valid(); iter.Next() {
			item := iter.Item()
			val, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}

			key := make([]byte, len(item.Key()))
			copy(key, item.Key())
			if err := writeFn(key); err != nil {
				return err
			}
			if err := writeFn(val); err != nil {
				return err
			}
		}
		return nil
	})
}

func Compress(data []byte) ([]byte, error) {
	return zstdEnc.EncodeAll(data, nil), nil
}

func Decompress(data []byte) ([]byte, error) {
	return zstdDec.DecodeAll(data, nil)
}
