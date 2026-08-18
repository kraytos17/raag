package app

import (
	"sync/atomic"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestLibraryScanner_MultipleProgressHandlers(t *testing.T) {
	sc := NewLibraryScanner(nil, nil, nil, nil, nil)

	var a, b atomic.Int32
	handlerA := func(p ScanProgress) { a.Add(1) }
	handlerB := func(p ScanProgress) { b.Add(1) }

	sc.OnProgress(handlerA)
	sc.OnProgress(handlerB)

	sc.notifyProgress(ScanProgress{Scanned: 1, Total: 2, Phase: domain.ScanPhaseParsing})
	sc.notifyProgress(ScanProgress{Scanned: 2, Total: 2, Phase: domain.ScanPhaseParsing})
	if a.Load() != 2 {
		t.Errorf("handlerA got %d calls, want 2", a.Load())
	}
	if b.Load() != 2 {
		t.Errorf("handlerB got %d calls, want 2", b.Load())
	}
}

func TestLibraryScanner_RemoveProgressHandler(t *testing.T) {
	sc := NewLibraryScanner(nil, nil, nil, nil, nil)

	var a, b atomic.Int32
	handlerA := func(p ScanProgress) { a.Add(1) }
	handlerB := func(p ScanProgress) { b.Add(1) }

	sc.OnProgress(handlerA)
	sc.OnProgress(handlerB)
	sc.RemoveProgressHandler(handlerA)

	sc.notifyProgress(ScanProgress{Scanned: 1, Total: 1, Phase: domain.ScanPhaseParsing})
	if a.Load() != 0 {
		t.Errorf("removed handlerA got %d calls, want 0", a.Load())
	}
	if b.Load() != 1 {
		t.Errorf("handlerB got %d calls, want 1", b.Load())
	}
}
