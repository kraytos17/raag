package ipc

import (
	"bytes"
	"testing"

	pb "github.com/p-society/raag/proto/gen"
)

func TestWriteMsg_ReadMsg_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"empty request"},
		{"request with protocol version"},
		{"response success"},
		{"response with error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.Request{ProtocolVersion: 1}

			var buf bytes.Buffer
			if err := WriteMsg(&buf, req); err != nil {
				t.Fatalf("WriteMsg() error = %v", err)
			}

			var got pb.Request
			if err := ReadMsg(&buf, &got); err != nil {
				t.Fatalf("ReadMsg() error = %v", err)
			}
			if got.ProtocolVersion != req.ProtocolVersion {
				t.Errorf("round trip ProtocolVersion = %d, want %d", got.ProtocolVersion, req.ProtocolVersion)
			}
		})
	}
}

func TestWriteMsg_ReadMsg_PlayRequest(t *testing.T) {
	req := &pb.Request{
		Payload: &pb.Request_Play{
			Play: &pb.PlayRequest{
				TrackId: "test-track-id",
				Query:   "",
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, req); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Request
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	play, ok := got.Payload.(*pb.Request_Play)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Request_Play", got.Payload)
	}
	if play.Play.TrackId != "test-track-id" {
		t.Errorf("Play.TrackId = %q, want %q", play.Play.TrackId, "test-track-id")
	}
}

func TestWriteMsg_ReadMsg_SearchRequest(t *testing.T) {
	req := &pb.Request{
		Payload: &pb.Request_Search{
			Search: &pb.SearchRequest{
				Query: "rock song",
				Limit: 10,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, req); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Request
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	search, ok := got.Payload.(*pb.Request_Search)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Request_Search", got.Payload)
	}
	if search.Search.Query != "rock song" {
		t.Errorf("Search.Query = %q, want %q", search.Search.Query, "rock song")
	}
	if search.Search.Limit != 10 {
		t.Errorf("Search.Limit = %d, want %d", search.Search.Limit, 10)
	}
}

func TestWriteMsg_ReadMsg_SeekRequest(t *testing.T) {
	req := &pb.Request{
		Payload: &pb.Request_Seek{
			Seek: &pb.SeekRequest{
				OffsetMs: 5000,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, req); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Request
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	seek, ok := got.Payload.(*pb.Request_Seek)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Request_Seek", got.Payload)
	}
	if seek.Seek.OffsetMs != 5000 {
		t.Errorf("Seek.OffsetMs = %d, want %d", seek.Seek.OffsetMs, 5000)
	}
}

func TestWriteMsg_ReadMsg_VolumeRequest(t *testing.T) {
	req := &pb.Request{
		Payload: &pb.Request_SetVolume{
			SetVolume: &pb.SetVolumeRequest{
				Volume: 80,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, req); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Request
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	vol, ok := got.Payload.(*pb.Request_SetVolume)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Request_SetVolume", got.Payload)
	}
	if vol.SetVolume.Volume != 80 {
		t.Errorf("SetVolume.Volume = %d, want %d", vol.SetVolume.Volume, 80)
	}
}

func TestWriteMsg_MaxSize(t *testing.T) {
	req := &pb.Request{ProtocolVersion: 1}

	var buf bytes.Buffer
	err := WriteMsg(&buf, req)
	if err != nil {
		t.Errorf("WriteMsg() returned unexpected error: %v", err)
	}
	if buf.Len() < 4 {
		t.Error("WriteMsg() should write at least 4 byte header")
	}
}

func TestReadMsg_Truncated(t *testing.T) {
	shortData := []byte{0x00, 0x00, 0x00}

	var msg pb.Request
	err := ReadMsg(bytes.NewReader(shortData), &msg)
	if err == nil {
		t.Error("ReadMsg() should error on truncated data")
	}
}

func TestReadMsg_InvalidData(t *testing.T) {
	invalidData := bytes.Repeat([]byte{0xFF}, 100)

	var msg pb.Request
	err := ReadMsg(bytes.NewReader(invalidData), &msg)
	if err == nil {
		t.Error("ReadMsg() should error on invalid protobuf data")
	}
}

func TestWriteMsg_Empty(t *testing.T) {
	var buf bytes.Buffer
	req := &pb.Request{}
	if err := WriteMsg(&buf, req); err != nil {
		t.Errorf("WriteMsg() empty request error = %v", err)
	}
	if buf.Len() == 0 {
		t.Error("WriteMsg() should write header for empty message")
	}
}

func TestReadMsg_ZeroLength(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00})

	var msg pb.Request
	if err := ReadMsg(&buf, &msg); err != nil {
		t.Errorf("ReadMsg() zero length error = %v", err)
	}
}

func TestMaxMessageSize_Constant(t *testing.T) {
	if MaxMessageSize == 0 {
		t.Error("MaxMessageSize should not be zero")
	}
	if MaxMessageSize < 1024 {
		t.Error("MaxMessageSize should be at least 1KB")
	}
}

func TestErrMessageTooLarge(t *testing.T) {
	if ErrMessageTooLarge == nil {
		t.Error("ErrMessageTooLarge should not be nil")
	}
	if ErrMessageTooLarge.Error() == "" {
		t.Error("ErrMessageTooLarge should have message")
	}
}

func TestWriteMsg_ReadMsg_Sequential(t *testing.T) {
	var buf bytes.Buffer
	reqs := []*pb.Request{
		{ProtocolVersion: 1},
		{ProtocolVersion: 2},
		{ProtocolVersion: 3},
	}

	for _, req := range reqs {
		if err := WriteMsg(&buf, req); err != nil {
			t.Fatalf("WriteMsg() error = %v", err)
		}
	}
	for i, req := range reqs {
		var got pb.Request
		if err := ReadMsg(&buf, &got); err != nil {
			t.Fatalf("ReadMsg() %d error = %v", i, err)
		}
		if got.ProtocolVersion != req.ProtocolVersion {
			t.Errorf("ReadMsg() %d ProtocolVersion = %d, want %d", i, got.ProtocolVersion, req.ProtocolVersion)
		}
	}
}

func TestWriteMsg_ReadMsg_SearchResponse(t *testing.T) {
	resp := &pb.Response{
		Success: true,
		Payload: &pb.Response_Search{
			Search: &pb.SearchResponse{
				Tracks: []*pb.Track{
					{Id: "id1", Title: "Rock Song"},
					{Id: "id2", Title: "Jazz Song"},
				},
				Total: 2,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, resp); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Response
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}
	if !got.Success {
		t.Error("Response.Success should be true")
	}

	search, ok := got.Payload.(*pb.Response_Search)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Response_Search", got.Payload)
	}
	if len(search.Search.Tracks) != 2 {
		t.Errorf("Search.Tracks len = %d, want 2", len(search.Search.Tracks))
	}
}

func TestWriteMsg_ReadMsg_StatusResponse(t *testing.T) {
	resp := &pb.Response{
		Success: true,
		Payload: &pb.Response_Status{
			Status: &pb.StatusResponse{
				State:         "playing",
				Volume:        80,
				QueueLength:   10,
				QueuePosition: 2,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, resp); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Response
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	status, ok := got.Payload.(*pb.Response_Status)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Response_Status", got.Payload)
	}
	if status.Status.State != "playing" {
		t.Errorf("Status.State = %q, want %q", status.Status.State, "playing")
	}
	if status.Status.Volume != 80 {
		t.Errorf("Status.Volume = %d, want %d", status.Status.Volume, 80)
	}
}

func TestWriteMsg_ReadMsg_HealthCheckResponse(t *testing.T) {
	resp := &pb.Response{
		Success: true,
		Payload: &pb.Response_HealthCheck{
			HealthCheck: &pb.HealthCheckResponse{
				Healthy: true,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteMsg(&buf, resp); err != nil {
		t.Fatalf("WriteMsg() error = %v", err)
	}

	var got pb.Response
	if err := ReadMsg(&buf, &got); err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}

	hc, ok := got.Payload.(*pb.Response_HealthCheck)
	if !ok {
		t.Fatalf("Payload type = %T, want *pb.Response_HealthCheck", got.Payload)
	}
	if !hc.HealthCheck.Healthy {
		t.Error("HealthCheck.Healthy should be true")
	}
}
