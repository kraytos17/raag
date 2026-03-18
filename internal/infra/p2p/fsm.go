package p2p

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/looplab/fsm"
	"github.com/p-society/raag/internal/domain"
)

type PeerEvent string

const (
	PeerEventFound          PeerEvent = "found"
	PeerEventConnectSuccess PeerEvent = "connect_success"
	PeerEventConnectFail    PeerEvent = "connect_fail"
	PeerEventScoreDegrade   PeerEvent = "score_degrade"
	PeerEventScoreImprove   PeerEvent = "score_improve"
	PeerEventRetry          PeerEvent = "retry"
	PeerEventDisconnect     PeerEvent = "disconnect"
)

type PeerState string

const (
	PeerStateDiscovered   PeerState = "discovered"
	PeerStateConnecting   PeerState = "connecting"
	PeerStateConnected    PeerState = "connected"
	PeerStateDegraded     PeerState = "degraded"
	PeerStateDisconnected PeerState = "disconnected"
)

type PeerLifecycleFSM struct {
	mu  sync.Mutex
	fsm *fsm.FSM
	bus domain.EventBus
}

func NewPeerLifecycleFSM(bus domain.EventBus) *PeerLifecycleFSM {
	p := &PeerLifecycleFSM{
		bus: bus,
	}

	p.fsm = fsm.NewFSM(
		string(PeerStateDiscovered),
		[]fsm.EventDesc{
			{Name: string(PeerEventFound), Src: []string{string(PeerStateDiscovered)}, Dst: string(PeerStateDiscovered)},
			{Name: string(PeerEventConnectSuccess), Src: []string{string(PeerStateDiscovered), string(PeerStateConnecting)}, Dst: string(PeerStateConnected)},
			{Name: string(PeerEventConnectFail), Src: []string{string(PeerStateDiscovered), string(PeerStateConnecting)}, Dst: string(PeerStateDisconnected)},
			{Name: string(PeerEventScoreDegrade), Src: []string{string(PeerStateConnected)}, Dst: string(PeerStateDegraded)},
			{Name: string(PeerEventScoreImprove), Src: []string{string(PeerStateDegraded)}, Dst: string(PeerStateConnected)},
			{Name: string(PeerEventRetry), Src: []string{string(PeerStateDegraded), string(PeerStateDisconnected)}, Dst: string(PeerStateConnecting)},
			{Name: string(PeerEventDisconnect), Src: []string{string(PeerStateConnected), string(PeerStateDegraded), string(PeerStateConnecting)}, Dst: string(PeerStateDisconnected)},
		},
		map[string]fsm.Callback{
			"enter_state": p.onStateChange,
		},
	)

	return p
}

func (p *PeerLifecycleFSM) onStateChange(ctx context.Context, e *fsm.Event) {
	currentState := PeerState(e.Dst)
	event := PeerEvent(e.Event)

	slog.Debug("peer lifecycle FSM state change",
		"to", currentState,
		"event", event,
	)

	innerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch currentState {
	case PeerStateConnected:
		p.bus.Publish(innerCtx, domain.NewEvent(domain.EventPeerConnected, domain.PeerConnectedPayload{
			PeerID: "",
		}))
	case PeerStateDisconnected:
		p.bus.Publish(innerCtx, domain.NewEvent(domain.EventPeerDisconnected, domain.PeerDisconnectedPayload{
			PeerID: "",
			Reason: "FSM transition",
		}))
	}
}

func (p *PeerLifecycleFSM) CurrentState() PeerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PeerState(p.fsm.Current())
}

func (p *PeerLifecycleFSM) Send(event PeerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.fsm.Event(context.Background(), string(event)); err != nil {
		slog.Warn("peer lifecycle FSM event rejected", "event", event, "current", p.fsm.Current(), "error", err)
		return err
	}
	return nil
}
