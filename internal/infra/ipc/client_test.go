package ipc

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

// mockPlaybackHandler implements PlaybackHandler for testing.
type mockPlaybackHandler struct {
	state        domain.PlayerState
	volume       int
	currentTrack *domain.Track
}

func newMockPlaybackHandler() *mockPlaybackHandler {
	return &mockPlaybackHandler{
		state:  domain.PlayerStateIdle,
		volume: 50,
	}
}

func (m *mockPlaybackHandler) Play(_ context.Context, _ domain.TrackID) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *mockPlaybackHandler) PlayQuery(_ context.Context, _ string) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *mockPlaybackHandler) Pause(_ context.Context) error {
	m.state = domain.PlayerStatePaused
	return nil
}

func (m *mockPlaybackHandler) Resume(_ context.Context) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *mockPlaybackHandler) Stop(_ context.Context) error {
	m.state = domain.PlayerStateIdle
	return nil
}

func (m *mockPlaybackHandler) Seek(_ context.Context, _ time.Duration) error {
	return nil
}

func (m *mockPlaybackHandler) SetVolume(_ context.Context, vol int) error {
	m.volume = vol
	return nil
}

func (m *mockPlaybackHandler) GetState() domain.PlayerState {
	return m.state
}

func (m *mockPlaybackHandler) GetVolume() int {
	return m.volume
}

func (m *mockPlaybackHandler) GetCurrentTrack() *domain.Track {
	return m.currentTrack
}

func (m *mockPlaybackHandler) OnProgress(callback func(positionMs, durationMs int64)) {
	// No-op for testing
}

// mockScannerHandler implements ScannerHandler for testing.
type mockScannerHandler struct {
	progressFn func(app.ScanProgress)
}

func newMockScannerHandler() *mockScannerHandler {
	return &mockScannerHandler{}
}

func (m *mockScannerHandler) Scan(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockScannerHandler) ScanIncremental(_ context.Context) (added, modified, removed int, err error) {
	return 0, 0, 0, nil
}

func (m *mockScannerHandler) OnProgress(fn func(app.ScanProgress)) {
	m.progressFn = fn
}

// mockSearchHandler implements SearchHandler for testing.
type mockSearchHandler struct{}

func newMockSearchHandler() *mockSearchHandler {
	return &mockSearchHandler{}
}

func (m *mockSearchHandler) Search(_ context.Context, _ string, _ int) ([]*domain.Track, error) {
	return nil, nil
}

// mockLibraryRepoHandler implements LibraryRepoHandler for testing.
type mockLibraryRepoHandler struct {
	tracks []*domain.Track
}

func newMockLibraryRepoHandler() *mockLibraryRepoHandler {
	return &mockLibraryRepoHandler{}
}

func (m *mockLibraryRepoHandler) FindByID(_ context.Context, _ domain.TrackID) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepoHandler) FindByPath(_ context.Context, _ string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepoHandler) ListAll(_ context.Context) ([]*domain.Track, error) {
	return m.tracks, nil
}

// mockPeerRepoHandler implements PeerRepoHandler for testing.
type mockPeerRepoHandler struct{}

func newMockPeerRepoHandler() *mockPeerRepoHandler {
	return &mockPeerRepoHandler{}
}

func (m *mockPeerRepoHandler) ListAll(_ context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {}
}

// mockQueueHandler implements QueueHandler for testing.
type mockQueueHandler struct {
	tracks   []*domain.Track
	position int
}

func newMockQueueHandler() *mockQueueHandler {
	return &mockQueueHandler{
		tracks: make([]*domain.Track, 0),
	}
}

func (m *mockQueueHandler) Add(track *domain.Track) {
	m.tracks = append(m.tracks, track)
}

func (m *mockQueueHandler) Insert(pos int, track *domain.Track) {
	if pos < 0 || pos > len(m.tracks) {
		return
	}
	m.tracks = append(m.tracks[:pos], append([]*domain.Track{track}, m.tracks[pos:]...)...)
}

