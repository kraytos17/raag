package p2p

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// fakeKeychain substitutes an in-memory OS keychain. Pass fail=true to make
// every keychain operation fail, forcing the file backend.
func fakeKeychain(t *testing.T, fail bool) {
	t.Helper()
	oldGet, oldSet := keychainGet, keychainSet
	t.Cleanup(func() { keychainGet, keychainSet = oldGet, oldSet })
	if fail {
		keychainGet = func(_, _ string) (string, error) { return "", errors.New("no keychain") }
		keychainSet = func(_, _, _ string) error { return errors.New("no keychain") }
		return
	}
	mem := map[string]string{}
	keychainGet = func(service, user string) (string, error) {
		if v, ok := mem[service+"\x00"+user]; ok {
			return v, nil
		}
		return "", errors.New("not found")
	}
	keychainSet = func(service, user, password string) error {
		mem[service+"\x00"+user] = password
		return nil
	}
}

func TestIdentityManager_LoadOrCreateRoundTripFile(t *testing.T) {
	fakeKeychain(t, true) // no OS keychain: exercise the file backend
	dir := t.TempDir()
	mgr := NewIdentityManager(dir)

	_, id1, err := mgr.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if id1 == "" {
		t.Fatal("LoadOrCreate() returned empty peer ID")
	}
	if _, err := os.Stat(filepath.Join(dir, identityKeyFile)); err != nil {
		t.Fatalf("key file not written: %v", err)
	}

	// A second manager over the same dir must recover the same identity.
	mgr2 := NewIdentityManager(dir)
	_, id2, err := mgr2.LoadOrCreate()
	if err != nil {
		t.Fatalf("second LoadOrCreate() error = %v", err)
	}
	if id2 != id1 {
		t.Errorf("second LoadOrCreate() id = %s, want %s", id2, id1)
	}
}

func TestIdentityManager_LoadOrCreateRoundTripKeychain(t *testing.T) {
	fakeKeychain(t, false) // working OS keychain: file must stay untouched
	dir := t.TempDir()

	_, id1, err := NewIdentityManager(dir).LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, identityKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("key file written despite working keychain: %v", err)
	}

	_, id2, err := NewIdentityManager(dir).LoadOrCreate()
	if err != nil {
		t.Fatalf("second LoadOrCreate() error = %v", err)
	}
	if id2 != id1 {
		t.Errorf("second LoadOrCreate() id = %s, want %s", id2, id1)
	}
}

func TestIdentityManager_FileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mgr := NewIdentityManager(dir)

	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	keyBytes, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	wantID, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatalf("IDFromPrivateKey: %v", err)
	}

	if err := mgr.writeFile(keyBytes); err != nil {
		t.Fatalf("writeFile() error = %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, identityKeyFile)); err != nil {
		t.Fatalf("Stat key file: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file perm = %o, want 600", perm)
	}

	if _, gotID, err := mgr.tryFile(); err != nil {
		t.Fatalf("tryFile() error = %v", err)
	} else if gotID != wantID {
		t.Errorf("tryFile() id = %s, want %s", gotID, wantID)
	}
}

func TestIdentityManager_TryFileRejectsGarbage(t *testing.T) {
	badPEM := pem.EncodeToMemory(&pem.Block{Type: "LIBP2P PRIVATE KEY", Bytes: []byte("not a libp2p key")})
	for name, data := range map[string][]byte{
		// missing: don't write the file at all (nil sentinel below).
		"empty":  {},
		"junk":   []byte("not a pem block"),
		"badkey": badPEM,
	} {
		t.Run(name, func(t *testing.T) {
			mgr := NewIdentityManager(t.TempDir())
			if err := os.WriteFile(mgr.keyFile, data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, _, err := mgr.tryFile(); err == nil {
				t.Error("tryFile() = nil error, want error for unreadable key")
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		mgr := NewIdentityManager(t.TempDir())
		if _, _, err := mgr.tryFile(); err == nil {
			t.Error("tryFile() = nil error, want error for missing file")
		}
	})
}

func TestDecodeIdentity(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	keyBytes, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	wantID, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatalf("IDFromPrivateKey: %v", err)
	}

	if _, gotID, err := decodeIdentity(base64.StdEncoding.EncodeToString(keyBytes)); err != nil {
		t.Fatalf("decodeIdentity() error = %v", err)
	} else if gotID != wantID {
		t.Errorf("decodeIdentity() id = %s, want %s", gotID, wantID)
	}

	for name, data := range map[string]string{
		"not-base64": "!!!",
		"not-a-key":  base64.StdEncoding.EncodeToString([]byte("junk")),
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := decodeIdentity(data); err == nil {
				t.Error("decodeIdentity() = nil error, want error")
			}
		})
	}
}

func TestKeyringKey_DistinctDataDirs_DistinctSlots(t *testing.T) {
	a := keyringKey(filepath.Join(t.TempDir(), "a"))
	b := keyringKey(filepath.Join(t.TempDir(), "b"))
	if a == b {
		t.Fatalf("expected distinct keyring slots for distinct data dirs, got %q == %q", a, b)
	}
}

func TestKeyringKey_SameDataDir_SameSlot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	if a, b := keyringKey(dir), keyringKey(dir); a != b {
		t.Fatalf("expected stable keyring slot for same data dir, got %q != %q", a, b)
	}
}

func TestEnsureDirPerms(t *testing.T) {
	parent := t.TempDir()

	// Missing dir is created locked down.
	dir := filepath.Join(parent, "newdir")
	mgr := NewIdentityManager(dir)
	if err := mgr.ensureDirPerms(); err != nil {
		t.Fatalf("ensureDirPerms() error = %v", err)
	}
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("Stat: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 700", perm)
	}

	// Loose existing dir is tightened.
	loose := filepath.Join(parent, "loose")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := NewIdentityManager(loose).ensureDirPerms(); err != nil {
		t.Fatalf("ensureDirPerms() error = %v", err)
	}
	if info, err := os.Stat(loose); err != nil {
		t.Fatalf("Stat: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 700", perm)
	}
}

func TestWipeBytes(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	wipeBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("b[%d] = %d, want 0 after wipe", i, v)
		}
	}
}
