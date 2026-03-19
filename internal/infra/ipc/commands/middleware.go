package commands

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	traceIDKey contextKey = "trace_id"
	spanIDKey  contextKey = "span_id"
)

func LoggingMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			start := time.Now()
			traceID := uuid.New().String()
			ctx = context.WithValue(ctx, traceIDKey, traceID)
			slog.Info("command started",
				"trace_id", traceID,
				"type", cmd.Type,
			)

			response, err := next(ctx, cmd)
			duration := time.Since(start)
			if err != nil {
				slog.Error("command failed",
					"trace_id", traceID,
					"type", cmd.Type,
					"duration", duration,
					"error", err,
				)
			} else {
				slog.Info("command completed",
					"trace_id", traceID,
					"type", cmd.Type,
					"duration", duration,
					"status", response.Status,
				)
			}
			return response, err
		}
	}
}

func TracingMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			traceID, ok := ctx.Value(traceIDKey).(string)
			if !ok {
				traceID = uuid.New().String()
			}

			ctx = context.WithValue(ctx, spanIDKey, uuid.New().String())
			ctx = context.WithValue(ctx, traceIDKey, traceID)
			return next(ctx, cmd)
		}
	}
}

func ValidationMiddleware(validate func(Command) error) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			if validate != nil {
				if err := validate(cmd); err != nil {
					return Response{
						Status: "error",
						Error:  "validation failed: " + err.Error(),
					}, nil
				}
			}
			return next(ctx, cmd)
		}
	}
}

func RecoveryMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic recovered in command handler",
						"type", cmd.Type,
						"panic", r,
						"stack", string(debug.Stack()),
					)
				}
			}()
			return next(ctx, cmd)
		}
	}
}

func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, cmd)
		}
	}
}

func RetryMiddleware(maxRetries int, delay time.Duration) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, cmd Command) (Response, error) {
			var lastErr error
			for attempt := range maxRetries {
				if attempt > 0 {
					select {
					case <-ctx.Done():
						return Response{}, ctx.Err()
					case <-time.After(delay * time.Duration(attempt)):
					}
				}

				response, err := next(ctx, cmd)
				if err == nil {
					return response, nil
				}

				lastErr = err
				slog.Warn("retrying command",
					"type", cmd.Type,
					"attempt", attempt+1,
					"max_retries", maxRetries,
					"error", err,
				)
			}

			return Response{
				Status: "error",
				Error:  lastErr.Error(),
			}, lastErr
		}
	}
}

func ValidateCommand(cmd Command) error {
	if cmd.Type == "" {
		return &ValidationError{Field: "Type", Message: "command type is required"}
	}
	return nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func MarshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func UnmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
