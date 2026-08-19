package main

import (
	"errors"
	"os"
	"os/exec"
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

func TestCheckStalePidFile_Missing(t *testing.T) {
	if err := checkStalePidFile(filepath.Join(t.TempDir(), "nope.pid")); err != nil {
		t.Fatalf("checkStalePidFile(missing) error = %v, want nil", err)
	}
	if err := checkStalePidFile(""); err != nil {
		t.Fatalf("checkStalePidFile(\"\") error = %v, want nil", err)
	}
}

func TestCheckStalePidFile_Stale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raagd.pid")
	// A pid that no longer exists (spawned and reaped) is stale.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	deadPID := cmd.ProcessState.Pid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(deadPID)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkStalePidFile(path); err != nil {
		t.Fatalf("checkStalePidFile(stale) error = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale pid file was not removed: %v", err)
	}
}

func TestCheckStalePidFile_Live(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raagd.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkStalePidFile(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("checkStalePidFile(live) error = %v, want ErrAlreadyRunning", err)
	}
}

func TestCheckStalePidFile_Garbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raagd.pid")
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkStalePidFile(path); err != nil {
		t.Fatalf("checkStalePidFile(garbage) error = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("garbage pid file was not removed: %v", err)
	}
}
