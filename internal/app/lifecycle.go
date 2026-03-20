package app

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
)

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

var (
	ErrComponentNotFound = errors.New("component not found")
	ErrStartupFailed     = errors.New("startup failed")
	ErrShutdownFailed    = errors.New("shutdown failed")
)

type LifecycleManager struct {
	mu         sync.Mutex
	components map[string]Component
	startOrder []string
	states     map[string]ComponentState
}

func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		components: make(map[string]Component),
		states:     make(map[string]ComponentState),
	}
}

func (m *LifecycleManager) Register(component Component) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := component.Name()
	if _, exists := m.components[name]; exists {
		slog.Warn("component already registered", "name", name)
		return
	}

	m.components[name] = component
	m.startOrder = append(m.startOrder, name)
	m.states[name] = ComponentState{
		Name:   name,
		Status: StatusUnknown,
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
	order := append([]string(nil), m.startOrder...)
	comps := make(map[string]Component, len(m.components))
	maps.Copy(comps, m.components)
	for _, name := range order {
		m.states[name] = ComponentState{
			Name:   name,
			Status: StatusStarting,
		}
	}

	m.mu.Unlock()
	for _, name := range order {
		component := comps[name]
		slog.Info("starting component", "name", name)
		if err := component.Start(ctx); err != nil {
			slog.Error("component start failed", "name", name, "error", err)
			m.mu.Lock()
			m.states[name] = ComponentState{
				Name:   name,
				Status: StatusFailed,
				Error:  err,
			}
			m.mu.Unlock()
			return errors.Join(ErrStartupFailed, err)
		}

		m.mu.Lock()
		m.states[name] = ComponentState{
			Name:   name,
			Status: StatusRunning,
		}

		m.mu.Unlock()
		slog.Info("component started", "name", name)
	}
	return nil
}

func (m *LifecycleManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	order := append([]string(nil), m.startOrder...)
	comps := make(map[string]Component, len(m.components))
	maps.Copy(comps, m.components)
	for _, name := range order {
		m.states[name] = ComponentState{
			Name:   name,
			Status: StatusStopping,
		}
	}
	m.mu.Unlock()

	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		component := comps[name]
		slog.Info("stopping component", "name", name)
		if err := component.Stop(ctx); err != nil {
			slog.Error("component stop failed", "name", name, "error", err)
			errs = append(errs, err)
			m.mu.Lock()
			m.states[name] = ComponentState{
				Name:   name,
				Status: StatusFailed,
				Error:  err,
			}
			m.mu.Unlock()
			continue
		}

		m.mu.Lock()
		m.states[name] = ComponentState{
			Name:   name,
			Status: StatusStopped,
		}

		m.mu.Unlock()
		slog.Info("component stopped", "name", name)
	}
	if len(errs) > 0 {
		return errors.Join(append([]error{ErrShutdownFailed}, errs...)...)
	}
	return nil
}

func (m *LifecycleManager) GetComponent(name string) (Component, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	component, exists := m.components[name]
	if !exists {
		return nil, ErrComponentNotFound
	}
	return component, nil
}

func (m *LifecycleManager) GetState(name string) (ComponentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[name]
	if !exists {
		return ComponentState{}, ErrComponentNotFound
	}
	return state, nil
}

func (m *LifecycleManager) GetAllStates() []ComponentState {
	m.mu.Lock()
	defer m.mu.Unlock()

	states := make([]ComponentState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	return states
}
