package protocols

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/transcoder"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

// testCodec is the codec requested for transcoding in tests.
const testCodec = "mp3"

// recordingStream captures written bytes so wire.WriteMsg output can be decoded.
type recordingStream struct {
	buf bytes.Buffer
}

func (s *recordingStream) Read(p []byte) (int, error)         { return 0, io.EOF }
func (s *recordingStream) Write(p []byte) (int, error)        { return s.buf.Write(p) }
func (s *recordingStream) Close() error                       { return nil }
func (s *recordingStream) Reset() error                       { return nil }
func (s *recordingStream) CloseRead() error                   { return nil }
func (s *recordingStream) CloseWrite() error                  { return nil }
func (s *recordingStream) SetDeadline(t time.Time) error      { return nil }
func (s *recordingStream) SetReadDeadline(t time.Time) error  { return nil }
func (s *recordingStream) SetWriteDeadline(t time.Time) error { return nil }
func (s *recordingStream) Conn() network.Conn                 { return nil }
func (s *recordingStream) ID() string                         { return "test-stream" }

func (s *recordingStream) Protocol() protocol.ID                             { return "/raag/stream/1.0.0" }
func (s *recordingStream) SetProtocol(id protocol.ID) error                  { return nil }
func (s *recordingStream) ResetWithError(code network.StreamErrorCode) error { return nil }
func (s *recordingStream) Scope() network.StreamScope                        { return nil }
func (s *recordingStream) Stat() network.Stats                               { return network.Stats{} }

func (s *recordingStream) readResponse() (*pb.ChunkResponse, error) {
	frame, err := wire.ReadFrame(&s.buf)
	if err != nil {
		return nil, err
	}
	var resp pb.ChunkResponse
	if err := proto.Unmarshal(frame, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// testLibrary is a minimal LibraryRepository stub; serveTranscoded does not use it.
type testLibrary struct{}

func (l *testLibrary) Save(ctx context.Context, track *domain.Track) error { return nil }
func (l *testLibrary) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (l *testLibrary) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (l *testLibrary) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (l *testLibrary) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}
func (l *testLibrary) Delete(ctx context.Context, id domain.TrackID) error { return nil }
func (l *testLibrary) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}
func (l *testLibrary) ListAll(ctx context.Context) ([]*domain.Track, error) { return nil, nil }
func (l *testLibrary) ListAllPaths(ctx context.Context) ([]string, error)   { return nil, nil }

// genTestWav creates a short valid WAV using ffmpeg's sine source, skipping the
// test when ffmpeg is unavailable.
func genTestWav(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-ac", "1", "-ar", "44100", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate test audio with ffmpeg: %v (%s)", err, out)
	}
	return path
}

func newTestStreamHandler(t *testing.T) *StreamHandler {
	t.Helper()
	h := NewStreamHandler(&testLibrary{})
	h.SetTranscoder(transcoder.New(transcoder.Config{
		FFmpegPath:    "ffmpeg",
		StreamCodec:   testCodec,
		StreamBitrate: "96k",
	}))
	return h
}

func TestServeTranscoded_SetsTotalSize(t *testing.T) {
	src := genTestWav(t)
	h := newTestStreamHandler(t)
	tr := h.Transcoder()
	track := &domain.Track{
		ID:    domain.GenerateTrackID(src),
		Path:  src,
		Codec: "pcm",
	}

	stream := &recordingStream{}
	req := &pb.ChunkRequest{
		TrackId: string(track.ID),
		Codec:   testCodec,
		Length:  512,
		Offset:  0,
	}

	h.serveTranscoded(context.Background(), stream, tr, track, req)

	resp, err := stream.readResponse()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("response error: %q", resp.Error)
	}

	// Determine the cached temp file size via the transcoder cache key.
	if resp.TotalSize == 0 {
		t.Fatal("TotalSize = 0, want > 0")
	}

	// Re-transcode and stat the output file directly.
	tmpPath, err := tr.TranscodeToFile(context.Background(), src, testCodec, "128k")
	if err != nil {
		t.Fatalf("TranscodeToFile: %v", err)
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatalf("stat transcode output: %v", err)
	}
	if resp.TotalSize != info.Size() {
		t.Errorf("TotalSize = %d, want %d (cached file size)", resp.TotalSize, info.Size())
	}
	if resp.MimeType != "audio/mp3" {
		t.Errorf("MimeType = %q, want %q", resp.MimeType, "audio/mp3")
	}
	if len(resp.Data) == 0 {
		t.Error("Data is empty, want transcoded audio bytes")
	}
}

