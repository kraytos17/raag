package main

import (
	"fmt"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type appRuntime struct {
	v      *viper.Viper
	cfg    *config.Config
	store  *storage.Storage
	lib    *library.Library
	player *player.Player
	netMgr *network.NetworkManager
	pm     *playlist.Manager
}

func loadConfigOnly(cmd *cobra.Command) (*viper.Viper, *config.Config, error) {
	v, err := config.InitViper(cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize config: %w", err)
	}

	cfg, err := config.LoadConfig(v)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	logger.SetLevel(cfg.LogLevel)
	return v, cfg, nil
}

func bootstrapRuntime(cmd *cobra.Command) (*appRuntime, error) {
	v, cfg, err := loadConfigOnly(cmd)
	if err != nil {
		return nil, err
	}

	store, err := storage.New()
	if err != nil {
		logger.Warnf("Could not initialize storage error=%v", err)
	}

	lib, err := library.NewLibrary(cfg.MusicDir)
	if err != nil {
		return nil, err
	}

	p, err := player.NewPlayer()
	if err != nil {
		return nil, err
	}
	if err := p.SetVolume(float64(cfg.Volume)); err != nil {
		return nil, err
	}

	netMgr, err := network.NewNetwork(cfg, v, lib, cfg.MusicDir)
	if err != nil {
		return nil, err
	}

	pm := playlist.NewManager()
	if store != nil {
		if err := store.LoadPlaylists(pm); err != nil {
			logger.Warnf("Could not load playlists error=%v", err)
		}
	}

	return &appRuntime{
		v:      v,
		cfg:    cfg,
		store:  store,
		lib:    lib,
		player: p,
		netMgr: netMgr,
		pm:     pm,
	}, nil
}

func applyRuntime(rt *appRuntime) {
	v = rt.v
	cfg = rt.cfg
	store = rt.store
	lib = rt.lib
	p = rt.player
	netMgr = rt.netMgr
	pm = rt.pm
}

func shouldStartTUI(cmd *cobra.Command, noTUI bool) bool {
	if noTUI {
		return false
	}

	tuiFlag := cmd.Flags().Lookup("tui")
	rootTuiFlag := cmd.Root().Flags().Lookup("tui")
	if tuiFlag != nil && tuiFlag.Value.String() == "true" {
		return true
	}
	if rootTuiFlag != nil && rootTuiFlag.Value.String() == "true" {
		return true
	}
	return false
}
