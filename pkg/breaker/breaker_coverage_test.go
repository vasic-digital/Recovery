package breaker

import (
	"fmt"
	"testing"
	"time"

	vasicbreaker "digital.vasic.concurrency/pkg/breaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapBreakState_AllBranches directly tests the unexported mapBreakState
// function, including the default branch that cannot be reached via normal
// CircuitBreaker usage (the underlying breaker only produces Closed, Open,
// and HalfOpen states).
func TestMapBreakState_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		input    vasicbreaker.State
		expected CircuitState
	}{
		{"Closed", vasicbreaker.Closed, StateClosed},
		{"Open", vasicbreaker.Open, StateOpen},
		{"HalfOpen", vasicbreaker.HalfOpen, StateHalfOpen},
		{"Unknown positive", vasicbreaker.State(99), StateClosed},
		{"Unknown negative", vasicbreaker.State(-1), StateClosed},
		{"Unknown large", vasicbreaker.State(1000), StateClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapBreakState(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNewCircuitBreaker_CustomConfig verifies that explicit MaxFailures and
// ResetTimeout values are respected without falling back to defaults.
func TestNewCircuitBreaker_CustomConfig(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "custom",
		MaxFailures:  10,
		ResetTimeout: 2 * time.Minute,
	})
	require.NotNil(t, cb)
	assert.Equal(t, 10, cb.maxFailures)
	assert.Equal(t, 2*time.Minute, cb.resetTimeout)
}

// TestNewCircuitBreaker_ZeroValues verifies that zero/negative config values
// fall back to defaults.
func TestNewCircuitBreaker_ZeroValues(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "zero-defaults",
		MaxFailures:  0,
		ResetTimeout: 0,
	})
	require.NotNil(t, cb)
	assert.Equal(t, 5, cb.maxFailures, "should default to 5")
	assert.Equal(t, 60*time.Second, cb.resetTimeout, "should default to 60s")
}

// TestNewCircuitBreaker_NegativeValues verifies that negative config values
// also fall back to defaults.
func TestNewCircuitBreaker_NegativeValues(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "negative-defaults",
		MaxFailures:  -3,
		ResetTimeout: -time.Second,
	})
	require.NotNil(t, cb)
	assert.Equal(t, 5, cb.maxFailures)
	assert.Equal(t, 60*time.Second, cb.resetTimeout)
}

// TestCircuitBreaker_Execute_NoLogger verifies that Execute works correctly
// when no logger is set.
func TestCircuitBreaker_Execute_NoLogger(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:        "no-logger",
		MaxFailures: 3,
	})

	// Success without logger should not panic
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)

	// Failure without logger should not panic
	err = cb.Execute(func() error { return fmt.Errorf("fail") })
	assert.Error(t, err)
}

// TestCircuitBreaker_Execute_StateChange_NoLogger verifies state transition
// logging is safely skipped when logger is nil.
func TestCircuitBreaker_Execute_StateChange_NoLogger(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "no-logger-transition",
		MaxFailures:  1,
		ResetTimeout: 10 * time.Second,
	})

	// This will cause Closed -> Open with no logger set
	err := cb.Execute(func() error { return fmt.Errorf("trip") })
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())
}

// TestCircuitBreaker_Execute_StateChange_NoCallback verifies state transition
// proceeds without panic when no callback is registered.
func TestCircuitBreaker_Execute_StateChange_NoCallback(t *testing.T) {
	logger := newTestLogger()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "no-callback-transition",
		MaxFailures:  1,
		ResetTimeout: 10 * time.Second,
		Logger:       logger,
	})
	// Deliberately do NOT set a state change callback.

	err := cb.Execute(func() error { return fmt.Errorf("trip") })
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Verify the state change was logged even without callback
	logger.mu.Lock()
	defer logger.mu.Unlock()
	found := false
	for _, entry := range logger.messages {
		if entry.level == "info" && entry.msg == "Circuit breaker state changed" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected state change log even without callback")
}

// TestCircuitBreaker_Reset_NoLogger verifies Reset works without a logger.
func TestCircuitBreaker_Reset_NoLogger(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "reset-no-logger",
		MaxFailures:  1,
		ResetTimeout: 10 * time.Second,
	})

	// Trip it
	_ = cb.Execute(func() error { return fmt.Errorf("trip") })
	assert.Equal(t, StateOpen, cb.GetState())

	// Reset without logger should not panic
	cb.Reset()
	assert.Equal(t, StateClosed, cb.GetState())
}

// TestCircuitBreakerManager_GetOrCreate_NilLogger verifies manager works
// when created with a nil logger.
func TestCircuitBreakerManager_GetOrCreate_NilLogger(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	cb := mgr.GetOrCreate("nil-logger-breaker", CircuitBreakerConfig{
		MaxFailures: 3,
	})
	require.NotNil(t, cb)

	// The breaker should have the manager's nil logger
	assert.Nil(t, cb.logger)
}

// TestCircuitBreakerManager_GetOrCreate_PreservesConfigLogger verifies that
// when the config already has a logger, it is not overridden by the manager's.
func TestCircuitBreakerManager_GetOrCreate_PreservesConfigLogger(t *testing.T) {
	mgrLogger := newTestLogger()
	mgr := NewCircuitBreakerManager(mgrLogger)

	configLogger := newTestLogger()
	cb := mgr.GetOrCreate("with-own-logger", CircuitBreakerConfig{
		MaxFailures: 3,
		Logger:      configLogger,
	})
	require.NotNil(t, cb)

	// The breaker should use the config's logger, not the manager's
	assert.Equal(t, configLogger, cb.logger)
}

// TestCircuitBreakerManager_Reset_NilLogger verifies manager Reset works
// without a logger.
func TestCircuitBreakerManager_Reset_NilLogger(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	cb := mgr.GetOrCreate("nil-reset", CircuitBreakerConfig{MaxFailures: 1})
	_ = cb.Execute(func() error { return fmt.Errorf("trip") })
	assert.Equal(t, StateOpen, cb.GetState())

	// Reset with nil logger should not panic
	mgr.Reset()
	assert.Equal(t, StateClosed, cb.GetState())
}

// TestCircuitBreakerManager_GetAll_Empty verifies GetAll returns an empty map
// when no breakers have been created.
func TestCircuitBreakerManager_GetAll_Empty(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	all := mgr.GetAll()
	assert.NotNil(t, all)
	assert.Len(t, all, 0)
}

// TestCircuitBreakerManager_GetStats_Empty verifies GetStats returns an empty
// map when no breakers have been created.
func TestCircuitBreakerManager_GetStats_Empty(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	stats := mgr.GetStats()
	assert.NotNil(t, stats)
	assert.Len(t, stats, 0)
}

// TestCircuitState_String_Exhaustive ensures the String method is stable for
// all named constants and default case.
func TestCircuitState_String_Exhaustive(t *testing.T) {
	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "unknown", CircuitState(42).String())
}
