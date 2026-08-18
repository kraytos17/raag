package ipc

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
func (c *Client) LibScanAsync(
	incremental bool,
	onProgress func(ScanProgress),
) (*pb.Response, error) {
	listener := make(chan *pb.Response, 16)
	c.mu.Lock()
	c.nextScanID++
	id := c.nextScanID
	c.scanSubs[id] = listener
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.scanSubs, id)
		c.mu.Unlock()
	}()

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_LibScan{LibScan: &pb.LibScanRequest{Incremental: incremental}},
	}
	if _, err := c.send(req); err != nil {
		return nil, fmt.Errorf("ipc: scan request failed: %w", err)
	}
	for {
		select {
		case <-c.closed:
			return nil, ErrClientClosed
		case resp := <-listener:
			if resp == nil {
				// The reader closed the listener after a connection drop.
				return nil, fmt.Errorf("ipc: scan interrupted: connection lost")
			}

			sp := resp.GetScanProgress()
			if sp == nil {
				continue
			}
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
				return &pb.Response{Success: true, JobId: sp.JobId}, nil
			}
		case <-time.After(domain.IPCReadTimeout):
			return nil, fmt.Errorf("ipc: scan timed out")
		}
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

// SetEqualizer sets the EQ (enabled + band gains) and returns the resulting
// state.
func (c *Client) SetEqualizer(enabled bool, bass, mid, treble float64) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload: &pb.Request_Equalizer{Equalizer: &pb.EqualizerRequest{
			Enabled:  enabled,
			BassDb:   float32(bass),
			MidDb:    float32(mid),
			TrebleDb: float32(treble),
		}},
	}
	return c.send(req)
}

// GetEqualizer returns the current EQ state without mutating it.
func (c *Client) GetEqualizer() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload: &pb.Request_Equalizer{Equalizer: &pb.EqualizerRequest{
			Get: true,
		}},
	}
	return c.send(req)
}

// DebugStats returns aggregated stream-pool, search-index, and DB stats.
func (c *Client) DebugStats() (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_DebugStats{DebugStats: &pb.DebugStatsRequest{}},
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
	return c.search(query, limit, false)
}

// SearchRemote searches local + all connected peers' libraries.
func (c *Client) SearchRemote(query string, limit int32) (*pb.Response, error) {
	return c.search(query, limit, true)
}

func (c *Client) search(query string, limit int32, includePeers bool) (*pb.Response, error) {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Search{Search: &pb.SearchRequest{Query: query, Limit: limit, IncludePeers: includePeers}},
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

// ConnState reports the lifecycle of an event subscription's connection.
type ConnState int

const (
	// ConnConnected is emitted after a successful (re)subscribe.
	ConnConnected ConnState = iota
	// ConnReconnecting is emitted when the subscription lost its connection
	// and is backing off before retrying.
	ConnReconnecting
)

// EventClient handles event subscriptions with backpressure. The client's
// single reader goroutine fans events out to registered subscribers; the
// EventClient does not read the connection itself.
type EventClient struct {
	events    chan *pb.Event
	errChan   chan error
	state     chan ConnState
	closeOnce sync.Once
	closed    chan struct{}
	client    *Client
	eventMask uint32
	subID     uint64
}

// Subscribe subscribes to events from the server.
// Returns an EventClient that streams events through the Events channel.
func (c *Client) Subscribe(eventMask uint32) (*EventClient, error) {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		state:     make(chan ConnState, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	c.mu.Lock()
	c.nextSubID++
	ec.subID = c.nextSubID
	c.subs[ec.subID] = ec
	c.mu.Unlock()

	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: eventMask}},
	}
	if _, err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.subs, ec.subID)
		c.mu.Unlock()
		return nil, fmt.Errorf("subscribe failed: %w", err)
	}
	return ec, nil
}

// SubscribeWithRetry subscribes with automatic reconnection and retry logic.
// Events are queued on backpressure, with exponential backoff on disconnect.
func (c *Client) SubscribeWithRetry(eventMask uint32) *EventClient {
	ec := &EventClient{
		events:    make(chan *pb.Event, 100),
		errChan:   make(chan error, 1),
		state:     make(chan ConnState, 1),
		closed:    make(chan struct{}),
		client:    c,
		eventMask: eventMask,
	}

	c.mu.Lock()
	c.nextSubID++
	ec.subID = c.nextSubID
	c.subs[ec.subID] = ec
	c.mu.Unlock()

	go ec.eventLoopWithRetry()
	return ec
}

// eventLoopWithRetry re-subscribes until the client is closed, using
// exponential backoff on failure. The reader goroutine signals a connection
// drop by delivering to errChan (via notifyConnLost).
func (ec *EventClient) eventLoopWithRetry() {
	policy := backoff.NewExponential(100*time.Millisecond, 5*time.Second)
	for {
		select {
		case <-ec.closed:
			return
		default:
		}

		err := ec.resubscribe()
		if err != nil {
			ec.setState(ConnReconnecting)
			if backoff.Wait(ec.closed, policy.Next()) {
				return
			}
			continue
		}

		policy.Reset()
		ec.setState(ConnConnected)
		select {
		case <-ec.closed:
			return
		case <-ec.errChan:
			// Connection dropped. Report reconnecting immediately — the
			// resubscribe below may block in the client's dial backoff.
			ec.setState(ConnReconnecting)
		}
	}
}

// setState emits a connection-state signal to the subscriber (non-blocking).
func (ec *EventClient) setState(state ConnState) {
	select {
	case ec.state <- state:
	default:
	}
}

// resubscribe (re-)issues the Subscribe request on the shared connection.
func (ec *EventClient) resubscribe() error {
	req := &pb.Request{
		ProtocolVersion: domain.IPCProtocolVersion,
		Payload:         &pb.Request_Subscribe{Subscribe: &pb.SubscribeRequest{EventMask: ec.eventMask}},
	}
	_, err := ec.client.send(req)
	return err
}

// sendEvent sends an event to the channel with backpressure handling.
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

// notifyConnLost delivers a connection-drop signal to the subscriber.
func (ec *EventClient) notifyConnLost(err error) {
	select {
	case ec.errChan <- err:
	default:
	}
}

// Events returns the channel to receive events
func (ec *EventClient) Events() <-chan *pb.Event {
	return ec.events
}

// State returns the channel that reports connection-state transitions
// (ConnConnected / ConnReconnecting) for this subscription.
func (ec *EventClient) State() <-chan ConnState {
	return ec.state
}

// Err returns the error channel
func (ec *EventClient) Err() <-chan error {
	return ec.errChan
}

// Close unsubscribes and closes the event client.
// The unsubscribe request is written without waiting for a response: the
// server removes the subscription when the connection closes regardless.
func (ec *EventClient) Close() error {
	ec.closeOnce.Do(func() {
		close(ec.closed)
		ec.client.mu.Lock()
		delete(ec.client.subs, ec.subID)
		conn := ec.client.conn
		ec.client.mu.Unlock()

		if conn != nil {
			req := &pb.Request{
				ProtocolVersion: domain.IPCProtocolVersion,
				Payload:         &pb.Request_Unsubscribe{Unsubscribe: &pb.UnsubscribeRequest{}},
			}
			_ = wire.WriteMsg(conn, req)
		}
	})
	return nil
}
