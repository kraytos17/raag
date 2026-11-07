package client

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/backoff"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

var (
	ErrTrackNotFound  = domain.ErrTrackNotFound
	ErrPlaybackFailed = errors.New("playback failed")
	ErrInvalidCommand = errors.New("invalid command")
	ErrServerError    = errors.New("server error")
	ErrClientClosed   = errors.New("ipc: client is closed")
)

func mapResponseError(resp *pb.Response) error {
	if resp.Success {
		return nil
	}

	errStr := resp.Error
	switch {
	case strings.Contains(errStr, "not found"):
		return ErrTrackNotFound
	case strings.Contains(errStr, "playback"):
		return ErrPlaybackFailed
	case strings.Contains(errStr, "invalid"):
		return ErrInvalidCommand
	default:
		return fmt.Errorf("%w: %s", ErrServerError, errStr)
	}
}

type ScanProgress struct {
	JobID       string
	Scanned     int
	Total       int
	CurrentFile string
	Phase       string
}

// Client maintains a persistent connection to the IPC server,
// automatically reconnecting on failure and sending keepalive pings
// to prevent server-side timeouts.
//
// A single reader goroutine owns all reads from the connection and
// demultiplexes each frame to a pending request waiter, an event
// subscriber, or a scan-progress listener. This avoids concurrent
// readers splitting frames.
type Client struct {
	socketPath string

	mu            sync.Mutex
	conn          net.Conn
	closeOnce     sync.Once
	closed        chan struct{}
	keepaliveDone chan struct{}

	// pending maps a request ID to its response waiter.
	pending map[string]chan *roundTripResult
	// subs maps a subscriber ID to its event client.
	subs map[uint64]*EventClient
	// nextSubID is the next subscriber ID.
	nextSubID uint64
	// scanSubs maps a scan listener ID to its progress channel.
	scanSubs map[uint64]chan *pb.Response
	// nextScanID is the next scan listener ID.
	nextScanID uint64
}

// roundTripResult carries a response or the error that prevented it.
type roundTripResult struct {
	resp *pb.Response
	err  error
}

// NewClient creates a new persistent IPC client and starts the keepalive goroutine.
func NewClient(socketPath string) *Client {
	c := &Client{
		socketPath:    socketPath,
		closed:        make(chan struct{}),
		keepaliveDone: make(chan struct{}),
		pending:       make(map[string]chan *roundTripResult),
		subs:          make(map[uint64]*EventClient),
		scanSubs:      make(map[uint64]chan *pb.Response),
	}

	go c.keepalive()
	return c
}

// connect dials the socket and starts the reader goroutine.
// Caller must hold c.mu.
func (c *Client) connect() error {
	if c.conn != nil {
		return nil
	}

	conn, err := net.DialTimeout("unix", c.socketPath, domain.IPCConnectTimeout)
	if err != nil {
		return fmt.Errorf("ipc: connect: %w", err)
	}

	c.conn = conn
	// The reader goroutine is the sole owner of reads on this connection.
	// It demultiplexes frames to request waiters, event subscribers, and
	// scan listeners, and tears the connection down on error.
	go c.readLoop(conn)
	return nil
}

// reconnect closes any broken connection and re-dials with exponential backoff,
// restarting the reader goroutine on success.
// Caller must hold c.mu.
func (c *Client) reconnect() error {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	delay := backoff.NewExponential(domain.IPCReconnectBaseDelay, domain.IPCReconnectMaxDelay)
	for attempt := range domain.IPCReconnectMaxAttempts {
		select {
		case <-c.closed:
			return ErrClientClosed
		default:
		}

		conn, err := net.DialTimeout("unix", c.socketPath, domain.IPCConnectTimeout)
		if err == nil {
			c.conn = conn
			go c.readLoop(conn)
			return nil
		}
		if attempt == domain.IPCReconnectMaxAttempts-1 {
			return fmt.Errorf("ipc: reconnect failed after %d attempts: %w",
				domain.IPCReconnectMaxAttempts, err)
		}

		c.mu.Unlock()
		if backoff.Wait(c.closed, delay.Next()) {
			c.mu.Lock()
			return ErrClientClosed
		}
		c.mu.Lock()
	}
	return errors.New("ipc: reconnect exhausted")
}

// send transmits a request and returns the response, using the persistent connection.
// On connection failure, it attempts exactly one reconnect before giving up.
func (c *Client) send(req *pb.Request) (*pb.Response, error) {
	req.RequestId = uuid.New().String()
	resp, err := c.sendOnce(req)
	if err == nil || errors.Is(err, ErrClientClosed) {
		return resp, err
	}

	// Connection-level failure: reconnect once and retry.
	c.mu.Lock()
	if reconnErr := c.reconnect(); reconnErr != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("ipc: send failed and reconnect failed: %w", reconnErr)
	}

	c.mu.Unlock()
	req.RequestId = uuid.New().String()
	return c.sendOnce(req)
}

