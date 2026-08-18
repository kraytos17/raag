package audio

import (
	"math"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
)

// DSPConfig is the signal-processing section of the audio pipeline, derived
// from playback config. It is pure (no speaker dependency) so it is unit
// testable and can be rebuilt live by the engine.
type DSPConfig struct {
	Equalizer EqualizerConfig
	Normalize NormalizeConfig
	Crossfade time.Duration
}

type EqualizerConfig struct {
	Enabled bool
	Bass    float64 // dB, [-12, 12]
	Mid     float64 // dB, [-12, 12]
	Treble  float64 // dB, [-12, 12]
}

type NormalizeConfig struct {
	Enabled  bool
	TargetDB float64 // dBFS target loudness, [-40, 0]
}

// Enabled reports whether any DSP processing is configured.
func (c DSPConfig) Enabled() bool {
	return c.Equalizer.Enabled || c.Normalize.Enabled || c.Crossfade > 0
}

// Apply wraps the base streamer with the configured processing in order:
// Equalizer → loudness normalization. The returned streamer runs at the given
// sample rate. trackGainDB is the track's measured loudness (see LoudnessDB);
// when normalize is enabled the gain applied is target − track loudness.
func (c DSPConfig) Apply(s beep.Streamer, sr beep.SampleRate, trackGainDB float64) beep.Streamer {
	if c.Equalizer.Enabled {
		s = effects.NewEqualizer(s, sr, c.equalizerSections())
	}
	if c.Normalize.Enabled {
		gain := c.Normalize.TargetDB - trackGainDB
		// effects.Gain multiplies by 1+Gain, so subtract unity to land on the
		// linear multiplier for the dB offset.
		s = &effects.Gain{Streamer: s, Gain: dbToGain(gain) - 1}
	}
	return s
}

// equalizerSections builds a three-band parametric EQ. Each section uses the
// shelving/peak parameters recommended for the GK Nilsen equalizer.
func (c DSPConfig) equalizerSections() effects.MonoEqualizerSections {
	return effects.MonoEqualizerSections{
		{F0: 100, Bf: 120, GB: 3, G0: 0, G: c.Equalizer.Bass},
		{F0: 1000, Bf: 500, GB: 3, G0: 0, G: c.Equalizer.Mid},
		{F0: 6000, Bf: 4000, GB: 3, G0: 0, G: c.Equalizer.Treble},
	}
}

// dbToGain converts a gain in decibels to a linear multiplier (10^(db/20)).
func dbToGain(db float64) float64 {
	return math.Pow(10, db/20.0)
}

// volumeToDB maps the user-facing 0..100 volume to the beep Volume parameter
// (Base 2 exponent). 0% maps to −5 (≈ −30 dB, effectively silent) and 100%
// maps to 0 (unity), giving a human-natural exponential response.
func volumeToDB(volume int) float64 {
	if volume <= 0 {
		return -5
	}
	if volume >= 100 {
		return 0
	}
	return (float64(volume)/100.0)*5.0 - 5.0
}

// crossfadeChain composes an overlap fade between an outgoing and an incoming
// streamer over the given number of samples. The result is a single streamer
// that plays the old (fading out) and new (fading in) mixed together; when the
// old drains it is dropped by the mixer and the new continues to the end.
func crossfadeChain(old, new beep.Streamer, sr beep.SampleRate, crossfade time.Duration) beep.Streamer {
	if crossfade <= 0 || old == nil || new == nil {
		return new
	}

	n := sr.N(crossfade)
	if n <= 0 {
		return new
	}
	return beep.Mix(
		effects.Transition(old, n, 1, 0, effects.TransitionEqualPower),
		effects.Transition(new, n, 0, 1, effects.TransitionEqualPower),
	)
}
