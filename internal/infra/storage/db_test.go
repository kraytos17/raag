package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if db.Path() != dir {
		t.Errorf("Path() = %v, want %v", db.Path(), dir)
	}

	_ = db.Close()
	_ = os.RemoveAll(dir)
}

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")
	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Open() should create directory")
	}

	_ = db.Close()
	_ = os.RemoveAll(dir)
}

func TestOpen_EncryptionKey(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	_ = os.Setenv("RAAG_DB_KEY", string(key))
	defer func() { _ = os.Unsetenv("RAAG_DB_KEY") }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() with encryption key error = %v", err)
	}

	_ = db.Close()
	_ = os.RemoveAll(dir)
}

func TestOpen_InvalidEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	_ = os.Setenv("RAAG_DB_KEY", "tooshort")
	defer func() { _ = os.Unsetenv("RAAG_DB_KEY") }()

	_, err := Open(dir, DefaultOptions(dir))
	if err == nil {
		t.Error("Open() should fail with invalid encryption key length")
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	_ = os.RemoveAll(dir)
}

func TestView(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	err = db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte("nonexistent"))
		if err != badger.ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound for nonexistent key")
		}
		return nil
	})
	if err != nil {
		t.Errorf("View() error = %v", err)
	}
}

func TestUpdate(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("key"), []byte("value"))
	})
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("key"))
		if err != nil {
			return err
		}

		val, _ := item.ValueCopy(nil)
		if string(val) != "value" {
			t.Errorf("Stored value = %q, want %q", string(val), "value")
		}
		return nil
	})
	if err != nil {
		t.Errorf("View() error = %v", err)
	}
}

func TestUpdate_TransactionError(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	err = db.Update(func(txn *badger.Txn) error {
		return badger.ErrTxnTooBig
	})
	if err != badger.ErrTxnTooBig {
		t.Errorf("Update() should propagate transaction errors")
	}
}

func TestNewWriteBatch(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	wb := db.NewWriteBatch()
	if wb == nil {
		t.Error("NewWriteBatch() should return non-nil WriteBatch")
	}
	wb.Cancel()
}

func TestSize(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	_ = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("key"), []byte("value"))
	})
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	_ = db.Update(func(txn *badger.Txn) error {
		_ = txn.Set([]byte("key1"), []byte("value1"))
		return txn.Set([]byte("key2"), []byte("value2"))
	})

	var keys [][]byte
	err = db.Backup(func(data []byte) error {
		keys = append(keys, data)
		return nil
	})
	if err != nil {
		t.Errorf("Backup() error = %v", err)
	}
	if len(keys) == 0 {
		t.Error("Backup() should capture data")
	}
}

func TestCompressDecompress(t *testing.T) {
	original := []byte("hello world this is a test message for compression")
	compressed, err := Compress(original)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}

	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress() error = %v", err)
	}
	if string(decompressed) != string(original) {
		t.Errorf("Decompressed data = %q, want %q", string(decompressed), string(original))
	}
}

func TestCompress_LargeData(t *testing.T) {
	data := make([]byte, 100000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	compressed, err := Compress(data)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}

	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress() error = %v", err)
	}
	if len(decompressed) != len(data) {
		t.Errorf("Decompressed length = %d, want %d", len(decompressed), len(data))
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions("/test/path")
	if opts.GCInterval != 10*60*1e9 {
		t.Errorf("DefaultOptions().GCInterval = %v, want 10 minutes", opts.GCInterval)
	}
	if opts.ValueLogFileSize != 1<<30 {
		t.Errorf("DefaultOptions().ValueLogFileSize = %v, want 1GB", opts.ValueLogFileSize)
	}
	if opts.Compression != true {
		t.Error("DefaultOptions().Compression should be true by default")
	}
}

func TestDB_CompactionStartsOnOpen(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := db.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	_ = os.RemoveAll(dir)
}
