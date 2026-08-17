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
// speaker.Init can only be called once per process, so the first call wins;
// subsequent calls with a different rate are ignored.
func initSpeaker(rate beep.SampleRate) error {
	initSpeakerOnce.Do(func() {
		bufferSize := rate.N(time.Millisecond * 100)
		initSpeakerErr = speaker.Init(rate, bufferSize)
	})
	return initSpeakerErr
}

type Engine struct {
	mu            sync.RWMutex
	sampleRate    beep.SampleRate
	ctrl          *beep.Ctrl
	vol           *effects.Volume
	streamer      beep.StreamSeekCloser
	format        beep.Format
	state         domain.PlayerState
	position      time.Duration
	done          chan struct{}
	desiredVolume int
	audioSource   AudioSource
}

func NewEngine(sampleRate int) *Engine {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	return &Engine{
		sampleRate: beep.SampleRate(sampleRate),
		state:      domain.PlayerStateIdle,
		done:       make(chan struct{}, 1),
	}
}

func (e *Engine) Play(ctx context.Context, reader io.Reader, mimeType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopLocked()
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

	e.streamer = streamer
	e.format = format
	if err := e.initSpeaker(); err != nil {
		e.state = domain.PlayerStateError
		return err
	}

	var s beep.Streamer = e.streamer
	if format.SampleRate != e.sampleRate {
		s = beep.Resample(4, format.SampleRate, e.sampleRate, s)
	}

	e.ctrl = &beep.Ctrl{Streamer: s, Paused: false}
	e.vol = &effects.Volume{
		Streamer: e.ctrl,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}
	if e.desiredVolume > 0 {
		e.setVolumeLocked(e.desiredVolume)
	}

	speaker.Clear()
	speaker.Play(beep.Seq(e.vol, beep.Callback(func() {
		e.mu.Lock()
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
	slog.Info("playback started", "mime", mimeType, "sample_rate", format.SampleRate)
	return nil
}

func (e *Engine) PlayStreaming(ctx context.Context, source AudioSource, mimeType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopLocked()
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

	e.streamer = streamer
	e.format = format
	e.audioSource = source

	var s beep.Streamer = e.streamer
	if format.SampleRate != e.sampleRate {
		s = beep.Resample(4, format.SampleRate, e.sampleRate, s)
	}

	e.ctrl = &beep.Ctrl{Streamer: s, Paused: false}
	e.vol = &effects.Volume{
		Streamer: e.ctrl,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}
	if e.desiredVolume > 0 {
		e.setVolumeLocked(e.desiredVolume)
	}

	speaker.Clear()
	speaker.Play(beep.Seq(e.vol, beep.Callback(func() {
		e.mu.Lock()
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
	slog.Info("streaming playback started", "mime", mimeType, "sample_rate", format.SampleRate, "buffer_fill", source.FillLevel())
	return nil
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
	if e.streamer != nil {
		_ = e.streamer.Close()
	}
	if e.audioSource != nil {
		_ = e.audioSource.Close()
		e.audioSource = nil
	}

	e.streamer = nil
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
		e.vol.Volume = (float64(volume)/100.0)*8.0 - 8.0
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
	return initSpeaker(e.sampleRate)
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
