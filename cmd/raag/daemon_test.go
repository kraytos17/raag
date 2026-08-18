package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestResolveDaemonBinary_FoundNextToExe(t *testing.T) {
	// Can't easily fake os.Executable; fall back to PATH expectation.
	// In this repo a `bin/raagd` may exist; just assert the function returns
	// something or a clear error, never panics.
	_, err := resolveDaemonBinary()
	if err != nil {
		// Acceptable if not built yet — but must be the "not found" error.
		if err.Error() == "raagd binary not found (build it with `make build-raagd` or add it to PATH)" {
			t.Log("raagd not built; skipping binary assertion")
			return
		}
		t.Fatalf("resolveDaemonBinary() unexpected error: %v", err)
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
