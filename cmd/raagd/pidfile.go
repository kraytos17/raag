package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// writePidFile records the current process ID so scripts and other tools can
// manage the daemon. The path comes from config (daemon.pid_file); an empty
// path disables it.
func writePidFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// removePidFile deletes the pid file, ignoring a missing file.
func removePidFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return
	}
}

// ErrAlreadyRunning is returned when the pid file names a live process,
// indicating another daemon instance is active.
var ErrAlreadyRunning = errors.New("another raagd daemon is already running")

// checkStalePidFile inspects the pid file left by a previous daemon run. A
// stale file (dead process) is removed so a fresh start can write its own; a
// live process is reported so the operator can stop it before starting again.
func checkStalePidFile(path string) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		// Garbage pid file: it is stale by definition, clear it.
		removePidFile(path)
		return nil
	}
	if pid <= 0 {
		removePidFile(path)
		return nil
	}

	// Signal 0 probes liveness without sending a signal. Permission errors
	// (EPERM) mean the process exists but belongs to another user — treat as
	// alive.
	process, err := os.FindProcess(pid)
	if err != nil {
		removePidFile(path)
		return nil
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		// ESRCH: no such process — the file is stale.
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			removePidFile(path)
			return nil
		}
		return err
	}
	return ErrAlreadyRunning
}
