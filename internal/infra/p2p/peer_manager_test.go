package p2p

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"iter"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

type peerFetchStreamHost struct {
	host.Host
	streams  chan *peerFetchBlockingStream
	started  chan int
	finished chan int
}

func (h *peerFetchStreamHost) NewStream(ctx context.Context, _ peer.ID, _ ...protocol.ID) (network.Stream, error) {
	select {
	case stream := <-h.streams:
		stream.ctx = ctx
		return stream, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type peerFetchBlockingStream struct {
	*mockStream
	group     int
	ctx       context.Context
	started   chan<- int
	finished  chan<- int
	startOne  sync.Once
	finishOne sync.Once
}

func (s *peerFetchBlockingStream) Read([]byte) (int, error) {
	s.startOne.Do(func() { s.started <- s.group })
	<-s.ctx.Done()
	s.finishOne.Do(func() { s.finished <- s.group })
	return 0, s.ctx.Err()
}

func (s *peerFetchBlockingStream) Write(p []byte) (int, error) {
	return s.mockStream.Write(p)
}

func newPeerFetchBlockingStream(group int, started, finished chan<- int) *peerFetchBlockingStream {
	return &peerFetchBlockingStream{
		mockStream: &mockStream{id: "peer-fetch"},
		group:      group,
		started:    started,
		finished:   finished,
	}
}

func waitForPeerFetchGroup(t *testing.T, ch <-chan int, group, count int) {
	t.Helper()
	seen := 0
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for seen < count {
		select {
		case got := <-ch:
			if got == group {
				seen++
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %d events for fetch group %d; got %d", count, group, seen)
		}
	}
}

// flakySyncHost serves canned sync responses, failing the first failReads
// response reads with EOF to simulate transient transport failures. The
// canned response is armed from the request payload, so manifest and
// capabilities fetches can share one host deterministically.
type flakySyncHost struct {
	host.Host
	mu            sync.Mutex
	newStreams    int
	failReads     int
	manifestFrame []byte
	capsFrame     []byte
}

func newFlakySyncHost(manifest, caps *pb.SyncResponse, failReads int) *flakySyncHost {
	return &flakySyncHost{
		failReads:     failReads,
		manifestFrame: mustFrameSyncResponse(manifest),
		capsFrame:     mustFrameSyncResponse(caps),
	}
}

func mustFrameSyncResponse(resp *pb.SyncResponse) []byte {
	var buf bytes.Buffer
	if err := wire.WriteMsg(&buf, resp); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (h *flakySyncHost) NewStream(ctx context.Context, _ peer.ID, _ ...protocol.ID) (network.Stream, error) {
	h.mu.Lock()
	h.newStreams++
	h.mu.Unlock()
	return &flakySyncStream{mockStream: &mockStream{id: "flaky-sync"}, host: h, ctx: ctx}, nil
}

func (h *flakySyncHost) takeFailure() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failReads > 0 {
		h.failReads--
		return true
	}
	return false
}

func (h *flakySyncHost) streamCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.newStreams
}

type flakySyncStream struct {
	*mockStream
	host   *flakySyncHost
	ctx    context.Context
	reqBuf []byte
	resp   []byte
	pos    int
}

func (s *flakySyncStream) Write(p []byte) (int, error) {
	s.reqBuf = append(s.reqBuf, p...)
	if len(s.reqBuf) >= 4 {
		size := binary.BigEndian.Uint32(s.reqBuf[:4])
		if len(s.reqBuf) >= 4+int(size) {
			var req pb.SyncRequest
			if err := proto.Unmarshal(s.reqBuf[4:4+size], &req); err == nil {
				switch req.Payload.(type) {
				case *pb.SyncRequest_ManifestRequest:
					s.resp = s.host.manifestFrame
				case *pb.SyncRequest_CapabilitiesRequest:
					s.resp = s.host.capsFrame
				}
			}
		}
	}
	return len(p), nil
}

func (s *flakySyncStream) Read(p []byte) (int, error) {
	if s.host.takeFailure() {
		return 0, io.EOF
	}
	if s.pos >= len(s.resp) {
		return 0, io.EOF
	}
	n := copy(p, s.resp[s.pos:])
	s.pos += n
	return n, nil
}

func TestPeerManager_FetchPeerDataRetriesTransientFailures(t *testing.T) {
	manifest := &pb.SyncResponse{Payload: &pb.SyncResponse_Manifest{
		Manifest: &pb.LibraryManifest{TrackIds: []string{"t1"}},
	}}
	caps := &pb.SyncResponse{Payload: &pb.SyncResponse_Capabilities{
		Capabilities: &pb.PeerCapabilities{SupportedCodecs: []string{"opus"}},
	}}
	h := newFlakySyncHost(manifest, caps, 2)
	pid := peer.ID("flaky-fetch-peer")
	pm := newPeerManager(h, discovery.NewPeerCache(10), nil, nil, time.Hour, nil)

	pm.FetchPeerData(context.Background(), pid)

	if got := h.streamCount(); got != 4 {
		t.Fatalf("NewStream calls = %d, want 4 (2 transient failures retried)", got)
	}
	if got := pm.GetManifest(pid); got == nil || len(got.TrackIds) != 1 {
		t.Errorf("GetManifest() = %v, want manifest stored after retry", got)
	}
	if got := pm.GetCapabilities(pid); got == nil {
		t.Error("GetCapabilities() = nil, want capabilities stored after retry")
	}
}

func TestPeerManager_FetchManifestPermissionDeniedDoesNotRetry(t *testing.T) {
	denied := &pb.SyncResponse{Payload: &pb.SyncResponse_Error{
		Error: &pb.ErrorResponse{Message: "library sharing disabled", Code: pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED},
	}}
	h := newFlakySyncHost(denied, denied, 0)
	pid := peer.ID("denied-fetch-peer")
	pm := newPeerManager(h, discovery.NewPeerCache(10), nil, nil, time.Hour, nil)

	err := pm.FetchManifest(context.Background(), pid)
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("FetchManifest() error = %v, want *SyncError", err)
	}
	if syncErr.Code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("SyncError.Code = %v, want PERMISSION_DENIED", syncErr.Code)
	}
	if got := h.streamCount(); got != 1 {
		t.Errorf("NewStream calls = %d, want 1 (protocol errors must not retry)", got)
	}
}

