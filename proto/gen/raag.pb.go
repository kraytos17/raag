package gen

type Track struct {
	Id              string   `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Path            string   `protobuf:"bytes,2,opt,name=path,proto3" json:"path,omitempty"`
	Title           string   `protobuf:"bytes,3,opt,name=title,proto3" json:"title,omitempty"`
	Artist          string   `protobuf:"bytes,4,opt,name=artist,proto3" json:"artist,omitempty"`
	AlbumArtist     string   `protobuf:"bytes,5,opt,name=album_artist,json=albumArtist" json:"album_artist,omitempty"`
	Album           string   `protobuf:"bytes,6,opt,name=album,proto3" json:"album,omitempty"`
	TrackNumber     uint32   `protobuf:"varint,7,opt,name=track_number,json=trackNumber" json:"track_number,omitempty"`
	DiscNumber      uint32   `protobuf:"varint,8,opt,name=disc_number,json=discNumber" json:"disc_number,omitempty"`
	Year            uint32   `protobuf:"varint,9,opt,name=year,proto3" json:"year,omitempty"`
	Genres          []string `protobuf:"bytes,10,rep,name=genres,proto3" json:"genres,omitempty"`
	DurationMs      uint64   `protobuf:"varint,11,opt,name=duration_ms,json=durationMs" json:"duration_ms,omitempty"`
	SizeBytes       uint64   `protobuf:"varint,12,opt,name=size_bytes,json=sizeBytes" json:"size_bytes,omitempty"`
	MimeType        string   `protobuf:"bytes,13,opt,name=mime_type,json=mimeType" json:"mime_type,omitempty"`
	Codec           string   `protobuf:"bytes,14,opt,name=codec,proto3" json:"codec,omitempty"`
	Bitrate         uint32   `protobuf:"varint,15,opt,name=bitrate,proto3" json:"bitrate,omitempty"`
	SampleRate      uint32   `protobuf:"varint,16,opt,name=sample_rate,json=sampleRate" json:"sample_rate,omitempty"`
	Channels        uint32   `protobuf:"varint,17,opt,name=channels,proto3" json:"channels,omitempty"`
	Lyrics          string   `protobuf:"bytes,18,opt,name=lyrics,proto3" json:"lyrics,omitempty"`
	CoverArt        []byte   `protobuf:"bytes,19,opt,name=cover_art,json=coverArt" json:"cover_art,omitempty"`
	AddedAt         int64    `protobuf:"varint,20,opt,name=added_at,json=addedAt" json:"added_at,omitempty"`
	ModifiedAt      int64    `protobuf:"varint,21,opt,name=modified_at,json=modifiedAt" json:"modified_at,omitempty"`
	PlayCount       uint64   `protobuf:"varint,22,opt,name=play_count,json=playCount" json:"play_count,omitempty"`
	LastPlayed      int64    `protobuf:"varint,23,opt,name=last_played,json=lastPlayed" json:"last_played,omitempty"`
	ReplayGainTrack float32  `protobuf:"fixed32,24,opt,name=replay_gain_track,json=replayGainTrack" json:"replay_gain_track,omitempty"`
	ReplayGainAlbum float32  `protobuf:"fixed32,25,opt,name=replay_gain_album,json=replayGainAlbum" json:"replay_gain_album,omitempty"`
	ContentHash     string   `protobuf:"bytes,26,opt,name=content_hash,json=contentHash" json:"content_hash,omitempty"`
}
