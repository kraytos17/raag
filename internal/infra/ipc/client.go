package ipc

import (
	"errors"
	"fmt"
	"io"
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
type Client struct {
	socketPath string

	mu        sync.Mutex
	conn      net.Conn
	closeOnce sync.Once
	closed    chan struct{}

	keepaliveDone chan struct{}
}

// NewClient creates a new persistent IPC client and starts the keepalive goroutine.
func NewClient(socketPath string) *Client {
	c := &Client{
		socketPath:    socketPath,
		closed:        make(chan struct{}),
		keepaliveDone: make(chan struct{}),
	}

	go c.keepalive()
	return c
}

// connect dials the socket and stores the connection.
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
	return nil
}

// reconnect closes any broken connection and re-dials with exponential backoff.
// Caller must hold c.mu.
func (c *Client) reconnect() error {
	if c.conn != nil {
		_ = c.conn.Close()
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
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.closed:
		return nil, ErrClientClosed
	default:
	}

	if c.conn == nil {
		if err := c.connect(); err != nil {
			return nil, err
		}
	}

	resp, err := c.doRoundTrip(req)
	if err == nil {
		return resp, nil
	}
	if reconnErr := c.reconnect(); reconnErr != nil {
		return nil, fmt.Errorf("ipc: send failed and reconnect failed: %w", reconnErr)
	}

	req.RequestId = uuid.New().String()
	return c.doRoundTrip(req)
}

// doRoundTrip writes req and reads resp on c.conn.
// It loops discarding any broadcast messages (non-matching request IDs)
// until it receives the response matching our request.
// Caller must hold c.mu and c.conn must be non-nil.
func (c *Client) doRoundTrip(req *pb.Request) (*pb.Response, error) {
	if err := c.conn.SetWriteDeadline(time.Now().Add(domain.IPCWriteTimeout)); err != nil {
		return nil, err
	}
	if err := wire.WriteMsg(c.conn, req); err != nil {
		return nil, err
	}
	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout)); err != nil {
			return nil, err
		}

		var resp pb.Response
		if err := wire.ReadMsg(c.conn, &resp); err != nil {
			return nil, err
		}
		if resp.RequestId == req.RequestId {
			return &resp, nil
		}
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
// This method holds the connection mutex for the entire duration of the scan,
// blocking other commands until the scan completes.
func (c *Client) LibScanAsync(
	incremental bool,
	onProgress func(ScanProgress),
) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		RequestId:       uuid.New().String(),
		Payload:         &pb.Request_LibScan{LibScan: &pb.LibScanRequest{Incremental: incremental}},
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.closed:
		return nil, ErrClientClosed
	default:
	}

	if c.conn == nil {
		if err := c.connect(); err != nil {
			return nil, err
		}
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(domain.IPCWriteTimeout)); err != nil {
		return nil, err
	}
	if err := wire.WriteMsg(c.conn, req); err != nil {
		_ = c.reconnect()
		return nil, fmt.Errorf("ipc: scan request write failed: %w", err)
	}
	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout)); err != nil {
			return nil, err
		}

		var resp pb.Response
		if err := wire.ReadMsg(c.conn, &resp); err != nil {
			_ = c.reconnect()
			return nil, fmt.Errorf("ipc: scan read failed: %w", err)
		}
		if sp := resp.GetScanProgress(); sp != nil {
			if onProgress != nil {
				onProgress(ScanProgress{
					JobID:       sp.JobId,
					Scanned:     int(sp.Scanned),
					Total:       int(sp.Total),
					CurrentFile: sp.CurrentFile,
					Phase:       sp.Phase,
				})
			}
			if sp.Phase == "complete" {
				return &pb.Response{
					Success:   true,
					JobId:     sp.JobId,
					RequestId: resp.RequestId,
				}, nil
			}
			continue
		}
		return &resp, nil
	}
}

// Play starts playback of a track by ID or query.
func (c *Client) Play(trackID, query string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Play{Play: &pb.PlayRequest{TrackId: trackID, Query: query}},
	}
	return c.send(req)
}

// Pause pauses the current playback.
func (c *Client) Pause() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Pause{Pause: &pb.PauseRequest{}},
	}
	return c.send(req)
}

// Resume resumes paused playback.
func (c *Client) Resume() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Resume{Resume: &pb.ResumeRequest{}},
	}
	return c.send(req)
}

// Stop stops the current playback.
func (c *Client) Stop() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Stop{Stop: &pb.StopRequest{}},
	}
	return c.send(req)
}

// Next skips to the next track in the queue.
func (c *Client) Next() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Next{Next: &pb.NextRequest{}},
	}
	return c.send(req)
}

// Prev goes back to the previous track in the queue.
func (c *Client) Prev() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Prev{Prev: &pb.PrevRequest{}},
	}
	return c.send(req)
}

