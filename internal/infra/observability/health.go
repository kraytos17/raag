package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

const defaultFFmpegPath = "ffmpeg"

// HealthResult describes the status of a single health check.
type HealthResult struct {
	Name   string
	OK     bool
	Detail string
	Err    error
}

// HealthChecker runs a named health check.
type HealthChecker interface {
	Check(ctx context.Context) HealthResult
}

// HealthCheckerFunc adapts a function to the HealthChecker interface.
type HealthCheckerFunc func(ctx context.Context) HealthResult

func (f HealthCheckerFunc) Check(ctx context.Context) HealthResult {
	return f(ctx)
}

// FFmpegHealthCheck verifies the ffmpeg binary is usable.
func FFmpegHealthCheck(ffmpegPath string) HealthChecker {
	return HealthCheckerFunc(func(ctx context.Context) HealthResult {
		if ffmpegPath == "" {
			ffmpegPath = defaultFFmpegPath
		}
		cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := exec.CommandContext(cmdCtx, ffmpegPath, "-version").Run(); err != nil {
			return HealthResult{Name: "ffmpeg", OK: false, Detail: "ffmpeg not usable", Err: err}
		}
		return HealthResult{Name: "ffmpeg", OK: true, Detail: "ok"}
	})
}

// ComponentHealthCheck wraps a simple ok/err check.
func ComponentHealthCheck(name string, fn func(ctx context.Context) error) HealthChecker {
	return HealthCheckerFunc(func(ctx context.Context) HealthResult {
		if err := fn(ctx); err != nil {
			return HealthResult{Name: name, OK: false, Detail: err.Error(), Err: err}
		}
		return HealthResult{Name: name, OK: true, Detail: "ok"}
	})
}

// RunAll runs all checks and returns a summary. A check error is logged and
// recorded; RunAll never fails overall (reporting is best-effort).
func RunAll(ctx context.Context, checks ...HealthChecker) []HealthResult {
	results := make([]HealthResult, 0, len(checks))
	for _, c := range checks {
		res := c.Check(ctx)
		if !res.OK {
			slog.Warn("health check failed", "check", res.Name, "detail", res.Detail, "err", res.Err)
		}
		results = append(results, res)
	}
	return results
}

// AllOK reports whether every check passed.
func AllOK(results []HealthResult) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

// FormatResults renders the check results as a pass/fail table.
func FormatResults(results []HealthResult) string {
	var s string
	for _, r := range results {
		status := "FAIL"
		if r.OK {
			status = "ok"
		}
		s += fmt.Sprintf("%-12s %s", r.Name, status)
		if r.Detail != "" && !r.OK {
			s += "  " + r.Detail
		}
		s += "\n"
	}
	return s
}
