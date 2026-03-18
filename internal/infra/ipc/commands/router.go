package commands

import (
	"context"
	"log/slog"
	"time"
)

type CommandType string

const (
	CmdPlay           CommandType = "play"
	CmdPause          CommandType = "pause"
	CmdResume         CommandType = "resume"
	CmdStop           CommandType = "stop"
	CmdNext           CommandType = "next"
	CmdPrev           CommandType = "prev"
	CmdSeekTo         CommandType = "seek_to"
	CmdSetVolume      CommandType = "set_volume"
	CmdQueueAdd       CommandType = "queue_add"
	CmdQueueRemove    CommandType = "queue_remove"
	CmdQueueClear     CommandType = "queue_clear"
	CmdQueueMove      CommandType = "queue_move"
	CmdSearch         CommandType = "search"
	CmdLibScan        CommandType = "lib_scan"
	CmdListPeers      CommandType = "list_peers"
	CmdStreamFrom     CommandType = "stream_from"
	CmdCreatePlaylist CommandType = "create_playlist"
	CmdDeletePlaylist CommandType = "delete_playlist"
	CmdStatus         CommandType = "status"
	CmdSubscribe      CommandType = "subscribe"
	CmdDebugPeers     CommandType = "debug_peers"
	CmdDebugStreams   CommandType = "debug_streams"
	CmdHealthCheck    CommandType = "health_check"
)

type Command struct {
	Type    CommandType
	Payload []byte
}

type Response struct {
	Status string
	Data   []byte
	Error  string
}

type Handler func(ctx context.Context, cmd Command) (Response, error)

type Middleware func(Handler) Handler

type CommandRouter struct {
	handlers   map[CommandType]Handler
	middleware []Middleware
}

func NewRouter() *CommandRouter {
	return &CommandRouter{
		handlers:   make(map[CommandType]Handler),
		middleware: make([]Middleware, 0),
	}
}

func (r *CommandRouter) Register(cmdType CommandType, handler Handler) {
	r.handlers[cmdType] = handler
}

func (r *CommandRouter) Use(mw Middleware) {
	r.middleware = append(r.middleware, mw)
}

func (r *CommandRouter) Dispatch(ctx context.Context, cmd Command) (Response, error) {
	handler, exists := r.handlers[cmd.Type]
	if !exists {
		return Response{
			Status: "error",
			Error:  "unknown command",
		}, nil
	}
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = r.middleware[i](handler)
	}

	start := time.Now()
	response, err := handler(ctx, cmd)
	duration := time.Since(start)
	if err != nil {
		slog.Warn("command failed",
			"type", cmd.Type,
			"duration", duration,
			"error", err,
		)
	} else {
		slog.Debug("command executed",
			"type", cmd.Type,
			"duration", duration,
		)
	}
	return response, err
}

func (r *CommandRouter) ListCommands() []CommandType {
	commands := make([]CommandType, 0, len(r.handlers))
	for cmd := range r.handlers {
		commands = append(commands, cmd)
	}
	return commands
}
