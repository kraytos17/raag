package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	"github.com/p-society/raag/internal/domain"
)

var (
	ErrNoStreamer = errors.New("audio: no active streamer")
	ErrNotPlaying = errors.New("audio: not playing")
	ErrNotPaused  = errors.New("audio: not paused")
)

var (
	initSpeakerOnce sync.Once
	initSpeakerErr  error
)

// initSpeaker initializes the global speaker at the requested sample rate.
// bufferSize is the speaker buffer in samples; a non-positive value falls back
// to a 100ms buffer. speaker.Init can only be called once per process, so the
// first call wins; subsequent calls with a different rate are ignored.
func initSpeaker(rate beep.SampleRate, bufferSize int) error {
	initSpeakerOnce.Do(func() {
		if bufferSize <= 0 {
			bufferSize = rate.N(time.Millisecond * 100)
		}
		initSpeakerErr = speaker.Init(rate, bufferSize)
	})
	return initSpeakerErr
}

type Engine struct {
	mu            sync.RWMutex
	sampleRate    beep.SampleRate
	speakerBuffer int
	dsp           DSPConfig
	trackGainDB   float64
	ctrl          *beep.Ctrl
	vol           *effects.Volume
	streamer      beep.StreamSeekCloser
	baseStreamer  beep.Streamer
	format        beep.Format
	state         domain.PlayerState
	position      time.Duration
	done          chan struct{}
	desiredVolume int
	audioSource   AudioSource
	fading        bool
}

// NewEngine creates an audio engine. bufferSize is the speaker buffer in
// samples (config playback.buffer_size); a non-positive value uses a 100ms
// default buffer. dsp carries the EQ/normalize/crossfade processing.
func NewEngine(sampleRate int, bufferSize int, dsp DSPConfig) *Engine {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	return &Engine{
		sampleRate:    beep.SampleRate(sampleRate),
		speakerBuffer: bufferSize,
		dsp:           dsp,
		state:         domain.PlayerStateIdle,
		done:          make(chan struct{}, 1),
	}
}

func (e *Engine) Play(ctx context.Context, reader io.Reader, mimeType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rc, err := ensureReadSeekCloser(reader)
	if err != nil {
		e.state = domain.PlayerStateError
		return fmt.Errorf("failed to read audio data: %w", err)
	}

	streamer, format, err := decode(rc, mimeType)
	if err != nil {
		e.state = domain.PlayerStateError
		return fmt.Errorf("decode failed: %w", err)
	}
	if err := e.initSpeaker(); err != nil {
		e.state = domain.PlayerStateError
		return err
	}

	e.startChainLocked(streamer, format, nil)
	slog.Info("playback started", "mime", mimeType, "sample_rate", format.SampleRate)
	return nil
}

func (e *Engine) PlayStreaming(ctx context.Context, source AudioSource, mimeType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.initSpeaker(); err != nil {
		e.state = domain.PlayerStateError
		return err
	}

	streamer, format, err := decode(source, mimeType)
	if err != nil {
		e.state = domain.PlayerStateError
		_ = source.Close()
		return fmt.Errorf("decode failed: %w", err)
	}

	e.startChainLocked(streamer, format, source)
	slog.Info("streaming playback started", "mime", mimeType, "sample_rate", format.SampleRate, "buffer_fill", source.FillLevel())
	return nil
}

