package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/spf13/viper"
)

type App struct {
	V      *viper.Viper
	Cfg    *config.Config
	Lib    *library.Library
	Player *player.Player
	NetMgr *network.NetworkManager
	PM     *playlist.Manager
	Store  *storage.Store

	StartTime time.Time
	cancel    context.CancelFunc
}

func NewApp(v *viper.Viper, cfg *config.Config) (*App, error) {
	store, err := storage.NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}

	ctx := context.Background()

	lib, err := library.NewLibrary(cfg.MusicDir)
	if err != nil {
		return nil, fmt.Errorf("init library: %w", err)
	}

	p, err := player.NewPlayer()
	if err != nil {
		return nil, fmt.Errorf("init player: %w", err)
	}
	if err := p.SetVolume(float64(cfg.Volume)); err != nil {
		return nil, fmt.Errorf("set volume: %w", err)
	}
	p.SetNextCallback(func() error {
		return p.Next()
	})

	netMgr, err := network.NewNetwork(cfg, v, lib, cfg.MusicDir, store)
	if err != nil {
		return nil, fmt.Errorf("init network: %w", err)
	}

	pm := playlist.NewManager()
	if err := storage.LoadPlaylists(ctx, store, pm); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load playlists: %v\n", err)
	}

	state, err := storage.LoadState(ctx, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load state: %v\n", err)
	} else if state != nil && state.Volume > 0 {
		_ = p.SetVolume(float64(state.Volume))
	}

	startTime := time.Now()
	app := &App{
		V:         v,
		Cfg:       cfg,
		Lib:       lib,
		Player:    p,
		NetMgr:    netMgr,
		PM:        pm,
		Store:     store,
		StartTime: startTime,
	}
	return app, nil
}

func (a *App) StartNetwork(ctx context.Context) {
	go func() {
		if err := a.NetMgr.Start(ctx); err != nil {
			if err != context.Canceled {
				fmt.Fprintf(os.Stderr, "network error: %v\n", err)
			}
		}
	}()
}

func (a *App) SaveState(ctx context.Context) {
	state := &storage.PlayerState{
		Volume: int(a.Player.GetVolume()),
	}
	if song := a.Player.GetCurrentSong(); song != nil {
		state.LastSong = song.Title
		state.Position = a.Player.GetPosition()
	}
	if err := storage.SaveState(ctx, a.Store, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save player state: %v\n", err)
	}
	if err := storage.SavePlaylists(ctx, a.Store, a.PM); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save playlists: %v\n", err)
	}
	if err := config.SaveConfig(a.V, a.Cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save config: %v\n", err)
	}
}

func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.NetMgr != nil {
		if err := a.NetMgr.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close network manager: %v\n", err)
		}
	}
	if a.Store != nil {
		if err := a.Store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close store: %v\n", err)
		}
	}
	a.SaveState(context.Background())
}
