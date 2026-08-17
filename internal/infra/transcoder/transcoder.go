package transcoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	transcodeTimeout = 5 * time.Minute
	// tmpFilePrefix is used for cached transcoded files.
	tmpFilePrefix = "raag-transcode-"
)

const (
	defaultFFmpegPath = "ffmpeg"
	codecMP3          = "mp3"
	codecOpus         = "opus"
	codecFlac         = "flac"
	codecAAC          = "aac"
)

// Config controls how ffmpeg is invoked for streaming/transcoding.
type Config struct {
	FFmpegPath    string
	StreamCodec   string
	StreamBitrate string
}

// Transcoder wraps ffmpeg to stream or transcode audio on demand.
type Transcoder struct {
	cfg Config

	mu    sync.Mutex
	cache map[string]string // key -> temp file path
}

// New creates a Transcoder. If ffmpegPath is empty, "ffmpeg" is used.
func New(cfg Config) *Transcoder {
	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = defaultFFmpegPath
	}
	if cfg.StreamCodec == "" {
		cfg.StreamCodec = codecOpus
	}
	if cfg.StreamBitrate == "" {
		cfg.StreamBitrate = "128k"
	}
	return &Transcoder{
		cfg:   cfg,
		cache: make(map[string]string),
	}
}

// codecFlag maps a codec name to an ffmpeg -acodec value.
func codecFlag(codec string) string {
	switch codec {
	case "", "copy":
		return "copy"
	case codecMP3:
		return "libmp3lame"
	case codecOpus:
		return "libopus"
	case codecFlac:
		return "flac"
	case codecAAC:
		return "aac"
	default:
		return codec
	}
}

// containerFor maps a codec to its output container.
func containerFor(codec string) string {
	switch codec {
	case codecMP3:
		return "mp3"
	case codecOpus:
		return "ogg"
	case codecFlac:
		return "flac"
	case codecAAC:
		return "adts"
	default:
		return "matroska"
	}
}

// StreamTo transcodes path to the writer w. offset is in nanoseconds and is
// used for fast seeking before decoding. The command environment is cleared
// for sandboxing.
func (t *Transcoder) StreamTo(ctx context.Context, path, codec, bitrate string, offset int64, w io.Writer) error {
	ctx, cancel := transcodeCtx(ctx)
	defer cancel()

	cmd, err := t.buildCmd(ctx, path, codec, bitrate, offset)
	if err != nil {
		return err
	}
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("transcoder: ffmpeg failed: %w", err)
	}
	return nil
}

// TranscodeToFile transcodes path to a temp file and returns its path.
// Results are cached per (path, codec, bitrate) so byte-range serving can
// re-read the same output. The caller is responsible for calling Cleanup.
func (t *Transcoder) TranscodeToFile(ctx context.Context, path, codec, bitrate string) (string, error) {
	key := fmt.Sprintf("%s|%s|%s", path, codec, bitrate)
	t.mu.Lock()
	if cached, ok := t.cache[key]; ok {
		if _, err := os.Stat(cached); err == nil {
			t.mu.Unlock()
			return cached, nil
		}
		delete(t.cache, key)
	}
	t.mu.Unlock()

	tmp, err := os.CreateTemp("", tmpFilePrefix+"*.bin")
	if err != nil {
		return "", fmt.Errorf("transcoder: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close()

	ctx, cancel := transcodeCtx(ctx)
	defer cancel()

	cmd, err := t.buildCmd(ctx, path, codec, bitrate, 0)
	if err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	out, err := os.Create(tmpName)
	if err != nil {
		return "", fmt.Errorf("transcoder: open temp output: %w", err)
	}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		_ = os.Remove(tmpName)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("transcoder: ffmpeg failed: %w", runErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return "", closeErr
	}

	t.mu.Lock()
	t.cache[key] = tmpName
	t.mu.Unlock()
	return tmpName, nil
}

// transcodeCtx bounds a transcode operation by a hard timeout, capped by any
// existing deadline on ctx. The returned cancel must be deferred by the caller.
func transcodeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := transcodeTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(ctx, timeout)
}

// Cleanup removes cached transcoded temp files. Call on daemon shutdown.
func (t *Transcoder) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, path := range t.cache {
		_ = os.Remove(path)
		delete(t.cache, key)
	}
	slog.Debug("transcoder cache cleaned")
}

// buildCmd constructs the sandboxed ffmpeg command.
func (t *Transcoder) buildCmd(ctx context.Context, path, codec, bitrate string, offset int64) (*exec.Cmd, error) {
	args := []string{
		"-nostdin",
		"-loglevel", "error",
	}
	if offset > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", float64(offset)/1e9))
	}
	args = append(args,
		"-i", path,
		"-vn",
		"-acodec", codecFlag(codec),
		"-b:a", bitrate,
		"-f", containerFor(codec),
		"pipe:1",
	)

	cmd := sandboxCmd(ctx, t.cfg.FFmpegPath, args...)
	return cmd, nil
}

// ErrNotFound is returned when a requested transcode source is missing.
var ErrNotFound = errors.New("transcoder: source not found")

// IsSupportedCodec reports whether the codec maps to a known ffmpeg encoder.
func IsSupportedCodec(codec string) bool {
	_, ok := map[string]struct{}{
		codecMP3:  {},
		codecOpus: {},
		codecFlac: {},
		codecAAC:  {},
	}[codec]
	return ok
}

// ResolvePath expands a path to an absolute path (helpers for callers).
func ResolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