// startChainLocked wires a freshly decoded streamer into the speaker. When a
// previous track is still mid-play and crossfade is configured, the old chain
// fades out while the new fades in (overlap); otherwise the old is hard-swapped
// out. Caller must hold e.mu.
func (e *Engine) startChainLocked(streamer beep.StreamSeekCloser, format beep.Format, source AudioSource) {
	oldVol := e.vol
	oldStreamer := e.streamer
	oldSource := e.audioSource
	crossfade := e.dsp.Crossfade > 0 && oldVol != nil && e.state == domain.PlayerStatePlaying && !e.fading

	if !crossfade {
		e.stopLocked()
	} else {
		e.fading = true
	}

	e.streamer = streamer
	e.format = format
	e.audioSource = source

	e.baseStreamer = resampleIfNeeded(e.streamer, format.SampleRate, e.sampleRate)
	e.ctrl = &beep.Ctrl{Streamer: e.dsp.Apply(e.baseStreamer, e.sampleRate, e.trackGainDB), Paused: false}
	e.vol = &effects.Volume{
		Streamer: e.ctrl,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}
	if e.desiredVolume > 0 {
		e.setVolumeLocked(e.desiredVolume)
	}

	var play beep.Streamer
	if crossfade {
		// The old vol is still referenced by the previous speaker.Play; wrap it
		// in a fade-out and close its underlying streamer once it drains.
		oldWithClose := beep.Seq(oldVol, beep.Callback(func() {
			if oldStreamer != nil {
				_ = oldStreamer.Close()
			}
			if oldSource != nil {
				_ = oldSource.Close()
			}
		}))
		play = crossfadeChain(oldWithClose, e.vol, e.sampleRate, e.dsp.Crossfade)
	} else {
		play = e.vol
	}

	speaker.Clear()
	speaker.Play(beep.Seq(play, beep.Callback(func() {
		e.mu.Lock()
		e.fading = false
		if e.audioSource != nil {
			_ = e.audioSource.Close()
			e.audioSource = nil
		}

		e.state = domain.PlayerStateIdle
		e.streamer = nil
		e.ctrl = nil
		e.vol = nil
		e.mu.Unlock()

		select {
		case e.done <- struct{}{}:
		default:
		}
	})))

	e.state = domain.PlayerStatePlaying
	e.position = 0
}

func (e *Engine) GetBufferFillLevel() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.audioSource != nil {
		return e.audioSource.FillLevel()
	}
	return 0
}

func (e *Engine) IsBuffering() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.audioSource != nil {
		return e.audioSource.IsBuffering()
	}
	return false
}

func (e *Engine) Pause(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != domain.PlayerStatePlaying {
		return ErrNotPlaying
	}
	if e.ctrl != nil {
		speaker.Lock()
		e.ctrl.Paused = true
		speaker.Unlock()
	}
	e.state = domain.PlayerStatePaused
	return nil
}

func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != domain.PlayerStatePaused {
		return ErrNotPaused
	}
	if e.ctrl != nil {
		speaker.Lock()
		e.ctrl.Paused = false
		speaker.Unlock()
	}

	e.state = domain.PlayerStatePlaying
	return nil
}

func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
	return nil
}

func (e *Engine) stopLocked() {
	speaker.Clear()
	e.fading = false
	if e.streamer != nil {
		_ = e.streamer.Close()
	}
	if e.audioSource != nil {
		_ = e.audioSource.Close()
		e.audioSource = nil
	}

	e.streamer = nil
	e.baseStreamer = nil
	e.ctrl = nil
	e.vol = nil
	e.format = beep.Format{}
	e.state = domain.PlayerStateIdle
	e.position = 0
}

func (e *Engine) Seek(ctx context.Context, position time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.streamer == nil {
		return ErrNoStreamer
	}

	speaker.Lock()
	err := e.streamer.Seek(e.sampleRate.N(position))
	speaker.Unlock()
	if err != nil {
		return err
	}

	e.position = position
	return nil
}

func (e *Engine) setVolumeLocked(volume int) {
	speaker.Lock()
	if volume <= 0 {
		e.vol.Silent = true
	} else {
		e.vol.Silent = false
		e.vol.Volume = volumeToDB(volume)
	}
	speaker.Unlock()
}

func (e *Engine) SetVolume(ctx context.Context, volume int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.desiredVolume = volume
	if e.vol == nil {
		return nil
	}
	e.setVolumeLocked(volume)
	return nil
}

