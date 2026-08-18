package db

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/p-society/raag/internal/domain"
)

func TrackKey(id domain.TrackID) []byte {
	return []byte(domain.PrefixTrackData + string(id))
}

func PathKey(path string) []byte {
	return []byte(domain.PrefixTrackPath + path)
}

func ArtistIndexKey(artist string, id domain.TrackID) []byte {
	return []byte(domain.PrefixTrackArtist + domain.NormalizeKey(artist) + "\x00" + string(id))
}

func AlbumIndexKey(album string, id domain.TrackID) []byte {
	return []byte(domain.PrefixTrackAlbum + domain.NormalizeKey(album) + "\x00" + string(id))
}

func TermIndexKey(term string, id domain.TrackID) []byte {
	return []byte(domain.PrefixIdxTerm + term + ":" + string(id))
}

func TrigramIndexKey(trigram string, id domain.TrackID) []byte {
	return []byte(domain.PrefixIdxTrigram + trigram + ":" + string(id))
}

func FileStatKey(path string) []byte {
	return []byte(domain.PrefixFileStat + pathHash(path))
}

func PlaylistKey(id domain.PlaylistID) []byte {
	return []byte(domain.PrefixPlaylist + string(id))
}

func PeerKey(id domain.PeerID) []byte {
	return []byte(domain.PrefixPeerInfo + string(id))
}

func PeerLibraryKey(id domain.PeerID) []byte {
	return []byte(domain.PrefixPeerLib + string(id))
}

func PeerScoreKey(id domain.PeerID) []byte {
	return []byte(domain.PrefixPeerScore + string(id))
}

func SettingsKey(name string) []byte {
	return []byte(domain.PrefixSettings + name)
}

func CoverArtKey(id domain.TrackID) []byte {
	return []byte(domain.PrefixTrackCover + string(id))
}

func ContentHashKey(hash string) []byte {
	return []byte(domain.PrefixContentHash + hash)
}

func IdentityKey() []byte {
	return []byte(domain.KeyIdentity)
}

func pathHash(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])
}
