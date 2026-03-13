package network

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type PingMessageType string

const (
	PingTypeHello  PingMessageType = "hello"
	PingTypePong   PingMessageType = "pong"
	PingTypeJoined PingMessageType = "joined"
	PingTypeLeft   PingMessageType = "left"
)

type PingMessage struct {
	Version   string          `json:"version"`
	Type      PingMessageType `json:"type"`
	PeerID    string          `json:"peer_id"`
	Timestamp time.Time       `json:"timestamp"`
	Message   string          `json:"message"`
}

func BuildPingMessage(peerID string, msgType PingMessageType, customMsg string) PingMessage {
	msg := "hello"
	if customMsg != "" {
		msg = customMsg
	}
	if msgType == PingTypePong {
		msg = "pong"
	}
	return PingMessage{
		Version:   constants.PingProtocolVersion,
		Type:      msgType,
		PeerID:    peerID,
		Timestamp: time.Now(),
		Message:   msg,
	}
}

func WritePingMessage(stream libp2pnetwork.Stream, msg PingMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ping message: %w", err)
	}
	if err := stream.SetWriteDeadline(time.Now().Add(constants.TransferIdleTimeout)); err != nil {
		logger.Debugf("failed to set write deadline error=%v", err)
	}

	var lengthPrefix [4]byte
	binary.BigEndian.PutUint32(lengthPrefix[:], uint32(len(msgBytes)))
	if _, err := stream.Write(lengthPrefix[:]); err != nil {
		return fmt.Errorf("write ping length: %w", err)
	}
	if _, err := stream.Write(msgBytes); err != nil {
		return fmt.Errorf("write ping payload: %w", err)
	}
	return nil
}

func ReadPingMessage(stream libp2pnetwork.Stream) (PingMessage, error) {
	if err := stream.SetReadDeadline(time.Now().Add(constants.TransferIdleTimeout)); err != nil {
		logger.Debugf("failed to set read deadline error=%v", err)
	}

	var lengthPrefix [4]byte
	if _, err := io.ReadFull(stream, lengthPrefix[:]); err != nil {
		return PingMessage{}, fmt.Errorf("read ping length: %w", err)
	}

	msgLen := binary.BigEndian.Uint32(lengthPrefix[:])
	if msgLen == 0 || msgLen > 1024 {
		return PingMessage{}, fmt.Errorf("invalid ping message length: %d", msgLen)
	}

	msgBytes := make([]byte, msgLen)
	if _, err := io.ReadFull(stream, msgBytes); err != nil {
		return PingMessage{}, fmt.Errorf("read ping payload: %w", err)
	}

	var msg PingMessage
	if err := json.Unmarshal(msgBytes, &msg); err != nil {
		return PingMessage{}, fmt.Errorf("unmarshal ping message: %w", err)
	}
	return msg, nil
}
