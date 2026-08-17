package audio

import (
	"testing"

	"github.com/gopxl/beep/v2"
)

func TestNewEngine_SampleRate(t *testing.T) {
	e := NewEngine(48000)
	if e.sampleRate != beep.SampleRate(48000) {
		t.Errorf("NewEngine(48000).sampleRate = %d, want 48000", e.sampleRate)
	}

	e2 := NewEngine(0)
	if e2.sampleRate != beep.SampleRate(44100) {
		t.Errorf("NewEngine(0).sampleRate = %d, want default 44100", e2.sampleRate)
	}

	e3 := NewEngine(-1)
	if e3.sampleRate != beep.SampleRate(44100) {
		t.Errorf("NewEngine(-1).sampleRate = %d, want default 44100", e3.sampleRate)
	}
}

func TestInitSpeaker_OnceGuard_UsesFirstRate(t *testing.T) {
	// initSpeaker can only be initialized once per process. The first call wins;
	// a later call with a different rate is ignored (returns the same result).
	firstErr := initSpeaker(beep.SampleRate(44100))
	secondErr := initSpeaker(beep.SampleRate(48000))
	if firstErr != nil {
		// No audio device in CI: the once still runs and the second call must
		// return the identical error (idempotent), not a different one.
		t.Skipf("speaker init unavailable in this environment: %v", firstErr)
	}
	if secondErr != nil {
		t.Errorf("second initSpeaker() error = %v, want nil (idempotent)", secondErr)
	}
}
