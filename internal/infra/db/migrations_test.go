package db

import (
	"os"
	"testing"
)

func TestRunMigrations(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close(); _ = os.RemoveAll(dir) }()

	if err := RunMigrations(db); err != nil {
		t.Errorf("RunMigrations() error = %v", err)
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close(); _ = os.RemoveAll(dir) }()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() first run error = %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Errorf("RunMigrations() second run error = %v", err)
	}
}

func TestGetSchemaVersion_NewDB(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close(); _ = os.RemoveAll(dir) }()

	version, err := getSchemaVersion(db)
	if err != nil {
		t.Errorf("getSchemaVersion() error = %v", err)
	}
	if version != 0 {
		t.Errorf("getSchemaVersion() on new DB = %d, want 0", version)
	}
}

func TestGetSchemaVersion_AfterMigrations(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close(); _ = os.RemoveAll(dir) }()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	version, err := getSchemaVersion(db)
	if err != nil {
		t.Errorf("getSchemaVersion() error = %v", err)
	}
	if version != len(migrations) {
		t.Errorf("getSchemaVersion() = %d, want %d (latest migration)", version, len(migrations))
	}
}

func TestSetSchemaVersion(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir, DefaultOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close(); _ = os.RemoveAll(dir) }()

	if err := setSchemaVersion(db, 5); err != nil {
		t.Errorf("setSchemaVersion() error = %v", err)
	}

	version, err := getSchemaVersion(db)
	if err != nil {
		t.Errorf("getSchemaVersion() error = %v", err)
	}
	if version != 5 {
		t.Errorf("getSchemaVersion() = %d, want 5", version)
	}
}

func TestMigrationCount(t *testing.T) {
	if len(migrations) != 5 {
		t.Errorf("Expected 5 migrations, got %d", len(migrations))
	}
}

func TestMigrationVersions(t *testing.T) {
	for i, m := range migrations {
		if m.Version != i+1 {
			t.Errorf("Migration[%d].Version = %d, want %d", i, m.Version, i+1)
		}
	}
}

func TestMigrationDescriptions(t *testing.T) {
	expected := []string{
		"initial schema",
		"add play_count field",
		"add gain values",
		"add inverted index",
		"add peer score cache",
	}

	for i, m := range migrations {
		if m.Description != expected[i] {
			t.Errorf("Migration[%d].Description = %q, want %q", i, m.Description, expected[i])
		}
	}
}

func TestMigrationUpDown(t *testing.T) {
	for i, m := range migrations {
		if m.Up == nil {
			t.Errorf("Migration[%d].Up is nil", i)
		}
		if m.Down == nil {
			t.Errorf("Migration[%d].Down is nil", i)
		}
	}
}
