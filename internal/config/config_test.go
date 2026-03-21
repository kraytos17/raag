package config

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := &Config{
		P2P: P2PConfig{
			MaxPeers: 20,
		},
		Playback: PlaybackConfig{
			Volume: 80,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() valid config error = %v", err)
	}
}

func TestConfig_Validate_InvalidVolume(t *testing.T) {
	tests := []struct {
		name    string
		volume  int
		wantErr bool
	}{
		{"negative volume", -1, true},
		{"volume over 100", 101, true},
		{"volume at 100", 100, false},
		{"volume at 0", 0, false},
		{"normal volume", 50, false},
		{"max valid volume", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				P2P: P2PConfig{
					MaxPeers: 20,
				},
				Playback: PlaybackConfig{
					Volume: tt.volume,
				},
			}

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() volume=%d error = %v, wantErr %v", tt.volume, err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_MaxPeers(t *testing.T) {
	tests := []struct {
		name     string
		maxPeers int
		wantErr  bool
	}{
		{"zero max peers", 0, true},
		{"negative max peers", -1, true},
		{"positive max peers", 20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				P2P: P2PConfig{
					MaxPeers: tt.maxPeers,
				},
				Playback: PlaybackConfig{
					Volume: 80,
				},
			}

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_ValidateForStart_NoPath(t *testing.T) {
	cfg := &Config{
		Library: LibraryConfig{
			Paths: []string{},
		},
		P2P: P2PConfig{
			MaxPeers: 20,
		},
		Playback: PlaybackConfig{
			Volume: 80,
		},
	}

	err := cfg.ValidateForStart()
	if err != ErrNoMusicPath {
		t.Errorf("ValidateForStart() error = %v, want ErrNoMusicPath", err)
	}
}

func TestConfig_ValidateForStart_WithPath(t *testing.T) {
	cfg := &Config{
		Library: LibraryConfig{
			Paths: []string{"/music"},
		},
		P2P: P2PConfig{
			MaxPeers: 20,
		},
		Playback: PlaybackConfig{
			Volume: 80,
		},
	}
	if err := cfg.ValidateForStart(); err != nil {
		t.Errorf("ValidateForStart() with path error = %v", err)
	}
}

func TestExpandHome_Basic(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Skip("cannot get current user")
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"tilde home", "~/Music", filepath.Join(usr.HomeDir, "Music")},
		{"tilde subdir", "~/Music/Rock", filepath.Join(usr.HomeDir, "Music", "Rock")},
		{"absolute path", "/music", "/music"},
		{"relative path", "music", "music"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandHome(tt.input)
			if got != tt.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandHome_Absolute(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"/music", "/music"},
		{"/home/user/Music", "/home/user/Music"},
		{"/absolute/path", "/absolute/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandHome(tt.input)
			if got != tt.input {
				t.Errorf("ExpandHome(%q) = %q, should not change absolute path", tt.input, got)
			}
		})
	}
}

func TestGetConfigDir(t *testing.T) {
	dir := GetConfigDir()
	if dir == "" {
		t.Error("GetConfigDir() should not return empty string")
	}
	if dir != filepath.Join(os.Getenv("HOME"), ".config", "raag") {
		t.Errorf("GetConfigDir() = %q, expected format: ~/.config/raag", dir)
	}
}

func TestGetDataDir(t *testing.T) {
	dir := GetDataDir()
	if dir == "" {
		t.Error("GetDataDir() should not return empty string")
	}
	if dir != filepath.Join(os.Getenv("HOME"), ".local", "share", "raag") {
		t.Errorf("GetDataDir() = %q, expected format: ~/.local/share/raag", dir)
	}
}

func TestDetectMusicDirectory(t *testing.T) {
	dir := detectMusicDirectory()
	if dir == "" {
		t.Log("detectMusicDirectory() returned empty (no common paths exist)")
	}
}

func TestConfig_Validate_InvalidVolumeMessage(t *testing.T) {
	tests := []struct {
		volume       int
		wantContains string
	}{
		{-1, "non-negative"},
		{101, "at most 100"},
	}

	for _, tt := range tests {
		cfg := &Config{
			P2P: P2PConfig{
				MaxPeers: 20,
			},
			Playback: PlaybackConfig{
				Volume: tt.volume,
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Errorf("Validate() volume=%d should return error", tt.volume)
			continue
		}
		if tt.volume < 0 && err.Error() == "" {
			t.Errorf("Validate() should return descriptive error for negative volume")
		}
	}
}

func TestConfig_ExpandPaths(t *testing.T) {
	cfg := &Config{
		Daemon: DaemonConfig{
			DataDir:    "~/data",
			SocketPath: "~/socket",
			PidFile:    "~/pid",
		},
		Library: LibraryConfig{
			Paths: []string{"~/Music", "/absolute/path"},
		},
	}

	if err := cfg.expandPaths(); err != nil {
		t.Errorf("expandPaths() error = %v", err)
	}

	usr, _ := user.Current()
	expected := filepath.Join(usr.HomeDir, "data")
	if cfg.Daemon.DataDir != expected {
		t.Errorf("expandPaths() DataDir = %q, want %q", cfg.Daemon.DataDir, expected)
	}
}

func TestErrNoMusicPath(t *testing.T) {
	if ErrNoMusicPath == nil {
		t.Error("ErrNoMusicPath should not be nil")
	}
	if ErrNoMusicPath.Error() == "" {
		t.Error("ErrNoMusicPath should have message")
	}
}
