package network

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
)

type transferMetadata struct {
	Version   string `json:"version"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Filename  string `json:"filename"`
	Extension string `json:"extension"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

func buildTransferMetadata(song metadata.Song, fileSize int64, digest string) transferMetadata {
	ext := strings.ToLower(filepath.Ext(song.Path))
	return transferMetadata{
		Version:   constants.ShareProtocolVersion,
		Title:     song.Title,
		Artist:    song.Artist,
		Album:     song.Album,
		Filename:  filepath.Base(song.Path),
		Extension: ext,
		SizeBytes: fileSize,
		SHA256:    digest,
	}
}

func writeTransferMetadata(stream libp2pnetwork.Stream, meta transferMetadata) error {
	metadataBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal transfer metadata: %w", err)
	}
	if len(metadataBytes) > constants.TransferMaxMetadataSize {
		return fmt.Errorf("transfer metadata too large: %d", len(metadataBytes))
	}
	if err := stream.SetWriteDeadline(time.Now().Add(constants.TransferIdleTimeout)); err != nil {
		logger.Debugf("failed to set write deadline error=%v", err)
	}

	var lengthPrefix [4]byte
	binary.BigEndian.PutUint32(lengthPrefix[:], uint32(len(metadataBytes)))
	if _, err := stream.Write(lengthPrefix[:]); err != nil {
		return fmt.Errorf("write metadata length: %w", err)
	}
	if _, err := stream.Write(metadataBytes); err != nil {
		return fmt.Errorf("write metadata payload: %w", err)
	}
	return nil
}

func readTransferMetadata(stream libp2pnetwork.Stream) (transferMetadata, error) {
	if err := stream.SetReadDeadline(time.Now().Add(constants.TransferIdleTimeout)); err != nil {
		logger.Debugf("failed to set read deadline error=%v", err)
	}

	var lengthPrefix [4]byte
	if _, err := io.ReadFull(stream, lengthPrefix[:]); err != nil {
		return transferMetadata{}, fmt.Errorf("read metadata length: %w", err)
	}

	metadataLen := binary.BigEndian.Uint32(lengthPrefix[:])
	if metadataLen == 0 {
		return transferMetadata{}, fmt.Errorf("empty metadata frame")
	}
	if metadataLen > constants.TransferMaxMetadataSize {
		return transferMetadata{}, fmt.Errorf("metadata frame too large: %d", metadataLen)
	}

	metadataBytes := make([]byte, metadataLen)
	if _, err := io.ReadFull(stream, metadataBytes); err != nil {
		return transferMetadata{}, fmt.Errorf("read metadata payload: %w", err)
	}

	var meta transferMetadata
	if err := json.Unmarshal(metadataBytes, &meta); err != nil {
		return transferMetadata{}, fmt.Errorf("decode metadata payload: %w", err)
	}
	if meta.SizeBytes < 0 {
		return transferMetadata{}, fmt.Errorf("invalid file size: %d", meta.SizeBytes)
	}
	if meta.SizeBytes > constants.TransferMaxFileSize {
		return transferMetadata{}, fmt.Errorf("file too large: %d", meta.SizeBytes)
	}
	if meta.Extension == "" {
		meta.Extension = strings.ToLower(filepath.Ext(meta.Filename))
	}
	return meta, nil
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open file for hashing: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash file: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func sanitizeTransferName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if name == "" {
		return "received"
	}
	return name
}