// memPeerRepo is an in-memory app.PeerRepository for testing persistence.
type memPeerRepo struct {
	mu        sync.Mutex
	infos     map[domain.PeerID]*domain.PeerInfo
	scores    map[domain.PeerID]*domain.PeerScore
	manifests map[domain.PeerID]*domain.LibraryManifest
}

func newMemPeerRepo() *memPeerRepo {
	return &memPeerRepo{
		infos:     make(map[domain.PeerID]*domain.PeerInfo),
		scores:    make(map[domain.PeerID]*domain.PeerScore),
		manifests: make(map[domain.PeerID]*domain.LibraryManifest),
	}
}

func (m *memPeerRepo) SavePeerInfo(_ context.Context, info *domain.PeerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos[info.ID] = info
	return nil
}

func (m *memPeerRepo) GetPeerInfo(_ context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.infos[id]; ok {
		return info, nil
	}
	return nil, domain.ErrPeerUnavailable
}

func (m *memPeerRepo) SavePeerScore(_ context.Context, id domain.PeerID, score *domain.PeerScore) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scores[id] = score
	return nil
}

func (m *memPeerRepo) GetPeerScore(_ context.Context, id domain.PeerID) (*domain.PeerScore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if score, ok := m.scores[id]; ok {
		return score, nil
	}
	return nil, nil
}

func (m *memPeerRepo) SaveLibraryManifest(_ context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifests[id] = manifest
	return nil
}

func (m *memPeerRepo) GetLibraryManifest(_ context.Context, id domain.PeerID) (*domain.LibraryManifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if manifest, ok := m.manifests[id]; ok {
		return manifest, nil
	}
	return nil, nil
}

func (m *memPeerRepo) ListAll(_ context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, info := range m.infos {
			if !yield(info, nil) {
				return
			}
		}
	}
}

