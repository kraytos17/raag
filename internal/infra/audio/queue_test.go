package audio

import (
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func track(id string) *domain.Track {
	return &domain.Track{ID: domain.TrackID(id), Title: id}
}

func TestQueue_MoveTo_ExistingTrack(t *testing.T) {
	q := NewQueue()
	q.Add(track("a"))
	q.Add(track("b"))
	q.Add(track("c"))

	// Advance to b (pos = 1): first Next goes -1 -> 0, second goes 0 -> 1.
	q.Next()
	q.Next()
	if q.Position() != 1 {
		t.Fatalf("Position() = %d, want 1", q.Position())
	}
	if !q.MoveTo(track("c")) {
		t.Fatal("MoveTo(c) = false, want true")
	}
	if q.Position() != 2 {
		t.Errorf("Position() = %d, want 2", q.Position())
	}
	if got := q.Current(); got == nil || got.ID != domain.TrackID("c") {
		t.Errorf("Current() = %v, want c", got)
	}
	if q.Length() != 3 {
		t.Errorf("Length() = %d, want 3 (no duplicate added)", q.Length())
	}
}

func TestQueue_MoveTo_AbsentTrackAppends(t *testing.T) {
	q := NewQueue()
	q.Add(track("a"))
	q.Add(track("b"))

	if !q.MoveTo(track("z")) {
		t.Fatal("MoveTo(z) = false, want true")
	}
	if q.Length() != 3 {
		t.Errorf("Length() = %d, want 3", q.Length())
	}
	if q.Position() != 2 {
		t.Errorf("Position() = %d, want 2", q.Position())
	}
	if got := q.Current(); got == nil || got.ID != domain.TrackID("z") {
		t.Errorf("Current() = %v, want z", got)
	}
}

func TestQueue_MoveTo_EmptyQueue(t *testing.T) {
	q := NewQueue()

	if !q.MoveTo(track("first")) {
		t.Fatal("MoveTo(first) = false, want true")
	}
	if q.Length() != 1 {
		t.Errorf("Length() = %d, want 1", q.Length())
	}
	if q.Position() != 0 {
		t.Errorf("Position() = %d, want 0", q.Position())
	}
	if got := q.Current(); got == nil || got.ID != domain.TrackID("first") {
		t.Errorf("Current() = %v, want first", got)
	}
}

func TestQueue_MoveTo_NilTrack(t *testing.T) {
	q := NewQueue()
	q.Add(track("a"))

	if q.MoveTo(nil) {
		t.Fatal("MoveTo(nil) = true, want false")
	}
	if q.Length() != 1 {
		t.Errorf("Length() = %d, want 1", q.Length())
	}
}
