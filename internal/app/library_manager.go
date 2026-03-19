package app

import (
	"context"
)

type LibraryManager struct {
	scanner *LibraryScanner
}

func NewLibraryManager(scanner *LibraryScanner) *LibraryManager {
	return &LibraryManager{
		scanner: scanner,
	}
}

func (m *LibraryManager) ScanLibrary(ctx context.Context) (int, error) {
	return m.scanner.Scan(ctx)
}

func (m *LibraryManager) ScanIncremental(ctx context.Context) (added int, modified int, removed int, err error) {
	return m.scanner.ScanIncremental(ctx)
}
