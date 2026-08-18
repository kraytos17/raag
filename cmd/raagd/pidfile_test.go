package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWritePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "raagd.pid")
	if err := writePidFile(path); err != nil {
		t.Fatalf("writePidFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("pid file contains %q, not an int", string(data))
	}
	if pid != os.Getpid() {
		t.Fatalf("pid file = %d, want %d", pid, os.Getpid())
	}
}

func TestWritePidFile_EmptyPath(t *testing.T) {
	if err := writePidFile(""); err != nil {
		t.Fatalf("writePidFile(\"\") error = %v, want nil", err)
	}
}

func TestRemovePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raagd.pid")
	if err := writePidFile(path); err != nil {
		t.Fatalf("writePidFile() error = %v", err)
	}

	removePidFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after removePidFile: %v", err)
	}
}

func TestRemovePidFile_Missing(t *testing.T) {
	// Must not panic or error on a missing file.
	removePidFile(filepath.Join(t.TempDir(), "nonexistent.pid"))
	removePidFile("")
}
