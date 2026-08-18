package fsroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_Empty(t *testing.T) {
	if r := Open(nil); r != nil {
		t.Fatal("Open(nil) = non-nil, want nil")
	}
	if r := Open([]string{""}); r != nil {
		t.Fatal("Open([\"\"]) = non-nil, want nil")
	}
}

func TestResolve_WithinRoot(t *testing.T) {
	dir := t.TempDir()
	r := Open([]string{dir})
	defer r.Close()

	inside := filepath.Join(dir, "album", "song.mp3")
	osRoot, rel, ok := r.Resolve(inside)
	if !ok {
		t.Fatal("Resolve within root = not found")
	}
	if osRoot.Name() != filepath.Clean(dir) {
		t.Errorf("root = %q, want %q", osRoot.Name(), dir)
	}

	wantRel := filepath.Join("album", "song.mp3")
	if rel != wantRel {
		t.Errorf("rel = %q, want %q", rel, wantRel)
	}
}

func TestResolve_NestedRootWins(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	r := Open([]string{base, nested})
	defer r.Close()

	insideNested := filepath.Join(nested, "a.mp3")
	osRoot, rel, ok := r.Resolve(insideNested)
	if !ok {
		t.Fatal("Resolve nested = not found")
	}
	if osRoot.Name() != nested {
		t.Errorf("root = %q, want nested %q", osRoot.Name(), nested)
	}
	if rel != "a.mp3" {
		t.Errorf("rel = %q, want a.mp3", rel)
	}
}

func TestResolve_OutsideRoot(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(t.TempDir(), "evil.mp3")
	r := Open([]string{dir})
	defer r.Close()

	if _, _, ok := r.Resolve(other); ok {
		t.Fatal("Resolve outside root = found, want not-found")
	}
}

func TestResolve_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	r := Open([]string{dir})
	defer r.Close()

	// A path with '..' that escapes the root must not resolve.
	escape := filepath.Join(dir, "..", "evil.mp3")
	if _, _, ok := r.Resolve(escape); ok {
		t.Fatal("Resolve with '..' escape = found, want not-found")
	}
}

func TestOpen_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.mp3")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlink inside the root pointing outside it.
	link := filepath.Join(dir, "link.mp3")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := Open([]string{dir})
	defer r.Close()

	f, err := r.Open(link)
	if err == nil {
		f.Close()
		t.Fatal("Open(symlink escape) succeeded, want error (os.Root must refuse)")
	}
}

func TestOpen_ValidFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(file, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Open([]string{dir})
	defer r.Close()

	f, err := r.Open(file)
	if err != nil {
		t.Fatalf("Open(valid) = %v", err)
	}
	defer f.Close()
}

func TestStat_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.mp3")
	if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link.mp3")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := Open([]string{dir})
	defer r.Close()

	if _, err := r.Stat(link); err == nil {
		t.Fatal("Stat(symlink escape) succeeded, want error")
	}
}

func TestContains_Outside(t *testing.T) {
	dir := t.TempDir()
	r := Open([]string{dir})
	defer r.Close()

	if r.Contains(filepath.Join(t.TempDir(), "evil.mp3")) {
		t.Fatal("Contains(outside) = true, want false")
	}
	if !r.Contains(filepath.Join(dir, "a", "b.mp3")) {
		t.Fatal("Contains(inside) = false, want true")
	}
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	r := Open([]string{dir})
	r.Close()
	r.Close() // must not panic
}
