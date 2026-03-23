package app

import (
	"context"
	"errors"
	"fmt"
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
	names      map[string]struct{}
}

func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		names: make(map[string]struct{}),
	}
}

func (m *LifecycleManager) Register(component Component) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := component.Name()
	if _, ok := m.names[name]; ok {
		return fmt.Errorf("component already registered: %s", name)
	}

	m.components = append(m.components, component)
	m.names[name] = struct{}{}
	return nil
}

func (m *LifecycleManager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	comps := make([]Component, 0, len(m.components))
	comps = append(comps, m.components...)
	m.mu.Unlock()

	var started []Component
	for _, c := range comps {
		slog.Info("starting component", "name", c.Name())
		if err := c.Start(ctx); err != nil {
			slog.Error("component start failed", "name", c.Name(), "error", err)
			for i := len(started) - 1; i >= 0; i-- {
				comp := started[i]
				slog.Info("stopping component", "name", comp.Name(), "reason", "rollback")
				if stopErr := comp.Stop(ctx); stopErr != nil {
					slog.Error("component stop failed", "name", comp.Name(), "error", stopErr)
				}
			}
			return errors.Join(ErrStartupFailed, err)
		}

		started = append(started, c)
		slog.Info("component started", "name", c.Name())
	}
	return nil
}

func (m *LifecycleManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	comps := make([]Component, 0, len(m.components))
	comps = append(comps, m.components...)
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
