package rpc

import (
	"context"
	"net"
	"os"
	"sync"

	"github.com/p-society/raag/app"
	"github.com/p-society/raag/internal/logger"
	pb "github.com/p-society/raag/rpc/proto"
	"google.golang.org/grpc"
)

type GrpcServer struct {
	pb.UnimplementedRaagServer
	socketPath string
	listener   net.Listener
	grpcServer *grpc.Server
	app        *app.App
	shutdownFn func()
	playerCh   chan *pb.PlayerEvent
	peerCh     chan *pb.PeerEvent
	transferCh chan *pb.TransferEvent
	stopOnce   sync.Once
}

func NewGrpcServer(socketPath string, a *app.App, shutdownFn func()) *GrpcServer {
	return &GrpcServer{
		socketPath: socketPath,
		app:        a,
		shutdownFn: shutdownFn,
		playerCh:   make(chan *pb.PlayerEvent, 100),
		peerCh:     make(chan *pb.PeerEvent, 100),
		transferCh: make(chan *pb.TransferEvent, 100),
	}
}

func (s *GrpcServer) Start() error {
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("Failed to remove existing socket: %v", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}

	s.listener = listener
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
		return err
	}

	s.grpcServer = grpc.NewServer()
	pb.RegisterRaagServer(s.grpcServer, s)

	logger.Infof("gRPC server listening socket_path=%s", s.socketPath)
	go s.grpcServer.Serve(listener)
	return nil
}

func (s *GrpcServer) StopServer() {
	s.stopOnce.Do(func() {
		if s.grpcServer != nil {
			s.grpcServer.GracefulStop()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
		_ = os.Remove(s.socketPath)
	})
}

func (s *GrpcServer) StartPlayerWatcher() {
	go func() {
		for {
			select {
			case event := <-s.playerCh:
				// This will be called by the player when state changes
				logger.Debugf("Player event: %v", event)
			}
		}
	}()
}

func (s *GrpcServer) StartPeerWatcher() {
	go func() {
		for {
			select {
			case event := <-s.peerCh:
				logger.Debugf("Peer event: %v", event)
			}
		}
	}()
}

// WatchPlayer implements the streaming RPC
func (s *GrpcServer) WatchPlayer(req *pb.Empty, stream pb.Raag_WatchPlayerServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-s.playerCh:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// WatchPeers implements the streaming RPC
func (s *GrpcServer) WatchPeers(req *pb.Empty, stream pb.Raag_WatchPeersServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-s.peerCh:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// WatchTransfers implements the streaming RPC
func (s *GrpcServer) WatchTransfers(req *pb.Empty, stream pb.Raag_WatchTransfersServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-s.transferCh:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// Simple unary RPC implementations
func (s *GrpcServer) Status(ctx context.Context, req *pb.Empty) (*pb.StatusResponse, error) {
	return &pb.StatusResponse{
		Running:       true,
		PeerCount:     int32(s.app.NetMgr.GetPeerCount()),
		NetworkOnline: s.app.NetMgr.IsOnline(),
		Uptime:        "0s",
		Version:       "1.0.0",
	}, nil
}

func (s *GrpcServer) Shutdown(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	go s.shutdownFn()
	return &pb.Empty{}, nil
}

func (s *GrpcServer) NetworkStatus(ctx context.Context, req *pb.Empty) (*pb.NetworkStatusResponse, error) {
	state, err := s.app.NetMgr.GetNetworkState()
	if err != nil {
		return nil, err
	}
	return &pb.NetworkStatusResponse{
		DhtEnabled: state.DHTEnabled,
		DhtPeers:   int32(state.DHTPeers),
		MdnsPeers:  int32(state.MDNSDiscovered),
		Online:     len(state.ConnectedPeers) > 0,
	}, nil
}

func (s *GrpcServer) PeersList(ctx context.Context, req *pb.Empty) (*pb.PeerListResponse, error) {
	peers := s.app.NetMgr.GetPeers()
	result := &pb.PeerListResponse{
		Peers: make([]*pb.PeerInfo, len(peers)),
	}
	for i, p := range peers {
		addr := ""
		if len(p.Addrs) > 0 {
			addr = p.Addrs[0].String()
		}
		result.Peers[i] = &pb.PeerInfo{
			Id:        p.ID.String(),
			Addr:      addr,
			Connected: true,
		}
	}
	return result, nil
}

func (s *GrpcServer) LibraryList(ctx context.Context, req *pb.Empty) (*pb.LibraryListResponse, error) {
	// This would need to be implemented - for now return empty
	return &pb.LibraryListResponse{Songs: []*pb.LibrarySong{}}, nil
}
