package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/p-society/raag/internal/domain"
)

func (m *Model) fetchInitialData() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.Status()
		if err != nil {
			return StatusMsg{Err: err}
		}
		if !resp.Success {
			return StatusMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if eqResp, err := m.IPCClient.GetEqualizer(); err == nil && eqResp.Success {
			if eq := eqResp.GetEqualizer(); eq != nil {
				m.EQ = domain.EqualizerSettings{
					Enabled: eq.Enabled,
					Bass:    float64(eq.BassDb),
					Mid:     float64(eq.MidDb),
					Treble:  float64(eq.TrebleDb),
				}
			}
		}
		return StatusMsg{Status: resp.GetStatus()}
	}
}

// fetchLibrary pulls the full track list from the daemon.
func (m *Model) fetchLibrary() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListTracks(0, 0)
		if err != nil {
			return LibraryMsg{Err: err}
		}
		if !resp.Success {
			return LibraryMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return LibraryMsg{Tracks: resp.GetListTracks().Tracks}
	}
}

// fetchQueue pulls the current playback queue from the daemon.
func (m *Model) fetchQueue() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.GetQueue()
		if err != nil {
			return QueueMsg{Err: err}
		}
		if !resp.Success {
			return QueueMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return QueueMsg{Tracks: resp.GetQueueList().Tracks}
	}
}

// fetchQueueMode pulls the current shuffle/repeat state from the daemon.
func (m *Model) fetchQueueMode() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.QueueGetMode()
		if err != nil {
			return QueueModeMsg{Err: err}
		}
		if !resp.Success {
			return QueueModeMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if qm := resp.GetQueueMode(); qm != nil {
			return QueueModeMsg{Shuffle: qm.Shuffle, Repeat: qm.Repeat}
		}
		return QueueModeMsg{}
	}
}

// fetchPeers pulls the discovered peers from the daemon.
func (m *Model) fetchPeers() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListPeers()
		if err != nil {
			return PeersMsg{Err: err}
		}
		if !resp.Success {
			return PeersMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return PeersMsg{Peers: resp.GetListPeers().Peers}
	}
}

func (m *Model) cmdPause() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Pause()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

// fetchPlaylists pulls the playlists from the daemon.
func (m *Model) fetchPlaylists() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListPlaylists()
		if err != nil {
			return PlaylistsMsg{Err: err}
		}
		if !resp.Success {
			return PlaylistsMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if lp := resp.GetListPlaylists(); lp != nil {
			return PlaylistsMsg{Playlists: lp.Playlists}
		}
		return PlaylistsMsg{}
	}
}

// cmdShowSelectedPlaylist loads the tracks of the selected playlist into the
// library panel so they can be played.
func (m *Model) cmdShowSelectedPlaylist() tea.Cmd {
	return func() tea.Msg {
		idx := m.Playlists.Index()
		items := m.Playlists.Items()
		if idx < 0 || idx >= len(items) {
			return nil
		}

		item, ok := items[idx].(PlaylistItem)
		if !ok || item.Playlist == nil {
			return nil
		}

		resp, err := m.IPCClient.GetPlaylist(item.Playlist.Id)
		if err != nil {
			return PlaylistTracksMsg{Err: err}
		}
		if !resp.Success {
			return PlaylistTracksMsg{Err: fmt.Errorf("%s", resp.Error)}
		}

		gp := resp.GetGetPlaylist()
		if gp == nil || gp.Playlist == nil {
			return PlaylistTracksMsg{Err: fmt.Errorf("playlist not found")}
		}
		return PlaylistTracksMsg{Playlist: gp.Playlist, Tracks: gp.Tracks}
	}
}

func (m *Model) cmdResume() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Resume()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdNext() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Next()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdPrev() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Prev()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdStop() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Stop()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdSeek(deltaMs int64) tea.Cmd {
	return func() tea.Msg {
		newPos := min(max(m.Player.PositionMs+deltaMs, 0), m.Player.DurationMs)
		err := m.IPCClient.SeekTo(newPos)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdVolume(delta int32) tea.Cmd {
	return func() tea.Msg {
		newVol := min(max(m.Volume+delta, 0), 100)
		_, err := m.IPCClient.SetVolume(newVol)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdSearch(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.SearchRemote(query, 200)
		if err != nil {
			return LibraryMsg{Err: err}
		}
		if !resp.Success {
			return LibraryMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return LibraryMsg{Tracks: resp.GetSearch().Tracks}
	}
}

func (m *Model) cmdPlaySelected() tea.Cmd {
	return func() tea.Msg {
		var trackID string
		activeList := m.activeList()
		if activeList != nil {
			idx := activeList.Index()
			items := activeList.Items()
			if idx >= 0 && idx < len(items) {
				switch item := items[idx].(type) {
				case TrackItem:
					if item.Track != nil {
						trackID = item.Track.Id
					}
				case QueueItem:
					// Playing a queued track: Play(trackId) already makes it the
					// queue-current item
					if item.Track != nil {
						trackID = item.Track.Id
					}
				}
			}
		}
		if trackID != "" {
			_, err := m.IPCClient.Play(trackID, "")
			if err != nil {
				return ErrorMsg{Err: err.Error()}
			}
		}
		return nil
	}
}

func (m *Model) cmdQueueRemove() tea.Cmd {
	return func() tea.Msg {
		activeList := m.activeList()
		if activeList != nil {
			idx := activeList.Index()
			if idx >= 0 {
				_, err := m.IPCClient.QueueRemove(int32(idx))
				if err != nil {
					return ErrorMsg{Err: err.Error()}
				}
			}
		}
		return nil
	}
}

func (m *Model) cmdQueueShuffle() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.QueueSetShuffle(!m.Player.Shuffle)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		m.Player.Shuffle = !m.Player.Shuffle
		return nil
	}
}

func (m *Model) cmdQueueRepeat() tea.Cmd {
	return func() tea.Msg {
		next := repeatAll
		switch m.Player.RepeatMode {
		case "":
			next = repeatAll
		case repeatAll:
			next = repeatOne
		case repeatOne:
			next = repeatNone
		}

		_, err := m.IPCClient.QueueSetRepeat(next)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}

		m.Player.RepeatMode = next
		return nil
	}
}

// cmdCycleEQ cycles to the next EQ preset and applies it via IPC.
func (m *Model) cmdCycleEQ() tea.Cmd {
	return func() tea.Msg {
		idx := 0
		for i, p := range domain.EQPresets {
			if p == m.EQ {
				idx = (i + 1) % len(domain.EQPresets)
				break
			}
		}

		next := domain.EQPresets[idx]
		_, err := m.IPCClient.SetEqualizer(next.Enabled, next.Bass, next.Mid, next.Treble)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}

		m.EQ = next
		m.ShowEq = true
		return nil
	}
}
