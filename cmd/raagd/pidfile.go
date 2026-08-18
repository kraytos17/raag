package main

import (
	"os"
	"path/filepath"
	"strconv"
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
