package breaker

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitState_String_AllValues(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{StateClosed, "closed"},
		{StateHalfOpen, "half-open"},
		{StateOpen, "open"},
		{CircuitState(99), "unknown"},
		{CircuitState(-1), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestCircuitBreaker_RapidStateTransitions(t *testing.T) {
	logger := newTestLogger()

	var mu sync.Mutex
	var transitions []struct {
		from, to CircuitState
	}

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "rapid",
		MaxFailures:  2,
		ResetTimeout: 50 * time.Millisecond, // Short timeout so half-open triggers quickly
		Logger:       logger,
	})

	cb.SetStateChangeCallback(func(name string, from, to CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, struct {
			from, to CircuitState
		}{from, to})
	})

	// Phase 1: Closed -> Open (2 failures)
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Phase 2: Wait for timeout so breaker transitions to HalfOpen
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// Phase 3: A success in half-open should close the circuit
	err := cb.Execute(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())

	// Phase 4: Trip it again
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail again") })
	}
	assert.Equal(t, StateOpen, cb.GetState())

	// Phase 5: Wait again, then fail in half-open to go back to open
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	err = cb.Execute(func() error { return fmt.Errorf("still broken") })
	require.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Verify we had multiple state transitions
	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, len(transitions), 3,
		"expected at least 3 state transitions, got %d", len(transitions))
}

func TestCircuitBreakerManager_Concurrent_ManyBreakers(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	const numGoroutines = 50
	const breakersPerGoroutine = 2 // 50 * 2 = 100 unique breakers

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < breakersPerGoroutine; i++ {
				name := fmt.Sprintf("breaker-%d-%d", goroutineID, i)
				cb := mgr.GetOrCreate(name, CircuitBreakerConfig{MaxFailures: 3})
				require.NotNil(t, cb)
			}
		}(g)
	}
	wg.Wait()

	all := mgr.GetAll()
	assert.Equal(t, numGoroutines*breakersPerGoroutine, len(all),
		"expected %d breakers, got %d", numGoroutines*breakersPerGoroutine, len(all))

	// Verify each breaker is retrievable individually
	for g := 0; g < numGoroutines; g++ {
		for i := 0; i < breakersPerGoroutine; i++ {
			name := fmt.Sprintf("breaker-%d-%d", g, i)
			cb := mgr.Get(name)
			assert.NotNil(t, cb, "breaker %s should exist", name)
		}
	}
}

func TestCircuitBreakerManager_GetOrCreate_SameName_Concurrent(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	const numGoroutines = 100
	results := make([]*CircuitBreaker, numGoroutines)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = mgr.GetOrCreate("shared-breaker", CircuitBreakerConfig{
				MaxFailures: 5,
			})
		}(i)
	}
	wg.Wait()

	// All results should be the exact same pointer
	first := results[0]
	require.NotNil(t, first)
	for i := 1; i < numGoroutines; i++ {
		assert.Same(t, first, results[i],
			"goroutine %d got a different breaker instance", i)
	}

	// Manager should contain exactly one breaker
	all := mgr.GetAll()
	assert.Equal(t, 1, len(all))
}

func TestCircuitBreaker_Execute_PanicRecovery(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "panic-test",
		MaxFailures:  5,
		ResetTimeout: time.Second,
	})

	// The underlying concurrency breaker does not recover panics,
	// so a panic in the wrapped function propagates. Verify this behavior.
	assert.Panics(t, func() {
		_ = cb.Execute(func() error {
			panic("something went wrong")
		})
	})

	// After a panic, the breaker should still be usable for subsequent calls.
	// Note: The panic occurs after the mutex is unlocked in the underlying breaker,
	// so the breaker is not left in a deadlocked state.
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_Stats_AfterMixedCalls(t *testing.T) {
	logger := newTestLogger()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "mixed-stats",
		MaxFailures:  10,
		ResetTimeout: 30 * time.Second,
		Logger:       logger,
	})

	// 3 successes
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error { return nil })
		require.NoError(t, err)
	}

	stats := cb.GetStats()
	assert.Equal(t, "closed", stats["state"])
	assert.Equal(t, 0, stats["failures"],
		"failures should be 0 after successes (counter resets on success)")

	// 2 failures
	for i := 0; i < 2; i++ {
		err := cb.Execute(func() error { return fmt.Errorf("fail-%d", i) })
		require.Error(t, err)
	}

	stats = cb.GetStats()
	assert.Equal(t, "closed", stats["state"])
	assert.Equal(t, 2, stats["failures"])
	assert.Equal(t, "mixed-stats", stats["name"])
	assert.Equal(t, 10, stats["max_failures"])
	assert.Equal(t, 30*time.Second, stats["reset_timeout"])

	// 1 success should reset failure counter
	err := cb.Execute(func() error { return nil })
	require.NoError(t, err)

	stats = cb.GetStats()
	assert.Equal(t, 0, stats["failures"],
		"failures should reset to 0 after a success")

	// Verify logger received both debug (success) and warn (failure) messages
	logger.mu.Lock()
	defer logger.mu.Unlock()

	debugCount := 0
	warnCount := 0
	for _, entry := range logger.messages {
		switch entry.level {
		case "debug":
			if entry.msg == "Circuit breaker recorded success" {
				debugCount++
			}
		case "warn":
			if entry.msg == "Circuit breaker recorded failure" {
				warnCount++
			}
		}
	}
	assert.Equal(t, 4, debugCount, "expected 4 success debug logs (3 initial + 1 final)")
	assert.Equal(t, 2, warnCount, "expected 2 failure warn logs")
}
