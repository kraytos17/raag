package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	log   *slog.Logger
	mu    sync.RWMutex
	level = slog.LevelInfo
)

func init() {
	log = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
}

func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	log = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

func SetLevel(l slog.Level) {
	mu.Lock()
	defer mu.Unlock()
	level = l
	log = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: l,
	}))
}

func Debug(msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.Debug(msg, args...)
	}
}

func DebugContext(ctx context.Context, msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.DebugContext(ctx, msg, args...)
	}
}

func Info(msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.Info(msg, args...)
	}
}

func InfoContext(ctx context.Context, msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.InfoContext(ctx, msg, args...)
	}
}

func Warn(msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.Warn(msg, args...)
	}
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.WarnContext(ctx, msg, args...)
	}
}

func Error(msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.Error(msg, args...)
	}
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		l.ErrorContext(ctx, msg, args...)
	}
}

func With(args ...any) *slog.Logger {
	mu.RLock()
	l := log
	mu.RUnlock()
	return l.With(args...)
}
