package audio

import (
	"bytes"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

// newPreparedEngine prepares a real WAV into a fresh engine and returns it.
func newPreparedEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(44100, 0, DSPConfig{})
	data := genWav(t, 0.5, 44100)
	if err := e.Prepare(nopCloser{bytes.NewReader(data)}, "audio/wav"); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	return e
}

func TestEngine_Prepare_DoesNotPlay(t *testing.T) {
	e := newPreparedEngine(t)
	if !e.HasNext() {
		t.Fatal("HasNext() = false, want true after Prepare")
	}
	if e.state != domain.PlayerStateIdle {
		t.Fatalf("state = %v, want idle (Prepare must not play)", e.state)
	}
}

func TestEngine_Prepare_ReplacesPrevious(t *testing.T) {
	e := NewEngine(44100, 0, DSPConfig{})
	data := genWav(t, 0.5, 44100)
	if err := e.Prepare(nopCloser{bytes.NewReader(data)}, "audio/wav"); err != nil {
		t.Fatalf("Prepare(1) error = %v", err)
	}
	first := e.nextStreamer
	if err := e.Prepare(nopCloser{bytes.NewReader(data)}, "audio/wav"); err != nil {
		t.Fatalf("Prepare(2) error = %v", err)
	}
	if e.nextStreamer == first {
		t.Fatal("second Prepare did not replace the prepared streamer")
	}
	if !e.HasNext() {
		t.Fatal("HasNext() = false after second Prepare")
	}
}

func TestEngine_CommitNext_NoPrepared(t *testing.T) {
	e := NewEngine(44100, 0, DSPConfig{})
	if err := e.CommitNext(); err != nil {
		t.Fatalf("CommitNext() with nothing prepared error = %v, want nil", err)
	}
	if e.state != domain.PlayerStateIdle {
		t.Fatalf("state = %v, want idle", e.state)
	}
}

func TestEngine_Stop_ClearsPrepared(t *testing.T) {
	e := newPreparedEngine(t)
	if !e.HasNext() {
		t.Fatal("HasNext() = false after Prepare")
	}

	e.mu.Lock()
	e.stopLocked()
	e.mu.Unlock()

	if e.HasNext() {
		t.Fatal("HasNext() = true after Stop, want false")
	}
	if e.nextStreamer != nil {
		t.Fatal("nextStreamer not cleared after Stop")
	}
}

func TestEngine_Prepare_InvalidAudio(t *testing.T) {
	e := NewEngine(44100, 0, DSPConfig{})
	err := e.Prepare(nopCloser{bytes.NewReader([]byte("not audio"))}, "audio/wav")
	if err == nil {
		t.Fatal("Prepare() on garbage = nil error, want error")
	}
	if e.HasNext() {
		t.Fatal("HasNext() = true after failed Prepare")
	}
}
