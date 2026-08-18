package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestResolveDaemonBinary_FoundInBinDir(t *testing.T) {
	repoBin := filepath.Clean(filepath.Join("..", "..", "bin"))
	path, ok := findDaemonBinary(repoBin)
	if !ok {
		t.Skipf("bin/raagd not built (run `make build-raagd`); resolved path %q", repoBin)
	}
	if got := filepath.Base(path); got != "raagd" {
		t.Fatalf("findDaemonBinary(%q) = %q, want .../raagd", repoBin, path)
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		t.Fatalf("findDaemonBinary(%q) = %q is not a regular file", repoBin, path)
	}
}

func TestFindDaemonBinary_NextToBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "raagd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, ok := findDaemonBinary(dir)
	if !ok {
		t.Fatalf("findDaemonBinary(%q) did not find raagd", dir)
	}
	if path != filepath.Join(dir, "raagd") {
		t.Fatalf("findDaemonBinary(%q) = %q, want %q", dir, path, filepath.Join(dir, "raagd"))
	}
}

func TestFindDaemonBinary_Missing(t *testing.T) {
	dir := t.TempDir()
	if _, ok := findDaemonBinary(dir); ok {
		t.Fatalf("findDaemonBinary(%q) found a raagd, want not-found", dir)
	}
}

func TestFindDaemonBinary_DirectoryIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "raagd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := findDaemonBinary(dir); ok {
		t.Fatalf("findDaemonBinary(%q) treated a raagd directory as a binary", dir)
	}
}

func TestReadDaemonPid_Missing(t *testing.T) {
	pid, alive := readDaemonPid(filepath.Join(t.TempDir(), "nope.pid"))
	if alive {
		t.Fatalf("alive = true for missing pid file, want false")
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0", pid)
	}
}

func TestReadDaemonPid_Empty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pid")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	_, alive := readDaemonPid(path)
	if alive {
		t.Fatal("alive = true for empty pid file, want false")
	}
}

func TestReadDaemonPid_NonNumeric(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, alive := readDaemonPid(path)
	if alive {
		t.Fatal("alive = true for non-numeric pid, want false")
	}
}

func TestReadDaemonPid_OwnProcess(t *testing.T) {
	// Our own live PID should be reported alive.
	path := filepath.Join(t.TempDir(), "me.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	pid, alive := readDaemonPid(path)
	if !alive {
		t.Fatal("own pid not reported alive")
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestReadDaemonPid_DeadProcess(t *testing.T) {
	// Spawn a short-lived process and capture its pid after exit.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "dead.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}

	_, alive := readDaemonPid(path)
	if alive {
		t.Fatal("dead process reported alive")
	}
}
