package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

var (
	ErrStartupFailed  = errors.New("startup failed")
	ErrShutdownFailed = errors.New("shutdown failed")
)

type LifecycleManager struct {
	mu         sync.Mutex
	components []Component
}

func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		components: make([]Component, 0),
	}
}

func (m *LifecycleManager) Register(component Component) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.components {
		if c.Name() == component.Name() {
			slog.Warn("component already registered", "name", component.Name())
			return
		}
	}
	m.components = append(m.components, component)
}

func (m *LifecycleManager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	comps := make([]Component, len(m.components))
	copy(comps, m.components)
	m.mu.Unlock()

	for _, c := range comps {
		slog.Info("starting component", "name", c.Name())
		if err := c.Start(ctx); err != nil {
			slog.Error("component start failed", "name", c.Name(), "error", err)
			return errors.Join(ErrStartupFailed, err)
		}
		slog.Info("component started", "name", c.Name())
	}
	return nil
}

func (m *LifecycleManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	comps := make([]Component, len(m.components))
	copy(comps, m.components)
	m.mu.Unlock()

	var errs []error
	for i := len(comps) - 1; i >= 0; i-- {
		c := comps[i]
		slog.Info("stopping component", "name", c.Name())
		if err := c.Stop(ctx); err != nil {
			slog.Error("component stop failed", "name", c.Name(), "error", err)
			errs = append(errs, err)
		} else {
			slog.Info("component stopped", "name", c.Name())
		}
	}
	if len(errs) > 0 {
		return errors.Join(append([]error{ErrShutdownFailed}, errs...)...)
	}
	return nil
}
