package db

import (
	"context"
	"testing"
)

func TestSettingsRepo_GetSetRoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewSettingsRepo(db)
	ctx := context.Background()

	if err := repo.SetVolume(ctx, 55); err != nil {
		t.Fatalf("SetVolume() error = %v", err)
	}

	vol, ok, err := repo.GetVolume(ctx)
	if err != nil {
		t.Fatalf("GetVolume() error = %v", err)
	}
	if !ok {
		t.Fatal("GetVolume() ok = false, want true after SetVolume")
	}
	if vol != 55 {
		t.Fatalf("GetVolume() = %d, want 55", vol)
	}
}

func TestSettingsRepo_GetUnset(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewSettingsRepo(db)
	ctx := context.Background()

	vol, ok, err := repo.GetVolume(ctx)
	if err != nil {
		t.Fatalf("GetVolume() error = %v", err)
	}
	if ok {
		t.Fatalf("GetVolume() ok = true, want false when unset")
	}
	if vol != 0 {
		t.Fatalf("GetVolume() = %d, want 0 when unset", vol)
	}
}

func TestSettingsRepo_Overwrite(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewSettingsRepo(db)
	ctx := context.Background()

	if err := repo.SetVolume(ctx, 30); err != nil {
		t.Fatalf("SetVolume(30) error = %v", err)
	}
	if err := repo.SetVolume(ctx, 70); err != nil {
		t.Fatalf("SetVolume(70) error = %v", err)
	}

	vol, ok, err := repo.GetVolume(ctx)
	if err != nil {
		t.Fatalf("GetVolume() error = %v", err)
	}
	if !ok || vol != 70 {
		t.Fatalf("GetVolume() = %d (ok=%v), want 70", vol, ok)
	}
}
