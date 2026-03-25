package app

import (
	"context"

	"github.com/p-society/raag/internal/domain"
)

// PlayerComponent wraps an audio engine to implement Component interface
type PlayerComponent struct {
	name   string
	player Player
}

// NewPlayerComponent creates a Component wrapper for the audio player
func NewPlayerComponent(player Player, name string) Component {
	if name == "" {
		name = "player"
	}
	return &PlayerComponent{
		name:   name,
		player: player,
	}
}

func (p *PlayerComponent) Name() string {
	return p.name
}

func (p *PlayerComponent) Start(ctx context.Context) error {
	// Player doesn't need explicit start
	return nil
}

func (p *PlayerComponent) Stop(ctx context.Context) error {
	return p.player.Stop(ctx)
}

type P2PComponent struct {
	name string
	node P2PNode
	bus  domain.EventBus
}

type P2PNode interface {
	Start(ctx context.Context, bus domain.EventBus) error
	Stop(ctx context.Context) error
}

// NewP2PComponent creates a Component wrapper for the P2P node
func NewP2PComponent(node P2PNode, bus domain.EventBus, name string) Component {
	if name == "" {
		name = "p2p"
	}
	return &P2PComponent{
		name: name,
		node: node,
		bus:  bus,
	}
}

func (p *P2PComponent) Name() string {
	return p.name
}

func (p *P2PComponent) Start(ctx context.Context) error {
	return p.node.Start(ctx, p.bus)
}

func (p *P2PComponent) Stop(ctx context.Context) error {
	return p.node.Stop(ctx)
}
