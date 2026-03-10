package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-society/raag/internal/cli"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/p-society/raag/internal/tui"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		showHelp()
		return
	}

	cfg, err := config.ParseFlags()
	if err != nil {
		log.Fatalf("Error parsing flags: %v", err)
	}

	store, err := storage.New()
	if err != nil {
		log.Printf("Warning: Could not initialize storage: %v", err)
	}

	var appCfg *storage.AppConfig
	if store != nil {
		appCfg, err = store.LoadConfig()
		if err != nil {
			log.Printf("Warning: Could not load config: %v", err)
		}
	}
	if appCfg != nil {
		if cfg.MusicDir == "./music" && appCfg.MusicDir != "" {
			cfg.MusicDir = appCfg.MusicDir
		}
		if !cfg.TUI && appCfg.TUIEnabled {
			cfg.TUI = appCfg.TUIEnabled
		}
		if !cfg.Wifi && appCfg.WifiMode {
			cfg.Wifi = appCfg.WifiMode
		}
		if !cfg.Offline && appCfg.Offline {
			cfg.Offline = appCfg.Offline
		}
	}

	var state *storage.PlayerState
	if store != nil {
		state, err = store.LoadState()
		if err != nil {
			log.Printf("Warning: Could not load state: %v", err)
		}
	}

	lib, err := library.NewLibrary(cfg.MusicDir)
	if err != nil {
		log.Fatalf("Error initializing library: %v", err)
	}

	p, err := player.NewPlayer()
	if err != nil {
		log.Fatalf("Error initializing player: %v", err)
	}
	if state != nil {
		p.SetVolume(float64(state.Volume))
	}

	net, err := network.NewNetwork(cfg, lib, cfg.MusicDir)
	if err != nil {
		log.Fatalf("Error initializing network: %v", err)
	}

	pm := playlist.NewManager()
	if store != nil {
		if err := store.LoadPlaylists(pm); err != nil {
			log.Printf("Warning: Could not load playlists: %v", err)
		}
	}
	if cfg.TUI {
		if err := tui.Start(lib, p, net, pm); err != nil {
			log.Printf("Error in TUI: %v", err)
		}
		saveAll(store, p, pm)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	go func() {
		errChan <- net.Start(ctx)
	}()

	cli := cli.NewCLI(lib, p, net, pm, store)
	go func() {
		if err := cli.Start(); err != nil {
			log.Printf("Error in CLI: %v", err)
			cancel()
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Received termination signal, shutting down...")
	case err := <-errChan:
		log.Printf("Error in network: %v", err)
	}
	saveAll(store, p, pm)
}

func saveAll(store *storage.Storage, p *player.Player, pm *playlist.Manager) {
	if store == nil {
		return
	}

	state := &storage.PlayerState{
		Volume: int(p.GetVolume()),
	}
	if song := p.GetCurrentSong(); song != nil {
		state.LastSong = song.Title
		state.Position = p.GetPosition()
	}
	if err := store.SaveState(state); err != nil {
		log.Printf("Warning: Could not save state: %v", err)
	}
	if err := store.SavePlaylists(pm); err != nil {
		log.Printf("Warning: Could not save playlists: %v", err)
	}
}

func showHelp() {
	store, _ := storage.New()
	lib, _ := library.NewLibrary("./music")
	p, _ := player.NewPlayer()
	net, _ := network.NewNetwork(&config.Config{
		ListenHost: "127.0.0.1",
		MusicDir:   "./music",
	}, lib, "./music")
	pm := playlist.NewManager()
	c := cli.NewCLI(lib, p, net, pm, store)
	c.Start()
}
