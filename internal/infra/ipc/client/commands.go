package client

import (
	"fmt"
	"time"

	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

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
