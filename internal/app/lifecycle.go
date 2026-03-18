package app

import "context"

type ComponentStatus string

const (
	StatusUnknown  ComponentStatus = "unknown"
	StatusStarting ComponentStatus = "starting"
	StatusRunning  ComponentStatus = "running"
	StatusStopping ComponentStatus = "stopping"
	StatusStopped  ComponentStatus = "stopped"
	StatusFailed   ComponentStatus = "failed"
)

type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type ComponentState struct {
	Name   string
	Status ComponentStatus
	Error  error
}
