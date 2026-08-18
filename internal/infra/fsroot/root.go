// Package fsroot scopes file access to configured library directories using
// os.Root, preventing paths (including symlinks) from escaping the library.
package fsroot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Roots holds one os.Root per configured library directory. All file I/O on
// library paths should go through it so a symlink or '..' component can never
// escape the configured roots — both at scan time and when serving files to
// peers.
type Roots struct {
	roots []*root
}

type root struct {
	dir  string
	root *os.Root
}

// Open opens an os.Root for each of the given directory paths. Directories
// that cannot be opened are skipped (best-effort, mirroring the scanner's
// tolerance of a partially-unreadable library). A nil *Roots is returned only
// when no path succeeds.
func Open(paths []string) *Roots {
	r := &Roots{}
	for _, p := range paths {
		if p == "" {
			continue
		}

		dir := filepath.Clean(p)
		osRoot, err := os.OpenRoot(dir)
		if err != nil {
			continue
		}
		r.roots = append(r.roots, &root{dir: dir, root: osRoot})
	}
	if len(r.roots) == 0 {
		return nil
	}
	return r
}

// Close closes all roots. The Roots must not be used afterwards.
func (r *Roots) Close() {
	if r == nil {
		return
	}
	for _, rt := range r.roots {
		_ = rt.root.Close()
	}
}

// Resolve finds the root that owns path and returns the corresponding os.Root
// plus the root-relative path. ok is false when no configured root contains
// path.
func (r *Roots) Resolve(path string) (*os.Root, string, bool) {
	if r == nil {
		return nil, "", false
	}

	clean := filepath.Clean(path)
	best := -1
	bestLen := -1
	var bestRel string
	for i, rt := range r.roots {
		if !isWithin(rt.dir, clean) {
			continue
		}

		rel, err := filepath.Rel(rt.dir, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		// Longest matching root wins, so a nested library path resolves to the
		// most specific root.
		if len(rt.dir) > bestLen {
			bestLen = len(rt.dir)
			best = i
			bestRel = rel
		}
	}
	if best < 0 {
		return nil, "", false
	}
	return r.roots[best].root, bestRel, true
}

// Open opens path scoped to the root that owns it. It fails if path is not
// inside any configured root, or if the resolved root-relative path escapes
// via a symlink or '..' component (enforced by os.Root at the OS level).
func (r *Roots) Open(path string) (*os.File, error) {
	osRoot, rel, ok := r.Resolve(path)
	if !ok {
		return nil, fmt.Errorf("fsroot: %q is not within any configured library root", path)
	}

	f, err := osRoot.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("fsroot: open %q: %w", path, err)
	}
	return f, nil
}

// Contains reports whether path is lexically inside one of the configured
// roots. Unlike Open it does not follow symlinks; it is the appropriate guard
// for handing a path to a subprocess (e.g. ffmpeg) that os.Root cannot scope.
func (r *Roots) Contains(path string) bool {
	if r == nil {
		return false
	}
	_, _, ok := r.Resolve(path)
	return ok
}

// Stat returns file info for path, scoped to the root that owns it. It fails
// if path is outside every configured root.
func (r *Roots) Stat(path string) (os.FileInfo, error) {
	osRoot, rel, ok := r.Resolve(path)
	if !ok {
		return nil, fmt.Errorf("fsroot: %q is not within any configured library root", path)
	}

	info, err := osRoot.Stat(rel)
	if err != nil {
		return nil, fmt.Errorf("fsroot: stat %q: %w", path, err)
	}
	return info, nil
}

// isWithin reports whether child is equal to parent or nested beneath it.
func isWithin(parent, child string) bool {
	if child == parent {
		return true
	}

	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
