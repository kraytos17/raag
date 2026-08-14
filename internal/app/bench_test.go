package app

import (
	"context"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func BenchmarkIndex_Index_Single(b *testing.B) {
	idx := NewSearchIndex(nil)
	ctx := context.Background()
	track := &domain.Track{
		ID:     domain.GenerateTrackID("/music/benchmark.mp3"),
		Title:  benchmarkSong,
		Artist: benchmarkArtist,
		Album:  benchmarkAlbum,
	}
	for b.Loop() {
		_ = idx.Index(ctx, track)
	}
}

func BenchmarkIndex_Index_Batch(b *testing.B) {
	idx := NewSearchIndex(nil)
	ctx := context.Background()
	tracks := make([]*domain.Track, 100)
	for i := range 100 {
		tracks[i] = &domain.Track{
			ID:     domain.GenerateTrackID("/music/track" + string(rune('a'+i)) + ".mp3"),
			Title:  benchmarkSong,
			Artist: benchmarkArtist,
			Album:  benchmarkAlbum,
		}
	}
	for b.Loop() {
		_ = idx.IndexBatch(ctx, tracks)
	}
}

func BenchmarkIndex_Search(b *testing.B) {
	idx := NewSearchIndex(nil)
	ctx := context.Background()
	for i := range 1000 {
		track := &domain.Track{
			ID:     domain.GenerateTrackID("/music/track" + string(rune('a'+i%26)) + ".mp3"),
			Title:  rockSong,
			Artist: rockBand,
			Album:  rockAlbum,
		}
		_ = idx.Index(ctx, track)
	}
	for b.Loop() {
		_, _ = idx.Search(ctx, rockQuery, 20)
	}
}

func BenchmarkIndex_Delete(b *testing.B) {
	idx := NewSearchIndex(nil)
	ctx := context.Background()
	trackID := domain.GenerateTrackID("/music/benchmark.mp3")
	_ = idx.Index(ctx, &domain.Track{
		ID:     trackID,
		Title:  benchmarkSong,
		Artist: benchmarkArtist,
		Album:  benchmarkAlbum,
	})

	for b.Loop() {
		_ = idx.Delete(ctx, trackID)
	}
}
