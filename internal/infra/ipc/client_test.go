package ipc

import (
	"context"
	"fmt"
	"iter"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/events"
	p2p "github.com/p-society/raag/internal/infra/p2p"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

const (
	queueTrackTitle = "Queue Track"
	testSongTitle   = "Test Song"
	testArtist      = "Test Artist"
)

// mockPlaybackHandler implements PlaybackHandler for testing.
type mockPlaybackHandler struct {
	state        domain.PlayerState
	volume       int
	currentTrack *domain.Track
	position     time.Duration
	bufferFill   float64
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

func (m *mockPlaybackHandler) GetPosition() time.Duration {
	return m.position
}

func (m *mockPlaybackHandler) GetBufferFillLevel() float64 {
	return m.bufferFill
}

func (m *mockPlaybackHandler) OnProgress(callback func(positionMs, durationMs int64)) {
	// No-op for testing
}

// mockScannerHandler implements ScannerHandler for testing.
type mockScannerHandler struct {
	mu         sync.Mutex
	progressFn []func(app.ScanProgress)
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressFn = append(m.progressFn, fn)
}

func (m *mockScannerHandler) RemoveProgressHandler(fn func(app.ScanProgress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, h := range m.progressFn {
		if reflect.ValueOf(h).Pointer() == reflect.ValueOf(fn).Pointer() {
			m.progressFn = append(m.progressFn[:i], m.progressFn[i+1:]...)
			return
		}
	}
}

// mockSearchHandler implements SearchHandler for testing.
type mockSearchHandler struct {
	tracks []*domain.Track
}

func newMockSearchHandler() *mockSearchHandler {
	return &mockSearchHandler{}
}

func (m *mockSearchHandler) Search(_ context.Context, _ string, _ int) ([]*domain.Track, error) {
	return m.tracks, nil
}

// SearchWithRemote satisfies the optional interface used by handleSearch when
// include_peers is set.
func (m *mockSearchHandler) SearchWithRemote(_ context.Context, _ string, _ int) ([]*domain.Track, error) {
	return m.tracks, nil
}

// mockLibraryRepoHandler implements LibraryRepoHandler for testing.
type mockLibraryRepoHandler struct {
	tracks []*domain.Track
}

func newMockLibraryRepoHandler() *mockLibraryRepoHandler {
	return &mockLibraryRepoHandler{}
}

func (m *mockLibraryRepoHandler) FindByID(_ context.Context, id domain.TrackID) (*domain.Track, error) {
	for _, t := range m.tracks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepoHandler) FindByPath(_ context.Context, path string) (*domain.Track, error) {
	for _, t := range m.tracks {
		if t.Path == path {
			return t, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepoHandler) ListAll(_ context.Context) ([]*domain.Track, error) {
	return m.tracks, nil
}

// mockPeerRepoHandler implements PeerRepoHandler for testing.
type mockPeerRepoHandler struct {
	peers []*domain.PeerInfo
}

func newMockPeerRepoHandler() *mockPeerRepoHandler {
	return &mockPeerRepoHandler{}
}

func (m *mockPeerRepoHandler) ListAll(_ context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {
		for _, p := range m.peers {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (m *mockPeerRepoHandler) GetPeerInfo(_ context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	for _, p := range m.peers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, domain.ErrPeerUnavailable
}

// mockPlaylistRepo implements app.PlaylistRepository for testing.
type mockPlaylistRepo struct {
	playlists map[domain.PlaylistID]*domain.Playlist
}

func newMockPlaylistRepo() *mockPlaylistRepo {
	return &mockPlaylistRepo{
		playlists: make(map[domain.PlaylistID]*domain.Playlist),
	}
}

func (m *mockPlaylistRepo) Save(_ context.Context, playlist *domain.Playlist) error {
	m.playlists[playlist.ID] = playlist
	return nil
}

func (m *mockPlaylistRepo) FindByID(_ context.Context, id domain.PlaylistID) (*domain.Playlist, error) {
	if p, ok := m.playlists[id]; ok {
		return p, nil
	}
	return nil, domain.ErrPlaylistNotFound
}

func (m *mockPlaylistRepo) ListAll(_ context.Context) iter.Seq2[*domain.Playlist, error] {
	return func(yield func(*domain.Playlist, error) bool) {
		for _, p := range m.playlists {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (m *mockPlaylistRepo) Delete(_ context.Context, id domain.PlaylistID) error {
	delete(m.playlists, id)
	return nil
}

// mockQueueHandler implements QueueHandler for testing.
type mockQueueHandler struct {
	tracks   []*domain.Track
	position int
	shuffle  bool
	repeat   domain.RepeatMode
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

func (m *mockQueueHandler) MoveTo(track *domain.Track) bool {
	if track == nil {
		return false
	}
	for i, t := range m.tracks {
		if t.ID == track.ID {
			m.position = i
			return true
		}
	}

	m.tracks = append(m.tracks, track)
	m.position = len(m.tracks) - 1
	return true
}

func (m *mockQueueHandler) Tracks() []*domain.Track {
	out := make([]*domain.Track, len(m.tracks))
	copy(out, m.tracks)
	return out
}

func (m *mockQueueHandler) ToggleShuffle() {
	m.shuffle = !m.shuffle
}

func (m *mockQueueHandler) SetShuffle(shuffle bool) {
	m.shuffle = shuffle
}

func (m *mockQueueHandler) SetRepeat(mode domain.RepeatMode) {
	m.repeat = mode
}

func (m *mockQueueHandler) GetShuffle() bool {
	return m.shuffle
}

func (m *mockQueueHandler) GetRepeat() domain.RepeatMode {
	return m.repeat
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
	bus := events.New()
	config := ServerConfig{
		Playback:     newMockPlaybackHandler(),
		Scanner:      newMockScannerHandler(),
		Search:       newMockSearchHandler(),
		LibraryRepo:  newMockLibraryRepoHandler(),
		PeerRepo:     newMockPeerRepoHandler(),
		Queue:        newMockQueueHandler(),
		PlaylistRepo: newMockPlaylistRepo(),
		EventBus:     bus,
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

// TestEventClient_StateTransitions verifies the subscription reports
// ConnReconnecting when its connection drops and ConnConnected after a
// successful resubscribe
func TestEventClient_StateTransitions(t *testing.T) {
	srv := startTestServer(t)
	socketPath := srv.socketPath

	c := NewClient(socketPath)
	defer func() { _ = c.Close() }()

	// First request establishes connection, then subscribe.
	if _, err := c.Status(); err != nil {
		t.Fatalf("status: %v", err)
	}
	ec := c.SubscribeWithRetry(EventPlaybackState)
	defer ec.Close()

	// The initial successful subscribe should report ConnConnected.
	select {
	case state := <-ec.State():
		if state != ConnConnected {
			t.Fatalf("initial state = %v, want ConnConnected", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial ConnConnected state")
	}

	// Drop the server; the subscription must enter ConnReconnecting.
	srv.Stop()

	select {
	case state := <-ec.State():
		if state != ConnReconnecting {
			t.Fatalf("state after server stop = %v, want ConnReconnecting", state)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ConnReconnecting state")
	}

	// Restart the server at the same socket; the retry loop should reconnect.
	srv2 := startTestServerAt(t, socketPath)
	defer srv2.Stop()

	select {
	case state := <-ec.State():
		if state != ConnConnected {
			t.Fatalf("state after restart = %v, want ConnConnected", state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ConnConnected after restart")
	}
}

// TestServer_SubscribeDispatch_Success verifies Request_Subscribe and
// Request_Empty get a Success reply from dispatch instead of falling through
// to "unknown request type"
func TestServer_SubscribeDispatch_Success(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: EventPlaybackState}},
	}
	if err := wire.WriteMsg(conn, req); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	var resp pb.Response
	if err := wire.ReadMsg(conn, &resp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("subscribe response Success = false, error = %q; want success", resp.Error)
	}

	emptyReq := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Empty{Empty: &pb.Empty{}},
	}
	if err := wire.WriteMsg(conn, emptyReq); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if err := wire.ReadMsg(conn, &resp); err != nil {
		t.Fatalf("read empty response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("empty response Success = false, error = %q; want success", resp.Error)
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
		Title:  testSongTitle,
		Artist: testArtist,
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
	if lt.Tracks[0].Title != testSongTitle {
		t.Fatalf("unexpected track title: %s", lt.Tracks[0].Title)
	}
}

func TestPersistentClient_GetTrack(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	track := &domain.Track{
		ID:     domain.TrackID("track-1"),
		Title:  testSongTitle,
		Artist: testArtist,
		Album:  "Test Album",
	}

	mock := newMockLibraryRepoHandler()
	mock.tracks = append(mock.tracks, track)
	srv.libraryRepo = mock

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.GetTrack("track-1")
	if err != nil {
		t.Fatalf("get track: %v", err)
	}
	if !resp.Success {
		t.Fatalf("get track failed: %s", resp.Error)
	}

	gt := resp.GetGetTrack()
	if gt == nil || gt.Track == nil {
		t.Fatal("expected GetTrack response payload")
	}
	if gt.Track.Title != testSongTitle {
		t.Fatalf("unexpected track title: %s", gt.Track.Title)
	}
}

func TestPersistentClient_GetTrackByPath(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	track := &domain.Track{
		ID:    domain.TrackID("track-1"),
		Title: testSongTitle,
		Path:  "/music/song.mp3",
	}

	mock := newMockLibraryRepoHandler()
	mock.tracks = append(mock.tracks, track)
	srv.libraryRepo = mock

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.GetTrackByPath("/music/song.mp3")
	if err != nil {
		t.Fatalf("get track by path: %v", err)
	}
	if !resp.Success {
		t.Fatalf("get track by path failed: %s", resp.Error)
	}

	gt := resp.GetGetTrack()
	if gt == nil || gt.Track == nil {
		t.Fatal("expected GetTrack response payload")
	}
	if gt.Track.Title != testSongTitle {
		t.Fatalf("unexpected track title: %s", gt.Track.Title)
	}
}

// TestPersistentClient_QueueList verifies GetQueue returns queue contents.
func TestPersistentClient_QueueList(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	queue := newMockQueueHandler()
	queue.Add(&domain.Track{ID: domain.TrackID("q-1"), Title: queueTrackTitle})
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
	if ql.Tracks[0].Title != queueTrackTitle {
		t.Fatalf("unexpected queue track title: %s", ql.Tracks[0].Title)
	}
}

// TestTranslateEvent_QueueUpdated verifies queue events translate to
// EVENT_TYPE_QUEUE_UPDATED with the queue contents.
// TestPersistentClient_Play_PopulatesQueue verifies that playing a track from
// the library adds it to the queue as the current item.
func TestPersistentClient_Play_PopulatesQueue(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	track := &domain.Track{ID: domain.TrackID("play-me"), Title: "Play Me"}
	mockLib, _ := srv.libraryRepo.(*mockLibraryRepoHandler)
	mockLib.tracks = []*domain.Track{track}

	queue := newMockQueueHandler()
	srv.queue = queue
	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.Play(string(track.ID), "")
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if !resp.Success {
		t.Fatalf("play failed: %s", resp.Error)
	}

	ql, err := c.GetQueue()
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if !ql.Success {
		t.Fatalf("get queue failed: %s", ql.Error)
	}

	list := ql.GetQueueList()
	if list == nil {
		t.Fatal("expected QueueList response payload")
	}
	if len(list.Tracks) != 1 {
		t.Fatalf("expected 1 queued track after play, got %d", len(list.Tracks))
	}
	if list.Tracks[0].Id != string(track.ID) {
		t.Errorf("queued track id = %q, want %q", list.Tracks[0].Id, track.ID)
	}
	if queue.Position() != 0 {
		t.Errorf("queue position = %d, want 0 (played track is current)", queue.Position())
	}
}

// TestPersistentClient_Status_PopulatesPositionMs verifies handleStatus reports
// the current playback position
func TestPersistentClient_Status_PopulatesPositionMs(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	mockPlayback, _ := srv.playback.(*mockPlaybackHandler)
	mockPlayback.position = 90 * time.Second
	mockPlayback.bufferFill = 0.75

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Error)
	}

	st := resp.GetStatus()
	if st == nil {
		t.Fatal("expected StatusResponse payload")
	}
	if st.PositionMs != 90000 {
		t.Errorf("PositionMs = %d, want 90000", st.PositionMs)
	}
	if st.BufferFill != 0.75 {
		t.Errorf("BufferFill = %v, want 0.75", st.BufferFill)
	}
}

// TestTranslateEvent_PeerScoreUpdated verifies score updates map to their own
// event type, not PEER_CONNECTED.
func TestTranslateEvent_PeerScoreUpdated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	pid := domain.PeerID("peer-1")
	et, _ := translateEvent(srv.Server, domain.NewEvent(
		domain.EventPeerScoreUpdated,
		domain.PeerScoreUpdatedPayload{PeerID: pid, Score: 1.5},
	))
	if et != pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED {
		t.Errorf("event type = %v, want EVENT_TYPE_PEER_SCORE_UPDATED", et)
	}

	// Peer connect still maps to PEER_CONNECTED.
	et2, _ := translateEvent(srv.Server, domain.NewEvent(
		domain.EventPeerConnected,
		domain.PeerConnectedPayload{PeerID: pid},
	))
	if et2 != pb.EventType_EVENT_TYPE_PEER_CONNECTED {
		t.Errorf("peer connected event type = %v, want EVENT_TYPE_PEER_CONNECTED", et2)
	}
}

func TestTranslateEvent_QueueUpdated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	queue := newMockQueueHandler()
	queue.Add(&domain.Track{ID: domain.TrackID("q-1"), Title: queueTrackTitle})
	srv.queue = queue

	et, payload := translateEvent(srv.Server, domain.NewEvent(domain.EventQueueUpdated, nil))
	if et != pb.EventType_EVENT_TYPE_QUEUE_UPDATED {
		t.Fatalf("event type = %v, want QUEUE_UPDATED", et)
	}
	var qr pb.QueueResponse
	if err := proto.Unmarshal(payload, &qr); err != nil {
		t.Fatalf("unmarshal queue payload: %v", err)
	}
	if len(qr.Tracks) != 1 || qr.Tracks[0].Title != queueTrackTitle {
		t.Fatalf("unexpected queue payload: %+v", qr.Tracks)
	}
}

// TestTranslateEvent_PlaybackBufferingReady verifies buffering/refill domain
// events translate to PLAYBACK_STATE with "buffering"/"playing" payloads.
func TestTranslateEvent_PlaybackBufferingReady(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	et, payload := translateEvent(srv.Server, domain.NewEvent(domain.EventPlaybackBuffering, nil))
	if et != pb.EventType_EVENT_TYPE_PLAYBACK_STATE {
		t.Fatalf("buffering event type = %v, want PLAYBACK_STATE", et)
	}
	if string(payload) != "buffering" {
		t.Fatalf("buffering payload = %q, want \"buffering\"", payload)
	}

	et, payload = translateEvent(srv.Server, domain.NewEvent(domain.EventPlaybackReady, nil))
	if et != pb.EventType_EVENT_TYPE_PLAYBACK_STATE {
		t.Fatalf("ready event type = %v, want PLAYBACK_STATE", et)
	}
	if string(payload) != "playing" {
		t.Fatalf("ready payload = %q, want \"playing\"", payload)
	}
}

// TestTranslateEvent_PeerConnected verifies peer events translate to
// EVENT_TYPE_PEER_CONNECTED with a Peer payload.
func TestTranslateEvent_PeerConnected(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	et, payload := translateEvent(srv.Server, domain.NewEvent(domain.EventPeerConnected, domain.PeerConnectedPayload{
		PeerID: domain.PeerID(testPeerID),
	}))
	if et != pb.EventType_EVENT_TYPE_PEER_CONNECTED {
		t.Fatalf("event type = %v, want PEER_CONNECTED", et)
	}
	// mockPeerRepoHandler returns ErrPeerUnavailable, so payload falls back to raw peer id
	if string(payload) != testPeerID {
		t.Fatalf("payload = %q, want raw peer id", payload)
	}
}

// TestWireEventBus_DeliversToClient verifies domain bus events reach a
// subscribed IPC client end-to-end.
func TestWireEventBus_DeliversToClient(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	ec, err := c.Subscribe(EventPeerConnected | EventQueueUpdated)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = ec.Close() }()

	srv.eventBus.Publish(context.Background(), domain.NewEvent(domain.EventQueueUpdated, nil))

	select {
	case ev := <-ec.Events():
		if ev.EventType != pb.EventType_EVENT_TYPE_QUEUE_UPDATED {
			t.Fatalf("received event type = %v, want QUEUE_UPDATED", ev.EventType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued event")
	}
}

// TestPersistentClient_ListPeers_Enriched verifies ListPeers returns peers
// with score attached (enriched via the p2p node when available).
func TestPersistentClient_ListPeers_Enriched(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	score := domain.NewPeerScore(domain.PeerID(testPeerID))
	score.AvgLatency = 10 * time.Millisecond
	score.AvgBandwidth = 1_000_000
	score.SuccessCount = 10

	mock := newMockPeerRepoHandler()
	mock.peers = append(mock.peers, &domain.PeerInfo{
		ID:    domain.PeerID(testPeerID),
		Addrs: []string{"/ip4/127.0.0.1/tcp/7844"},
		Score: score,
	})
	srv.peerRepo = mock

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.ListPeers()
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if !resp.Success {
		t.Fatalf("list peers failed: %s", resp.Error)
	}
	lp := resp.GetListPeers()
	if lp == nil {
		t.Fatal("expected ListPeers response payload")
	}
	if len(lp.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(lp.Peers))
	}
	p := lp.Peers[0]
	if p.Id != testPeerID {
		t.Fatalf("unexpected peer id: %s", p.Id)
	}
	if p.Score == nil {
		t.Fatal("expected peer score to be populated")
	}
	if p.Score.Score <= 0 {
		t.Fatalf("expected positive score, got %f", p.Score.Score)
	}
}

// TestPersistentClient_ListPeers_IncludesDiscovered verifies ListPeers surfaces
// mDNS-discovered, not-yet-connected peers
func TestPersistentClient_ListPeers_IncludesDiscovered(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	mock := newMockPeerRepoHandler()
	srv.peerRepo = mock

	// Build a real (listener-free) p2p node so handleListPeers can report
	// discovered peers. The node is never started, so a nil library repo is fine.
	node, err := p2p.NewP2PNode(p2p.P2PNodeConfig{
		DataDir:            t.TempDir(),
		LANOnly:            true,
		MaxKnownPeers:      100,
		PerPeerRateLimit:   0,
		UploadBandwidth:    0,
		CBFailureThreshold: 5,
		CBCooldown:         30 * time.Second,
	}, nil)
	if err != nil {
		t.Fatalf("NewP2PNode: %v", err)
	}
	srv.p2pNode = node

	discovered := peer.AddrInfo{ID: peer.ID("discovered-peer")}
	node.AddDiscovered(discovered)
	wantID := discovered.ID.String()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.ListPeers()
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if !resp.Success {
		t.Fatalf("list peers failed: %s", resp.Error)
	}
	lp := resp.GetListPeers()
	if lp == nil {
		t.Fatal("expected ListPeers response payload")
	}

	var found bool
	for _, p := range lp.Peers {
		if p.Id == wantID {
			found = true
			if p.Connected {
				t.Error("discovered peer should be marked Connected=false")
			}
		}
	}
	if !found {
		t.Fatalf("discovered peer %q not in ListPeers; got %+v", wantID, lp.Peers)
	}
}

// TestPersistentClient_QueueShuffleRepeat verifies shuffle/repeat round-trips.
func TestPersistentClient_QueueShuffleRepeat(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	queue := newMockQueueHandler()
	srv.queue = queue

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	// Toggle shuffle on.
	resp, err := c.QueueSetShuffle(true)
	if err != nil || !resp.Success {
		t.Fatalf("queue shuffle: err=%v resp=%+v", err, resp)
	}
	if !queue.GetShuffle() {
		t.Fatal("expected shuffle to be enabled")
	}

	// Set repeat to all.
	resp, err = c.QueueSetRepeat("all")
	if err != nil || !resp.Success {
		t.Fatalf("queue repeat: err=%v resp=%+v", err, resp)
	}
	if queue.GetRepeat() != domain.RepeatModeAll {
		t.Fatalf("repeat = %q, want all", queue.GetRepeat())
	}

	// Query mode.
	resp, err = c.QueueGetMode()
	if err != nil || !resp.Success {
		t.Fatalf("queue mode: err=%v resp=%+v", err, resp)
	}
	qm := resp.GetQueueMode()
	if qm == nil {
		t.Fatal("expected QueueMode payload")
	}
	if !qm.Shuffle || qm.Repeat != "all" {
		t.Fatalf("mode = shuffle=%v repeat=%q, want true/all", qm.Shuffle, qm.Repeat)
	}

	// Invalid repeat mode is rejected.
	resp, err = c.QueueSetRepeat("bogus")
	if err != nil {
		t.Fatalf("queue repeat bogus: err=%v", err)
	}
	if resp.Success {
		t.Fatal("expected invalid repeat mode to fail")
	}
}

// TestPersistentClient_Playlists verifies playlist create/list/add/get round-trips.
func TestPersistentClient_Playlists(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	// Seed the mock playlist repo with a known playlist containing a track.
	pl := &domain.Playlist{
		ID:       domain.PlaylistID("pl-1"),
		Name:     "My Favorites",
		TrackIDs: []domain.TrackID{domain.TrackID("track-1")},
	}
	srv.playlistRepo.(*mockPlaylistRepo).playlists[pl.ID] = pl

	// Seed the library repo so GetPlaylist can resolve the track.
	mock := newMockLibraryRepoHandler()
	mock.tracks = append(mock.tracks, &domain.Track{ID: domain.TrackID("track-1"), Title: testSongTitle, Artist: testArtist})
	srv.libraryRepo = mock

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	// List.
	resp, err := c.ListPlaylists()
	if err != nil || !resp.Success {
		t.Fatalf("list playlists: err=%v resp=%+v", err, resp)
	}
	if got := len(resp.GetListPlaylists().Playlists); got != 1 {
		t.Fatalf("list playlists count = %d, want 1", got)
	}

	// Create.
	resp, err = c.CreatePlaylist("New List")
	if err != nil || !resp.Success {
		t.Fatalf("create playlist: err=%v resp=%+v", err, resp)
	}
	cp := resp.GetCreatePlaylist()
	if cp == nil || cp.PlaylistId == "" {
		t.Fatal("expected created playlist id")
	}

	// Get with tracks resolved.
	resp, err = c.GetPlaylist("pl-1")
	if err != nil || !resp.Success {
		t.Fatalf("get playlist: err=%v resp=%+v", err, resp)
	}
	gp := resp.GetGetPlaylist()
	if gp == nil || gp.Playlist == nil {
		t.Fatal("expected playlist payload")
	}
	if gp.Playlist.Name != "My Favorites" {
		t.Fatalf("playlist name = %q, want My Favorites", gp.Playlist.Name)
	}
	if len(gp.Tracks) != 1 || gp.Tracks[0].Title != testSongTitle {
		t.Fatalf("expected 1 resolved track, got %+v", gp.Tracks)
	}

	// Add track.
	resp, err = c.AddToPlaylist("pl-1", "track-2")
	if err != nil || !resp.Success {
		t.Fatalf("add to playlist: err=%v resp=%+v", err, resp)
	}
	ap := resp.GetAddToPlaylist()
	if ap == nil || ap.TrackCount != 2 {
		t.Fatalf("track count = %+v, want 2", ap)
	}
}

// TestPersistentClient_ConcurrentRequestsWithEvents exercises the single-reader
// demultiplexer: requests and event broadcasts interleave on one connection.
// Under -race this would previously corrupt frames.
func TestPersistentClient_ConcurrentRequestsWithEvents(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	ec, err := c.Subscribe(EventQueueUpdated | EventTrackChanged)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = ec.Close() }()

	const workers = 8
	const reqsPerWorker = 25

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	// Concurrent request workers: a mix of Status and QueueGetMode so both
	// response payload shapes traverse the demultiplexer.
	for range workers {
		wg.Go(func() {
			for range reqsPerWorker {
				resp, err := c.Status()
				if err != nil {
					errCh <- fmt.Errorf("status: %w", err)
					return
				}
				if !resp.Success {
					errCh <- fmt.Errorf("status returned success=false: %+v", resp)
					return
				}

				resp, err = c.QueueGetMode()
				if err != nil {
					errCh <- fmt.Errorf("queue mode: %w", err)
					return
				}
				if !resp.Success {
					errCh <- fmt.Errorf("queue mode returned success=false: %+v", resp)
					return
				}
			}
		})
	}

	// Concurrent event publisher: broadcast queue/track events while requests
	// are in flight on the same connection.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			srv.eventBus.Publish(context.Background(), domain.NewEvent(domain.EventQueueUpdated, nil))
			srv.eventBus.Publish(context.Background(), domain.NewEvent(domain.EventTrackStarted, nil))
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	close(errCh)
	<-done

	for err := range errCh {
		t.Errorf("worker error: %v", err)
	}

	// The subscription must have observed events. Count queue-updated events
	// (translateEvent maps EventQueueUpdated -> EVENT_TYPE_QUEUE_UPDATED).
	var queueEvents int
	deadline := time.After(3 * time.Second)
collect:
	for queueEvents < 5 {
		select {
		case ev := <-ec.Events():
			switch ev.EventType {
			case pb.EventType_EVENT_TYPE_QUEUE_UPDATED:
				queueEvents++
			case pb.EventType_EVENT_TYPE_TRACK_CHANGED:
				// expected; ignored for the count
			default:
				t.Errorf("unexpected event type %v", ev.EventType)
			}
		case <-deadline:
			break collect
		}
	}
	if queueEvents < 5 {
		t.Errorf("received %d queue-updated events, want >= 5", queueEvents)
	}
}

// TestPersistentClient_SearchRemote verifies the include_peers search path
// round-trips and returns tracks tagged with their source peer.
func TestPersistentClient_SearchRemote(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	search := newMockSearchHandler()
	search.tracks = []*domain.Track{
		{ID: domain.TrackID("local-1"), Title: "Local Song", Artist: "A", Album: "X"},
		{ID: domain.TrackID("remote-1"), Title: "Remote Song", Artist: "B", Album: "Y", PeerID: testPeerID},
	}

	srv.search = search
	c := NewClient(srv.socketPath)
	defer func() { _ = c.Close() }()

	resp, err := c.SearchRemote("song", 20)
	if err != nil {
		t.Fatalf("SearchRemote() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("SearchRemote() failed: %s", resp.Error)
	}

	sr := resp.GetSearch()
	if sr == nil || len(sr.Tracks) != 2 {
		t.Fatalf("SearchRemote() tracks = %+v, want 2", sr)
	}

	var remoteFound bool
	for _, tr := range sr.Tracks {
		if tr.Id == "remote-1" && tr.PeerId == testPeerID {
			remoteFound = true
		}
	}
	if !remoteFound {
		t.Fatalf("expected remote track with peer_id, got %+v", sr.Tracks)
	}
}

// TestServer_ConcurrentLibScans_NoClobber verifies each scan registers its own
// progress handler so concurrent scans don't clobber each other's jobID
func TestServer_ConcurrentLibScans_NoClobber(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	mockScanner := newMockScannerHandler()
	srv.scanner = mockScanner

	// Both scans register their own progress handlers.
	resp1 := srv.handleLibScanAsync(&pb.LibScanRequest{})
	resp2 := srv.handleLibScanAsync(&pb.LibScanRequest{})

	if resp1.JobId == "" || resp2.JobId == "" {
		t.Fatal("expected non-empty job ids")
	}
	if resp1.JobId == resp2.JobId {
		t.Fatal("concurrent scans must have distinct job ids")
	}

	// Both handlers must be registered before the scans complete.
	mockScanner.mu.Lock()
	registered := len(mockScanner.progressFn)
	mockScanner.mu.Unlock()
	if registered != 2 {
		t.Fatalf("registered progress handlers = %d, want 2", registered)
	}

	// The server's goroutine removes the handler on completion; since the mock
	// scanner returns immediately, wait for the removal.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mockScanner.mu.Lock()
		remaining := len(mockScanner.progressFn)
		mockScanner.mu.Unlock()
		if remaining == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("progress handlers were not removed after scans completed")
}

// TestServer_Broadcast_WithConcurrentResponseWrites verifies writeToConn
// serializes concurrent writes so frames don't interleave.
func TestServer_Broadcast_WithConcurrentResponseWrites(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	// A net.Pipe pair stands in for a connected client conn.
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	srv.mu.Lock()
	srv.conns = append(srv.conns, serverConn)
	srv.connWriteMu[serverConn] = &sync.Mutex{}
	srv.mu.Unlock()
	defer srv.removeConn(serverConn)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Concurrent writer: broadcast and per-conn response writes.
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				srv.broadcast(&pb.Response{Success: true, JobId: "broadcast"})
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = srv.writeToConn(serverConn, &pb.Response{Success: true, JobId: "resp"})
			}
		}
	}()

	// Reader drains frames and verifies each decodes cleanly.
	readErr := make(chan error, 1)
	go func() {
		for {
			var resp pb.Response
			if err := wire.ReadMsg(clientConn, &resp); err != nil {
				readErr <- err
				return
			}
			if resp.JobId != "broadcast" && resp.JobId != "resp" {
				readErr <- fmt.Errorf("corrupted frame: job_id=%q", resp.JobId)
				return
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	clientConn.Close()

	select {
	case err := <-readErr:
		if !isConnClosed(err) && !strings.Contains(err.Error(), "closed pipe") {
			t.Fatalf("frame corruption or read error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not terminate after writes stopped")
	}
}
