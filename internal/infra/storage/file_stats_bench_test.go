package db

import (
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func BenchmarkDiffFileStats_AllAdded(b *testing.B) {
	current := make(map[string]*domain.FileStat)
	for i := range 1000 {
		current["/music/song"+string(rune('a'+i%26))+".mp3"] = &domain.FileStat{
			Path:  "/music/song.mp3",
			Mtime: int64(i),
			Size:  int64(i * 1000),
		}
	}

	previous := make(map[string]*domain.FileStat)
	for b.Loop() {
		diffFileStats(current, previous)
	}
}

func BenchmarkDiffFileStats_Mixed(b *testing.B) {
	current := make(map[string]*domain.FileStat)
	previous := make(map[string]*domain.FileStat)
	for i := range 500 {
		path := "/music/unchanged" + string(rune('a'+i%26)) + ".mp3"
		stat := &domain.FileStat{
			Path:  path,
			Mtime: 1000,
			Size:  1000,
		}

		current[path] = stat
		previous[path] = stat
	}
	for i := range 250 {
		path := "/music/modified" + string(rune('a'+i%26)) + ".mp3"
		current[path] = &domain.FileStat{
			Path:  path,
			Mtime: 2000,
			Size:  2000,
		}
		previous[path] = &domain.FileStat{
			Path:  path,
			Mtime: 1000,
			Size:  1000,
		}
	}
	for i := range 250 {
		current["/music/added"+string(rune('a'+i%26))+".mp3"] = &domain.FileStat{
			Path:  "/music/added.mp3",
			Mtime: 1000,
			Size:  1000,
		}
	}
	for i := range 250 {
		path := "/music/deleted" + string(rune('a'+i%26)) + ".mp3"
		previous[path] = &domain.FileStat{
			Path:  path,
			Mtime: 1000,
			Size:  1000,
		}
	}
	for b.Loop() {
		diffFileStats(current, previous)
	}
}

func BenchmarkDiffFileStats_Unchanged(b *testing.B) {
	current := make(map[string]*domain.FileStat)
	previous := make(map[string]*domain.FileStat)
	for i := range 1000 {
		path := "/music/song" + string(rune('a'+i%26)) + ".mp3"
		stat := &domain.FileStat{
			Path:  path,
			Mtime: 1000,
			Size:  1000,
		}

		current[path] = stat
		previous[path] = stat
	}
	for b.Loop() {
		diffFileStats(current, previous)
	}
}
