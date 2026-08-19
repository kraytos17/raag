package p2p

import (
	"path/filepath"
	"testing"
)

// TestKeyringKey_DistinctDataDirs_DistinctSlots verifies the OS-keychain slot
// is scoped by data dir. Before scoping, two co-located raagd instances shared
// the fixed "identity" slot and silently got the same peer ID (the smoke-test
// finding); distinct data dirs must map to distinct slots.
func TestKeyringKey_DistinctDataDirs_DistinctSlots(t *testing.T) {
	a := keyringKey(filepath.Join(t.TempDir(), "a"))
	b := keyringKey(filepath.Join(t.TempDir(), "b"))
	if a == b {
		t.Fatalf("expected distinct keyring slots for distinct data dirs, got %q == %q", a, b)
	}
}

// TestKeyringKey_SameDataDir_SameSlot ensures a single instance's keyring slot
// is stable across reloads (peer ID must not churn between restarts).
func TestKeyringKey_SameDataDir_SameSlot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	if a, b := keyringKey(dir), keyringKey(dir); a != b {
		t.Fatalf("expected stable keyring slot for same data dir, got %q != %q", a, b)
	}
}
