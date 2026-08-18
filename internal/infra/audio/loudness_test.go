package audio

import (
	"bytes"
	"math"
	"os"
	"testing"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
)

// sineStreamer emits a short sine tone at the given amplitude.
type sineStreamer struct {
	sr        beep.SampleRate
	amp       float64
	freq      float64
	remaining int
	pos       int
}

func (s *sineStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		if s.remaining <= 0 {
			return i, i > 0
		}

		v := s.amp * math.Sin(2*math.Pi*s.freq*float64(s.pos)/float64(s.sr))
		samples[i][0] = v
		samples[i][1] = v
		s.pos++
		s.remaining--
	}
	return len(samples), true
}

func (s *sineStreamer) Err() error { return nil }

// genWav writes a WAV of a sine tone and returns its bytes.
func genWav(t *testing.T, amp float64, samples int) []byte {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "tone-*.wav")
	if err != nil {
		t.Fatalf("create temp wav: %v", err)
	}
	defer func() { _ = f.Close() }()

	sr := beep.SampleRate(44100)
	format := beep.Format{NumChannels: 2, SampleRate: sr, Precision: 2}
	s := &sineStreamer{sr: sr, amp: amp, freq: 440, remaining: samples}
	if err := wav.Encode(f, s, format); err != nil {
		t.Fatalf("wav encode: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read temp wav: %v", err)
	}
	return data
}

func TestEstimateLoudnessDB_LoudTone(t *testing.T) {
	data := genWav(t, 0.5, 44100) // 1s at -6 dBFS amplitude
	rc := nopCloser{bytes.NewReader(data)}
	db, err := EstimateLoudnessDB(rc, "audio/wav")
	if err != nil {
		t.Fatalf("EstimateLoudnessDB() error = %v", err)
	}
	// A 0.5 amplitude sine has RMS 0.5/sqrt(2) ≈ -9 dBFS.
	if db > -6 || db < -13 {
		t.Fatalf("loudness = %v dB, want ≈ -9 dBFS", db)
	}
}

func TestEstimateLoudnessDB_Silence(t *testing.T) {
	data := genWav(t, 0.0, 44100)
	rc := nopCloser{bytes.NewReader(data)}
	db, err := EstimateLoudnessDB(rc, "audio/wav")
	if err != nil {
		t.Fatalf("EstimateLoudnessDB() error = %v", err)
	}
	if db > -30 {
		t.Fatalf("silence loudness = %v dB, want very negative", db)
	}
}

func TestEstimateLoudnessDB_Invalid(t *testing.T) {
	rc := nopCloser{bytes.NewReader([]byte("not audio"))}
	if _, err := EstimateLoudnessDB(rc, "audio/wav"); err == nil {
		t.Fatal("EstimateLoudnessDB() on garbage = nil error, want error")
	}
}
