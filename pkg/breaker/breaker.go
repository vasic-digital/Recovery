// Package breaker provides a named circuit breaker manager that wraps
// digital.vasic.concurrency/pkg/breaker with application-level fault tolerance.
//
// Design patterns applied:
//   - Decorator: CircuitBreaker wraps the concurrency breaker with logging and callbacks
//   - Registry: CircuitBreakerManager provides centralized lookup and creation of named breakers
//   - Observer: state change callbacks notify listeners of circuit state transitions
//   - Facade: simplified API surface for callers
package breaker

import (
	"sync"
	"time"

	vasicbreaker "digital.vasic.concurrency/pkg/breaker"
)

// Logger is a structured logger interface that decouples this package from
// any specific logging implementation. Implementations must accept a message
// string followed by alternating key/value pairs.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
}

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	StateClosed   CircuitState = iota // Normal operation — requests pass through
	StateHalfOpen                     // Probing — limited requests pass through
	StateOpen                         // Failing — requests are rejected immediately
)

// String returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// mapBreakState translates vasicbreaker.State to CircuitState.
func mapBreakState(s vasicbreaker.State) CircuitState {
	switch s {
	case vasicbreaker.Closed:
		return StateClosed
	case vasicbreaker.HalfOpen:
		return StateHalfOpen
	case vasicbreaker.Open:
		return StateOpen
	default:
		return StateClosed
	}
}

// CircuitBreakerConfig contains configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	Name         string
	MaxFailures  int
	ResetTimeout time.Duration
	Logger       Logger
}

// CircuitBreaker wraps digital.vasic.concurrency/pkg/breaker.CircuitBreaker
// with application-level features: structured logging, named identification,
// and state change callbacks.
type CircuitBreaker struct {
	name          string
	inner         *vasicbreaker.CircuitBreaker
	logger        Logger
	onStateChange func(string, CircuitState, CircuitState)
	maxFailures   int           // stored for GetStats
	resetTimeout  time.Duration // stored for GetStats
}

// NewCircuitBreaker creates a new circuit breaker backed by
// digital.vasic.concurrency/pkg/breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	maxFailures := config.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}
	resetTimeout := config.ResetTimeout
	if resetTimeout <= 0 {
		resetTimeout = 60 * time.Second
	}

	cfg := &vasicbreaker.Config{
		MaxFailures:      maxFailures,
		Timeout:          resetTimeout,
		HalfOpenRequests: 1,
	}

	return &CircuitBreaker{
		name:         config.Name,
		inner:        vasicbreaker.New(cfg),
		logger:       config.Logger,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

// SetStateChangeCallback registers a callback invoked on every state transition.
func (cb *CircuitBreaker) SetStateChangeCallback(callback func(string, CircuitState, CircuitState)) {
	cb.onStateChange = callback
}

// Execute wraps fn with circuit breaker protection, delegating to the
// digital.vasic.concurrency breaker engine and adding logging and callbacks.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	prevState := mapBreakState(cb.inner.State())

	err := cb.inner.Execute(fn)

	newState := mapBreakState(cb.inner.State())

	if cb.logger != nil {
		if err != nil {
			cb.logger.Warn("Circuit breaker recorded failure",
				"name", cb.name,
				"failures", cb.inner.Failures(),
				"state", newState.String())
		} else {
			cb.logger.Debug("Circuit breaker recorded success",
				"name", cb.name,
				"state", newState.String())
		}
	}

	if prevState != newState {
		if cb.logger != nil {
			cb.logger.Info("Circuit breaker state changed",
				"name", cb.name,
				"old_state", prevState.String(),
				"new_state", newState.String())
		}
		if cb.onStateChange != nil {
			cb.onStateChange(cb.name, prevState, newState)
		}
	}

	return err
}

// GetState returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() CircuitState {
	return mapBreakState(cb.inner.State())
}

// GetFailures returns the current consecutive failure count.
func (cb *CircuitBreaker) GetFailures() int {
	return cb.inner.Failures()
}

// GetStats returns circuit breaker statistics as a map.
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"name":          cb.name,
		"state":         mapBreakState(cb.inner.State()).String(),
		"failures":      cb.inner.Failures(),
		"max_failures":  cb.maxFailures,
		"reset_timeout": cb.resetTimeout,
	}
}

// Reset forces the circuit breaker back to the closed state.
func (cb *CircuitBreaker) Reset() {
	cb.inner.Reset()
	if cb.logger != nil {
		cb.logger.Info("Circuit breaker manually reset", "name", cb.name)
	}
}

// CircuitBreakerManager manages a named registry of circuit breakers.
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
	logger   Logger
}

// NewCircuitBreakerManager creates a new circuit breaker manager.
func NewCircuitBreakerManager(logger Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

// GetOrCreate retrieves an existing circuit breaker by name, or creates a new one.
func (m *CircuitBreakerManager) GetOrCreate(name string, config CircuitBreakerConfig) *CircuitBreaker {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if cb, exists := m.breakers[name]; exists {
		return cb
	}

	config.Name = name
	if config.Logger == nil {
		config.Logger = m.logger
	}

	cb := NewCircuitBreaker(config)
	m.breakers[name] = cb

	if m.logger != nil {
		m.logger.Info("Created new circuit breaker", "name", name)
	}
	return cb
}

// Get retrieves a circuit breaker by name. Returns nil if not found.
func (m *CircuitBreakerManager) Get(name string) *CircuitBreaker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.breakers[name]
}

// GetAll returns a snapshot of all managed circuit breakers.
func (m *CircuitBreakerManager) GetAll() map[string]*CircuitBreaker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]*CircuitBreaker, len(m.breakers))
	for name, cb := range m.breakers {
		result[name] = cb
	}
	return result
}

// GetStats returns aggregated statistics for all managed circuit breakers.
func (m *CircuitBreakerManager) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := make(map[string]interface{}, len(m.breakers))
	for name, cb := range m.breakers {
		stats[name] = cb.GetStats()
	}
	return stats
}

// Reset resets all managed circuit breakers to the closed state.
func (m *CircuitBreakerManager) Reset() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for _, cb := range m.breakers {
		cb.Reset()
	}

	if m.logger != nil {
		m.logger.Info("All circuit breakers reset")
	}
}
