package app

import (
	"cmp"
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
	return &PlayerComponent{
		name:   cmp.Or(name, "player"),
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
	return &P2PComponent{
		name: cmp.Or(name, "p2p"),
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

// FileWatcherComponent wraps a FileWatcher to implement the Component interface.
type FileWatcherComponent struct {
	name string
	fw   *FileWatcher
}

// NewFileWatcherComponent creates a Component wrapper for the library file watcher.
func NewFileWatcherComponent(fw *FileWatcher, name string) Component {
	return &FileWatcherComponent{
		name: cmp.Or(name, "filewatcher"),
		fw:   fw,
	}
}

func (c *FileWatcherComponent) Name() string {
	return c.name
}

func (c *FileWatcherComponent) Start(ctx context.Context) error {
	c.fw.Start(ctx)
	return nil
}

func (c *FileWatcherComponent) Stop(ctx context.Context) error {
	return c.fw.Close()
}
