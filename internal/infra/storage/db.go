package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	db     *badger.DB
	path   string
	opts   Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func Open(dir string, opts Options) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	encKey := os.Getenv("RAAG_DB_KEY")
	var encryptionKey []byte
	if encKey != "" {
		encryptionKey = []byte(encKey)
		if len(encryptionKey) != 32 {
			return nil, fmt.Errorf("encryption key must be 32 bytes")
		}
	}

	compression := options.ZSTD
	if !opts.Compression {
		compression = options.None
	}

	badgerOpts := badger.DefaultOptions(dir).
		WithValueLogFileSize(opts.ValueLogFileSize).
		WithCompression(compression)

	if encryptionKey != nil {
		badgerOpts = badgerOpts.WithEncryptionKey(encryptionKey)
	}

	db, err := badger.Open(badgerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &DB{
		db:     db,
		path:   dir,
		opts:   opts,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (d *DB) Close() error {
	d.cancel()
	d.wg.Wait()
	return d.db.Close()
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

func (d *DB) Path() string {
	return d.path
}

func (d *DB) StartCompaction() {
	d.wg.Go(func() {
		ticker := time.NewTicker(d.opts.GCInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.ctx.Done():
				return
			case <-ticker.C:
				if err := d.RunValueLogGC(0.5); err != nil {
					slog.Warn("value log GC failed", "error", err)
				}
			}
		}
	})
}

func (d *DB) RunValueLogGC(discardRatio float64) error {
	for {
		err := d.db.RunValueLogGC(discardRatio)
		if err == badger.ErrNoRewrite {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (d *DB) Size() (int64, error) {
	var size int64
	err := filepath.Walk(d.path, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
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
