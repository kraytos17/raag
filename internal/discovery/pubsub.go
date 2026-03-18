package discovery

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type LibraryAnnounceMessage struct {
	PeerID    string   `json:"peer_id"`
	Action    string   `json:"action"`
	Song      SongInfo `json:"song"`
	CID       string   `json:"cid"`
	Timestamp int64    `json:"timestamp"`
}

type SongInfo struct {
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size"`
	Extension string `json:"extension"`
	CID       string `json:"cid"`
}

type PubSubManager struct {
	host         host.Host
	pubsub       *pubsub.PubSub
	libraryTopic *pubsub.Topic

	OnPeerDiscover func(peer.AddrInfo)
	OnSongAnnounce func(peer.ID, LibraryAnnounceMessage)

	mu sync.RWMutex
}

func NewPubSubManager(h host.Host) (*PubSubManager, error) {
	ps, err := pubsub.NewGossipSub(
		context.Background(),
		h,
		pubsub.WithPeerExchange(true),
		pubsub.WithFloodPublish(true),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictSign),
	)
	if err != nil {
		return nil, err
	}

	psm := &PubSubManager{
		host:   h,
		pubsub: ps,
	}

	logger.Debugf("PubSub manager created")
	return psm, nil
}

func (p *PubSubManager) Start(ctx context.Context) error {
	err := p.pubsub.RegisterTopicValidator(
		constants.LibraryAnnounceTopic,
		p.topicValidator,
		pubsub.WithValidatorTimeout(500),
	)
	if err != nil {
		logger.Warnf("Failed to register topic validator: %v", err)
	}

	libraryTopic, err := p.pubsub.Join(constants.LibraryAnnounceTopic)
	if err != nil {
		return err
	}

	p.libraryTopic = libraryTopic

	subLibrary, err := libraryTopic.Subscribe()
	if err != nil {
		return err
	}

	go p.handleLibraryMessages(ctx, subLibrary)
	logger.Infof("GossipSub initialized: topic=%s", constants.LibraryAnnounceTopic)
	return nil
}

func (p *PubSubManager) topicValidator(ctx context.Context, pid peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	var announce LibraryAnnounceMessage
	if err := json.Unmarshal(msg.Data, &announce); err != nil {
		return pubsub.ValidationReject
	}
	if announce.Action != "add" && announce.Action != "remove" {
		return pubsub.ValidationReject
	}
	if announce.PeerID == "" {
		return pubsub.ValidationReject
	}
	return pubsub.ValidationAccept
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

func (p *PubSubManager) PublishLibraryAnnounce(ctx context.Context, action string, song SongInfo) {
	msg := LibraryAnnounceMessage{
		PeerID:    p.host.ID().String(),
		Action:    action,
		Song:      song,
		CID:       song.CID,
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

func (p *PubSubManager) Stop() error {
	if p.libraryTopic != nil {
		p.libraryTopic.Close()
	}
	return nil
}
