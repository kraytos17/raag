package mocks

import (
	"context"
	"iter"
	"reflect"
	"sync"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

// The handler fakes below implement the playback/queue/library/search IPC
// server interfaces (server.PlaybackHandler and friends) for testing, both
// for server unit tests and client-server integration tests.

// MockPlaybackHandler implements PlaybackHandler for testing.
type MockPlaybackHandler struct {
	state        domain.PlayerState
	volume       int
	currentTrack *domain.Track
	Position     time.Duration
	BufferFill   float64
	EQ           domain.EqualizerSettings
}

func NewMockPlaybackHandler() *MockPlaybackHandler {
	return &MockPlaybackHandler{
		state:  domain.PlayerStateIdle,
		volume: 50,
	}
}

func (m *MockPlaybackHandler) Play(_ context.Context, _ domain.TrackID) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *MockPlaybackHandler) PlayQuery(_ context.Context, _ string) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *MockPlaybackHandler) Pause(_ context.Context) error {
	m.state = domain.PlayerStatePaused
	return nil
}

func (m *MockPlaybackHandler) Resume(_ context.Context) error {
	m.state = domain.PlayerStatePlaying
	return nil
}

func (m *MockPlaybackHandler) Stop(_ context.Context) error {
	m.state = domain.PlayerStateIdle
	return nil
}

func (m *MockPlaybackHandler) Seek(_ context.Context, _ time.Duration) error {
	return nil
}

func (m *MockPlaybackHandler) SetVolume(_ context.Context, vol int) error {
	m.volume = vol
	return nil
}

func (m *MockPlaybackHandler) GetState() domain.PlayerState {
	return m.state
}

func (m *MockPlaybackHandler) GetVolume() int {
	return m.volume
}

func (m *MockPlaybackHandler) GetCurrentTrack() *domain.Track {
	return m.currentTrack
}

func (m *MockPlaybackHandler) GetPosition() time.Duration {
	return m.Position
}

func (m *MockPlaybackHandler) GetBufferFillLevel() float64 {
	return m.BufferFill
}

func (m *MockPlaybackHandler) SetEqualizer(settings domain.EqualizerSettings) {
	m.EQ = settings
}

func (m *MockPlaybackHandler) GetEqualizer() domain.EqualizerSettings {
	return m.EQ
}

func (m *MockPlaybackHandler) OnProgress(callback func(positionMs, durationMs int64)) {
	// No-op for testing
}

// MockScannerHandler implements ScannerHandler for testing.
type MockScannerHandler struct {
	mu         sync.Mutex
	progressFn []func(app.ScanProgress)
}

func NewMockScannerHandler() *MockScannerHandler {
	return &MockScannerHandler{}
}

func (m *MockScannerHandler) Scan(_ context.Context) (int, error) {
	return 0, nil
}

func (m *MockScannerHandler) ScanIncremental(_ context.Context) (added, modified, removed int, err error) {
	return 0, 0, 0, nil
}

func (m *MockScannerHandler) OnProgress(fn func(app.ScanProgress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressFn = append(m.progressFn, fn)
}

func (m *MockScannerHandler) RemoveProgressHandler(fn func(app.ScanProgress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, h := range m.progressFn {
		if reflect.ValueOf(h).Pointer() == reflect.ValueOf(fn).Pointer() {
			m.progressFn = append(m.progressFn[:i], m.progressFn[i+1:]...)
			return
		}
	}
}

// ProgressHandlerCount reports how many scan progress handlers are registered.
func (m *MockScannerHandler) ProgressHandlerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.progressFn)
}

// MockSearchHandler implements SearchHandler for testing.
type MockSearchHandler struct {
	Tracks []*domain.Track
}

func NewMockSearchHandler() *MockSearchHandler {
	return &MockSearchHandler{}
}

func (m *MockSearchHandler) Search(_ context.Context, _ string, _ int) ([]*domain.Track, error) {
	return m.Tracks, nil
}

// SearchWithRemote satisfies the optional interface used by handleSearch when
// include_peers is set.
func (m *MockSearchHandler) SearchWithRemote(_ context.Context, _ string, _ int) ([]*domain.Track, error) {
	return m.Tracks, nil
}

// MockLibraryRepoHandler implements LibraryRepoHandler for testing.
type MockLibraryRepoHandler struct {
	Tracks []*domain.Track
}

func NewMockLibraryRepoHandler() *MockLibraryRepoHandler {
	return &MockLibraryRepoHandler{}
}

