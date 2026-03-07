package facade

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"digital.vasic.recovery/pkg/breaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResilience_ConcurrentExecuteAndAddBreaker(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Half the goroutines Execute through named breakers
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("exec-breaker-%d", idx%10)
			err := r.Execute(name, func() error { return nil })
			assert.NoError(t, err)
		}(i)
	}

	// Other half create/get breakers explicitly
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("exec-breaker-%d", idx%10)
			cb := r.GetOrCreateBreaker(name, breaker.CircuitBreakerConfig{
				MaxFailures: 5,
			})
			assert.NotNil(t, cb)
		}(i)
	}

	wg.Wait()

	// All 10 distinct breakers should exist in stats
	stats := r.Stats()
	breakers, ok := stats["breakers"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 10, len(breakers),
		"expected 10 unique breakers, got %d", len(breakers))
}

func TestResilience_ConcurrentHealthCheckAddAndStop(t *testing.T) {
	r := New(newTestLogger())

	const numAdders = 20
	var wg sync.WaitGroup
	wg.Add(numAdders)

	// Goroutines adding health checks concurrently
	for i := 0; i < numAdders; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("hc-%d", idx)
			r.AddHealthCheck(name, func() error {
				return nil
			}, 100*time.Millisecond)
		}(i)
	}

	// Let some health checks register before stopping
	time.Sleep(30 * time.Millisecond)

	// Stop while adds might still be in flight
	r.Stop()

	// Wait for all adders to finish
	wg.Wait()

	// After stop, verify stats is still callable (no panic)
	stats := r.Stats()
	require.Contains(t, stats, "breakers")
	require.Contains(t, stats, "health")
}

func TestResilience_StopIdempotent(t *testing.T) {
	r := New(newTestLogger())

	var count atomic.Int32
	r.AddHealthCheck("idempotent-hc", func() error {
		count.Add(1)
		return nil
	}, 50*time.Millisecond)

	time.Sleep(80 * time.Millisecond)

	// Multiple Stop calls should not panic
	r.Stop()
	r.Stop()
	r.Stop()

	snapshot := count.Load()
	time.Sleep(150 * time.Millisecond)

	// Health checks should not keep running
	assert.Equal(t, snapshot, count.Load(),
		"health check should not run after Stop")
}

func TestResilience_ExecuteNonexistentBreaker(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	// Execute with a breaker name that has never been created.
	// The facade auto-creates it with default config.
	called := false
	err := r.Execute("never-created", func() error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called, "function should have been called")

	// The breaker should now exist in stats
	stats := r.Stats()
	breakers, ok := stats["breakers"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, breakers, "never-created")

	// Verify the auto-created breaker uses defaults (MaxFailures=5)
	breakerStats, ok := breakers["never-created"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 5, breakerStats["max_failures"])
}

func TestResilience_Stats_WithMixedBreakersAndHealthChecks(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	// Create multiple breakers with different states
	r.GetOrCreateBreaker("healthy-breaker", breaker.CircuitBreakerConfig{
		MaxFailures: 10,
	})

	trippedBreaker := r.GetOrCreateBreaker("tripped-breaker", breaker.CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 10 * time.Second,
	})
	// Trip it
	_ = r.Execute("tripped-breaker", func() error {
		return fmt.Errorf("fail")
	})
	assert.Equal(t, breaker.StateOpen, trippedBreaker.GetState())

	// Add healthy and unhealthy health checks
	r.AddHealthCheck("db-healthy", func() error {
		return nil
	}, 50*time.Millisecond)

	r.AddHealthCheck("cache-unhealthy", func() error {
		return fmt.Errorf("redis down")
	}, 50*time.Millisecond)

	// Wait for health checks to run
	time.Sleep(80 * time.Millisecond)

	stats := r.Stats()

	// Verify breakers section
	breakers, ok := stats["breakers"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, breakers, "healthy-breaker")
	assert.Contains(t, breakers, "tripped-breaker")

	healthyBreakerStats, ok := breakers["healthy-breaker"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "closed", healthyBreakerStats["state"])

	trippedBreakerStats, ok := breakers["tripped-breaker"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "open", trippedBreakerStats["state"])
	assert.Equal(t, 1, trippedBreakerStats["failures"])

	// Verify health section
	healthStats, ok := stats["health"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, healthStats, "db-healthy")
	assert.Contains(t, healthStats, "cache-unhealthy")

	dbStats, ok := healthStats["db-healthy"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", dbStats["status"])

	cacheStats, ok := healthStats["cache-unhealthy"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "unhealthy", cacheStats["status"])
}
