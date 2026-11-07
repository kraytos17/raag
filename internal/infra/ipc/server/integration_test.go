package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/events"
	ipcclient "github.com/p-society/raag/internal/infra/ipc/client"
	"github.com/p-society/raag/internal/infra/p2p"
	"github.com/p-society/raag/internal/infra/wire"
	"github.com/p-society/raag/internal/testutil/mocks"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

const (
	queueTrackTitle = "Queue Track"
	testSongTitle   = "Test Song"
	testArtist      = "Test Artist"
)

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

	os.Remove(socketPath)
	bus := events.New()
	config := Config{
		Playback:     mocks.NewMockPlaybackHandler(),
		Scanner:      mocks.NewMockScannerHandler(),
		Search:       mocks.NewMockSearchHandler(),
		LibraryRepo:  mocks.NewMockLibraryRepoHandler(),
		PeerRepo:     mocks.NewMockPeerRepoHandler(),
		Queue:        mocks.NewMockQueueHandler(),
		PlaylistRepo: mocks.NewMockPlaylistRepo(),
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
	ts.Server.Stop(context.Background())
}

// TestPersistentClient_ReuseConn verifies that multiple requests
// reuse the same underlying connection.

func TestPersistentClient_ReuseConn(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	c := ipcclient.NewClient(socketPath)
	defer func() { c.Close() }()

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
// ipcclient.ConnReconnecting when its connection drops and ipcclient.ConnConnected after a
// successful resubscribe

func TestEventClient_StateTransitions(t *testing.T) {
	srv := startTestServer(t)
	socketPath := srv.socketPath

	c := ipcclient.NewClient(socketPath)
	defer func() { c.Close() }()

	// First request establishes connection, then subscribe.
	if _, err := c.Status(); err != nil {
		t.Fatalf("status: %v", err)
	}
	ec := c.SubscribeWithRetry(ipcclient.EventPlaybackState)
	defer ec.Close()

	// The initial successful subscribe should report ipcclient.ConnConnected.
	select {
	case state := <-ec.State():
		if state != ipcclient.ConnConnected {
			t.Fatalf("initial state = %v, want ipcclient.ConnConnected", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial ipcclient.ConnConnected state")
	}

	// Drop the server; the subscription must enter ipcclient.ConnReconnecting.
	srv.Stop()

	select {
	case state := <-ec.State():
		if state != ipcclient.ConnReconnecting {
			t.Fatalf("state after server stop = %v, want ipcclient.ConnReconnecting", state)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ipcclient.ConnReconnecting state")
	}

	// Restart the server at the same socket; the retry loop should reconnect.
	srv2 := startTestServerAt(t, socketPath)
	defer srv2.Stop()

	select {
	case state := <-ec.State():
		if state != ipcclient.ConnConnected {
			t.Fatalf("state after restart = %v, want ipcclient.ConnConnected", state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ipcclient.ConnConnected after restart")
	}
}

// TestServer_SubscribeDispatch_Success verifies Request_Subscribe gets a
// Success reply from dispatch instead of falling through to "unknown request
// type"

func TestServer_SubscribeDispatch_Success(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { conn.Close() }()

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: ipcclient.EventPlaybackState}},
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
}

// TestPersistentClient_CloseIdempotent verifies that Close can be
// called multiple times without panicking.

func TestPersistentClient_CloseIdempotent(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)

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

	c := ipcclient.NewClient(srv.socketPath)
	// Establish connection and close
	c.Status()
	c.Close()

	// Sending after close should fail
	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error after close, got nil")
	}
	if err != ipcclient.ErrClientClosed {
		t.Fatalf("expected ipcclient.ErrClientClosed, got: %v", err)
	}
}

// TestPersistentClient_HealthCheck verifies the health check request works.

func TestPersistentClient_HealthCheck(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

func TestPersistentClient_SetEqualizer_RoundTrip(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

	resp, err := c.SetEqualizer(true, 3.5, -2, 0)
	if err != nil {
		t.Fatalf("set equalizer: %v", err)
	}
	if !resp.Success {
		t.Fatalf("set equalizer failed: %s", resp.Error)
	}
	eq := resp.GetEqualizer()
	if eq == nil {
		t.Fatal("expected equalizer response payload")
	}
	if !eq.Enabled {
		t.Error("enabled = false, want true")
	}
	if eq.BassDb != 3.5 || eq.MidDb != -2 || eq.TrebleDb != 0 {
		t.Errorf("EQ = (%v, %v, %v), want (3.5, -2, 0)", eq.BassDb, eq.MidDb, eq.TrebleDb)
	}
}

func TestPersistentClient_GetEqualizer(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	mockPlayback, _ := srv.playback.(*mocks.MockPlaybackHandler)
	mockPlayback.EQ = domain.EqualizerSettings{Enabled: true, Bass: 2, Mid: 0, Treble: 1}

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

	resp, err := c.GetEqualizer()
	if err != nil {
		t.Fatalf("get equalizer: %v", err)
	}
	if !resp.Success {
		t.Fatalf("get equalizer failed: %s", resp.Error)
	}
	eq := resp.GetEqualizer()
	if eq == nil {
		t.Fatal("expected equalizer response payload")
	}
	if !eq.Enabled || eq.BassDb != 2 || eq.MidDb != 0 || eq.TrebleDb != 1 {
		t.Errorf("EQ = (%v, %v, %v, %v), want (true, 2, 0, 1)", eq.Enabled, eq.BassDb, eq.MidDb, eq.TrebleDb)
	}
}

// mockDBStats implements DebugStatsProvider.

func TestPersistentClient_DebugStats(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	srv.dbStats = mocks.NewMockDBStats(1024, 2048)

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

	resp, err := c.DebugStats()
	if err != nil {
		t.Fatalf("debug stats: %v", err)
	}
	if !resp.Success {
		t.Fatalf("debug stats failed: %s", resp.Error)
	}
	ds := resp.GetDebugStats()
	if ds == nil {
		t.Fatal("expected debug stats response payload")
	}
	// P2P is disabled in the test server, so streams is nil; index is nil (no
	// Stats method on the mock search); db should round-trip.
	if ds.GetDb() == nil {
		t.Fatal("expected db stats")
	}
	if ds.GetDb().LsmBytes != 1024 || ds.GetDb().VlogBytes != 2048 {
		t.Errorf("db = (%d, %d), want (1024, 2048)", ds.GetDb().LsmBytes, ds.GetDb().VlogBytes)
	}
}

// TestPersistentClient_MultipleRequests verifies multiple sequential requests work.

func TestPersistentClient_MultipleRequests(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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
	mock := mocks.NewMockLibraryRepoHandler()
	mock.Tracks = append(mock.Tracks, track)
	srv.libraryRepo = mock

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	mock := mocks.NewMockLibraryRepoHandler()
	mock.Tracks = append(mock.Tracks, track)
	srv.libraryRepo = mock

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	mock := mocks.NewMockLibraryRepoHandler()
	mock.Tracks = append(mock.Tracks, track)
	srv.libraryRepo = mock

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	queue := mocks.NewMockQueueHandler()
	queue.Add(&domain.Track{ID: domain.TrackID("q-1"), Title: queueTrackTitle})
	srv.queue = queue

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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
	mockLib, _ := srv.libraryRepo.(*mocks.MockLibraryRepoHandler)
	mockLib.Tracks = []*domain.Track{track}

	queue := mocks.NewMockQueueHandler()
	srv.queue = queue
	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	mockPlayback, _ := srv.playback.(*mocks.MockPlaybackHandler)
	mockPlayback.Position = 90 * time.Second
	mockPlayback.BufferFill = 0.75

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	queue := mocks.NewMockQueueHandler()
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

// TestTranslateEvent_VolumeChanged verifies volume changes translate to
// EVENT_TYPE_VOLUME_CHANGED with a SetVolumeRequest payload

func TestTranslateEvent_VolumeChanged(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	et, payload := translateEvent(srv.Server, domain.NewEvent(domain.EventVolumeChanged, domain.VolumeChangedPayload{Volume: 42}))
	if et != pb.EventType_EVENT_TYPE_VOLUME_CHANGED {
		t.Fatalf("volume event type = %v, want EVENT_TYPE_VOLUME_CHANGED", et)
	}

	var req pb.SetVolumeRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if req.Volume != 42 {
		t.Fatalf("volume payload = %d, want 42", req.Volume)
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
	// mockPeerRepoHandler returns ErrPeerUnavailable, so payload falls back to
	// the peer id in its String() (base58) form.
	want := domain.PeerID(testPeerID).String()
	if string(payload) != want {
		t.Fatalf("payload = %q, want %q (String() form of peer id)", payload, want)
	}
}

// TestWireEventBus_DeliversToClient verifies domain bus events reach a
// subscribed IPC client end-to-end.

func TestWireEventBus_DeliversToClient(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

	ec, err := c.Subscribe(ipcclient.EventPeerConnected | ipcclient.EventQueueUpdated)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { ec.Close() }()

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

	mock := mocks.NewMockPeerRepoHandler()
	mock.Peers = append(mock.Peers, &domain.PeerInfo{
		ID:    domain.PeerID(testPeerID),
		Addrs: []string{"/ip4/127.0.0.1/tcp/7844"},
		Score: score,
	})
	srv.peerRepo = mock

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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
	wantID := domain.PeerID(testPeerID).String()
	if p.Id != wantID {
		t.Fatalf("unexpected peer id: %s, want %s", p.Id, wantID)
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

	mock := mocks.NewMockPeerRepoHandler()
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

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	queue := mocks.NewMockQueueHandler()
	srv.queue = queue

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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
	srv.playlistRepo.(*mocks.MockPlaylistRepo).Playlists[pl.ID] = pl

	// Seed the library repo so GetPlaylist can resolve the track.
	mock := mocks.NewMockLibraryRepoHandler()
	mock.Tracks = append(mock.Tracks, &domain.Track{ID: domain.TrackID("track-1"), Title: testSongTitle, Artist: testArtist})
	srv.libraryRepo = mock

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

	ec, err := c.Subscribe(ipcclient.EventQueueUpdated | ipcclient.EventTrackChanged)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { ec.Close() }()

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
	// (translateEvent maps ipcclient.EventQueueUpdated -> EVENT_TYPE_QUEUE_UPDATED).
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

	search := mocks.NewMockSearchHandler()
	search.Tracks = []*domain.Track{
		{ID: domain.TrackID("local-1"), Title: "Local Song", Artist: "A", Album: "X"},
		{ID: domain.TrackID("remote-1"), Title: "Remote Song", Artist: "B", Album: "Y", PeerID: testPeerID},
	}

	srv.search = search
	c := ipcclient.NewClient(srv.socketPath)
	defer func() { c.Close() }()

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

	mockScanner := mocks.NewMockScannerHandler()
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
	registered := mockScanner.ProgressHandlerCount()
	if registered != 2 {
		t.Fatalf("registered progress handlers = %d, want 2", registered)
	}

	// The server's goroutine removes the handler on completion; since the mock
	// scanner returns immediately, wait for the removal.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		remaining := mockScanner.ProgressHandlerCount()
		if remaining == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("progress handlers were not removed after scans completed")
}

// TestServer_Broadcast_WithConcurrentResponseWrites verifies the shared
// per-conn write lock serializes concurrent writes so frames don't interleave.

func TestServer_Broadcast_WithConcurrentResponseWrites(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	// A net.Pipe pair stands in for a connected client conn.
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	srv.subMgr.Register(serverConn)

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
				srv.subMgr.WriteTo(serverConn, &pb.Response{Success: true, JobId: "resp"})
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
