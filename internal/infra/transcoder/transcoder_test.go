package transcoder

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// genTestWav creates a short valid WAV using ffmpeg's sine source.
func genTestWav(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")
	cmd := exec.Command(defaultFFmpegPath, "-nostdin", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-ac", "1", "-ar", "44100", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate test audio with ffmpeg: %v (%s)", err, out)
	}
	return path
}

func TestTranscodeToFile_MP3(t *testing.T) {
	src := genTestWav(t)

	tr := New(Config{FFmpegPath: defaultFFmpegPath, StreamCodec: codecMP3, StreamBitrate: "96k"})
	defer tr.Cleanup()

	path, err := tr.TranscodeToFile(context.Background(), src, codecMP3, "96k")
	if err != nil {
		t.Fatalf("TranscodeToFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat transcode output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected non-empty transcode output")
	}

	// Cached path must be identical.
	cached, err := tr.TranscodeToFile(context.Background(), src, codecMP3, "96k")
	if err != nil {
		t.Fatalf("cached TranscodeToFile() error = %v", err)
	}
	if cached != path {
		t.Fatalf("cache path mismatch: %q vs %q", cached, path)
	}
}

func TestStreamTo_MP3(t *testing.T) {
	src := genTestWav(t)

	tr := New(Config{FFmpegPath: defaultFFmpegPath, StreamCodec: codecMP3, StreamBitrate: "128k"})

	var buf bytes.Buffer
	if err := tr.StreamTo(context.Background(), src, codecMP3, "128k", 0, &buf); err != nil {
		t.Fatalf("StreamTo() error = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty stream output")
	}
}

func TestIsSupportedCodec(t *testing.T) {
	for _, codec := range []string{codecMP3, codecOpus, codecFlac, codecAAC} {
		if !IsSupportedCodec(codec) {
			t.Errorf("expected %q to be supported", codec)
		}
	}
	if IsSupportedCodec("bogus") {
		t.Error("expected bogus codec to be unsupported")
	}
}
