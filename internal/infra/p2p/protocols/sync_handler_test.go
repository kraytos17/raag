package protocols

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

// syncTestStream serves a pre-baked request via Read and captures the written
// response via Write, so the SyncHandler can be driven without a real host.
type syncTestStream struct {
	req  bytes.Buffer
	resp bytes.Buffer
	conn network.Conn
}

func newSyncTestStream(req *pb.SyncRequest, remote peer.ID) *syncTestStream {
	data, _ := proto.Marshal(req)
	frame := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(data)))
	copy(frame[4:], data)
	s := &syncTestStream{conn: newMockConn(remote)}
	s.req.Write(frame)
	return s
}

func (s *syncTestStream) Read(p []byte) (int, error)  { return s.req.Read(p) }
func (s *syncTestStream) Write(p []byte) (int, error) { return s.resp.Write(p) }

func (s *syncTestStream) Close() error                       { return nil }
func (s *syncTestStream) Reset() error                       { return nil }
func (s *syncTestStream) CloseRead() error                   { return nil }
func (s *syncTestStream) CloseWrite() error                  { return nil }
func (s *syncTestStream) SetDeadline(t time.Time) error      { return nil }
func (s *syncTestStream) SetReadDeadline(t time.Time) error  { return nil }
func (s *syncTestStream) SetWriteDeadline(t time.Time) error { return nil }
func (s *syncTestStream) Conn() network.Conn                 { return s.conn }
func (s *syncTestStream) ID() string                         { return "sync-test-stream" }

func (s *syncTestStream) Protocol() protocol.ID                             { return SyncProtocol }
func (s *syncTestStream) SetProtocol(id protocol.ID) error                  { return nil }
func (s *syncTestStream) ResetWithError(code network.StreamErrorCode) error { return nil }
func (s *syncTestStream) Scope() network.StreamScope                        { return nil }
func (s *syncTestStream) Stat() network.Stats                               { return network.Stats{} }

func (s *syncTestStream) readResponse() (*pb.SyncResponse, error) {
	frame, err := wire.ReadFrame(&s.resp)
	if err != nil {
		return nil, err
	}

	var resp pb.SyncResponse
	if err := proto.Unmarshal(frame, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// mockConn is a minimal network.Conn carrying only the remote peer ID.
type mockConn struct {
	remote peer.ID
}

func newMockConn(remote peer.ID) *mockConn { return &mockConn{remote: remote} }
func (c *mockConn) Close() error           { return nil }
func (c *mockConn) CloseWithError(code network.ConnErrorCode) error {
	return nil
}

func (c *mockConn) ID() string { return "mock-conn" }
func (c *mockConn) NewStream(ctx context.Context) (network.Stream, error) {
	return nil, io.EOF
}

func (c *mockConn) GetStreams() []network.Stream         { return nil }
func (c *mockConn) IsClosed() bool                       { return false }
func (c *mockConn) As(target any) bool                   { return false }
func (c *mockConn) LocalPeer() peer.ID                   { return peer.ID("local") }
func (c *mockConn) RemotePeer() peer.ID                  { return c.remote }
func (c *mockConn) RemotePublicKey() crypto.PubKey       { return nil }
func (c *mockConn) ConnState() network.ConnectionState   { return network.ConnectionState{} }
func (c *mockConn) LocalMultiaddr() multiaddr.Multiaddr  { return nil }
func (c *mockConn) RemoteMultiaddr() multiaddr.Multiaddr { return nil }
func (c *mockConn) Stat() network.ConnStats              { return network.ConnStats{} }
func (c *mockConn) Scope() network.ConnScope             { return nil }

// syncTestLibrary returns the given track from FindByID.
type syncTestLibrary struct {
	track *domain.Track
}

func (l *syncTestLibrary) Save(ctx context.Context, track *domain.Track) error { return nil }
func (l *syncTestLibrary) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if l.track != nil && l.track.ID == id {
		return l.track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (l *syncTestLibrary) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (l *syncTestLibrary) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (l *syncTestLibrary) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}

func (l *syncTestLibrary) Delete(ctx context.Context, id domain.TrackID) error { return nil }
func (l *syncTestLibrary) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}

func (l *syncTestLibrary) ListAll(ctx context.Context) ([]*domain.Track, error) { return nil, nil }
func (l *syncTestLibrary) ListAllPaths(ctx context.Context) ([]string, error)   { return nil, nil }

func newTestSyncHandler(lib app.LibraryRepository, local peer.ID) *SyncHandler {
	h := NewSyncHandler(lib, nil, local)
	h.SetAdmissionRegistry(NewAdmissionRegistry())
	return h
}

// testTrackPath is the path used by sync handler tests.
const testTrackPath = "/tmp/a.mp3"

func TestSyncHandler_TrackDetail_UnauthorizedBeforeManifest(t *testing.T) {
	track := &domain.Track{ID: domain.TrackID("track-1"), Path: testTrackPath}
	h := newTestSyncHandler(&syncTestLibrary{track: track}, peer.ID("local"))
	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_TrackDetailRequest{
			TrackDetailRequest: &pb.TrackDetailRequest{TrackId: string(track.ID)},
		},
	}

	stream := newSyncTestStream(req, peer.ID("remote"))
	h.Handle(stream)
	resp, err := stream.readResponse()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	er, ok := resp.GetPayload().(*pb.SyncResponse_Error)
	if !ok {
		t.Fatalf("expected error response, got %T", resp.GetPayload())
	}
	if er.Error.Code != pb.ErrorCode_ERROR_CODE_UNAUTHORIZED {
		t.Errorf("error code = %v, want UNAUTHORIZED", er.Error.Code)
	}
}

