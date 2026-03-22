package ipc

import (
	"fmt"
	"net"
	"time"

	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const ipcTimeout = 3 * time.Second
const protocolVersion = 1

type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
	}
}

func (c *Client) Play(trackID, query string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Play{Play: &pb.PlayRequest{TrackId: trackID, Query: query}},
	}
	return c.send(req)
}

func (c *Client) Pause() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Pause{Pause: &pb.PauseRequest{}},
	}
	return c.send(req)
}

func (c *Client) Resume() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Resume{Resume: &pb.ResumeRequest{}},
	}
	return c.send(req)
}

func (c *Client) Stop() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Stop{Stop: &pb.StopRequest{}},
	}
	return c.send(req)
}

func (c *Client) Next() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Next{Next: &pb.NextRequest{}},
	}
	return c.send(req)
}

func (c *Client) Prev() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Prev{Prev: &pb.PrevRequest{}},
	}
	return c.send(req)
}

func (c *Client) SeekTo(offsetMs int64) error {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Seek{Seek: &pb.SeekRequest{OffsetMs: offsetMs}},
	}

	resp, err := c.send(req)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (c *Client) SetVolume(volume int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_SetVolume{SetVolume: &pb.SetVolumeRequest{Volume: volume}},
	}
	return c.send(req)
}

func (c *Client) QueueAdd(trackID string, position int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_QueueAdd{QueueAdd: &pb.QueueAddRequest{TrackId: trackID, Position: position}},
	}
	return c.send(req)
}

func (c *Client) QueueRemove(position int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_QueueRemove{QueueRemove: &pb.QueueRemoveRequest{Position: position}},
	}
	return c.send(req)
}

func (c *Client) QueueClear() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_QueueClear{QueueClear: &pb.QueueClearRequest{}},
	}
	return c.send(req)
}

func (c *Client) Search(query string, limit int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Search{Search: &pb.SearchRequest{Query: query, Limit: limit}},
	}
	return c.send(req)
}

func (c *Client) LibScan(incremental bool) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_LibScan{LibScan: &pb.LibScanRequest{Incremental: incremental}},
	}
	return c.send(req)
}

func (c *Client) ListPeers() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_ListPeers{ListPeers: &pb.ListPeersRequest{}},
	}
	return c.send(req)
}

func (c *Client) Status() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_Status{Status: &pb.StatusRequest{}},
	}
	return c.send(req)
}

func (c *Client) HealthCheck() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_HealthCheck{HealthCheck: &pb.HealthCheckRequest{}},
	}
	return c.send(req)
}

func (c *Client) GetTrack(trackID string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_GetTrack{GetTrack: &pb.GetTrackRequest{TrackId: trackID}},
	}
	return c.send(req)
}

func (c *Client) GetTrackByPath(path string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: protocolVersion,
		Payload:         &pb.Request_GetTrackByPath{GetTrackByPath: &pb.GetTrackByPathRequest{Path: path}},
	}
	return c.send(req)
}

func (c *Client) send(req *pb.Request) (*pb.Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, ipcTimeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetWriteDeadline(time.Now().Add(ipcTimeout)); err != nil {
		return nil, err
	}
	if err := wire.WriteMsg(conn, req); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(ipcTimeout)); err != nil {
		return nil, err
	}

	var resp pb.Response
	if err := wire.ReadMsg(conn, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
