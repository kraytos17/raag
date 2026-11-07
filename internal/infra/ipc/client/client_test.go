package client

import (
	"path/filepath"
	"testing"
)

func TestPersistentClient_NoServerAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	c := NewClient(socketPath)
	defer func() { c.Close() }()

	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error when no server available")
	}
}

// TestPersistentClient_ListTracks verifies the ListTracks command returns
// all library tracks from the daemon.