func TestSyncHandler_ManifestExchange_Admits(t *testing.T) {
	track := &domain.Track{ID: domain.TrackID("track-1"), Path: testTrackPath}
	h := newTestSyncHandler(&syncTestLibrary{track: track}, peer.ID("local"))
	h.SetAnnounceLibrary(true)
	// First, a manifest request grants admission.
	mreq := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}

	manifestStream := newSyncTestStream(mreq, peer.ID("remote"))
	h.Handle(manifestStream)
	resp, err := manifestStream.readResponse()
	if err != nil {
		t.Fatalf("read manifest response: %v", err)
	}
	if _, ok := resp.GetPayload().(*pb.SyncResponse_Manifest); !ok {
		t.Fatalf("expected manifest response, got %T", resp.GetPayload())
	}

	// Now the same peer may request track details.
	dreq := &pb.SyncRequest{
		Payload: &pb.SyncRequest_TrackDetailRequest{
			TrackDetailRequest: &pb.TrackDetailRequest{TrackId: string(track.ID)},
		},
	}

	detailStream := newSyncTestStream(dreq, peer.ID("remote"))
	h.Handle(detailStream)
	resp, err = detailStream.readResponse()
	if err != nil {
		t.Fatalf("read track response: %v", err)
	}
	if _, ok := resp.GetPayload().(*pb.SyncResponse_Track); !ok {
		t.Errorf("expected track response, got %T", resp.GetPayload())
	}
}

func TestSyncHandler_Manifest_Disabled_ReturnsError(t *testing.T) {
	track := &domain.Track{ID: domain.TrackID("track-1"), Path: testTrackPath}
	h := newTestSyncHandler(&syncTestLibrary{track: track}, peer.ID("local"))
	// announceLibrary defaults to false: sharing disabled.
	mreq := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}

	stream := newSyncTestStream(mreq, peer.ID("remote"))
	h.Handle(stream)
	resp, err := stream.readResponse()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	er, ok := resp.GetPayload().(*pb.SyncResponse_Error)
	if !ok {
		t.Fatalf("expected error response, got %T", resp.GetPayload())
	}
	if er.Error.Code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("error code = %v, want PERMISSION_DENIED", er.Error.Code)
	}
	if er.Error.Message != "library sharing disabled" {
		t.Errorf("error message = %q, want %q", er.Error.Message, "library sharing disabled")
	}
	if !h.admission.IsAdmitted(peer.ID("remote")) {
		t.Error("peer should be admitted after manifest request even when sharing is disabled")
	}
}

var (
	_ network.Conn          = (*mockConn)(nil)
	_ network.Stream        = (*syncTestStream)(nil)
	_ app.LibraryRepository = (*syncTestLibrary)(nil)
)
