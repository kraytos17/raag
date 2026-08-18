package domain

import (
	"context"
	"time"
)

type EventType string

const (
	EventTrackStarted      EventType = "track.started"
	EventTrackFinished     EventType = "track.finished"
	EventTrackPaused       EventType = "track.paused"
	EventTrackResumed      EventType = "track.resumed"
	EventTrackSeeked       EventType = "track.seeked"
	EventPeerConnected     EventType = "peer.connected"
	EventPeerDisconnected  EventType = "peer.disconnected"
	EventPeerScoreUpdated  EventType = "peer.score_updated"
	EventScanStarted       EventType = "scan.started"
	EventScanProgress      EventType = "scan.progress"
	EventScanComplete      EventType = "scan.complete"
	EventVolumeChanged     EventType = "volume.changed"
	EventQueueUpdated      EventType = "queue.updated"
	EventPlaybackBuffering EventType = "playback.buffering"
	EventPlaybackReady     EventType = "playback.buffer_ready"
)

type EventHandler func(Event)

type Unsubscribe func()

type Event struct {
	Type    EventType
	Payload any
	At      time.Time
}

func NewEvent(eventType EventType, payload any) Event {
	return Event{
		Type:    eventType,
		Payload: payload,
		At:      time.Now(),
	}
}

type EventBus interface {
	Publish(ctx context.Context, event Event)
	Subscribe(eventType EventType, handler EventHandler) Unsubscribe
	Close()
}

type TrackStartedPayload struct {
	TrackID  TrackID
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
}

type TrackFinishedPayload struct {
	TrackID   TrackID
	PlayCount uint64
	Duration  time.Duration
	Completed bool
}

type TrackPausedPayload struct {
	TrackID  TrackID
	Position time.Duration
}

type TrackResumedPayload struct {
	TrackID  TrackID
	Position time.Duration
}

type TrackSeekedPayload struct {
	TrackID TrackID
	From    time.Duration
	To      time.Duration
}

type PeerConnectedPayload struct {
	PeerID       PeerID
	Addr         string
	Capabilities *PeerCapabilities
}

type PeerDisconnectedPayload struct {
	PeerID PeerID
	Reason string
}

type PeerScoreUpdatedPayload struct {
	PeerID PeerID
	Score  float64
}

type ScanStartedPayload struct {
	Paths     []string
	StartTime time.Time
}

type ScanProgressPayload struct {
	JobID       string
	Scanned     int
	Total       int
	CurrentFile string
	Phase       string
}

type ScanCompletePayload struct {
	Scanned  int
	Added    int
	Removed  int
	Duration time.Duration
	Errors   []string
}

type VolumeChangedPayload struct {
	Volume   int
	Previous int
}

// EqualizerSettings is the user-facing three-band EQ state.
type EqualizerSettings struct {
	Enabled bool
	Bass    float64 // dB, [-12, 12]
	Mid     float64 // dB, [-12, 12]
	Treble  float64 // dB, [-12, 12]
}

type QueueUpdatedPayload struct {
	Action   QueueAction
	TrackID  TrackID
	Position int
}

// PlaybackBufferingPayload carries the buffer fill level at underrun time.
type PlaybackBufferingPayload struct {
	FillLevel float64
}

// PlaybackReadyPayload carries the buffer fill level when playback resumes.
type PlaybackReadyPayload struct {
	FillLevel float64
}

type QueueAction string

const (
	QueueActionAdd    QueueAction = "add"
	QueueActionRemove QueueAction = "remove"
	QueueActionMove   QueueAction = "move"
	QueueActionClear  QueueAction = "clear"
)
