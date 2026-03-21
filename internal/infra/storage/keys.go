package db

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/p-society/raag/internal/domain"
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
	PrefixPeer         = "peer:"
	PrefixPeerLib      = "peer:lib:"
	PrefixPeerScore    = "peer:score:"
	PrefixTrackCover   = "trk:cover:"

	KeyIdentity      = "cfg:identity"
	KeySchemaVersion = "cfg:schema"
)

func TrackKey(id domain.TrackID) []byte {
	return []byte(PrefixTrackData + string(id))
}

func PathKey(path string) []byte {
	return []byte(PrefixTrackPath + pathHash(path))
}

func PathStrKey(path string) []byte {
	return []byte(PrefixTrackPathStr + path)
}

func ArtistIndexKey(artist string, id domain.TrackID) []byte {
	return []byte(PrefixTrackArtist + normalizeKey(artist) + "\x00" + string(id))
}

func AlbumIndexKey(album string, id domain.TrackID) []byte {
	return []byte(PrefixTrackAlbum + normalizeKey(album) + "\x00" + string(id))
}

func TermIndexKey(term string, id domain.TrackID) []byte {
	return []byte(PrefixIdxTerm + term + ":" + string(id))
}

func TrigramIndexKey(trigram string, id domain.TrackID) []byte {
	return []byte(PrefixIdxTrigram + trigram + ":" + string(id))
}

func FileStatKey(path string) []byte {
	return []byte(PrefixFileStat + pathHash(path))
}

func PlaylistKey(id domain.PlaylistID) []byte {
	return []byte(PrefixPlaylist + string(id))
}

func PeerKey(id domain.PeerID) []byte {
	return []byte(PrefixPeer + string(id))
}

func PeerLibraryKey(id domain.PeerID) []byte {
	return []byte(PrefixPeerLib + string(id))
}

func PeerScoreKey(id domain.PeerID) []byte {
	return []byte(PrefixPeerScore + string(id))
}

func CoverArtKey(id domain.TrackID) []byte {
	return []byte(PrefixTrackCover + string(id))
}

func normalizeKey(s string) string {
	return domain.NormalizeKey(s)
}

func pathHash(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])
}
