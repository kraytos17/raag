package p2p

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type mockPeerManager struct {
	peers        map[peer.ID]*domain.PeerInfo
	scores       map[peer.ID]*domain.PeerScore
	caps         map[peer.ID]*domain.PeerCapabilities
	banned       map[peer.ID]bool
	successCount map[peer.ID]int
	failCount    map[peer.ID]int
	findResults  map[domain.TrackID][]peer.ID
}

func newMockPeerManager() *mockPeerManager {
	return &mockPeerManager{
		peers:        make(map[peer.ID]*domain.PeerInfo),
		scores:       make(map[peer.ID]*domain.PeerScore),
		caps:         make(map[peer.ID]*domain.PeerCapabilities),
		banned:       make(map[peer.ID]bool),
		successCount: make(map[peer.ID]int),
		failCount:    make(map[peer.ID]int),
		findResults:  make(map[domain.TrackID][]peer.ID),
	}
}

func (m *mockPeerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	return m.findResults[trackID]
}

func (m *mockPeerManager) GetPeerScore(pid peer.ID) *domain.PeerScore {
	return m.scores[pid]
}

func (m *mockPeerManager) RecordSuccess(pid peer.ID) {
	m.successCount[pid]++
	delete(m.failCount, pid)
}

func (m *mockPeerManager) RecordFailure(pid peer.ID) {
	m.failCount[pid]++
}

func (m *mockPeerManager) IsBanned(pid peer.ID) bool {
	return m.banned[pid]
}

func (m *mockPeerManager) GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities {
	if c, ok := m.caps[pid]; ok {
		return c
	}
	return &domain.PeerCapabilities{}
}

type mockLibraryRepo struct {
	tracks map[domain.TrackID]*domain.Track
}

func newMockLibraryRepo() *mockLibraryRepo {
	return &mockLibraryRepo{
		tracks: make(map[domain.TrackID]*domain.Track),
	}
}

func (m *mockLibraryRepo) Save(ctx context.Context, track *domain.Track) error {
	m.tracks[track.ID] = track
	return nil
}

func (m *mockLibraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}

func (m *mockLibraryRepo) Delete(ctx context.Context, id domain.TrackID) error {
	delete(m.tracks, id)
	return nil
}

func (m *mockLibraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}

func (m *mockLibraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	return nil, nil
}

func TestP2PResolver_ResolveLocal(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/test.mp3")
	tmpFile := t.TempDir() + "/test.mp3"
	if err := os.WriteFile(tmpFile, []byte("test data"), 0o644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	repo.tracks[trackID] = &domain.Track{
		ID:   trackID,
		Path: tmpFile,
	}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader == nil {
		t.Fatal("Resolve() returned nil reader")
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "test data" {
		t.Fatalf("ReadAll() = %q, want %q", data, "test data")
	}
	reader.Close()
}

func TestP2PResolver_ResolveLocal_FileNotFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/missing.mp3")
	repo.tracks[trackID] = &domain.Track{
		ID:   trackID,
		Path: "/nonexistent/path/missing.mp3",
	}

	_, err := resolver.Resolve(context.Background(), trackID)
	if err == nil {
		t.Fatal("Resolve() should fail for missing file")
	}
}

func TestP2PResolver_ResolveRemote(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/remote.mp3")
	pid := peer.ID("remote-peer")
	peerMgr.findResults[trackID] = []peer.ID{pid}
	peerMgr.scores[pid] = &domain.PeerScore{
		PeerID:       pid,
		SuccessCount: 10,
		FailureCount: 0,
		AvgLatency:   100 * time.Millisecond,
		AvgBandwidth: 1000000,
	}
	peerMgr.caps[pid] = &domain.PeerCapabilities{
		SupportedCodecs: []string{codecMP3, codecFlac},
	}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader == nil {
		t.Fatal("Resolve() returned nil reader")
	}
	reader.Close()
}

func TestP2PResolver_ResolveRemoteRecordsSuccessAfterFirstBytes(t *testing.T) {
	trackID := domain.TrackID(track1ID)
	pid := peer.ID("first-byte-peer")
	host := &resolverSequenceHost{streams: []network.Stream{
		&mockStream{id: "metadata-stream"}, // Metadata probing is best-effort and may fail.
		newChunkResponseStream(t, []byte("audio bytes")),
	}}
	pool := NewStreamPool(host, protocols.StreamProtocol)
	defer pool.Close()
	repo := newMockLibraryRepo()
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()
	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	peerMgr.findResults[trackID] = []peer.ID{pid}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer reader.Close()
	if peerMgr.failCount[pid] != 0 {
		t.Fatalf("failure count after opening stream = %d, want 0", peerMgr.failCount[pid])
	}
	if peerMgr.successCount[pid] != 0 {
		t.Fatal("peer should not be recorded successful merely because the lazy reader opened")
	}
	buf := make([]byte, 32)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("first Read() error = %v", err)
	}
	if n == 0 {
		t.Fatal("first Read() returned no audio bytes")
	}
	if peerMgr.successCount[pid] != 1 {
		t.Fatalf("success count after first bytes = %d, want 1", peerMgr.successCount[pid])
	}
	if got := resolver.LastPeerID(); got != pid {
		t.Fatalf("LastPeerID() = %s, want %s after first bytes", got, pid)
	}
	if peerMgr.failCount[pid] != 0 {
		t.Fatalf("failure count after successful first bytes = %d, want 0", peerMgr.failCount[pid])
	}
}

