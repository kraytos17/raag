package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/transcoder"
)

// genTestAAC creates a short AAC/ADTS file via ffmpeg (a codec the local engine
// cannot decode natively).
func genTestAAC(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.aac")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-ac", "1", "-ar", "44100", "-c:a", codecAAC, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate AAC with ffmpeg: %v (%s)", err, out)
	}
	return path
}

func TestLocalResolver_Decodable_NoTranscode(t *testing.T) {
	src := genTestWav(t)
	track := &domain.Track{ID: domain.TrackID("mp3-1"), Path: src, Codec: codecMP3, MimeType: mimeTypeMPEG}
	repo := &testLibraryRepo{track: track}
	tr := transcoder.New(transcoder.Config{FFmpegPath: "ffmpeg"})
	defer tr.Cleanup()

	r := NewLocalResolver(repo, tr)
	resolved, err := r.Resolve(context.Background(), track.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer func() { _ = resolved.Reader.Close() }()

	if resolved.Source != SourceLocal {
		t.Fatalf("source = %v, want SourceLocal", resolved.Source)
	}
	if resolved.Track.MimeType != mimeTypeMPEG {
		t.Fatalf("mime = %q, want audio/mpeg", resolved.Track.MimeType)
	}
	data, _ := io.ReadAll(resolved.Reader)
	if len(data) == 0 {
		t.Fatal("expected audio data")
	}
}

func TestLocalResolver_TranscodesNonDecodable(t *testing.T) {
	src := genTestAAC(t)
	track := &domain.Track{ID: domain.TrackID("aac-1"), Path: src, Codec: codecAAC, MimeType: mimeTypeAAC}
	repo := &testLibraryRepo{track: track}
	tr := transcoder.New(transcoder.Config{FFmpegPath: "ffmpeg", StreamCodec: codecMP3, StreamBitrate: "96k"})
	defer tr.Cleanup()

	r := NewLocalResolver(repo, tr)
	resolved, err := r.Resolve(context.Background(), track.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer func() { _ = resolved.Reader.Close() }()

	if resolved.Source != SourceLocal {
		t.Fatalf("source = %v, want SourceLocal", resolved.Source)
	}
	// The transcode path must re-tag the track as mp3 so the engine decodes it.
	if resolved.Track.MimeType != mimeTypeMPEG {
		t.Fatalf("mime = %q, want audio/mpeg (transcoded)", resolved.Track.MimeType)
	}
	if resolved.Track.Codec != codecMP3 {
		t.Fatalf("codec = %q, want mp3 (transcoded)", resolved.Track.Codec)
	}
	data, _ := io.ReadAll(resolved.Reader)
	if len(data) == 0 {
		t.Fatal("expected transcoded audio data")
	}
}

func TestLocalResolver_TranscoderNil_NonDecodable_ReturnsRaw(t *testing.T) {
	src := genTestAAC(t)
	track := &domain.Track{ID: domain.TrackID("aac-2"), Path: src, Codec: codecAAC, MimeType: mimeTypeAAC}
	repo := &testLibraryRepo{track: track}

	r := NewLocalResolver(repo, nil)
	resolved, err := r.Resolve(context.Background(), track.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer func() { _ = resolved.Reader.Close() }()

	if resolved.Track.MimeType != mimeTypeAAC {
		t.Fatalf("mime = %q, want audio/aac (raw passthrough)", resolved.Track.MimeType)
	}
	if resolved.Track.Codec != codecAAC {
		t.Fatalf("codec = %q, want aac (raw passthrough)", resolved.Track.Codec)
	}
}

func TestLocalResolver_FileNotAccessible(t *testing.T) {
	track := &domain.Track{ID: domain.TrackID("missing"), Path: filepath.Join(t.TempDir(), "nope.mp3"), Codec: codecMP3}
	repo := &testLibraryRepo{track: track}
	r := NewLocalResolver(repo, nil)

	_, err := r.Resolve(context.Background(), track.ID)
	if err != domain.ErrFileNotAccessible {
		t.Fatalf("Resolve() error = %v, want ErrFileNotAccessible", err)
	}
}

// genTestWav mirrors the transcoder test helper: a short ffmpeg sine WAV.
func genTestWav(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-ac", "1", "-ar", "44100", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate test audio with ffmpeg: %v (%s)", err, out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("ffmpeg produced no output: %v", err)
	}
	return path
}