func TestServeTranscoded_CachedByteRangeSeek(t *testing.T) {
	src := genTestWav(t)
	h := newTestStreamHandler(t)
	tr := h.Transcoder()
	track := &domain.Track{
		ID:    domain.GenerateTrackID(src),
		Path:  src,
		Codec: "pcm",
	}

	// First request at offset 0.
	stream0 := &recordingStream{}
	h.serveTranscoded(context.Background(), stream0, tr, track, &pb.ChunkRequest{
		TrackId: string(track.ID),
		Codec:   testCodec,
		Length:  1024,
		Offset:  0,
	})
	resp0, err := stream0.readResponse()
	if err != nil {
		t.Fatalf("read first response: %v", err)
	}

	// Second request at offset 1024 serves from the same cached file.
	stream1 := &recordingStream{}
	h.serveTranscoded(context.Background(), stream1, tr, track, &pb.ChunkRequest{
		TrackId: string(track.ID),
		Codec:   testCodec,
		Length:  1024,
		Offset:  1024,
	})
	resp1, err := stream1.readResponse()
	if err != nil {
		t.Fatalf("read second response: %v", err)
	}

	// The cached file's bytes at the requested offsets must match what was served.
	tmpPath, err := tr.TranscodeToFile(context.Background(), src, testCodec, "128k")
	if err != nil {
		t.Fatalf("TranscodeToFile: %v", err)
	}
	n, err := os.Open(tmpPath)
	if err != nil {
		t.Fatalf("open transcode output: %v", err)
	}
	t.Cleanup(func() { _ = n.Close() })

	at0 := make([]byte, len(resp0.Data))
	if _, err := n.ReadAt(at0, 0); err != nil && err != io.EOF {
		t.Fatalf("ReadAt(0): %v", err)
	}
	if !bytes.Equal(resp0.Data, at0) {
		t.Error("offset 0 chunk does not match cached file bytes")
	}

	if len(resp1.Data) > 0 {
		at1 := make([]byte, len(resp1.Data))
		if _, err := n.ReadAt(at1, 1024); err != nil && err != io.EOF {
			t.Fatalf("ReadAt(1024): %v", err)
		}
		if !bytes.Equal(resp1.Data, at1) {
			t.Error("offset 1024 chunk does not match cached file bytes")
		}
	}
}

var (
	_ app.LibraryRepository = (*testLibrary)(nil)
	_ network.Stream        = (*recordingStream)(nil)
)

func TestStreamHandler_NoRateLimit_WhenZero(t *testing.T) {
	h := NewStreamHandler(&testLibrary{})
	h.SetRateLimiters(0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := h.waitRequest(ctx, peer.ID("a")); err != nil {
		t.Fatalf("waitRequest() error = %v", err)
	}
	if err := h.waitUpload(ctx, 1024); err != nil {
		t.Fatalf("waitUpload() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("zero limits should not block, took %v", elapsed)
	}
}

func TestStreamHandler_RateLimit_RequestsPerPeer(t *testing.T) {
	h := NewStreamHandler(&testLibrary{})
	// 2 requests/sec per peer with burst = 2.
	h.SetRateLimiters(2, 0)

	pidA := peer.ID("peer-a")
	pidB := peer.ID("peer-b")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First two requests pass immediately (burst).
	if err := h.waitRequest(ctx, pidA); err != nil {
		t.Fatalf("first waitRequest() error = %v", err)
	}
	if err := h.waitRequest(ctx, pidA); err != nil {
		t.Fatalf("second waitRequest() error = %v", err)
	}

	// Third request must wait ~0.5s for the bucket to refill.
	start := time.Now()
	if err := h.waitRequest(ctx, pidA); err != nil {
		t.Fatalf("third waitRequest() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("third request served too fast: %v, want >= ~0.5s", elapsed)
	}

	// A different peer has its own bucket and is not throttled.
	startB := time.Now()
	if err := h.waitRequest(ctx, pidB); err != nil {
		t.Fatalf("peer-b waitRequest() error = %v", err)
	}
	if elapsed := time.Since(startB); elapsed > 100*time.Millisecond {
		t.Errorf("peer-b was throttled by peer-a's limiter: %v", elapsed)
	}
}

func TestStreamHandler_RateLimit_UploadBandwidth_Global(t *testing.T) {
	h := NewStreamHandler(&testLibrary{})
	// 1000 bytes/sec, burst = 1000 (below MaxChunkSize).
	h.SetRateLimiters(0, 1000)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// One 500-byte response passes instantly.
	if err := h.waitUpload(ctx, 500); err != nil {
		t.Fatalf("first waitUpload() error = %v", err)
	}

	// The next 900-byte response exceeds remaining budget and must wait.
	start := time.Now()
	if err := h.waitUpload(ctx, 900); err != nil {
		t.Fatalf("second waitUpload() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("oversized upload served too fast: %v, want >= ~0.4s", elapsed)
	}
}

func TestStreamHandler_RateLimit_UploadBandwidth_SharedAcrossPeers(t *testing.T) {
	h := NewStreamHandler(&testLibrary{})
	h.SetRateLimiters(0, 500) // 500 bytes/sec global

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// peer-a and peer-b both draw from the single global bucket.
	if err := h.waitUpload(ctx, 500); err != nil {
		t.Fatalf("peer-a waitUpload() error = %v", err)
	}

	start := time.Now()
	if err := h.waitUpload(ctx, 500); err != nil {
		t.Fatalf("peer-b waitUpload() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("peer-b did not share the global bucket: %v", elapsed)
	}
}

func TestStreamHandler_RateLimit_Upload_Timeout(t *testing.T) {
	h := NewStreamHandler(&testLibrary{})
	h.SetRateLimiters(0, 100) // 100 bytes/sec

	// Exhaust the budget, then request far more than can refill in time.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = h.waitUpload(context.Background(), 100)
	if err := h.waitUpload(ctx, 100000); err == nil {
		t.Fatal("waitUpload() with tiny budget and deadline: expected error, got nil")
	}
}
