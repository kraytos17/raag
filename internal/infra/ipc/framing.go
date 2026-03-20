package ipc

import (
	"encoding/binary"
	"errors"
	"io"

	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

const MaxMessageSize = 16 * 1024 * 1024

var ErrMessageTooLarge = errors.New("ipc: message exceeds max size")

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

func WriteRequest(w io.Writer, req *pb.Request) error {
	return WriteMsg(w, req)
}

func ReadRequest(r io.Reader) (*pb.Request, error) {
	var req pb.Request
	if err := ReadMsg(r, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func WriteResponse(w io.Writer, resp *pb.Response) error {
	return WriteMsg(w, resp)
}

func ReadResponse(r io.Reader) (*pb.Response, error) {
	var resp pb.Response
	if err := ReadMsg(r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
