package db

import (
	"slices"
	"testing"
	"time"

	"github.com/p-society/raag/internal/domain"
)

func TestDiffFileStats_AllAdded(t *testing.T) {
	current := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
		song2Path: {Path: song2Path, Mtime: 1000, Size: 6000},
	}

	previous := map[string]*domain.FileStat{}
	result := diffFileStats(current, previous)
	if len(result.Added) != 2 {
		t.Errorf("Added count = %d, want 2", len(result.Added))
	}
	if len(result.Modified) != 0 {
		t.Errorf("Modified count = %d, want 0", len(result.Modified))
	}
	if len(result.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0", len(result.Deleted))
	}
}

func TestDiffFileStats_AllDeleted(t *testing.T) {
	current := map[string]*domain.FileStat{}
	previous := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
		song2Path: {Path: song2Path, Mtime: 1000, Size: 6000},
	}

	result := diffFileStats(current, previous)
	if len(result.Added) != 0 {
		t.Errorf("Added count = %d, want 0", len(result.Added))
	}
	if len(result.Modified) != 0 {
		t.Errorf("Modified count = %d, want 0", len(result.Modified))
	}
	if len(result.Deleted) != 2 {
		t.Errorf("Deleted count = %d, want 2", len(result.Deleted))
	}
}

func TestDiffFileStats_Modified(t *testing.T) {
	current := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 2000, Size: 5000},
	}
	previous := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
	}

	result := diffFileStats(current, previous)
	if len(result.Added) != 0 {
		t.Errorf("Added count = %d, want 0", len(result.Added))
	}
	if len(result.Modified) != 1 {
		t.Errorf("Modified count = %d, want 1", len(result.Modified))
	}
	if len(result.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0", len(result.Deleted))
	}
}

func TestDiffFileStats_SizeChanged(t *testing.T) {
	current := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 6000},
	}
	previous := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
	}

	result := diffFileStats(current, previous)
	if len(result.Modified) != 1 {
		t.Errorf("Modified count = %d, want 1", len(result.Modified))
	}
}

func TestDiffFileStats_Unchanged(t *testing.T) {
	current := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
	}
	previous := map[string]*domain.FileStat{
		song1Path: {Path: song1Path, Mtime: 1000, Size: 5000},
	}

	result := diffFileStats(current, previous)
	if len(result.Added) != 0 {
		t.Errorf("Added count = %d, want 0", len(result.Added))
	}
	if len(result.Modified) != 0 {
		t.Errorf("Modified count = %d, want 0", len(result.Modified))
	}
	if len(result.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0", len(result.Deleted))
	}
}

func TestDiffFileStats_Mixed(t *testing.T) {
	current := map[string]*domain.FileStat{
		song1Path:          {Path: song1Path, Mtime: 1000, Size: 5000},
		song2Path:          {Path: song2Path, Mtime: 2000, Size: 6000},
		"/music/song3.mp3": {Path: "/music/song3.mp3", Mtime: 1000, Size: 7000},
	}
	previous := map[string]*domain.FileStat{
		song1Path:          {Path: song1Path, Mtime: 1000, Size: 5000},
		song2Path:          {Path: song2Path, Mtime: 1000, Size: 6000},
		"/music/song4.mp3": {Path: "/music/song4.mp3", Mtime: 1000, Size: 8000},
	}

	result := diffFileStats(current, previous)
	if len(result.Added) != 1 {
		t.Errorf("Added count = %d, want 1", len(result.Added))
	}
	if len(result.Modified) != 1 {
		t.Errorf("Modified count = %d, want 1", len(result.Modified))
	}
	if len(result.Deleted) != 1 {
		t.Errorf("Deleted count = %d, want 1", len(result.Deleted))
	}
}

func TestDiffFileStats_EmptyMaps(t *testing.T) {
	result := diffFileStats(map[string]*domain.FileStat{}, map[string]*domain.FileStat{})
	if len(result.Added) != 0 || len(result.Modified) != 0 || len(result.Deleted) != 0 {
		t.Error("diffFileStats() with empty maps should return empty result")
	}
}

func TestFileStat_Changed(t *testing.T) {
	tests := []struct {
		name     string
		current  *domain.FileStat
		other    *domain.FileStat
		expected bool
	}{
		{
			name:     "mtime changed",
			current:  &domain.FileStat{Mtime: 2000, Size: 1000},
			other:    &domain.FileStat{Mtime: 1000, Size: 1000},
			expected: true,
		},
		{
			name:     "size changed",
			current:  &domain.FileStat{Mtime: 1000, Size: 2000},
			other:    &domain.FileStat{Mtime: 1000, Size: 1000},
			expected: true,
		},
		{
			name:     "both changed",
			current:  &domain.FileStat{Mtime: 2000, Size: 2000},
			other:    &domain.FileStat{Mtime: 1000, Size: 1000},
			expected: true,
		},
		{
			name:     "none changed",
			current:  &domain.FileStat{Mtime: 1000, Size: 1000},
			other:    &domain.FileStat{Mtime: 1000, Size: 1000},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.current.Changed(tt.other); got != tt.expected {
				t.Errorf("FileStat.Changed() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFileStat_Changed_WithHash(t *testing.T) {
	current := &domain.FileStat{Mtime: 1000, Size: 1000, Hash: abcHash}
	other := &domain.FileStat{Mtime: 1000, Size: 1000, Hash: "xyz"}
	if current.Changed(other) {
		t.Error("FileStat.Changed() should not consider Hash field")
	}
}

func TestDiffFileStats_ResultsContainExpectedPaths(t *testing.T) {
	current := map[string]*domain.FileStat{
		"/music/new.mp3": {Path: "/music/new.mp3", Mtime: 1000, Size: 5000},
		modifiedPath:     {Path: modifiedPath, Mtime: 2000, Size: 5000},
		unchangedPath:    {Path: unchangedPath, Mtime: 1000, Size: 5000},
	}
	previous := map[string]*domain.FileStat{
		modifiedPath:     {Path: modifiedPath, Mtime: 1000, Size: 5000},
		unchangedPath:    {Path: unchangedPath, Mtime: 1000, Size: 5000},
		"/music/deleted": {Path: "/music/deleted", Mtime: 1000, Size: 5000},
	}

	result := diffFileStats(current, previous)
	foundAdded := slices.Contains(result.Added, "/music/new.mp3")
	if !foundAdded {
		t.Error("Expected /music/new.mp3 in Added list")
	}

	foundModified := slices.Contains(result.Modified, modifiedPath)
	if !foundModified {
		t.Error("Expected /music/modified in Modified list")
	}

	foundDeleted := slices.Contains(result.Deleted, "/music/deleted")
	if !foundDeleted {
		t.Error("Expected /music/deleted in Deleted list")
	}
}

func TestFileStat_New(t *testing.T) {
	path := "/music/test.mp3"
	now := time.Now().Unix()
	stat := &domain.FileStat{
		Path:  path,
		Mtime: now,
		Size:  1024,
		Hash:  "testhash",
	}

	if stat.Path != path {
		t.Errorf("FileStat.Path = %v, want %v", stat.Path, path)
	}
	if stat.Mtime != now {
		t.Errorf("FileStat.Mtime = %v, want %v", stat.Mtime, now)
	}
	if stat.Size != 1024 {
		t.Errorf("FileStat.Size = %v, want 1024", stat.Size)
	}
}