func TestP2PResolver_ResolveRemoteRecordsFailureBeforeFirstBytes(t *testing.T) {
	trackID := domain.TrackID(track1ID)
	pid := peer.ID("first-read-failure-peer")
	host := &resolverSequenceHost{streams: []network.Stream{
		&mockStream{id: "metadata-stream"}, // Metadata probing is best-effort.
		&mockStream{id: "failed-audio-stream"},
	}}
	pool := NewStreamPool(host, protocols.StreamProtocol)
	defer pool.Close()
	peerMgr := newMockPeerManager()
	resolver := NewP2PResolver(newMockLibraryRepo(), pool, peerMgr, NewPeerScorer(), host)
	peerMgr.findResults[trackID] = []peer.ID{pid}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer reader.Close()
	if peerMgr.successCount[pid] != 0 || peerMgr.failCount[pid] != 0 {
		t.Fatalf("scores immediately after Resolve = success:%d failure:%d, want 0/0", peerMgr.successCount[pid], peerMgr.failCount[pid])
	}
	if _, err := reader.Read(make([]byte, 32)); err == nil {
		t.Fatal("first Read() error = nil, want stream failure")
	}
	if peerMgr.successCount[pid] != 0 || peerMgr.failCount[pid] != 1 {
		t.Fatalf("scores after failed first Read = success:%d failure:%d, want 0/1", peerMgr.successCount[pid], peerMgr.failCount[pid])
	}
}

func newChunkResponseStream(t *testing.T, data []byte) network.Stream {
	t.Helper()
	var frame bytes.Buffer
	if err := wire.WriteMsg(&frame, &pb.ChunkResponse{
		TrackId:   track1ID,
		Data:      data,
		LastChunk: true,
		TotalSize: int64(len(data)),
	}); err != nil {
		t.Fatalf("encode chunk response: %v", err)
	}
	return &resolverFrameStream{
		mockStream: &mockStream{id: "resolver-stream"},
		response:   frame.Bytes(),
	}
}

type resolverSequenceHost struct {
	streams []network.Stream
}

func (h *resolverSequenceHost) NewStream(context.Context, peer.ID, ...protocol.ID) (network.Stream, error) {
	if len(h.streams) == 0 {
		return nil, errors.New("unexpected NewStream call")
	}
	stream := h.streams[0]
	h.streams = h.streams[1:]
	return stream, nil
}

type resolverFrameStream struct {
	*mockStream
	response []byte
	readPos  int
}

func (s *resolverFrameStream) Read(p []byte) (int, error) {
	if s.readPos >= len(s.response) {
		return 0, io.EOF
	}
	n := copy(p, s.response[s.readPos:])
	s.readPos += n
	return n, nil
}

func TestP2PResolver_TrackNotFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/nonexistent.mp3")
	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound", err)
	}
}

func TestP2PResolver_ResolveRemote_AllPeersBanned(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/banned.mp3")
	pid := peer.ID("banned-peer")
	peerMgr.findResults[trackID] = []peer.ID{pid}
	peerMgr.banned[pid] = true
	peerMgr.caps[pid] = &domain.PeerCapabilities{}

	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound when all peers banned", err)
	}
}

func TestP2PResolver_ResolveRemote_NoPeersHaveTrack(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/orphan.mp3")
	peerMgr.findResults[trackID] = []peer.ID{} // empty peers

	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound for empty peers", err)
	}
}

func TestP2PResolver_LastPeerID_InitiallyEmpty(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	if pid := resolver.LastPeerID(); pid != "" {
		t.Fatalf("initial LastPeerID() = %q, want empty", pid)
	}
}

