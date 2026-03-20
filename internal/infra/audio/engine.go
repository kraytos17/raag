package audio

import (
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
	"github.com/p-society/raag/internal/app"
)

var (
	speakerOnce   sync.Once
	ErrNoStreamer = errors.New("audio: no active streamer")
	ErrNotPlaying = errors.New("audio: not playing")
	ErrNotPaused  = errors.New("audio: not paused")
)

type Engine struct {
	mu         sync.Mutex
	sampleRate beep.SampleRate
	ctrl       *beep.Ctrl
	vol        *effects.Volume
	streamer   beep.StreamSeekCloser
	format     beep.Format
	state      app.PlayerState
	position   time.Duration
	done       chan struct{}
}

func NewEngine(sampleRate int) *Engine {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	return &Engine{
		sampleRate: beep.SampleRate(sampleRate),
		state:      app.PlayerStateIdle,
		done:       make(chan struct{}, 1),
	}
}

func (e *Engine) Play(ctx context.Context, reader io.Reader, mimeType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopLocked()
	rc := ensureReadSeekCloser(reader)
	streamer, format, err := decode(rc, mimeType)
	if err != nil {
		e.state = app.PlayerStateError
		return fmt.Errorf("decode failed: %w", err)
	}

	e.streamer = streamer
	e.format = format
	if err := e.initSpeaker(); err != nil {
		e.state = app.PlayerStateError
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

	speaker.Clear()
	speaker.Play(beep.Seq(e.vol, beep.Callback(func() {
		e.mu.Lock()
		e.state = app.PlayerStateIdle
		e.streamer = nil
		e.ctrl = nil
		e.vol = nil
		e.mu.Unlock()
		select {
		case e.done <- struct{}{}:
		default:
		}
	})))

	e.state = app.PlayerStatePlaying
	e.position = 0
	slog.Info("playback started", "mime", mimeType, "sample_rate", format.SampleRate)
	return nil
}

func (e *Engine) Pause(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != app.PlayerStatePlaying {
		return ErrNotPlaying
	}
	if e.ctrl != nil {
		speaker.Lock()
		e.ctrl.Paused = true
		speaker.Unlock()
	}
	e.state = app.PlayerStatePaused
	return nil
}

func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != app.PlayerStatePaused {
		return ErrNotPaused
	}
	if e.ctrl != nil {
		speaker.Lock()
		e.ctrl.Paused = false
		speaker.Unlock()
	}

	e.state = app.PlayerStatePlaying
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
		e.streamer.Close()
	}

	e.streamer = nil
	e.ctrl = nil
	e.vol = nil
	e.format = beep.Format{}
	e.state = app.PlayerStateIdle
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

func (e *Engine) SetVolume(ctx context.Context, volume int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.vol == nil {
		return ErrNoStreamer
	}

	speaker.Lock()
	if volume <= 0 {
		e.vol.Silent = true
	} else {
		e.vol.Silent = false
		e.vol.Volume = (float64(volume)/100.0)*8.0 - 8.0
	}

	speaker.Unlock()
	return nil
}

func (e *Engine) GetState() app.PlayerState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *Engine) GetPosition() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.streamer == nil || e.format.SampleRate == 0 {
		return e.position
	}
	return e.sampleRate.D(e.streamer.Position())
}

func (e *Engine) Done() <-chan struct{} {
	return e.done
}

func (e *Engine) initSpeaker() error {
	var initErr error
	speakerOnce.Do(func() {
		bufferSize := e.sampleRate.N(time.Millisecond * 100)
		initErr = speaker.Init(e.sampleRate, bufferSize)
		if initErr != nil {
			slog.Error("speaker init failed", "error", initErr)
		}
	})
	return initErr
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
		streamer, format, err = mp3.Decode(rc)
		if err != nil {
			rc.Close()
			return nil, format, err
		}
	}
	return streamer, format, err
}

func ensureReadSeekCloser(r io.Reader) io.ReadSeekCloser {
	if rsc, ok := r.(io.ReadSeekCloser); ok {
		return rsc
	}
	if rs, ok := r.(io.ReadSeeker); ok {
		return nopCloser{rs}
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return &readCloserFromBytes{data: data}
	}
	return &readCloserFromBytes{data: data}
}

type nopCloser struct {
	io.ReadSeeker
}

func (n nopCloser) Close() error {
	return nil
}

type readCloserFromBytes struct {
	data []byte
	pos  int
}

func (r *readCloserFromBytes) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *readCloserFromBytes) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = int64(r.pos) + offset
	case io.SeekEnd:
		newPos = int64(len(r.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if newPos < 0 {
		return 0, errors.New("negative position")
	}

	r.pos = int(newPos)
	return newPos, nil
}

func (r *readCloserFromBytes) Close() error {
	r.data = nil
	return nil
}