// resampleIfNeeded returns the streamer resampled to the engine's output rate
// when its native rate differs; otherwise the streamer unchanged.
func resampleIfNeeded(s beep.Streamer, srcRate, outRate beep.SampleRate) beep.Streamer {
	if srcRate == outRate {
		return s
	}
	return beep.Resample(4, srcRate, outRate, s)
}

// SetDSP replaces the DSP processing live. The equalizer recomputes its filter
// coefficients, so the Ctrl streamer is rebuilt under the speaker lock. When
// nothing is playing the config is stored for the next track.
func (e *Engine) SetDSP(dsp DSPConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.dsp = dsp
	e.rebuildDSPChainLocked()
}

// SetLoudness sets the current track's measured loudness (dBFS), used by
// loudness normalization as the reference for the gain applied.
func (e *Engine) SetLoudness(db float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.trackGainDB = db
	e.rebuildDSPChainLocked()
}

// SetEqualizer replaces the EQ band gains live and enables/disables the EQ.
func (e *Engine) SetEqualizer(settings domain.EqualizerSettings) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.dsp.Equalizer = EqualizerConfig{
		Enabled: settings.Enabled,
		Bass:    settings.Bass,
		Mid:     settings.Mid,
		Treble:  settings.Treble,
	}
	e.rebuildDSPChainLocked()
}

// GetEqualizer returns the current EQ band gains and enable state.
func (e *Engine) GetEqualizer() domain.EqualizerSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return domain.EqualizerSettings{
		Enabled: e.dsp.Equalizer.Enabled,
		Bass:    e.dsp.Equalizer.Bass,
		Mid:     e.dsp.Equalizer.Mid,
		Treble:  e.dsp.Equalizer.Treble,
	}
}

// rebuildDSPChainLocked re-applies the DSP chain over the resampled base
// streamer and swaps it into the running Ctrl. No-op when idle. Caller must
// hold e.mu.
func (e *Engine) rebuildDSPChainLocked() {
	if e.baseStreamer == nil || e.ctrl == nil {
		return
	}

	speaker.Lock()
	e.ctrl.Streamer = e.dsp.Apply(e.baseStreamer, e.sampleRate, e.trackGainDB)
	speaker.Unlock()
}

func (e *Engine) GetState() domain.PlayerState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

func (e *Engine) GetPosition() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.streamer == nil || e.format.SampleRate == 0 {
		return e.position
	}
	return e.sampleRate.D(e.streamer.Position())
}

func (e *Engine) Done() <-chan struct{} {
	return e.done
}

func (e *Engine) initSpeaker() error {
	return initSpeaker(e.sampleRate, e.speakerBuffer)
}

func decode(rc io.ReadCloser, mimeType string) (beep.StreamSeekCloser, beep.Format, error) {
	var (
		streamer beep.StreamSeekCloser
		format   beep.Format
		err      error
	)

	switch strings.ToLower(mimeType) {
	case "audio/mpeg", "audio/mp3":
		streamer, format, err = mp3.Decode(rc)
	case "audio/flac":
		streamer, format, err = flac.Decode(rc)
	case "audio/wav":
		streamer, format, err = wav.Decode(rc)
	case "audio/ogg", "audio/vorbis":
		streamer, format, err = vorbis.Decode(rc)
	default:
		slog.Warn("unknown mime type; attempting mp3 decode", "mime", mimeType)
		streamer, format, err = mp3.Decode(rc)
	}

	if err != nil {
		_ = rc.Close()
		return nil, format, err
	}
	return streamer, format, err
}

func ensureReadSeekCloser(r io.Reader) (io.ReadSeekCloser, error) {
	if rsc, ok := r.(io.ReadSeekCloser); ok {
		return rsc, nil
	}
	if rs, ok := r.(io.ReadSeeker); ok {
		return nopCloser{rs}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return &readSeekNopCloser{bytes.NewReader(data)}, nil
}

type readSeekNopCloser struct {
	*bytes.Reader
}

func (readSeekNopCloser) Close() error { return nil }

type nopCloser struct {
	io.ReadSeeker
}

func (n nopCloser) Close() error {
	return nil
}