// TestPeerManager_PersistRoundTrip verifies that persist() writes peer info,
// score, and manifest through the repository.
func TestPeerManager_PersistRoundTrip(t *testing.T) {
	repo := newMemPeerRepo()
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, repo)

	pid := domain.PeerID("peer-1")
	score := domain.NewPeerScore(pid)
	score.SuccessCount = 5
	score.AvgLatency = 10 * time.Millisecond

	// Seed the scorer with a real score so persist() snapshots it.
	scorer := NewPeerScorer()
	scorer.RecordSuccess(pid)
	scorer.RecordSuccess(pid)
	scorer.RecordBandwidth(pid, 1_000_000)
	pm.scorer = scorer

	// Seed a manifest so persist() stores it too.
	pm.mu.Lock()
	pm.manifests[pid] = &peerManifest{
		data:    &pb.LibraryManifest{TrackIds: []string{"t1", "t2"}},
		addedAt: time.Now(),
	}
	pm.mu.Unlock()

	pm.persist(pid)

	got, err := repo.GetPeerInfo(context.Background(), pid)
	if err != nil {
		t.Fatalf("GetPeerInfo: %v", err)
	}
	if got == nil || got.ID != pid {
		t.Fatalf("expected persisted peer info for %s, got %+v", pid, got)
	}
	if got.Score == nil || got.Score.SuccessCount != 2 {
		t.Fatalf("expected score success_count=2, got %+v", got.Score)
	}

	manifest, err := repo.GetLibraryManifest(context.Background(), pid)
	if err != nil {
		t.Fatalf("GetLibraryManifest: %v", err)
	}
	if manifest == nil || len(manifest.TrackIDs) != 2 {
		t.Fatalf("expected manifest with 2 tracks, got %+v", manifest)
	}

	if _, err := repo.GetPeerScore(context.Background(), pid); err != nil {
		t.Fatalf("GetPeerScore: %v", err)
	}
}

// TestPeerManager_PersistNoRepo verifies persist() is a safe no-op when no
// repository is configured (unit tests / P2P-disabled builds).
func TestPeerManager_PersistNoRepo(t *testing.T) {
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, nil)
	pm.persist(domain.PeerID("peer-1")) // must not panic
}

// TestPeerManager_PersistNilScorer verifies persist() handles a nil scorer.
func TestPeerManager_PersistNilScorer(t *testing.T) {
	repo := newMemPeerRepo()
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, repo)
	pm.persist(domain.PeerID("peer-1"))

	if _, err := repo.GetPeerInfo(context.Background(), domain.PeerID("peer-1")); err != nil {
		t.Fatalf("expected peer info persisted even with nil scorer: %v", err)
	}
}

func TestPeerManager_OlderFetchCannotClearNewerCancellation(t *testing.T) {
	started := make(chan int, 8)
	finished := make(chan int, 8)
	host := &peerFetchStreamHost{
		streams: make(chan *peerFetchBlockingStream, 8),
		started: started, finished: finished,
	}
	peerID := peer.ID("overlapping-fetch-peer")
	pm := newPeerManager(host, discovery.NewPeerCache(10), nil, nil, 50*time.Millisecond, nil)

	queueFetchStreams := func(group int) {
		for range 2 { // FetchPeerData starts one manifest and one capabilities request.
			host.streams <- newPeerFetchBlockingStream(group, started, finished)
		}
	}
	queueFetchStreams(1)
	fetch1Done := make(chan struct{})
	go func() {
		defer close(fetch1Done)
		pm.FetchPeerData(context.Background(), peerID)
	}()
	waitForPeerFetchGroup(t, started, 1, 2)

	queueFetchStreams(2)
	fetch2Done := make(chan struct{})
	go func() {
		defer close(fetch2Done)
		pm.FetchPeerData(context.Background(), peerID)
	}()
	waitForPeerFetchGroup(t, started, 2, 2)

	// Starting fetch 2 cancels fetch 1. Wait for fetch 1 to fully exit so its
	// cleanup (which must not clear fetch 2's entry) has run.
	waitForPeerFetchGroup(t, finished, 1, 2)
	select {
	case <-fetch1Done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for older fetch to exit")
	}

	pm.onPeerDisconnected(peerID)
	waitForPeerFetchGroup(t, finished, 2, 2)
	select {
	case <-fetch2Done:
	case <-time.After(2 * time.Second):
		t.Fatal("newer fetch was not canceled by disconnect: older fetch cleared its cancellation entry")
	}
}

