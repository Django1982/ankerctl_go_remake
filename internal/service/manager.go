package service

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// ServiceManager manages service registration and reference-counted lifecycle.
type ServiceManager struct {
	mu    sync.Mutex
	svcs  map[string]Service
	refs  map[string]int
	order []string
}

// NewServiceManager creates an empty ServiceManager.
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		svcs: make(map[string]Service),
		refs: make(map[string]int),
	}
}

// Register adds a service under its Name().
func (m *ServiceManager) Register(svc Service) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := svc.Name()
	if _, exists := m.svcs[name]; exists {
		return
	}
	m.svcs[name] = svc
	m.refs[name] = 0
	m.order = append(m.order, name)
}

// Borrow increments the reference count and auto-starts on transition 0->1.
func (m *ServiceManager) Borrow(name string) (Service, error) {
	m.mu.Lock()
	svc, ok := m.svcs[name]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("unknown service %q", name)
	}
	m.refs[name]++
	shouldStart := m.refs[name] == 1
	m.mu.Unlock()

	if shouldStart {
		svc.Start(context.Background())
	}
	return svc, nil
}

// Return decrements the reference count and auto-stops on transition 1->0,
// except for VideoQueue when video is enabled.
func (m *ServiceManager) Return(name string) {
	m.mu.Lock()
	svc, ok := m.svcs[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	if m.refs[name] <= 0 {
		m.mu.Unlock()
		return
	}
	m.refs[name]--
	shouldStop := m.refs[name] == 0 && !keepVideoQueueRunning(name, svc)
	m.mu.Unlock()

	if shouldStop {
		svc.Stop()
	}
}

// Shutdown stops all services in reverse registration order and blocks until
// each service's internal goroutine has fully exited. This matches Python's
// ServiceManager.atexit() behaviour: stop → await_stopped → shutdown (join).
func (m *ServiceManager) Shutdown() {
	m.mu.Lock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	svcs := make(map[string]Service, len(m.svcs))
	for k, v := range m.svcs {
		svcs[k] = v
	}
	m.mu.Unlock()

	// Signal all services to stop before blocking on any one of them, so they
	// can begin their shutdown concurrently (mirrors Python's two-pass approach).
	for i := len(names) - 1; i >= 0; i-- {
		if svc, ok := svcs[names[i]]; ok {
			svc.Stop()
		}
	}

	// Now block on each service's goroutine in reverse order.
	for i := len(names) - 1; i >= 0; i-- {
		if svc, ok := svcs[names[i]]; ok {
			svc.Shutdown()
		}
	}
}

// Get returns a registered service by name.
func (m *ServiceManager) Get(name string) (Service, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	svc, ok := m.svcs[name]
	return svc, ok
}

// ServicesSnapshot returns a shallow copy of currently registered services.
func (m *ServiceManager) ServicesSnapshot() map[string]Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Service, len(m.svcs))
	for k, v := range m.svcs {
		out[k] = v
	}
	return out
}

// RefsSnapshot returns a copy of current reference counts.
func (m *ServiceManager) RefsSnapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int, len(m.refs))
	for k, v := range m.refs {
		out[k] = v
	}
	return out
}

// RestartAll restarts all registered services.
func (m *ServiceManager) RestartAll() {
	m.mu.Lock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	svcs := make(map[string]Service, len(m.svcs))
	for k, v := range m.svcs {
		svcs[k] = v
	}
	m.mu.Unlock()

	for _, name := range names {
		if svc, ok := svcs[name]; ok {
			svc.Restart()
		}
	}
}

func keepVideoQueueRunning(name string, svc Service) bool {
	if name != "videoqueue" {
		return false
	}

	type videoEnabledGetter interface {
		VideoEnabled() bool
	}
	if ve, ok := svc.(videoEnabledGetter); ok {
		return ve.VideoEnabled()
	}

	v := reflect.ValueOf(svc)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}

	fields := []string{"video_enabled", "videoEnabled", "VideoEnabled"}
	for _, fieldName := range fields {
		field := v.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.Bool {
			return field.Bool()
		}
	}
	return false
}
