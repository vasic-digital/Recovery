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

// TestNew_NilLogger verifies that the facade can be created with a nil logger.
func TestNew_NilLogger(t *testing.T) {
	r := New(nil)
	require.NotNil(t, r)
	defer r.Stop()

	// Basic operations should work without a logger
	err := r.Execute("no-logger", func() error { return nil })
	assert.NoError(t, err)
}

// TestResilience_GetOrCreateBreaker_DefaultConfig verifies that a breaker
// created through the facade with empty config gets sane defaults.
func TestResilience_GetOrCreateBreaker_DefaultConfig(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	cb := r.GetOrCreateBreaker("default-cfg", breaker.CircuitBreakerConfig{})
	require.NotNil(t, cb)
	assert.Equal(t, breaker.StateClosed, cb.GetState())

	stats := cb.GetStats()
	assert.Equal(t, 5, stats["max_failures"], "should default to 5 max failures")
}

// TestResilience_AddHealthCheck_ImmediateStatus verifies that after adding a
// healthy check, the status is immediately available.
func TestResilience_AddHealthCheck_ImmediateStatus(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	r.AddHealthCheck("immediate", func() error { return nil }, time.Second)

	// The check runs immediately during Start, so status should be available
	time.Sleep(20 * time.Millisecond)

	stats := r.Stats()
	healthStats, ok := stats["health"].(map[string]interface{})
	require.True(t, ok)
	require.Contains(t, healthStats, "immediate")

	hcStats, ok := healthStats["immediate"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", hcStats["status"])
}

// TestResilience_AddHealthCheck_Unhealthy verifies that an unhealthy check
// is reflected in stats.
func TestResilience_AddHealthCheck_Unhealthy(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	r.AddHealthCheck("failing", func() error {
		return fmt.Errorf("db unreachable")
	}, time.Second)

	time.Sleep(20 * time.Millisecond)

	stats := r.Stats()
	healthStats := stats["health"].(map[string]interface{})
	hcStats := healthStats["failing"].(map[string]interface{})
	assert.Equal(t, "unhealthy", hcStats["status"])
}

// TestResilience_Execute_CreatesBreaker verifies that Execute auto-creates a
// breaker if one does not exist, and that breaker persists.
func TestResilience_Execute_CreatesBreaker(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	// Execute creates the breaker on first use
	err := r.Execute("auto-created", func() error { return nil })
	require.NoError(t, err)

	// Subsequent execute on same name reuses the breaker
	err = r.Execute("auto-created", func() error { return nil })
	require.NoError(t, err)

	// Verify it appears in stats
	stats := r.Stats()
	breakers := stats["breakers"].(map[string]interface{})
	assert.Contains(t, breakers, "auto-created")
}

// TestResilience_Stats_Empty verifies that Stats returns valid empty maps
// when no breakers or health checks have been added.
func TestResilience_Stats_Empty(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	stats := r.Stats()
	require.Contains(t, stats, "breakers")
	require.Contains(t, stats, "health")

	breakers, ok := stats["breakers"].(map[string]interface{})
	require.True(t, ok)
	assert.Len(t, breakers, 0)

	healthStats, ok := stats["health"].(map[string]interface{})
	require.True(t, ok)
	assert.Len(t, healthStats, 0)
}

// TestResilience_Stats_HealthCheckLastCheck verifies that the last_check
// field in health stats contains a non-zero time after a check has run.
func TestResilience_Stats_HealthCheckLastCheck(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	r.AddHealthCheck("time-check", func() error { return nil }, time.Second)
	time.Sleep(20 * time.Millisecond)

	stats := r.Stats()
	healthStats := stats["health"].(map[string]interface{})
	hcStats := healthStats["time-check"].(map[string]interface{})

	lastCheck, ok := hcStats["last_check"].(time.Time)
	require.True(t, ok)
	assert.False(t, lastCheck.IsZero(), "last_check should be set")
}

// TestResilience_Stop_CancelsHealthChecks verifies that all health checks
// stop running after Stop is called.
func TestResilience_Stop_CancelsHealthChecks(t *testing.T) {
	r := New(newTestLogger())

	var count1, count2 atomic.Int32
	r.AddHealthCheck("hc1", func() error {
		count1.Add(1)
		return nil
	}, 50*time.Millisecond)
	r.AddHealthCheck("hc2", func() error {
		count2.Add(1)
		return nil
	}, 50*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	r.Stop()

	snap1 := count1.Load()
	snap2 := count2.Load()
	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, snap1, count1.Load(), "hc1 should stop after Stop")
	assert.Equal(t, snap2, count2.Load(), "hc2 should stop after Stop")
}

// TestResilience_AddHealthCheck_MultipleReplacements verifies that
// repeatedly replacing a health check stops previous ones.
func TestResilience_AddHealthCheck_MultipleReplacements(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	var counts [3]atomic.Int32
	for i := 0; i < 3; i++ {
		idx := i
		r.AddHealthCheck("replaced", func() error {
			counts[idx].Add(1)
			return nil
		}, 50*time.Millisecond)
		time.Sleep(80 * time.Millisecond)
	}

	// Snapshot all counters
	snaps := [3]int32{
		counts[0].Load(),
		counts[1].Load(),
		counts[2].Load(),
	}

	time.Sleep(120 * time.Millisecond)

	// Only the last (index 2) should still be incrementing
	assert.LessOrEqual(t, counts[0].Load(), snaps[0]+1,
		"first checker should have stopped")
	assert.LessOrEqual(t, counts[1].Load(), snaps[1]+1,
		"second checker should have stopped")
	assert.Greater(t, counts[2].Load(), snaps[2],
		"third checker should still be running")
}

// TestResilience_ConcurrentStatsAndExecute verifies that Stats and Execute
// can be called concurrently without races.
func TestResilience_ConcurrentStatsAndExecute(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	r.AddHealthCheck("concurrent-hc", func() error { return nil }, 50*time.Millisecond)

	var wg sync.WaitGroup
	const goroutines = 30

	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = r.Execute("concurrent-exec", func() error { return nil })
		}()
		go func() {
			defer wg.Done()
			_ = r.Stats()
		}()
	}
	wg.Wait()
}
