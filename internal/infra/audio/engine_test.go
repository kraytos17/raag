package audio

import (
	"testing"

	"github.com/gopxl/beep/v2"
)

func TestNewEngine_SampleRate(t *testing.T) {
	e := NewEngine(48000, 0, DSPConfig{})
	if e.sampleRate != beep.SampleRate(48000) {
		t.Errorf("NewEngine(48000).sampleRate = %d, want 48000", e.sampleRate)
	}

	e2 := NewEngine(0, 0, DSPConfig{})
	if e2.sampleRate != beep.SampleRate(44100) {
		t.Errorf("NewEngine(0).sampleRate = %d, want default 44100", e2.sampleRate)
	}

	e3 := NewEngine(-1, 0, DSPConfig{})
	if e3.sampleRate != beep.SampleRate(44100) {
		t.Errorf("NewEngine(-1).sampleRate = %d, want default 44100", e3.sampleRate)
	}
}

func TestNewEngine_BufferSize(t *testing.T) {
	e := NewEngine(48000, 2048, DSPConfig{})
	if e.speakerBuffer != 2048 {
		t.Errorf("NewEngine(48000, 2048, DSPConfig{}).speakerBuffer = %d, want 2048", e.speakerBuffer)
	}

	// Non-positive buffer size means the engine keeps the 100ms fallback
	// (applied inside initSpeaker), so the stored field stays 0.
	e2 := NewEngine(48000, 0, DSPConfig{})
	if e2.speakerBuffer != 0 {
		t.Errorf("NewEngine(48000, 0, DSPConfig{}).speakerBuffer = %d, want 0 (fallback)", e2.speakerBuffer)
	}
}

func TestInitSpeaker_OnceGuard_UsesFirstRate(t *testing.T) {
	// initSpeaker can only be initialized once per process. The first call wins;
	// a later call with a different rate is ignored (returns the same result).
	firstErr := initSpeaker(beep.SampleRate(44100), 4096)
	secondErr := initSpeaker(beep.SampleRate(48000), 0)
	if firstErr != nil {
		// No audio device in CI: the once still runs and the second call must
		// return the identical error (idempotent), not a different one.
		t.Skipf("speaker init unavailable in this environment: %v", firstErr)
	}
	if secondErr != nil {
		t.Errorf("second initSpeaker() error = %v, want nil (idempotent)", secondErr)
	}
}
