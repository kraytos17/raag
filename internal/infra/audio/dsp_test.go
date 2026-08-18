package audio

import (
	"math"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// constStreamer yields a constant amplitude on both channels.
type constStreamer struct {
	amp float64
}

func (c *constStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		samples[i][0] = c.amp
		samples[i][1] = c.amp
	}
	return len(samples), true
}

func (c *constStreamer) Err() error { return nil }

// finiteStreamer emits a constant amplitude for exactly `total` samples, then EOF.
type finiteStreamer struct {
	amp       float64
	remaining int
}

func (f *finiteStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if f.remaining <= 0 {
		return 0, false
	}
	n = min(len(samples), f.remaining)
	for i := 0; i < n; i++ {
		samples[i][0] = f.amp
		samples[i][1] = f.amp
	}
	f.remaining -= n
	return n, true
}

func (f *finiteStreamer) Err() error { return nil }

func TestDSP_Disabled_NoOp(t *testing.T) {
	s := &constStreamer{amp: 0.5}
	out := DSPConfig{}.Apply(s, 44100, 0)
	samples := make([][2]float64, 64)
	if n, ok := out.Stream(samples); !ok || n != 64 {
		t.Fatalf("Stream() = (%d, %v), want (64, true)", n, ok)
	}
	for _, sm := range samples {
		if math.Abs(sm[0]-0.5) > 1e-9 || math.Abs(sm[1]-0.5) > 1e-9 {
			t.Fatalf("disabled DSP changed samples: %v", sm)
		}
	}
}

func TestDSP_Equalizer_ZeroGains_NoOp(t *testing.T) {
	cfg := DSPConfig{Equalizer: EqualizerConfig{Enabled: true, Bass: 0, Mid: 0, Treble: 0}}
	s := &constStreamer{amp: 0.5}
	out := cfg.Apply(s, 44100, 0)
	samples := make([][2]float64, 64)
	if n, ok := out.Stream(samples); !ok || n != 64 {
		t.Fatalf("Stream() = (%d, %v), want (64, true)", n, ok)
	}
	for _, sm := range samples {
		if math.Abs(sm[0]-0.5) > 1e-6 {
			t.Fatalf("zero-gain EQ changed sample L to %v", sm[0])
		}
	}
}

func TestDSP_Equalizer_BassBoost_ChangesSamples(t *testing.T) {
	cfg := DSPConfig{Equalizer: EqualizerConfig{Enabled: true, Bass: 12, Mid: 0, Treble: 0}}
	s := &constStreamer{amp: 0.25}
	out := cfg.Apply(s, 44100, 0)
	samples := make([][2]float64, 512)
	if n, ok := out.Stream(samples); !ok || n != len(samples) {
		t.Fatalf("Stream() = (%d, %v), want (%d, true)", n, ok, len(samples))
	}

	var peak float64
	for _, sm := range samples {
		if a := math.Abs(sm[0]); a > peak {
			peak = a
		}
	}
	if peak <= 0.25+1e-6 {
		t.Fatalf("bass boost did not raise amplitude: peak = %v, want > 0.25", peak)
	}
}

func TestDSP_Normalize_GainApplied(t *testing.T) {
	// target −14 dB, track measured at −20 dB → +6 dB ≈ ×1.995.
	cfg := DSPConfig{Normalize: NormalizeConfig{Enabled: true, TargetDB: -14}}
	s := &constStreamer{amp: 0.5}
	out := cfg.Apply(s, 44100, -20)
	samples := make([][2]float64, 64)
	if n, ok := out.Stream(samples); !ok || n != 64 {
		t.Fatalf("Stream() = (%d, %v), want (64, true)", n, ok)
	}

	want := 0.5 * dbToGain(-14-(-20))
	for _, sm := range samples {
		if math.Abs(sm[0]-want) > 1e-9 {
			t.Fatalf("normalize sample = %v, want ~%v", sm[0], want)
		}
	}
}

func TestDSP_Normalize_Disabled_TrackGainIgnored(t *testing.T) {
	cfg := DSPConfig{} // normalize disabled
	s := &constStreamer{amp: 0.5}
	out := cfg.Apply(s, 44100, -100) // huge gain but normalize disabled
	samples := make([][2]float64, 64)
	_, ok := out.Stream(samples)
	if !ok {
		t.Fatal("Stream() = false, want true")
	}
	for _, sm := range samples {
		if math.Abs(sm[0]-0.5) > 1e-9 {
			t.Fatalf("disabled normalize applied gain: %v", sm[0])
		}
	}
}

func TestVolumeToDB(t *testing.T) {
	cases := []struct {
		vol  int
		want float64
	}{
		{0, -5},
		{100, 0},
		{50, -2.5},
		{-5, -5},
		{150, 0},
	}
	for _, tc := range cases {
		got := volumeToDB(tc.vol)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("volumeToDB(%d) = %v, want %v", tc.vol, got, tc.want)
		}
	}
}

func TestCrossfadeChain_NoCrossfade_ReturnsNew(t *testing.T) {
	oldS := &constStreamer{amp: 1}
	newS := &constStreamer{amp: 0.5}
	got := crossfadeChain(oldS, newS, 44100, 0)
	if got != beep.Streamer(newS) {
		t.Fatal("crossfadeChain with 0 duration should return the new streamer")
	}
}

func TestCrossfadeChain_OverlapsBothStreamers(t *testing.T) {
	oldS := &finiteStreamer{amp: 1, remaining: 4410}   // ~100ms
	newS := &finiteStreamer{amp: 0.5, remaining: 8820} // ~200ms
	chain := crossfadeChain(oldS, newS, 44100, 10*time.Millisecond)

	// Draining the chain must consume both streamers (the old fades to 0, the
	// new fades in and continues after the old drains).
	samples := make([][2]float64, 512)
	var totalOld, totalNew float64
	for {
		n, ok := chain.Stream(samples)
		for i := range samples[:n] {
			totalOld += samples[i][0]
			totalNew += samples[i][1]
		}
		if !ok {
			break
		}
	}
	if totalOld <= 0 {
		t.Fatal("old streamer contributed nothing to the crossfade")
	}
	if totalNew <= 0 {
		t.Fatal("new streamer contributed nothing to the crossfade")
	}
}
