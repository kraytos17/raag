package ipc

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/p-society/raag/internal/infra/ipc/commands"
	pb "github.com/p-society/raag/proto/gen"
)

type Server struct {
	socketPath string
	listener   net.Listener
	router     *commands.CommandRouter
	mu         sync.Mutex
	conns      []net.Conn
	done       chan struct{}
}

func NewServer(socketPath string, router *commands.CommandRouter) *Server {
	return &Server{
		socketPath: socketPath,
		router:     router,
		done:       make(chan struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	lis, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}

	s.listener = lis
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		return err
	}

	slog.Info("IPC server listening", "path", s.socketPath)
	go s.acceptLoop(ctx)
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	defer close(s.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			slog.Warn("accept failed", "error", err)
			continue
		}

		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		conn.Close()
		s.removeConn(conn)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		req, err := ReadRequest(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		cmdType := commands.CommandType(req.Type.String())
		cmd := commands.Command{
			Type:    cmdType,
			Payload: req.Payload,
		}

		resp, _ := s.router.Dispatch(ctx, cmd)
		pbResp := convertResponse(resp)
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := WriteResponse(conn, pbResp); err != nil {
			slog.Warn("write response failed", "error", err)
			return
		}
	}
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.conns {
		if c == conn {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return
		}
	}
}

func (s *Server) Stop(ctx context.Context) error {
	if s.listener != nil {
		s.listener.Close()
	}

	<-s.done
	s.mu.Lock()
	for _, conn := range s.conns {
		conn.Close()
	}

	s.conns = nil
	s.mu.Unlock()
	return os.Remove(s.socketPath)
}

func convertResponse(resp commands.Response) *pb.Response {
	return &pb.Response{
		Success: resp.Status == "ok",
		Error:   resp.Error,
		Payload: resp.Data,
	}
}
