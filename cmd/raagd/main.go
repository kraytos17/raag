package main

import (
	"github.com/p-society/raag/internal/infra/transcoder"
)

func main() {
	cfg, skipScan := loadConfig()
	tr := transcoder.New(transcoder.Config{FFmpegPath: cfg.Transcoder.FFmpegPath, StreamCodec: cfg.Transcoder.StreamCodec, StreamBitrate: cfg.Transcoder.StreamBitrate})
	database, libraryRepo, p2pNode, p2pEnabled, searchService, metrics := initializeServices(cfg, tr)
	runDaemon(cfg, database, libraryRepo, p2pNode, p2pEnabled, searchService, skipScan, metrics, tr)
}
