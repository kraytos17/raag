package ipc

import (
	"context"
	"net"
	"time"

	"github.com/p-society/raag/internal/infra/ipc/commands"
	pb "github.com/p-society/raag/proto/gen"
)

type Client struct {
	socketPath string
	conn       net.Conn
}

func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	conn, err := net.DialTimeout("unix", c.socketPath, 3*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Send(ctx context.Context, cmd commands.Command) (commands.Response, error) {
	if c.conn == nil {
		if err := c.Connect(ctx); err != nil {
			return commands.Response{Status: "error", Error: err.Error()}, err
		}
	}

	cmdType, err := commandTypeFromString(cmd.Type)
	if err != nil {
		return commands.Response{Status: "error", Error: err.Error()}, err
	}

	req := &pb.Request{
		ProtocolVersion: 1,
		Type:            cmdType,
		Payload:         cmd.Payload,
	}

	c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := WriteRequest(c.conn, req); err != nil {
		c.conn.Close()
		c.conn = nil
		return commands.Response{Status: "error", Error: err.Error()}, err
	}

	c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := ReadResponse(c.conn)
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return commands.Response{Status: "error", Error: err.Error()}, err
	}

	status := "ok"
	if !resp.Success {
		status = "error"
	}

	return commands.Response{
		Status: status,
		Error:  resp.Error,
		Data:   resp.Payload,
	}, nil
}

func commandTypeFromString(cmdType commands.CommandType) (pb.CommandType, error) {
	mapping := map[commands.CommandType]pb.CommandType{
		commands.CmdPlay:           pb.CommandType_PLAY,
		commands.CmdPause:          pb.CommandType_PAUSE,
		commands.CmdResume:         pb.CommandType_RESUME,
		commands.CmdStop:           pb.CommandType_STOP,
		commands.CmdNext:           pb.CommandType_NEXT,
		commands.CmdPrev:           pb.CommandType_PREV,
		commands.CmdSeekTo:         pb.CommandType_SEEK_TO,
		commands.CmdSetVolume:      pb.CommandType_SET_VOLUME,
		commands.CmdQueueAdd:       pb.CommandType_QUEUE_ADD,
		commands.CmdQueueRemove:    pb.CommandType_QUEUE_REMOVE,
		commands.CmdQueueClear:     pb.CommandType_QUEUE_CLEAR,
		commands.CmdSearch:         pb.CommandType_SEARCH,
		commands.CmdLibScan:        pb.CommandType_LIB_SCAN,
		commands.CmdListPeers:      pb.CommandType_LIST_PEERS,
		commands.CmdStreamFrom:     pb.CommandType_STREAM_FROM,
		commands.CmdDebugPeers:     pb.CommandType_DEBUG_PEERS,
		commands.CmdDebugStreams:   pb.CommandType_DEBUG_STREAMS,
		commands.CmdHealthCheck:    pb.CommandType_HEALTH_CHECK,
		commands.CmdStatus:         pb.CommandType_STATUS,
		commands.CmdSubscribe:      pb.CommandType_SUBSCRIBE,
		commands.CmdCreatePlaylist: pb.CommandType_CREATE_PLAYLIST,
		commands.CmdQueueMove:      pb.CommandType_UNKNOWN,
		commands.CmdDeletePlaylist: pb.CommandType_UNKNOWN,
	}

	if t, ok := mapping[cmdType]; ok {
		return t, nil
	}
	return pb.CommandType_UNKNOWN, nil
}