func (m *MockLibraryRepoHandler) FindByID(_ context.Context, id domain.TrackID) (*domain.Track, error) {
	for _, t := range m.Tracks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (m *MockLibraryRepoHandler) FindByPath(_ context.Context, path string) (*domain.Track, error) {
	for _, t := range m.Tracks {
		if t.Path == path {
			return t, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (m *MockLibraryRepoHandler) ListAll(_ context.Context) ([]*domain.Track, error) {
	return m.Tracks, nil
}

// MockPeerRepoHandler implements PeerRepoHandler for testing.
type MockPeerRepoHandler struct {
	Peers []*domain.PeerInfo
}

func NewMockPeerRepoHandler() *MockPeerRepoHandler {
	return &MockPeerRepoHandler{}
}

func (m *MockPeerRepoHandler) ListAll(_ context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {
		for _, p := range m.Peers {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (m *MockPeerRepoHandler) GetPeerInfo(_ context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	for _, p := range m.Peers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, domain.ErrPeerUnavailable
}

// MockPlaylistRepo implements app.PlaylistRepository for testing.
type MockPlaylistRepo struct {
	Playlists map[domain.PlaylistID]*domain.Playlist
}

func NewMockPlaylistRepo() *MockPlaylistRepo {
	return &MockPlaylistRepo{
		Playlists: make(map[domain.PlaylistID]*domain.Playlist),
	}
}

func (m *MockPlaylistRepo) Save(_ context.Context, playlist *domain.Playlist) error {
	m.Playlists[playlist.ID] = playlist
	return nil
}

func (m *MockPlaylistRepo) FindByID(_ context.Context, id domain.PlaylistID) (*domain.Playlist, error) {
	if p, ok := m.Playlists[id]; ok {
		return p, nil
	}
	return nil, domain.ErrPlaylistNotFound
}

func (m *MockPlaylistRepo) ListAll(_ context.Context) iter.Seq2[*domain.Playlist, error] {
	return func(yield func(*domain.Playlist, error) bool) {
		for _, p := range m.Playlists {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (m *MockPlaylistRepo) Delete(_ context.Context, id domain.PlaylistID) error {
	delete(m.Playlists, id)
	return nil
}

// MockQueueHandler implements QueueHandler for testing.
type MockQueueHandler struct {
	tracks   []*domain.Track
	position int
	shuffle  bool
	repeat   domain.RepeatMode
}

func NewMockQueueHandler() *MockQueueHandler {
	return &MockQueueHandler{
		tracks: make([]*domain.Track, 0),
	}
}

func (m *MockQueueHandler) Add(track *domain.Track) {
	m.tracks = append(m.tracks, track)
}

func (m *MockQueueHandler) Insert(pos int, track *domain.Track) {
	if pos < 0 || pos > len(m.tracks) {
		return
	}
	m.tracks = append(m.tracks[:pos], append([]*domain.Track{track}, m.tracks[pos:]...)...)
}

func (m *MockQueueHandler) Remove(pos int) {
	if pos < 0 || pos >= len(m.tracks) {
		return
	}
	m.tracks = append(m.tracks[:pos], m.tracks[pos+1:]...)
}

func (m *MockQueueHandler) Clear() {
	m.tracks = make([]*domain.Track, 0)
	m.position = 0
}

func (m *MockQueueHandler) Length() int {
	return len(m.tracks)
}

func (m *MockQueueHandler) Position() int {
	return m.position
}

func (m *MockQueueHandler) Next() *domain.Track {
	if m.position+1 >= len(m.tracks) {
		return nil
	}
	m.position++
	return m.tracks[m.position]
}

func (m *MockQueueHandler) Previous() *domain.Track {
	if m.position-1 < 0 {
		return nil
	}
	m.position--
	return m.tracks[m.position]
}

func (m *MockQueueHandler) MoveTo(track *domain.Track) bool {
	if track == nil {
		return false
	}
	for i, t := range m.tracks {
		if t.ID == track.ID {
			m.position = i
			return true
		}
	}

	m.tracks = append(m.tracks, track)
	m.position = len(m.tracks) - 1
	return true
}

func (m *MockQueueHandler) Tracks() []*domain.Track {
	out := make([]*domain.Track, len(m.tracks))
	copy(out, m.tracks)
	return out
}

func (m *MockQueueHandler) ToggleShuffle() {
	m.shuffle = !m.shuffle
}

func (m *MockQueueHandler) SetShuffle(shuffle bool) {
	m.shuffle = shuffle
}

func (m *MockQueueHandler) SetRepeat(mode domain.RepeatMode) {
	m.repeat = mode
}

func (m *MockQueueHandler) GetShuffle() bool {
	return m.shuffle
}

func (m *MockQueueHandler) GetRepeat() domain.RepeatMode {
	return m.repeat
}

// MockDBStats implements server.DebugStatsProvider for testing.
type MockDBStats struct {
	lsm, vlog int64
}

func NewMockDBStats(lsm, vlog int64) *MockDBStats {
	return &MockDBStats{lsm: lsm, vlog: vlog}
}

func (m *MockDBStats) Size() (int64, int64) { return m.lsm, m.vlog }