func TestP2PResolver_LastPeerID_ConcurrentAccess(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()
	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)

	// Simulate concurrent writes and reads to lastPeerID.
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			// Write via the lock (simulating what tryPeers does)
			resolver.lastPeerMu.Lock()
			resolver.lastPeerID = peer.ID("peer-" + string(rune('A'+n%26)))
			resolver.lastPeerMu.Unlock()
		}(i)
		go func() {
			defer wg.Done()
			resolver.LastPeerID()
			resolver.LastPeerIDString()
		}()
	}
	wg.Wait()
}

func TestP2PResolver_FindPeersWithTrack(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	trackID := domain.TrackID("track-abc")
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	peerMgr.findResults[trackID] = []peer.ID{p1, p2}

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	result := resolver.FindPeersWithTrack(context.Background(), trackID)

	if len(result) != 2 {
		t.Fatalf("FindPeersWithTrack() len = %d, want 2", len(result))
	}
	if result[0] != p1.String() || result[1] != p2.String() {
		t.Fatalf("FindPeersWithTrack() = %v, want [%s, %s]", result, p1, p2)
	}
}

func TestP2PResolver_FindPeersWithTrack_NoneFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	result := resolver.FindPeersWithTrack(context.Background(), "nonexistent")
	if len(result) != 0 {
		t.Fatalf("FindPeersWithTrack() len = %d, want 0", len(result))
	}
}

func TestP2PResolver_GetPeerLatency(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()
	pid := peer.ID("peer-1")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordLatency(pid, 200*time.Millisecond)

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	lat := resolver.GetPeerLatency(pid)
	if lat != 150*time.Millisecond {
		t.Fatalf("GetPeerLatency() = %v, want 150ms", lat)
	}
}

func TestP2PResolver_GetPeerLatency_NilScorer(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	peerMgr := newMockPeerManager()
	resolver := NewP2PResolver(repo, pool, peerMgr, nil, host)
	lat := resolver.GetPeerLatency(peer.ID("peer-1"))
	if lat != 0 {
		t.Fatalf("GetPeerLatency() = %v, want 0 with nil scorer", lat)
	}
}

func TestStreamingReader_DoubleClose(t *testing.T) {
	inner := io.NopCloser(bytes.NewReader([]byte("hello")))
	sr := &streamingReader{
		reader:  inner,
		trackID: track1ID,
		peerID:  peer.ID("peer-1"),
	}
	if err := sr.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := sr.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestStreamingReader_ReadThenClose(t *testing.T) {
	data := []byte("hello world")
	inner := io.NopCloser(bytes.NewReader(data))
	sr := &streamingReader{
		reader:  inner,
		trackID: track1ID,
		peerID:  peer.ID("peer-1"),
	}

	buf := make([]byte, 5)
	n, err := sr.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("Read() = %d, %q; want 5, %q", n, buf, "hello")
	}
	if err := sr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRequestedCodecFor(t *testing.T) {
	tests := []struct {
		name        string
		nativeCodec string
		sp          scoredPeer
		want        string
	}{
		{
			name:        "locally decodable native codec streams raw",
			nativeCodec: codecMP3,
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: true},
			want:        "",
		},
		{
			name:        "flac native codec streams raw",
			nativeCodec: codecFlac,
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: true},
			want:        "",
		},
		{
			name:        "vorbis native codec streams raw",
			nativeCodec: codecVorbis,
			sp:          scoredPeer{pid: peer.ID("p1")},
			want:        "",
		},
		{
			name:        "undecodable native codec requests mp3 transcode",
			nativeCodec: codecAAC,
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: true},
			want:        codecMP3,
		},
		{
			name:        "opus native codec requests mp3 transcode",
			nativeCodec: codecOpus,
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: true},
			want:        codecMP3,
		},
		{
			name:        "unknown native codec with transcoding peer requests mp3",
			nativeCodec: "",
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: true},
			want:        codecMP3,
		},
		{
			name:        "undecodable native codec without transcoding peer streams raw",
			nativeCodec: codecAAC,
			sp:          scoredPeer{pid: peer.ID("p1"), canTranscode: false},
			want:        "",
		},
		{
			name:        "unknown native codec without transcoding peer streams raw",
			nativeCodec: "",
			sp:          scoredPeer{pid: peer.ID("p1")},
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestedCodecFor(tt.nativeCodec, tt.sp); got != tt.want {
				t.Errorf("requestedCodecFor(%q, %+v) = %q, want %q", tt.nativeCodec, tt.sp, got, tt.want)
			}
		})
	}
}