// SeekTo seeks to a position in the current track.
func (c *Client) SeekTo(offsetMs int64) error {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Seek{Seek: &pb.SeekRequest{OffsetMs: offsetMs}},
	}

	resp, err := c.send(req)
	if err != nil {
		return err
	}
	return mapResponseError(resp)
}

// SetVolume sets the playback volume.
func (c *Client) SetVolume(volume int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_SetVolume{SetVolume: &pb.SetVolumeRequest{Volume: volume}},
	}
	return c.send(req)
}

// QueueAdd adds a track to the playback queue.
func (c *Client) QueueAdd(trackID string, position int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueAdd{QueueAdd: &pb.QueueAddRequest{TrackId: trackID, Position: position}},
	}
	return c.send(req)
}

// QueueRemove removes a track from the playback queue.
func (c *Client) QueueRemove(position int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueRemove{QueueRemove: &pb.QueueRemoveRequest{Position: position}},
	}
	return c.send(req)
}

// QueueClear clears the playback queue.
func (c *Client) QueueClear() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueClear{QueueClear: &pb.QueueClearRequest{}},
	}
	return c.send(req)
}

// GetQueue returns the current playback queue.
func (c *Client) GetQueue() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueList{QueueList: &pb.QueueListRequest{}},
	}
	return c.send(req)
}

// QueueSetShuffle toggles queue shuffle to the desired state.
func (c *Client) QueueSetShuffle(shuffle bool) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueShuffle{QueueShuffle: &pb.QueueShuffleRequest{Shuffle: shuffle}},
	}
	return c.send(req)
}

// QueueSetRepeat sets the queue repeat mode: "", "all", or "one".
func (c *Client) QueueSetRepeat(mode string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueRepeat{QueueRepeat: &pb.QueueRepeatRequest{Mode: mode}},
	}
	return c.send(req)
}

// QueueGetMode returns the current shuffle/repeat state.
func (c *Client) QueueGetMode() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_QueueMode{QueueMode: &pb.QueueModeRequest{}},
	}
	return c.send(req)
}

// ListTracks returns a page of the full library.
func (c *Client) ListTracks(offset, limit int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_ListTracks{ListTracks: &pb.ListTracksRequest{Offset: offset, Limit: limit}},
	}
	return c.send(req)
}

// CreatePlaylist creates a new playlist.
func (c *Client) CreatePlaylist(name string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_CreatePlaylist{CreatePlaylist: &pb.CreatePlaylistRequest{Name: name}},
	}
	return c.send(req)
}

// GetPlaylist returns a playlist by ID with its tracks resolved.
func (c *Client) GetPlaylist(id string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_GetPlaylist{GetPlaylist: &pb.GetPlaylistRequest{PlaylistId: id}},
	}
	return c.send(req)
}

// ListPlaylists returns all playlists.
func (c *Client) ListPlaylists() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_ListPlaylists{ListPlaylists: &pb.ListPlaylistsRequest{}},
	}
	return c.send(req)
}

// AddToPlaylist adds a track to a playlist.
func (c *Client) AddToPlaylist(playlistID, trackID string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_AddToPlaylist{AddToPlaylist: &pb.AddToPlaylistRequest{PlaylistId: playlistID, TrackId: trackID}},
	}
	return c.send(req)
}

// Search searches for tracks matching the query.
func (c *Client) Search(query string, limit int32) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Search{Search: &pb.SearchRequest{Query: query, Limit: limit}},
	}
	return c.send(req)
}

// LibScan initiates a library scan (blocking, non-streaming).
func (c *Client) LibScan(incremental bool) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_LibScan{LibScan: &pb.LibScanRequest{Incremental: incremental}},
	}
	return c.send(req)
}

// ListPeers returns a list of connected peers.
func (c *Client) ListPeers() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_ListPeers{ListPeers: &pb.ListPeersRequest{}},
	}
	return c.send(req)
}

// NetworkStatus returns the P2P network status.
func (c *Client) NetworkStatus() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_NetworkStatus{NetworkStatus: &pb.NetworkStatusRequest{}},
	}
	return c.send(req)
}

// BanPeer bans a peer by ID.
func (c *Client) BanPeer(peerID string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_BanPeer{BanPeer: &pb.BanPeerRequest{PeerId: peerID}},
	}
	return c.send(req)
}

// UnbanPeer unbans a peer by ID.
func (c *Client) UnbanPeer(peerID string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_UnbanPeer{UnbanPeer: &pb.UnbanPeerRequest{PeerId: peerID}},
	}
	return c.send(req)
}

// Status returns the current playback status.
func (c *Client) Status() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Status{Status: &pb.StatusRequest{}},
	}
	return c.send(req)
}

// HealthCheck checks if the server is healthy.
func (c *Client) HealthCheck() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_HealthCheck{HealthCheck: &pb.HealthCheckRequest{}},
	}
	return c.send(req)
}

