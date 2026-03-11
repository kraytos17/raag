package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/p-society/raag/internal/socket"
)

const DefaultSocketName = "daemon.sock"

type SocketClient struct {
	socketPath string
}

func NewSocketClient() *SocketClient {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}

	configDir := filepath.Join(home, ".config", "raag")
	socketPath := filepath.Join(configDir, DefaultSocketName)
	return &SocketClient{socketPath: socketPath}
}

func (c *SocketClient) Query(command string, args ...string) (*socket.Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	req := socket.Request{
		Command: command,
		Args:    args,
	}
	if err := encoder.Encode(req); err != nil {
		return nil, err
	}

	var resp socket.Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *SocketClient) IsAvailable() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