func TestIsLocallyDecodable(t *testing.T) {
	tests := []struct {
		codec string
		want  bool
	}{
		{codecMP3, true},
		{codecFlac, true},
		{codecVorbis, true},
		{codecPCM, true},
		{codecAAC, false},
		{codecOpus, false},
		{codecWMA, false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			if got := isLocallyDecodable(tt.codec); got != tt.want {
				t.Errorf("isLocallyDecodable(%q) = %v, want %v", tt.codec, got, tt.want)
			}
		})
	}
}

func TestLocalCapabilities_CanTranscode(t *testing.T) {
	tests := []struct {
		name         string
		canTranscode bool
		want         bool
	}{
		{name: "with transcoder", canTranscode: true, want: true},
		{name: "without transcoder", canTranscode: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := &LocalCapabilities{
				LibraryRepo:  newMockLibraryRepo(),
				PeerID:       peer.ID("local"),
				CanTranscode: tt.canTranscode,
			}
			if got := lc.GetLocalCapabilities().CanTranscode; got != tt.want {
				t.Errorf("GetLocalCapabilities().CanTranscode = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalCapabilities_UploadBandwidth(t *testing.T) {
	tests := []struct {
		name            string
		uploadBandwidth int64
		want            int64
	}{
		{name: "zero means unlimited", uploadBandwidth: 0, want: 0},
		{name: "advertises configured bandwidth", uploadBandwidth: 1_000_000, want: 1_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := &LocalCapabilities{
				LibraryRepo:     newMockLibraryRepo(),
				PeerID:          peer.ID("local"),
				UploadBandwidth: tt.uploadBandwidth,
			}
			if got := lc.GetLocalCapabilities().UploadBandwidth; got != tt.want {
				t.Errorf("GetLocalCapabilities().UploadBandwidth = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestP2PResolver_TryPeers_RequestsTranscodeCodec(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/remote.m4a")

	pid := peer.ID("transcoding-peer")
	peerMgr.caps[pid] = &domain.PeerCapabilities{
		SupportedCodecs: []string{codecMP3, codecFlac},
		CanTranscode:    true,
	}

	reader, err := resolver.tryPeers(context.Background(), trackID, []scoredPeer{
		{pid: pid, score: 1, canTranscode: true},
	}, codecAAC)
	if err != nil {
		t.Fatalf("tryPeers() error = %v", err)
	}
	defer reader.Close()

	sr, ok := reader.(*streamingReader)
	if !ok {
		t.Fatalf("reader type = %T, want *streamingReader", reader)
	}

	cr, ok := sr.reader.(*chunkedReader)
	if !ok {
		t.Fatalf("reader type = %T, want *chunkedReader", sr.reader)
	}
	if cr.codec != codecMP3 {
		t.Errorf("requested codec = %q, want %q", cr.codec, codecMP3)
	}
	if got := resolver.LastPeerID(); got != "" {
		t.Errorf("LastPeerID() = %v before the reader delivered bytes, want empty", got)
	}
}

func TestP2PResolver_TryPeers_StreamsRawForDecodableNative(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/remote.flac")

	pid := peer.ID("flac-peer")
	peerMgr.caps[pid] = &domain.PeerCapabilities{
		SupportedCodecs: []string{codecMP3, codecFlac},
		CanTranscode:    true,
	}

	reader, err := resolver.tryPeers(context.Background(), trackID, []scoredPeer{
		{pid: pid, score: 1, canTranscode: true},
	}, codecFlac)
	if err != nil {
		t.Fatalf("tryPeers() error = %v", err)
	}
	defer reader.Close()

	sr, ok := reader.(*streamingReader)
	if !ok {
		t.Fatalf("reader type = %T, want *streamingReader", reader)
	}

	cr, ok := sr.reader.(*chunkedReader)
	if !ok {
		t.Fatalf("reader type = %T, want *chunkedReader", sr.reader)
	}
	if cr.codec != "" {
		t.Errorf("requested codec = %q, want empty (raw stream)", cr.codec)
	}
}

func TestP2PResolver_TryPeers_AllBanned_ReturnsError(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()

	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/remote.m4a")

	pid1 := peer.ID("banned-1")
	pid2 := peer.ID("banned-2")
	peerMgr.banned[pid1] = true
	peerMgr.banned[pid2] = true

	reader, err := resolver.tryPeers(context.Background(), trackID, []scoredPeer{
		{pid: pid1, score: 1},
		{pid: pid2, score: 1},
	}, codecAAC)
	if err == nil {
		t.Fatal("tryPeers() error = nil, want non-nil when all peers are banned")
	}
	if reader != nil {
		t.Fatalf("tryPeers() reader = %v, want nil", reader)
	}
}