// sendOnce performs a single attempt: registers a waiter, writes the request
// under c.mu, then waits for the response outside the lock.
func (c *Client) sendOnce(req *pb.Request) (*pb.Response, error) {
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return nil, ErrClientClosed
	default:
	}

	if c.conn == nil {
		if err := c.connect(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}

	ch := make(chan *roundTripResult, 1)
	c.pending[req.RequestId] = ch
	if err := c.conn.SetWriteDeadline(time.Now().Add(domain.IPCWriteTimeout)); err != nil {
		delete(c.pending, req.RequestId)
		c.mu.Unlock()
		return nil, err
	}
	if err := wire.WriteMsg(c.conn, req); err != nil {
		delete(c.pending, req.RequestId)
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Unlock()
	return c.waitResponse(req.RequestId, ch)
}

// waitResponse blocks until the response for the request arrives, the
// connection fails, the client is closed, or the read deadline elapses.
func (c *Client) waitResponse(requestID string, ch chan *roundTripResult) (*pb.Response, error) {
	timer := time.NewTimer(domain.IPCReadTimeout)
	defer timer.Stop()

	for {
		select {
		case <-c.closed:
			return nil, ErrClientClosed
		case r := <-ch:
			if r.err != nil {
				return nil, r.err
			}
			return r.resp, nil
		case <-timer.C:
			c.mu.Lock()
			delete(c.pending, requestID)
			c.mu.Unlock()
			return nil, fmt.Errorf("ipc: request timed out")
		}
	}
}

// readLoop is the single reader for a connection. It reads frames and routes
// them to the correct consumer. On read error it fails all pending requests,
// notifies event subscribers, and returns (the connection is dead).
func (c *Client) readLoop(conn net.Conn) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout)); err != nil {
			c.onReadError(conn, err)
			return
		}

		frame, err := wire.ReadFrame(conn)
		if err != nil {
			c.onReadError(conn, err)
			return
		}

		var resp pb.Response
		if err := proto.Unmarshal(frame, &resp); err == nil {
			if resp.RequestId != "" {
				c.deliverResponse(&resp)
				continue
			}
			if resp.GetScanProgress() != nil {
				c.deliverScanProgress(&resp)
				continue
			}
		}

		var event pb.Event
		if err := proto.Unmarshal(frame, &event); err == nil {
			c.deliverEvent(&event)
			continue
		}
		slog.Debug("ipc: dropped unclassifiable frame")
	}
}

// deliverResponse routes a request/response frame to its pending waiter.
func (c *Client) deliverResponse(resp *pb.Response) {
	c.mu.Lock()
	ch := c.pending[resp.RequestId]
	delete(c.pending, resp.RequestId)
	c.mu.Unlock()
	if ch != nil {
		ch <- &roundTripResult{resp: resp}
	}
}

// deliverEvent fans an event frame out to all registered event subscribers.
func (c *Client) deliverEvent(event *pb.Event) {
	c.mu.Lock()
	subs := make([]*EventClient, 0, len(c.subs))
	for _, ec := range c.subs {
		subs = append(subs, ec)
	}

	c.mu.Unlock()
	for _, ec := range subs {
		ec.sendEvent(event)
	}
}

// deliverScanProgress fans a scan-progress broadcast out to scan listeners.
func (c *Client) deliverScanProgress(resp *pb.Response) {
	c.mu.Lock()
	listeners := make([]chan *pb.Response, 0, len(c.scanSubs))
	for _, ch := range c.scanSubs {
		listeners = append(listeners, ch)
	}

	c.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- resp:
		default:
		}
	}
}

// onReadError handles a failed read on conn. It fails every pending request,
// notifies event subscribers that the connection dropped, and clears scan
// listeners, then returns so the connection can be re-established lazily.
func (c *Client) onReadError(conn net.Conn, err error) {
	c.mu.Lock()
	if c.conn != conn {
		// A reconnect replaced this connection; a stale reader is exiting.
		c.mu.Unlock()
		return
	}

	c.conn = nil
	connErr := fmt.Errorf("ipc: connection lost: %w", err)
	for id, ch := range c.pending {
		delete(c.pending, id)
		select {
		case ch <- &roundTripResult{err: connErr}:
		default:
		}
	}

	subs := make([]*EventClient, 0, len(c.subs))
	for _, ec := range c.subs {
		subs = append(subs, ec)
	}
	for id, ch := range c.scanSubs {
		delete(c.scanSubs, id)
		close(ch)
	}

	c.mu.Unlock()
	for _, ec := range subs {
		ec.notifyConnLost(connErr)
	}
}

// keepalive sends periodic health checks to prevent server-side idle timeouts.
func (c *Client) keepalive() {
	defer close(c.keepaliveDone)

	ticker := time.NewTicker(domain.IPCKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			c.mu.Lock()
			hasConn := c.conn != nil
			c.mu.Unlock()
			if !hasConn {
				continue
			}
			req := &pb.Request{
				ProtocolVersion: domain.IPCProtocolVersion,
				Payload:         &pb.Request_HealthCheck{HealthCheck: &pb.HealthCheckRequest{}},
			}
			c.send(req)
		}
	}
}

// Close shuts down the client, stopping the keepalive goroutine and closing
// the connection. Safe to call multiple times.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		<-c.keepaliveDone

		c.mu.Lock()
		if c.conn != nil {
			err = c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()
	})
	return err
}

// LibScanAsync initiates a library scan and calls onProgress for each progress update.
// Scan-progress frames are broadcast by the server as Responses with an empty
// RequestId; the client's reader routes them to a dedicated scan listener here.

func (c *Client) Conn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}