func (m *mockQueueHandler) Remove(pos int) {
	if pos < 0 || pos >= len(m.tracks) {
		return
	}
	m.tracks = append(m.tracks[:pos], m.tracks[pos+1:]...)
}

func (m *mockQueueHandler) Clear() {
	m.tracks = make([]*domain.Track, 0)
	m.position = 0
}

func (m *mockQueueHandler) Length() int {
	return len(m.tracks)
}

func (m *mockQueueHandler) Position() int {
	return m.position
}

func (m *mockQueueHandler) Next() *domain.Track {
	if m.position+1 >= len(m.tracks) {
		return nil
	}
	m.position++
	return m.tracks[m.position]
}

func (m *mockQueueHandler) Previous() *domain.Track {
	if m.position-1 < 0 {
		return nil
	}
	m.position--
	return m.tracks[m.position]
}

func (m *mockQueueHandler) Tracks() []*domain.Track {
	out := make([]*domain.Track, len(m.tracks))
	copy(out, m.tracks)
	return out
}

// testServer wraps Server with helpers for testing.
type testServer struct {
	*Server
	socketPath string
	cancel     context.CancelFunc
}

// startTestServer creates and starts a test server with mock handlers.
func startTestServer(t *testing.T) *testServer {
	t.Helper()

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	return startTestServerAt(t, socketPath)
}

// startTestServerAt creates and starts a test server at a specific socket path.
func startTestServerAt(t *testing.T, socketPath string) *testServer {
	t.Helper()

	_ = os.Remove(socketPath)
	config := ServerConfig{
		Playback:    newMockPlaybackHandler(),
		Scanner:     newMockScannerHandler(),
		Search:      newMockSearchHandler(),
		LibraryRepo: newMockLibraryRepoHandler(),
		PeerRepo:    newMockPeerRepoHandler(),
		Queue:       newMockQueueHandler(),
	}

	srv, err := NewServer(socketPath, config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("failed to start server: %v", err)
	}

	ts := &testServer{
		Server:     srv,
		socketPath: socketPath,
		cancel:     cancel,
	}
	return ts
}

// Stop gracefully stops the test server.
func (ts *testServer) Stop() {
	ts.cancel()
	_ = ts.Server.Stop(context.Background())
}

// TestPersistentClient_ReuseConn verifies that multiple requests
// reuse the same underlying connection.
func TestPersistentClient_ReuseConn(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	// First request establishes connection
	_, err := c.Status()
	if err != nil {
		t.Fatalf("first status: %v", err)
	}
	conn1 := c.Conn()

	// Second request should reuse the same connection
	_, err = c.Status()
	if err != nil {
		t.Fatalf("second status: %v", err)
	}
	conn2 := c.Conn()

	if conn1 != conn2 {
		t.Fatal("expected same connection to be reused")
	}
}

// TestPersistentClient_ReconnectsAfterRestart verifies that the client
// automatically reconnects after the server is restarted.
func TestPersistentClient_ReconnectsAfterRestart(t *testing.T) {
	srv := startTestServer(t)
	socketPath := srv.socketPath

	c := NewClient(socketPath)
	defer func() { _ = c.Close() }()

	// First request establishes connection
	_, err := c.Status()
	if err != nil {
		t.Fatalf("first status: %v", err)
	}

	// Stop the server
	srv.Stop()

	// Give the server time to fully stop
	time.Sleep(50 * time.Millisecond)

	// Start a new server at the same socket path
	srv2 := startTestServerAt(t, socketPath)
	defer srv2.Stop()

	// Next command should reconnect transparently
	_, err = c.Status()
	if err != nil {
		t.Fatalf("status after reconnect: %v", err)
	}
}

