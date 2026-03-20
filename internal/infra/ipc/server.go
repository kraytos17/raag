package ipc

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/p-society/raag/internal/infra/ipc/commands"
	pb "github.com/p-society/raag/proto/gen"
)

type Server struct {
	socketPath string
	listener   net.Listener
	router     *commands.CommandRouter

	mu    sync.Mutex
	conns []net.Conn

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup // Tracks running background loops
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
		_ = s.listener.Close()
		return err
	}

	slog.Info("IPC server listening", "path", s.socketPath)
	s.wg.Add(1)
	go s.acceptLoop(ctx)
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if s.isListenerClosed(err) {
				return
			}
			slog.Warn("accept failed", "error", err)
			continue
		}

		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		_ = conn.Close()
		s.removeConn(conn)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return
		}

		req, err := ReadRequest(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		cmdType := commands.CommandType(strings.ToLower(req.Type.String()))
		cmd := commands.Command{
			Type:    cmdType,
			Payload: req.Payload,
		}

		resp, _ := s.router.Dispatch(ctx, cmd)
		pbResp := convertResponse(resp)

		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}
		if err := WriteResponse(conn, pbResp); err != nil {
			slog.Warn("write response failed", "error", err)
			return
		}
	}
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if i := slices.Index(s.conns, conn); i != -1 {
		s.conns = slices.Delete(s.conns, i, i+1)
	}
}

func (s *Server) Stop(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.done)
	})

	if s.listener != nil {
		_ = s.listener.Close()
	}

	s.mu.Lock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = nil
	s.mu.Unlock()

	s.wg.Wait()
	return os.Remove(s.socketPath)
}

func (s *Server) isListenerClosed(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		return strings.Contains(opErr.Error(), "closed")
	}
	return false
}

func convertResponse(resp commands.Response) *pb.Response {
	return &pb.Response{
		Success: resp.Status == "ok",
		Error:   resp.Error,
		Payload: resp.Data,
	}
}
