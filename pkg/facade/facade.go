// Package facade provides a unified resilience API that combines circuit breakers
// and health checkers into a single entry point.
//
// Design patterns applied:
//   - Facade: single Resilience struct unifies breaker management and health checking
//   - Decorator: Execute method adds circuit breaker protection around arbitrary functions
//   - Registry: delegates to CircuitBreakerManager for named breaker lookup/creation
//   - Observer: health checkers run asynchronously and report status transitions
package facade

import (
	"context"
	"sync"
	"time"

	"digital.vasic.recovery/pkg/breaker"
	"digital.vasic.recovery/pkg/health"
)

// Resilience is a unified fault tolerance facade that combines circuit breakers
// and health checkers behind a single API.
type Resilience struct {
	breakers *breaker.CircuitBreakerManager
	checkers map[string]*health.Checker
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	logger   breaker.Logger
}

// New creates a new Resilience facade.
func New(logger breaker.Logger) *Resilience {
	ctx, cancel := context.WithCancel(context.Background())
	return &Resilience{
		breakers: breaker.NewCircuitBreakerManager(logger),
		checkers: make(map[string]*health.Checker),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}
}

// GetOrCreateBreaker retrieves an existing circuit breaker by name, or creates
// a new one with the provided configuration.
func (r *Resilience) GetOrCreateBreaker(name string, cfg breaker.CircuitBreakerConfig) *breaker.CircuitBreaker {
	return r.breakers.GetOrCreate(name, cfg)
}

// AddHealthCheck registers a periodic health check with the given name, check
// function, and polling interval. The health checker starts immediately.
func (r *Resilience) AddHealthCheck(name string, check health.CheckFunc, interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Stop existing checker with the same name if present
	if existing, ok := r.checkers[name]; ok {
		existing.Stop()
	}

	c := health.NewChecker(name, check, interval)
	r.checkers[name] = c
	c.Start(r.ctx)
}

// Execute runs fn through the named circuit breaker. If the breaker does not
// exist, it is created with default configuration.
func (r *Resilience) Execute(breakerName string, fn func() error) error {
	cb := r.breakers.GetOrCreate(breakerName, breaker.CircuitBreakerConfig{})
	return cb.Execute(fn)
}

// Stats returns aggregated statistics for all circuit breakers and health
// checkers as a single map.
func (r *Resilience) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	healthStats := make(map[string]interface{}, len(r.checkers))
	for name, c := range r.checkers {
		healthStats[name] = map[string]interface{}{
			"status":     string(c.Status()),
			"last_check": c.LastCheck(),
		}
	}

	return map[string]interface{}{
		"breakers": r.breakers.GetStats(),
		"health":   healthStats,
	}
}

// Stop cancels all health checkers and releases resources.
func (r *Resilience) Stop() {
	r.cancel()

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.checkers {
		c.Stop()
	}
}
