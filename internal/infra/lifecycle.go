package infra

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/p-society/raag/internal/app"
)

var (
	ErrComponentNotFound = errors.New("component not found")
	ErrStartupFailed     = errors.New("startup failed")
	ErrShutdownFailed    = errors.New("shutdown failed")
)

type LifecycleManager struct {
	mu         sync.Mutex
	components map[string]app.Component
	startOrder []string
	states     map[string]app.ComponentState
}

func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		components: make(map[string]app.Component),
		states:     make(map[string]app.ComponentState),
	}
}

func (m *LifecycleManager) Register(component app.Component) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := component.Name()
	if _, exists := m.components[name]; exists {
		slog.Warn("component already registered", "name", name)
		return
	}

	m.components[name] = component
	m.startOrder = append(m.startOrder, name)
	m.states[name] = app.ComponentState{
		Name:   name,
		Status: app.StatusUnknown,
	}
}

func (m *LifecycleManager) Deregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.components, name)
	delete(m.states, name)
	newOrder := make([]string, 0, len(m.startOrder))
	for _, n := range m.startOrder {
		if n != name {
			newOrder = append(newOrder, n)
		}
	}
	m.startOrder = newOrder
}

func (m *LifecycleManager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	for _, name := range m.startOrder {
		m.states[name] = app.ComponentState{
			Name:   name,
			Status: app.StatusStarting,
		}
	}
	m.mu.Unlock()

	for _, name := range m.startOrder {
		component := m.components[name]
		slog.Info("starting component", "name", name)
		if err := component.Start(ctx); err != nil {
			slog.Error("component start failed", "name", name, "error", err)
			m.mu.Lock()
			m.states[name] = app.ComponentState{
				Name:   name,
				Status: app.StatusFailed,
				Error:  err,
			}
			m.mu.Unlock()
			return errors.Join(ErrStartupFailed, err)
		}

		m.mu.Lock()
		m.states[name] = app.ComponentState{
			Name:   name,
			Status: app.StatusRunning,
		}

		m.mu.Unlock()
		slog.Info("component started", "name", name)
	}
	return nil
}

func (m *LifecycleManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	for _, name := range m.startOrder {
		m.states[name] = app.ComponentState{
			Name:   name,
			Status: app.StatusStopping,
		}
	}
	m.mu.Unlock()

	var errs []error
	for i := len(m.startOrder) - 1; i >= 0; i-- {
		name := m.startOrder[i]
		component := m.components[name]
		slog.Info("stopping component", "name", name)
		if err := component.Stop(ctx); err != nil {
			slog.Error("component stop failed", "name", name, "error", err)
			errs = append(errs, err)
			m.mu.Lock()
			m.states[name] = app.ComponentState{
				Name:   name,
				Status: app.StatusFailed,
				Error:  err,
			}
			m.mu.Unlock()
			continue
		}

		m.mu.Lock()
		m.states[name] = app.ComponentState{
			Name:   name,
			Status: app.StatusStopped,
		}

		m.mu.Unlock()
		slog.Info("component stopped", "name", name)
	}
	if len(errs) > 0 {
		return errors.Join(ErrShutdownFailed, errs[0])
	}
	return nil
}

func (m *LifecycleManager) GetComponent(name string) (app.Component, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	component, exists := m.components[name]
	if !exists {
		return nil, ErrComponentNotFound
	}
	return component, nil
}

func (m *LifecycleManager) GetState(name string) (app.ComponentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[name]
	if !exists {
		return app.ComponentState{}, ErrComponentNotFound
	}
	return state, nil
}

func (m *LifecycleManager) GetAllStates() []app.ComponentState {
	m.mu.Lock()
	defer m.mu.Unlock()

	states := make([]app.ComponentState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	return states
}