func TestPeerManager_ExpiredCacheReadsAreConcurrentSafe(t *testing.T) {
	pid := peer.ID("expired-cache-peer")
	pm := newPeerManager(nil, discovery.NewPeerCache(10), nil, nil, -time.Second, nil)
	pm.manifests[pid] = &peerManifest{
		data:    &pb.LibraryManifest{TrackIds: []string{track1ID}},
		addedAt: time.Now().Add(-time.Hour),
	}
	pm.caps[pid] = &peerCaps{
		data:    &pb.PeerCapabilities{SupportedCodecs: []string{peerTestOpus}},
		addedAt: time.Now().Add(-time.Hour),
	}

	const readers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			<-start
			if got := pm.GetManifest(pid); got != nil {
				t.Errorf("GetManifest() = %v, want nil for expired entry", got)
			}
			if got := pm.GetCapabilities(pid); got != nil {
				t.Errorf("GetCapabilities() = %v, want nil for expired entry", got)
			}
			if got := pm.GetPeerCapabilities(pid); got == nil {
				t.Error("GetPeerCapabilities() = nil, want playable-codec fallback")
			}
			if pm.HasTrack(pid, track1ID) {
				t.Error("HasTrack() = true, want false for expired entry")
			}
			if got := pm.FindPeersWithTrack(context.Background(), domain.TrackID(track1ID)); len(got) != 0 {
				t.Errorf("FindPeersWithTrack() = %v, want no expired peers", got)
			}
			if got := pm.GetBestCodec(pid, peerTestOpus); got != codecMP3 {
				t.Errorf("GetBestCodec() = %q, want fallback %q", got, codecMP3)
			}
		})
	}
	close(start)
	wg.Wait()
}

func TestPeerManager_CacheGettersReturnIndependentSnapshots(t *testing.T) {
	pid := peer.ID("snapshot-cache-peer")
	pm := newPeerManager(nil, discovery.NewPeerCache(10), nil, nil, time.Hour, nil)
	pm.manifests[pid] = &peerManifest{
		data: &pb.LibraryManifest{
			PeerId:   "original-peer",
			TrackIds: []string{track1ID, "track-2"},
		},
		addedAt: time.Now(),
	}
	pm.caps[pid] = &peerCaps{
		data: &pb.PeerCapabilities{
			SupportedCodecs:   []string{peerTestOpus, peerTestMP3},
			SupportedBitrates: []int32{128, 256},
			ProtocolVersion:   peerProtocolV1,
		},
		addedAt: time.Now(),
	}

	manifest := pm.GetManifest(pid)
	manifest.PeerId = "corrupted-peer"
	manifest.TrackIds[0] = "corrupted-track"
	manifest.TrackIds = append(manifest.TrackIds, "injected-track")

	caps := pm.GetCapabilities(pid)
	caps.SupportedCodecs[0] = "corrupted-codec"
	caps.SupportedBitrates[0] = 999
	caps.ProtocolVersion = "corrupted-version"

	domainCaps := pm.GetPeerCapabilities(pid)
	domainCaps.SupportedCodecs[0] = "corrupted-domain-codec"
	domainCaps.SupportedBitrates[0] = 999
	domainCaps.ProtocolVersion = "corrupted-domain-version"

	gotManifest := pm.GetManifest(pid)
	if gotManifest.PeerId != "original-peer" || !slices.Equal(gotManifest.TrackIds, []string{track1ID, "track-2"}) {
		t.Errorf("cached manifest changed through returned snapshot: %+v", gotManifest)
	}
	gotCaps := pm.GetCapabilities(pid)
	if gotCaps.ProtocolVersion != peerProtocolV1 ||
		!slices.Equal(gotCaps.SupportedCodecs, []string{peerTestOpus, peerTestMP3}) ||
		!slices.Equal(gotCaps.SupportedBitrates, []int32{128, 256}) {
		t.Errorf("cached capabilities changed through returned snapshot: %+v", gotCaps)
	}
	gotDomainCaps := pm.GetPeerCapabilities(pid)
	if gotDomainCaps.ProtocolVersion != peerProtocolV1 ||
		!slices.Equal(gotDomainCaps.SupportedCodecs, []string{peerTestOpus, peerTestMP3}) ||
		!slices.Equal(gotDomainCaps.SupportedBitrates, []int32{128, 256}) {
		t.Errorf("cached domain capabilities changed through returned snapshot: %+v", gotDomainCaps)
	}
}

// compile-time check that memPeerRepo satisfies app.PeerRepository.
var _ app.PeerRepository = (*memPeerRepo)(nil)
