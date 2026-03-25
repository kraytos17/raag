package domain

import (
	"time"
)

const (
	SampleSize = 64 * 1024
)

var AudioExtensions = []string{
	".mp3", ".flac", ".ogg", ".wav", ".m4a",
	".aac", ".opus", ".wma",
}

type PlaybackEvent string

const (
	EventPlay        PlaybackEvent = "play"
	EventBufferReady PlaybackEvent = "buffer_ready"
	EventBufferFail  PlaybackEvent = "buffer_fail"
	EventPause       PlaybackEvent = "pause"
	EventResume      PlaybackEvent = "resume"
	EventEOF         PlaybackEvent = "eof"
	EventSeek        PlaybackEvent = "seek"
	EventSeekDone    PlaybackEvent = "seek_done"
	EventUnderrun    PlaybackEvent = "underrun"
	EventStop        PlaybackEvent = "stop"
	EventRetry       PlaybackEvent = "retry"
)

type ScanPhase string

const (
	ScanPhaseWalking ScanPhase = "walking"
	ScanPhaseParsing ScanPhase = "parsing"
)

type RepeatMode string

const (
	RepeatModeNone RepeatMode = "none"
	RepeatModeOne  RepeatMode = "one"
	RepeatModeAll  RepeatMode = "all"
)

type CBState string

const (
	CBStateClosed   CBState = "closed"
	CBStateOpen     CBState = "open"
	CBStateHalfOpen CBState = "half_open"
)

const (
	CBDefaultThreshold          = 5
	CBDefaultCooldown           = 30 * time.Second
	CBHalfOpenSuccessesRequired = 3
)

type DuplicateHandler int

const (
	DuplicateSkip DuplicateHandler = iota
	DuplicateWarn
	DuplicateKeep
)

const (
	RankMatchWeight       = 0.6
	RankPlayWeight        = 0.25
	RankRecencyWeight     = 0.15
	BoostTitle            = 3.0
	BoostArtist           = 2.0
	BoostAlbum            = 1.5
	DefaultSearchLimit    = 20
	DefaultFuzzyThreshold = 3
)

const (
	PrefixTrackData    = "trk:data:"
	PrefixTrackPath    = "trk:path:"
	PrefixTrackPathStr = "trk:pathstr:"
	PrefixTrackArtist  = "trk:artist:"
	PrefixTrackAlbum   = "trk:album:"
	PrefixIdxTerm      = "idx:term:"
	PrefixIdxTrigram   = "idx:trigram:"
	PrefixFileStat     = "meta:filestat:"
	PrefixPlaylist     = "pl:"
	PrefixPeerInfo     = "peer:info:"
	PrefixPeerLib      = "peer:lib:"
	PrefixPeerScore    = "peer:score:"
	PrefixTrackCover   = "trk:cover:"
	PrefixContentHash  = "hash:content:"
	KeyIdentity        = "cfg:identity"
)

const (
	DefaultCacheSize = 10000
	EventChannelSize = 64
)

const (
	MaxStreamsPerPeer = 3
	StreamIdleTimeout = 5 * time.Minute
	MaxPoolSize       = 100
)

const (
	IPCConnectTimeout       = 3 * time.Second
	IPCReadTimeout          = 30 * time.Second
	IPCWriteTimeout         = 5 * time.Second
	IPCProtocolVersion      = 1
	IPCKeepaliveInterval    = 25 * time.Second // must be less than IPCReadTimeout
	IPCReconnectBaseDelay   = 100 * time.Millisecond
	IPCReconnectMaxDelay    = 5 * time.Second
	IPCReconnectMaxAttempts = 10
)

var SupportedBitrates = []int32{128, 192, 256, 320}

type PlayerState string

const (
	PlayerStateIdle      PlayerState = "idle"
	PlayerStatePlaying   PlayerState = "playing"
	PlayerStatePaused    PlayerState = "paused"
	PlayerStateBuffering PlayerState = "buffering"
	PlayerStateError     PlayerState = "error"
	PlayerStateSeeking   PlayerState = "seeking"
)
