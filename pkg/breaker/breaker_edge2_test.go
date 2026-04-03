package breaker_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"digital.vasic.recovery/pkg/breaker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger implements breaker.Logger for test assertions.
type edgeTestLogger struct {
	mu       sync.Mutex
	messages []edgeLogEntry
}

type edgeLogEntry struct {
	level         string
	msg           string
	keysAndValues []interface{}
}

func newEdgeTestLogger() *edgeTestLogger {
	return &edgeTestLogger{}
}

func (l *edgeTestLogger) Info(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, edgeLogEntry{"info", msg, keysAndValues})
}

func (l *edgeTestLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, edgeLogEntry{"warn", msg, keysAndValues})
}

func (l *edgeTestLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, edgeLogEntry{"debug", msg, keysAndValues})
}

func TestCircuitBreaker_FullTransitionCycle(t *testing.T) {
	t.Parallel()

	logger := newEdgeTestLogger()
	var mu sync.Mutex
	var transitions []struct{ from, to breaker.CircuitState }

	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "full-cycle",
		MaxFailures:  2,
		ResetTimeout: 50 * time.Millisecond,
		Logger:       logger,
	})

	cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, struct{ from, to breaker.CircuitState }{from, to})
	})

	// Closed -> Open (2 failures)
	assert.Equal(t, breaker.StateClosed, cb.GetState())
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}
	assert.Equal(t, breaker.StateOpen, cb.GetState())

	// Open -> HalfOpen (wait for timeout)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, breaker.StateHalfOpen, cb.GetState())

	// HalfOpen -> Closed (success)
	err := cb.Execute(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, breaker.StateClosed, cb.GetState())

	mu.Lock()
	defer mu.Unlock()
	// Should have: closed->open, open->half-open, half-open->closed
	assert.GreaterOrEqual(t, len(transitions), 2,
		"expected at least 2 recorded transitions")
}

func TestCircuitBreaker_HalfOpenFailureReturnsToOpen(t *testing.T) {
	t.Parallel()

	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "halfopen-fail",
		MaxFailures:  1,
		ResetTimeout: 50 * time.Millisecond,
	})

	// Trip to open
	_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	assert.Equal(t, breaker.StateOpen, cb.GetState())

	// Wait for half-open
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, breaker.StateHalfOpen, cb.GetState())

	// Fail in half-open -> back to open
	err := cb.Execute(func() error { return fmt.Errorf("still broken") })
	assert.Error(t, err)
	assert.Equal(t, breaker.StateOpen, cb.GetState())
}

func TestCircuitBreaker_PanicRecovery_StillUsable(t *testing.T) {
	t.Parallel()

	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "panic-recover",
		MaxFailures:  5,
		ResetTimeout: time.Second,
	})

	// Execute a function that panics
	assert.Panics(t, func() {
		_ = cb.Execute(func() error {
			panic("kaboom")
		})
	})

	// After panic, breaker should still be usable
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, breaker.StateClosed, cb.GetState())
}

func TestCircuitBreaker_ConcurrentStateTransitions(t *testing.T) {
	t.Parallel()

	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "concurrent-transitions",
		MaxFailures:  3,
		ResetTimeout: 50 * time.Millisecond,
	})

	var wg sync.WaitGroup

	// Concurrent failures
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Execute(func() error { return fmt.Errorf("fail") })
		}()
	}
	wg.Wait()

	// Should be open after enough failures
	assert.Equal(t, breaker.StateOpen, cb.GetState())

	// Wait for half-open
	time.Sleep(80 * time.Millisecond)

	// Concurrent successes and failures in half-open
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = cb.Execute(func() error { return nil })
			} else {
				_ = cb.Execute(func() error { return fmt.Errorf("fail") })
			}
		}(i)
	}
	wg.Wait()

	// State should be deterministic (either open or closed), not corrupted
	state := cb.GetState()
	assert.Contains(t,
		[]breaker.CircuitState{breaker.StateClosed, breaker.StateOpen, breaker.StateHalfOpen},
		state, "state should be a valid value")
}

func TestCircuitBreaker_ResetDuringActiveRequests(t *testing.T) {
	t.Parallel()

	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "reset-active",
		MaxFailures:  2,
		ResetTimeout: time.Second,
	})

	// Trip the breaker
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}
	assert.Equal(t, breaker.StateOpen, cb.GetState())

	var wg sync.WaitGroup

	// Concurrent resets and executions
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cb.Reset()
		}()
		go func() {
			defer wg.Done()
			_ = cb.Execute(func() error { return nil })
		}()
	}
	wg.Wait()

	// After all resets and successes, should be closed
	assert.Equal(t, breaker.StateClosed, cb.GetState())
}

func TestCircuitBreaker_NilFallback_NilLogger(t *testing.T) {
	t.Parallel()

	// No logger, no callback -- should not panic
	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "nil-everything",
		MaxFailures:  2,
		ResetTimeout: time.Second,
	})

	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)

	err = cb.Execute(func() error { return fmt.Errorf("fail") })
	assert.Error(t, err)

	// Reset without logger should not panic
	cb.Reset()
	assert.Equal(t, breaker.StateClosed, cb.GetState())
}

func TestCircuitBreakerManager_ConcurrentGetOrCreate_DifferentNames(t *testing.T) {
	t.Parallel()

	mgr := breaker.NewCircuitBreakerManager(nil)
	var wg sync.WaitGroup

	const n = 100
	results := make([]*breaker.CircuitBreaker, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("breaker-%d", idx)
			results[idx] = mgr.GetOrCreate(name, breaker.CircuitBreakerConfig{
				MaxFailures: 3,
			})
		}(i)
	}
	wg.Wait()

	all := mgr.GetAll()
	assert.Equal(t, n, len(all))

	for i := 0; i < n; i++ {
		assert.NotNil(t, results[i])
	}
}

func TestCircuitBreakerManager_ResetAll_ConcurrentWithExecute(t *testing.T) {
	t.Parallel()

	logger := newEdgeTestLogger()
	mgr := breaker.NewCircuitBreakerManager(logger)

	// Create several breakers and trip them
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("cb-%d", i)
		cb := mgr.GetOrCreate(name, breaker.CircuitBreakerConfig{MaxFailures: 1})
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	var wg sync.WaitGroup

	// Concurrent reset and execute
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			mgr.Reset()
		}()
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("cb-%d", idx%5)
			cb := mgr.Get(name)
			if cb != nil {
				_ = cb.Execute(func() error { return nil })
			}
		}(i)
	}
	wg.Wait()
}

func TestCircuitBreaker_DefaultMaxFailures(t *testing.T) {
	t.Parallel()

	// MaxFailures <= 0 should default to 5
	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "default-max",
		MaxFailures:  0,
		ResetTimeout: time.Second,
	})

	stats := cb.GetStats()
	assert.Equal(t, 5, stats["max_failures"])
}

func TestCircuitBreaker_DefaultResetTimeout(t *testing.T) {
	t.Parallel()

	// ResetTimeout <= 0 should default to 60s
	cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
		Name:         "default-timeout",
		MaxFailures:  3,
		ResetTimeout: 0,
	})

	stats := cb.GetStats()
	assert.Equal(t, 60*time.Second, stats["reset_timeout"])
}
