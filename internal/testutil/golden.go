package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type GoldenFile struct {
	path string
}

func NewGoldenFile(t *testing.T, name string) *GoldenFile {
	t.Helper()
	return &GoldenFile{
		path: filepath.Join("testdata", "golden", name+".golden"),
	}
}

func (g *GoldenFile) Path() string {
	return g.path
}

func (g *GoldenFile) Read(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(g.path)
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", g.path, err)
	}
	return data
}

func (g *GoldenFile) ReadString(t *testing.T) string {
	t.Helper()
	return string(g.Read(t))
}

func (g *GoldenFile) Update(t *testing.T, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		t.Fatalf("failed to create golden directory: %v", err)
	}
	if err := os.WriteFile(g.path, data, 0o644); err != nil {
		t.Fatalf("failed to write golden file: %v", err)
	}
}

func (g *GoldenFile) UpdateString(t *testing.T, data string) {
	t.Helper()
	g.Update(t, []byte(data))
}

func (g *GoldenFile) Compare(t *testing.T, got []byte) {
	t.Helper()
	want := g.Read(t)
	if !strings.EqualFold(string(got), string(want)) {
		t.Errorf("golden file mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func (g *GoldenFile) CompareString(t *testing.T, got string) {
	t.Helper()
	g.Compare(t, []byte(got))
}

func (g *GoldenFile) Exists() bool {
	_, err := os.Stat(g.path)
	return err == nil
}
