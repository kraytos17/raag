package discovery

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type PresenceMessage struct {
	PeerID    string   `json:"peer_id"`
	Addrs     []string `json:"addrs"`
	Timestamp int64    `json:"timestamp"`
	LibHash   string   `json:"library_hash"`
}

type LibraryAnnounceMessage struct {
	PeerID    string   `json:"peer_id"`
	Action    string   `json:"action"`
	Song      SongInfo `json:"song"`
	Timestamp int64    `json:"timestamp"`
}

type SongInfo struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
}

type PubSubManager struct {
	host          host.Host
	pubsub        *pubsub.PubSub
	presenceTopic *pubsub.Topic
	libraryTopic  *pubsub.Topic

	OnPeerDiscover func(peer.AddrInfo)
	OnSongAnnounce func(peer.ID, LibraryAnnounceMessage)

	mu                sync.RWMutex
	ourAddrs          []string
	libraryHash       string
	heartbeatInterval time.Duration
}

func NewPubSubManager(h host.Host) (*PubSubManager, error) {
	ps, err := pubsub.NewGossipSub(context.Background(), h)
	if err != nil {
		return nil, err
	}
	psm := &PubSubManager{
		host:              h,
		pubsub:            ps,
		heartbeatInterval: constants.PubSubHeartbeatInterval,
		ourAddrs:          make([]string, 0),
	}

	logger.Debugf("PubSub manager created")
	return psm, nil
}

func (p *PubSubManager) Start(ctx context.Context) error {
	presenceTopic, err := p.pubsub.Join(constants.PresenceTopic)
	if err != nil {
		return err
	}

	p.presenceTopic = presenceTopic
	libraryTopic, err := p.pubsub.Join(constants.LibraryAnnounceTopic)
	if err != nil {
		return err
	}

	p.libraryTopic = libraryTopic
	subPresence, err := presenceTopic.Subscribe()
	if err != nil {
		return err
	}

	subLibrary, err := libraryTopic.Subscribe()
	if err != nil {
		return err
	}

	go p.handlePresenceMessages(ctx, subPresence)
	go p.handleLibraryMessages(ctx, subLibrary)
	go p.runPresenceHeartbeat(ctx)
	logger.Infof("GossipSub initialized: topics=%s,%s", constants.PresenceTopic, constants.LibraryAnnounceTopic)
	return nil
}

func (p *PubSubManager) handlePresenceMessages(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Debugf("Presence subscription error: %v", err)
				continue
			}
			p.processPresenceMessage(msg)
		}
	}
}

func (p *PubSubManager) processPresenceMessage(msg *pubsub.Message) {
	var presence PresenceMessage
	if err := json.Unmarshal(msg.Data, &presence); err != nil {
		logger.Debugf("Failed to unmarshal presence message: %v", err)
		return
	}
	if presence.PeerID == p.host.ID().String() {
		return
	}

	pid, err := peer.Decode(presence.PeerID)
	if err != nil {
		logger.Debugf("Failed to decode peer ID: %v", err)
		return
	}

	addrs := make([]multiaddr.Multiaddr, 0, len(presence.Addrs))
	for _, addrStr := range presence.Addrs {
		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}
		addrs = append(addrs, ma)
	}

	peerInfo := peer.AddrInfo{ID: pid, Addrs: addrs}
	if p.OnPeerDiscover != nil {
		p.OnPeerDiscover(peerInfo)
	}
}

func (p *PubSubManager) handleLibraryMessages(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Debugf("Library subscription error: %v", err)
				continue
			}
			p.processLibraryMessage(msg)
		}
	}
}

func (p *PubSubManager) processLibraryMessage(msg *pubsub.Message) {
	var announce LibraryAnnounceMessage
	if err := json.Unmarshal(msg.Data, &announce); err != nil {
		logger.Debugf("Failed to unmarshal library message: %v", err)
		return
	}
	if announce.PeerID == p.host.ID().String() {
		return
	}

	pid, err := peer.Decode(announce.PeerID)
	if err != nil {
		logger.Debugf("Failed to decode peer ID from library message: %v", err)
		return
	}
	if p.OnSongAnnounce != nil {
		p.OnSongAnnounce(pid, announce)
	}
}

func (p *PubSubManager) runPresenceHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(p.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publishPresence(ctx)
		}
	}
}

func (p *PubSubManager) publishPresence(ctx context.Context) {
	p.mu.RLock()
	addrs := make([]string, len(p.ourAddrs))
	copy(addrs, p.ourAddrs)
	libHash := p.libraryHash
	p.mu.RUnlock()

	msg := PresenceMessage{
		PeerID:    p.host.ID().String(),
		Addrs:     addrs,
		Timestamp: time.Now().Unix(),
		LibHash:   libHash,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Warnf("Failed to marshal presence message: %v", err)
		return
	}
	if err := p.presenceTopic.Publish(ctx, data); err != nil {
		logger.Debugf("Failed to publish presence: %v", err)
	}
}

func (p *PubSubManager) PublishLibraryAnnounce(ctx context.Context, action string, song SongInfo) {
	msg := LibraryAnnounceMessage{
		PeerID:    p.host.ID().String(),
		Action:    action,
		Song:      song,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Warnf("Failed to marshal library announcement: %v", err)
		return
	}
	if err := p.libraryTopic.Publish(ctx, data); err != nil {
		logger.Warnf("Failed to publish library announcement: %v", err)
	}
}

func (p *PubSubManager) UpdateAddrs(addrs []multiaddr.Multiaddr) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ourAddrs = make([]string, len(addrs))
	for i, addr := range addrs {
		p.ourAddrs[i] = addr.String()
	}
}

func (p *PubSubManager) UpdateLibraryHash(hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.libraryHash = hash
}

func (p *PubSubManager) Stop() error {
	if p.presenceTopic != nil {
		p.presenceTopic.Close()
	}
	if p.libraryTopic != nil {
		p.libraryTopic.Close()
	}
	return nil
}
