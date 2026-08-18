package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
	"github.com/p-society/raag/internal/infra/events"
)

// applyConfiguredVolume pushes the configured volume into the engine so it is
// applied on the first playback (the controller's field alone does not touch
// the engine).
func applyConfiguredVolume(playback *app.PlaybackController, volume int) {
	if err := playback.SetVolume(context.Background(), volume); err != nil {
		slog.Warn("failed to apply configured volume", "error", err)
	}
}

// warnUnsupportedOutputDevice acknowledges a non-default output_device config.
// The beep/oto backend cannot select an output device, so only the default
// ALSA/OS device is supported.
func warnUnsupportedOutputDevice(device string) {
	if device != "" && device != "default" {
		slog.Warn("playback.output_device is not supported by the current audio backend; using the default device", "device", device)
	}
}

// dspFromConfig maps the playback config to the engine's DSP processing.
func dspFromConfig(cfg config.PlaybackConfig) audio.DSPConfig {
	return audio.DSPConfig{
		Equalizer: audio.EqualizerConfig{
			Enabled: cfg.Equalizer.Enabled,
			Bass:    cfg.Equalizer.Bass,
			Mid:     cfg.Equalizer.Mid,
			Treble:  cfg.Equalizer.Treble,
		},
		Normalize: audio.NormalizeConfig{
			Enabled:  cfg.Normalize.Enabled,
			TargetDB: cfg.Normalize.TargetDB,
		},
		Crossfade: time.Duration(cfg.CrossfadeMs) * time.Millisecond,
	}
}

// loadPersistedVolume returns the restart-surviving volume from the DB, falling
// back to the config default when nothing is persisted.
func loadPersistedVolume(cfg *config.Config, settings app.SettingsRepository) int {
	volume := cfg.Playback.Volume
	if persisted, ok, err := settings.GetVolume(context.Background()); err == nil && ok {
		volume = persisted
	}
	return volume
}

// persistVolumeChanges writes every volume change to the DB so the setting
// survives restarts. Failures are logged, never fatal.
func persistVolumeChanges(settings app.SettingsRepository, bus *events.EventBus) {
	bus.Subscribe(domain.EventVolumeChanged, func(e domain.Event) {
		payload, ok := e.Payload.(domain.VolumeChangedPayload)
		if !ok {
			return
		}
		if err := settings.SetVolume(context.Background(), payload.Volume); err != nil {
			slog.Warn("failed to persist volume", "error", err)
		}
	})
}
