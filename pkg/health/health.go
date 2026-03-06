// Package health provides periodic health checking with status tracking.
//
// Design patterns applied:
//   - Observer: periodic health checks notify via status transitions
//   - Facade: simple Start/Stop/Status API hides the ticker and goroutine lifecycle
package health

import (
	"context"
	"sync"
	"time"
)

// Logger is a structured logger interface that decouples this package from
// any specific logging implementation.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown"
)

// CheckFunc is a function that checks the health of a component.
// It returns nil if the component is healthy, or an error describing the issue.
type CheckFunc func() error

// Checker periodically runs a health check function and tracks the result.
type Checker struct {
	name      string
	check     CheckFunc
	interval  time.Duration
	status    Status
	lastCheck time.Time
	lastError error
	mu        sync.RWMutex
	stopCh    chan struct{}
	logger    Logger
}

// NewChecker creates a new health checker with the given name, check function,
// and polling interval. The checker starts in StatusUnknown and must be started
// with Start to begin periodic checks.
func NewChecker(name string, check CheckFunc, interval time.Duration) *Checker {
	return &Checker{
		name:     name,
		check:    check,
		interval: interval,
		status:   StatusUnknown,
		stopCh:   make(chan struct{}),
	}
}

// SetLogger sets the logger for this checker.
func (c *Checker) SetLogger(logger Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logger = logger
}

// Start begins periodic health checking. It runs the check immediately once,
// then repeats at the configured interval. The context can be used to cancel
// the checker externally. Start is non-blocking.
func (c *Checker) Start(ctx context.Context) {
	// Run immediately
	c.runCheck()

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.runCheck()
			}
		}
	}()
}

// Stop stops periodic health checking.
func (c *Checker) Stop() {
	select {
	case <-c.stopCh:
		// Already stopped
	default:
		close(c.stopCh)
	}
}

// Status returns the current health status.
func (c *Checker) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// LastError returns the last error from the health check, or nil if healthy.
func (c *Checker) LastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

// LastCheck returns the time of the last health check.
func (c *Checker) LastCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastCheck
}

// Name returns the name of this checker.
func (c *Checker) Name() string {
	return c.name
}

// runCheck executes the health check function and updates internal state.
func (c *Checker) runCheck() {
	err := c.check()

	c.mu.Lock()
	defer c.mu.Unlock()

	prevStatus := c.status
	c.lastCheck = time.Now()
	c.lastError = err

	if err != nil {
		c.status = StatusUnhealthy
		if c.logger != nil {
			c.logger.Warn("Health check failed",
				"name", c.name,
				"error", err.Error())
		}
	} else {
		c.status = StatusHealthy
		if prevStatus != StatusHealthy && c.logger != nil {
			c.logger.Info("Health check recovered",
				"name", c.name,
				"previous_status", string(prevStatus))
		}
	}
}