// TestPersistentClient_CloseIdempotent verifies that Close can be
// called multiple times without panicking.
func TestPersistentClient_CloseIdempotent(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)

	// Establish a connection first
	_, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	// First close should succeed
	err = c.Close()
	if err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Second close should not panic and should return nil
	err = c.Close()
	if err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestPersistentClient_SendAfterClose verifies that sending after
// Close returns an appropriate error.
func TestPersistentClient_SendAfterClose(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)

	// Establish connection and close
	_, _ = c.Status()
	_ = c.Close()

	// Sending after close should fail
	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error after close, got nil")
	}
	if err != ErrClientClosed {
		t.Fatalf("expected ErrClientClosed, got: %v", err)
	}
}

// TestPersistentClient_HealthCheck verifies the health check request works.
func TestPersistentClient_HealthCheck(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.HealthCheck()
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
	hc := resp.GetHealthCheck()
	if hc == nil {
		t.Fatal("expected health check response")
	}
	if !hc.Healthy {
		t.Fatal("expected healthy=true")
	}
}

// TestPersistentClient_MultipleRequests verifies multiple sequential requests work.
func TestPersistentClient_MultipleRequests(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	for i := range 10 {
		resp, err := c.Status()
		if err != nil {
			t.Fatalf("status %d: %v", i, err)
		}
		if !resp.Success {
			t.Fatalf("status %d: expected success", i)
		}
	}
}

// TestPersistentClient_PlayPauseResume verifies playback commands work.
func TestPersistentClient_PlayPauseResume(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	// Pause (should work even if not playing)
	resp, err := c.Pause()
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !resp.Success {
		t.Fatalf("pause failed: %s", resp.Error)
	}

	// Resume
	resp, err = c.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resp.Success {
		t.Fatalf("resume failed: %s", resp.Error)
	}

	// Stop
	resp, err = c.Stop()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !resp.Success {
		t.Fatalf("stop failed: %s", resp.Error)
	}
}

// TestPersistentClient_NoServerAvailable verifies proper error handling
// when no server is available.
func TestPersistentClient_NoServerAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	c := NewClient(socketPath)
	defer func() { _ = c.Close() }()

	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error when no server available")
	}
}

// TestPersistentClient_ListTracks verifies the ListTracks command returns
// all library tracks from the daemon.
func TestPersistentClient_ListTracks(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	// Seed the mock library repo with tracks.
	track := &domain.Track{
		ID:     domain.TrackID("track-1"),
		Title:  "Test Song",
		Artist: "Test Artist",
		Album:  "Test Album",
	}
	mock := newMockLibraryRepoHandler()
	mock.tracks = append(mock.tracks, track)
	srv.libraryRepo = mock

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.ListTracks(0, 0)
	if err != nil {
		t.Fatalf("list tracks: %v", err)
	}
	if !resp.Success {
		t.Fatalf("list tracks failed: %s", resp.Error)
	}
	lt := resp.GetListTracks()
	if lt == nil {
		t.Fatal("expected ListTracks response payload")
	}
	if len(lt.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(lt.Tracks))
	}
	if lt.Tracks[0].Title != "Test Song" {
		t.Fatalf("unexpected track title: %s", lt.Tracks[0].Title)
	}
}

// TestPersistentClient_QueueList verifies GetQueue returns queue contents.
func TestPersistentClient_QueueList(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	queue := newMockQueueHandler()
	queue.Add(&domain.Track{ID: domain.TrackID("q-1"), Title: "Queue Track"})
	srv.queue = queue

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.GetQueue()
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if !resp.Success {
		t.Fatalf("get queue failed: %s", resp.Error)
	}
	ql := resp.GetQueueList()
	if ql == nil {
		t.Fatal("expected QueueList response payload")
	}
	if len(ql.Tracks) != 1 {
		t.Fatalf("expected 1 queue track, got %d", len(ql.Tracks))
	}
	if ql.Tracks[0].Title != "Queue Track" {
		t.Fatalf("unexpected queue track title: %s", ql.Tracks[0].Title)
	}
}
