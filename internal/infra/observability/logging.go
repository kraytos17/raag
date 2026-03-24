package observability

import (
	"log/slog"
	"os"
	"strings"
	"time"

	prettylogger "github.com/nentgroup/slog-prettylogger"
)

type Config struct {
	Level     slog.Level
	AddSource bool
}

func NewLogger(cfg Config) *slog.Logger {
	opts := prettylogger.HandlerOptions{
		SlogOpts: slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: cfg.AddSource,
		},
		TimeFormat: time.TimeOnly,
		NoColor:    os.Getenv("TERM") == "dumb",
	}
	return slog.New(prettylogger.NewHandler(os.Stdout, opts))
}

func ParseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
