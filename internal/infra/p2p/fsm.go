package p2p

import (
	"context"
	"log/slog"
	"sync"

	"github.com/p-society/raag/internal/domain"
)

type peerEvent string

const (
	peerEventFound          = "found"
	peerEventConnectSuccess = "connect_success"
	peerEventConnectFail    = "connect_fail"
	peerEventScoreDegrade   = "score_degrade"
	peerEventScoreImprove   = "score_improve"
	peerEventRetry          = "retry"
	peerEventDisconnect     = "disconnect"
)

type peerState string

const (
	peerStateDiscovered   = "discovered"
	peerStateConnecting   = "connecting"
	peerStateConnected    = "connected"
	peerStateDegraded     = "degraded"
	peerStateDisconnected = "disconnected"
)

type peerLifecycleFSM struct {
	mu    sync.Mutex
	state peerState
	bus   domain.EventBus
}

func newPeerLifecycleFSM(bus domain.EventBus) *peerLifecycleFSM {
	return &peerLifecycleFSM{
		state: peerStateDiscovered,
		bus:   bus,
	}
}

var peerTransitions = map[peerState]map[peerEvent]peerState{
	peerStateDiscovered:   {peerEventFound: peerStateDiscovered, peerEventConnectSuccess: peerStateConnected, peerEventConnectFail: peerStateDisconnected},
	peerStateConnecting:   {peerEventConnectSuccess: peerStateConnected, peerEventConnectFail: peerStateDisconnected},
	peerStateConnected:    {peerEventScoreDegrade: peerStateDegraded, peerEventDisconnect: peerStateDisconnected},
	peerStateDegraded:     {peerEventScoreImprove: peerStateConnected, peerEventRetry: peerStateConnecting, peerEventDisconnect: peerStateDisconnected},
	peerStateDisconnected: {peerEventRetry: peerStateConnecting},
}

func (p *peerLifecycleFSM) Send(event peerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	prev := p.state
	next, ok := peerTransitions[prev][event]
	if !ok {
		slog.Debug("peer FSM: no transition", "event", event, "from", prev)
		return nil
	}

	p.state = next
	slog.Debug("peer FSM transition", "from", prev, "to", next, "event", event)
	switch next {
	case peerStateConnected:
		p.bus.Publish(context.Background(), domain.NewEvent(domain.EventPeerConnected, domain.PeerConnectedPayload{}))
	case peerStateDisconnected:
		p.bus.Publish(context.Background(), domain.NewEvent(domain.EventPeerDisconnected, domain.PeerDisconnectedPayload{Reason: "FSM transition"}))
	}
	return nil
}
