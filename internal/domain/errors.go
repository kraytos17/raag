package domain

import "errors"

var (
	ErrTrackNotFound          = errors.New("track not found")
	ErrPlaylistNotFound       = errors.New("playlist not found")
	ErrPeerUnavailable        = errors.New("peer unavailable")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrSearchIndexCorrupt     = errors.New("search index corrupted")
	ErrInvalidConfig          = errors.New("invalid configuration")
	ErrStreamTimeout          = errors.New("stream timeout")
	ErrBufferOverflow         = errors.New("buffer overflow")
	ErrUnauthorized           = errors.New("unauthorized access")
	ErrTrackExists            = errors.New("track already exists")
	ErrPlaylistExists         = errors.New("playlist already exists")
	ErrInvalidTrackID         = errors.New("invalid track ID")
	ErrInvalidPlaylistName    = errors.New("invalid playlist name")
	ErrEmptyLibrary           = errors.New("library is empty")
	ErrNoPeersFound           = errors.New("no peers found")
	ErrCodecNotSupported      = errors.New("codec not supported")
	ErrFileNotAccessible      = errors.New("file not accessible")
	ErrInvalidTrack           = errors.New("invalid track")
)
