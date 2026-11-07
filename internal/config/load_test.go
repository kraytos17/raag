package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfigHome points HOME at a temp dir and moves CWD there so neither
// a real ~/.config/raag/config.toml nor a ./config.toml leaks in.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	return home
}

func writeConfigFile(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "raag")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_NoConfigFileUsesDefaults(t *testing.T) {
	isolateConfigHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.P2P.MaxPeers != 20 {
		t.Errorf("MaxPeers = %d, want 20", cfg.P2P.MaxPeers)
	}
	if cfg.Playback.Volume != 80 {
		t.Errorf("Volume = %d, want 80", cfg.Playback.Volume)
	}
	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = true, want false by default")
	}
}

func TestLoad_ReadsConfigFile(t *testing.T) {
	home := isolateConfigHome(t)
	writeConfigFile(t, home, "[p2p]\nmax_peers = 5\n[playback]\nvolume = 33\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.P2P.MaxPeers != 5 {
		t.Errorf("MaxPeers = %d, want 5 from config file", cfg.P2P.MaxPeers)
	}
	if cfg.Playback.Volume != 33 {
		t.Errorf("Volume = %d, want 33 from config file", cfg.Playback.Volume)
	}
}

func TestLoad_InvalidConfigFileErrors(t *testing.T) {
	home := isolateConfigHome(t)
	writeConfigFile(t, home, "[p2p\nmax_peers = !!!\n")
	if _, err := Load(); err == nil {
		t.Error("Load() = nil error for invalid TOML, want error")
	}
}

func TestLoad_InvalidValuesError(t *testing.T) {
	home := isolateConfigHome(t)
	writeConfigFile(t, home, "[playback]\nvolume = 101\n")
	if _, err := Load(); err == nil {
		t.Error("Load() = nil error for volume 101, want validation error")
	}
}

func TestLoadWithMusicPath_OverridesPaths(t *testing.T) {
	home := isolateConfigHome(t)
	music := filepath.Join(home, "tunes")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg, err := LoadWithMusicPath(music)
	if err != nil {
		t.Fatalf("LoadWithMusicPath() error = %v", err)
	}
	if len(cfg.Library.Paths) != 1 || cfg.Library.Paths[0] != music {
		t.Errorf("Paths = %v, want [%s]", cfg.Library.Paths, music)
	}
	if err := cfg.ValidateForStart(); err != nil {
		t.Errorf("ValidateForStart() error = %v, want nil with music path", err)
	}
}

func TestPromptDirectory(t *testing.T) {
	dir := t.TempDir()
	newReader := func(input string) *bufio.Reader {
		return bufio.NewReader(strings.NewReader(input))
	}

	t.Run("empty accepts default", func(t *testing.T) {
		if got, err := promptDirectory(newReader("\n"), dir); err != nil || got != dir {
			t.Errorf("promptDirectory() = %q, %v; want %q, nil", got, err, dir)
		}
	})

	t.Run("existing dir accepted", func(t *testing.T) {
		if got, err := promptDirectory(newReader(dir+"\n"), "/ignored"); err != nil || got != dir {
			t.Errorf("promptDirectory() = %q, %v; want %q, nil", got, err, dir)
		}
	})

	t.Run("missing dir created on yes", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "new-music")
		if got, err := promptDirectory(newReader(target+"\ny\n"), dir); err != nil || got != target {
			t.Errorf("promptDirectory() = %q, %v; want %q, nil", got, err, target)
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("directory was not created: %v", err)
		}
	})

	t.Run("no then valid path", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "declined")
		input := target + "\nn\n" + dir + "\n"
		if got, err := promptDirectory(newReader(input), dir); err != nil || got != dir {
			t.Errorf("promptDirectory() = %q, %v; want %q, nil", got, err, dir)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("declined directory should not exist: %v", err)
		}
	})

	t.Run("no default and empty errors", func(t *testing.T) {
		if _, err := promptDirectory(newReader("\n"), ""); err == nil {
			t.Error("promptDirectory() = nil error, want error with no default")
		}
	})
}

func TestRunSetup_WritesConfig(t *testing.T) {
	home := isolateConfigHome(t)
	music := filepath.Join(home, "Music")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Accept the detected default (~/Music exists).
	if err := runSetup(bufio.NewReader(strings.NewReader("\n"))); err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".config", "raag", "config.toml"))
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(string(data), music) {
		t.Errorf("config.toml does not contain music path %q", music)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() after setup error = %v", err)
	}
	if len(cfg.Library.Paths) != 1 || cfg.Library.Paths[0] != music {
		t.Errorf("Paths = %v, want [%s]", cfg.Library.Paths, music)
	}
	if err := cfg.ValidateForStart(); err != nil {
		t.Errorf("ValidateForStart() error = %v, want nil after setup", err)
	}
}
