package wire

import (
	"encoding/binary"
	"errors"
	"io"

	"google.golang.org/protobuf/proto"
)

const MaxMessageSize uint32 = 16 * 1024 * 1024

var ErrMessageTooLarge = errors.New("wire: message exceeds max size")

func WriteMsg(w io.Writer, msg proto.Message) error {
	size := proto.Size(msg)
	if uint32(size) > MaxMessageSize {
		return ErrMessageTooLarge
	}

	buf := make([]byte, 4, 4+size)
	binary.BigEndian.PutUint32(buf[:4], uint32(size))
	buf, err := proto.MarshalOptions{}.MarshalAppend(buf, msg)
	if err != nil {
		return err
	}

	_, err = w.Write(buf)
	return err
}

func ReadMsg(r io.Reader, msg proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	size := binary.BigEndian.Uint32(header[:])
	if size > MaxMessageSize {
		return ErrMessageTooLarge
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return proto.Unmarshal(data, msg)
}

// ReadFrame reads a single length-prefixed message and returns its raw bytes
// without unmarshaling, so the caller can decide how to interpret it.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint32(header[:])
	if size > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
