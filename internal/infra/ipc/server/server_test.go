package server

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	ipcclient "github.com/p-society/raag/internal/infra/ipc/client"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

// TestServer_ConcurrentScanAndEventBroadcast_NoFrameInterleave is a
// concurrent-load smoke test: scan-progress broadcast and event broadcast
// write to the same connection from different goroutines. Every frame the
// subscribed client receives must unmarshal cleanly (scan-progress Response or
// Event) and the total frame count must match exactly. Note: Go's net.Conn
// serializes writes per-fd, so this guards against a future regression where a
// writer sends a frame in multiple non-atomic conn.Write calls, rather than
// reproducing an interleave on the current code.
func TestServer_ConcurrentScanAndEventBroadcast_NoFrameInterleave(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { conn.Close() }()

	subReq := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: ipcclient.EventPlaybackState}},
	}
	if err := wire.WriteMsg(conn, subReq); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	var subResp pb.Response
	if err := wire.ReadMsg(conn, &subResp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if !subResp.Success {
		t.Fatalf("subscribe failed: %s", subResp.Error)
	}

	// Payloads comfortably exceed the unix-socket send buffer (208KB) so each
	// Write spans multiple write(2) syscalls, but stay small enough to keep
	// the test fast under -race.
	big := bytes.Repeat([]byte("x"), 512*1024)
	scanResp := &pb.Response{
		Success: true,
		Payload: &pb.Response_ScanProgress{ScanProgress: &pb.ScanProgress{
			Scanned: 1, Total: 100, CurrentFile: string(big),
		}},
	}
	eventPayload := big

	const iterations = 100
	expected := iterations * 2 // scan + event frames each iteration

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range iterations {
			srv.broadcast(scanResp)
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			srv.PublishEvent(ipcclient.EventPlaybackState, eventPayload)
		}
	}()

	// Slow reader: read a few frames, then stop draining briefly so the send
	// buffer fills and a writer blocks with a partially-written frame — the
	// window where a second writer's bytes get spliced in.
	var reads int
	var corrupt string
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for reads < expected {
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			frame, err := wire.ReadFrame(conn)
			if err != nil {
				corrupt = fmt.Sprintf("read frame %d: %v", reads, err)
				return
			}

			var resp pb.Response
			if err := proto.Unmarshal(frame, &resp); err == nil {
				reads++
				if reads%10 == 0 {
					time.Sleep(5 * time.Millisecond) // let the send buffer fill
				}
				continue
			}

			var ev pb.Event
			if err := proto.Unmarshal(frame, &ev); err == nil && ev.EventType != 0 {
				reads++
				if reads%10 == 0 {
					time.Sleep(5 * time.Millisecond)
				}
				continue
			}
			corrupt = fmt.Sprintf("frame %d failed to unmarshal (interleaved/corrupted)", reads)
			return
		}
	}()

	wg.Wait()
	<-readerDone
	if corrupt != "" {
		t.Fatalf("frame corruption detected: %s", corrupt)
	}
	if reads != expected {
		t.Fatalf("received %d frames, want %d (some frames were merged/torn)", reads, expected)
	}
}

func TestServer_Dispatch_UnknownRequest(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	req := &pb.Request{RequestId: "abc"}
	resp := srv.dispatch(context.Background(), req)
	if resp.Success {
		t.Fatal("dispatch(nil payload) success = true, want false")
	}
	if resp.Error != "unknown request type" {
		t.Errorf("dispatch(nil payload) error = %q, want %q", resp.Error, "unknown request type")
	}
	if resp.RequestId != "abc" {
		t.Errorf("dispatch() RequestId = %q, want echoed %q", resp.RequestId, "abc")
	}
}

func TestServer_Dispatch_RoutesKnownPayloads(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Stop()

	req := &pb.Request{
		RequestId: "hc-1",
		Payload:   &pb.Request_HealthCheck{HealthCheck: &pb.HealthCheckRequest{}},
	}
	resp := srv.dispatch(context.Background(), req)
	if !resp.Success {
		t.Fatalf("dispatch(healthcheck) success = false, want true (error %q)", resp.Error)
	}
	if resp.RequestId != "hc-1" {
		t.Errorf("dispatch() RequestId = %q, want echoed %q", resp.RequestId, "hc-1")
	}
	if _, ok := resp.Payload.(*pb.Response_HealthCheck); !ok {
		t.Errorf("dispatch(healthcheck) payload = %T, want *pb.Response_HealthCheck", resp.Payload)
	}
}

func TestServer_Dispatch_CoversAllRequestPayloads(t *testing.T) {
	// Every oneof member of pb.Request must have a dispatch table row, so
	// adding a request type without wiring it fails here instead of
	// surfacing as "unknown request type" at runtime.
	fields := (&pb.Request{}).ProtoReflect().Descriptor().Fields()
	want := 0
	for i := range fields.Len() {
		if oo := fields.Get(i).ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			want++
		}
	}
	if got := len(dispatchTable); got != want {
		t.Fatalf("dispatchTable has %d rows, want %d (one per pb.Request payload)", got, want)
	}
}

func TestIPCCommandLabel_AllPayloads(t *testing.T) {
	// Regression: oneof wrapper types (e.g. *Request_HealthCheck) do not
	// implement proto.Message; the label must come from reflection, not a
	// type assertion that panics on the daemon's per-request metrics path.
	cases := []struct {
		req  *pb.Request
		want string
	}{
		{nil, unknownCommand},
		{&pb.Request{}, unknownCommand},
		{&pb.Request{Payload: &pb.Request_Play{Play: &pb.PlayRequest{}}}, "Request_Play"},
		{&pb.Request{Payload: &pb.Request_HealthCheck{HealthCheck: &pb.HealthCheckRequest{}}}, "Request_HealthCheck"},
		{&pb.Request{Payload: &pb.Request_Status{Status: &pb.StatusRequest{}}}, "Request_Status"},
		{&pb.Request{Payload: &pb.Request_Search{Search: &pb.SearchRequest{}}}, "Request_Search"},
		{&pb.Request{Payload: &pb.Request_SetVolume{SetVolume: &pb.SetVolumeRequest{}}}, "Request_SetVolume"},
		{&pb.Request{Payload: &pb.Request_ListPeers{ListPeers: &pb.ListPeersRequest{}}}, "Request_ListPeers"},
	}

	for _, tc := range cases {
		if got := ipcCommandLabel(tc.req); got != tc.want {
			t.Errorf("ipcCommandLabel(%T) = %q, want %q", tc.req, got, tc.want)
		}
	}
}