// GetTrack retrieves a track by ID.
func (c *Client) GetTrack(trackID string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_GetTrack{GetTrack: &pb.GetTrackRequest{TrackId: trackID}},
	}
	return c.send(req)
}

// GetTrackByPath retrieves a track by file path.
func (c *Client) GetTrackByPath(path string) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_GetTrackByPath{GetTrackByPath: &pb.GetTrackByPathRequest{Path: path}},
	}
	return c.send(req)
}

// Conn returns the underlying connection for testing purposes.
func (c *Client) Conn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

const (
	EventPlaybackState    uint32 = 1
	EventTrackChanged     uint32 = 2
	EventProgress         uint32 = 4
	EventVolumeChanged    uint32 = 8
	EventQueueUpdated     uint32 = 16
	EventPeerConnected    uint32 = 32
	EventPeerDisconnected uint32 = 64
	EventLibraryUpdated   uint32 = 128
	EventError            uint32 = 256
)

// EventClient handles event subscriptions with backpressure
type EventClient struct {
	events    chan *pb.Event
	errChan   chan error
	closeOnce sync.Once
	closed    chan struct{}
	client    *Client
	eventMask uint32
}

// Subscribe subscribes to events from the server.
// Returns an EventClient that streams events through the Events channel.
func (c *Client) Subscribe(eventMask uint32) (*EventClient, error) {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: eventMask}},
	}

	_, err := c.send(req)
	if err != nil {
		return nil, fmt.Errorf("subscribe failed: %w", err)
	}

	go ec.readEvents()
	return ec, nil
}

// SubscribeWithRetry subscribes with automatic reconnection and retry logic.
// Events are queued on backpressure, with exponential backoff on disconnect.
func (c *Client) SubscribeWithRetry(eventMask uint32) *EventClient {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	go ec.eventLoopWithRetry()
	return ec
}

// eventLoopWithRetry handles reconnection automatically
func (ec *EventClient) eventLoopWithRetry() {
	policy := backoff.NewExponential(100*time.Millisecond, 5*time.Second)
	for {
		select {
		case <-ec.closed:
			return
		default:
		}

		err := ec.connect()
		if err != nil {
			if backoff.Wait(ec.closed, policy.Next()) {
				return
			}
			continue
		}

		policy.Reset()
		ec.readEventsLoop()
	}
}

// connect establishes the subscription connection
func (ec *EventClient) connect() error {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: ec.eventMask}},
	}

	_, err := ec.client.send(req)
	return err
}

// readEventsLoop continuously reads events from the server
func (ec *EventClient) readEventsLoop() error {
	for {
		select {
		case <-ec.closed:
			return nil
		default:
		}

		ec.client.mu.Lock()
		if ec.client.conn != nil {
			_ = ec.client.conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout))
		}
		ec.client.mu.Unlock()

		var event pb.Event
		if err := wire.ReadMsg(ec.client.conn, &event); err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "use of closed") {
				return err
			}
			return err
		}
		ec.sendEvent(&event)
	}
}

// sendEvent sends an event to the channel with backpressure handling
func (ec *EventClient) sendEvent(event *pb.Event) {
	select {
	case ec.events <- event:
	default:
		select {
		case <-ec.events:
		default:
		}

		select {
		case ec.events <- event:
		default:
			slog.Warn("dropping event due to backpressure", "eventType", event.EventType)
		}
	}
}

// readEvents starts the event reader (non-retrying version)
func (ec *EventClient) readEvents() {
	go func() {
		for {
			select {
			case <-ec.closed:
				return
			default:
			}

			ec.client.mu.Lock()
			if ec.client.conn != nil {
				_ = ec.client.conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout))
			}
			ec.client.mu.Unlock()

			var event pb.Event
			err := wire.ReadMsg(ec.client.conn, &event)
			if err != nil {
				ec.errChan <- err
				return
			}
			ec.sendEvent(&event)
		}
	}()
}

// Events returns the channel to receive events
func (ec *EventClient) Events() <-chan *pb.Event {
	return ec.events
}

// Err returns the error channel
func (ec *EventClient) Err() <-chan error {
	return ec.errChan
}

// Close unsubscribes and closes the event client.
// The unsubscribe request is written without waiting for a response: the event
// reader and request round-trips share one connection, so a synchronous send
// would block until the read deadline while readEventsLoop consumes the reply.
// The server removes the subscription when the connection closes regardless.
func (ec *EventClient) Close() error {
	ec.closeOnce.Do(func() {
		close(ec.closed)
		req := &pb.Request{
			ProtocolVersion: domain.IPCProtocolVersion,
			Payload:         &pb.Request_Unsubscribe{Unsubscribe: &pb.UnsubscribeRequest{}},
		}

		ec.client.mu.Lock()
		if ec.client.conn != nil {
			_ = wire.WriteMsg(ec.client.conn, req)
		}
		ec.client.mu.Unlock()
	})
	return nil
}
