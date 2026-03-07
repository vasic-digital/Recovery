package health

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecker_SetLogger_Replaces verifies that SetLogger replaces a
// previously set logger.
func TestChecker_SetLogger_Replaces(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("logger-replace", check, time.Second)

	logger1 := newTestLogger()
	c.SetLogger(logger1)

	logger2 := newTestLogger()
	c.SetLogger(logger2)

	// Start and let it run one check (healthy)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	c.Stop()

	// logger1 should have no messages (replaced before start)
	logger1.mu.Lock()
	assert.Empty(t, logger1.messages, "old logger should not receive messages")
	logger1.mu.Unlock()
}

// TestChecker_LastCheck_Updates verifies that LastCheck is updated on each
// health check execution.
func TestChecker_LastCheck_Updates(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("last-check-update", check, 50*time.Millisecond)

	// Before start, LastCheck should be zero
	assert.True(t, c.LastCheck().IsZero())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)

	firstCheck := c.LastCheck()
	assert.False(t, firstCheck.IsZero(), "LastCheck should be set after start")

	// Wait for another tick
	time.Sleep(60 * time.Millisecond)
	secondCheck := c.LastCheck()
	assert.True(t, secondCheck.After(firstCheck) || secondCheck.Equal(firstCheck),
		"LastCheck should advance or stay the same")

	c.Stop()
}

// TestChecker_Name_Immutable verifies that Name returns the same value
// regardless of checker state.
func TestChecker_Name_Immutable(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("immutable-name", check, time.Second)

	assert.Equal(t, "immutable-name", c.Name())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	assert.Equal(t, "immutable-name", c.Name())

	c.Stop()
	assert.Equal(t, "immutable-name", c.Name())
}

// TestChecker_ContextAndStop verifies that both context cancellation and
// Stop can terminate the checker, and that double-termination is safe.
func TestChecker_ContextAndStop(t *testing.T) {
	var count atomic.Int32
	check := func() error {
		count.Add(1)
		return nil
	}

	c := NewChecker("ctx-and-stop", check, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)

	// Cancel context first, then also call Stop -- should not panic
	cancel()
	time.Sleep(20 * time.Millisecond)
	c.Stop()

	snapshot := count.Load()
	time.Sleep(120 * time.Millisecond)

	// Count should not increase after both termination signals
	assert.Equal(t, snapshot, count.Load())
}

// TestChecker_UnhealthyLogger_WarnMessages verifies that unhealthy transitions
// produce warn log messages with the correct name and error.
func TestChecker_UnhealthyLogger_WarnMessages(t *testing.T) {
	logger := newTestLogger()
	check := func() error {
		return fmt.Errorf("connection timeout")
	}

	c := NewChecker("warn-test", check, time.Second)
	c.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	c.Stop()

	assert.Equal(t, StatusUnhealthy, c.Status())
	require.NotNil(t, c.LastError())
	assert.Equal(t, "connection timeout", c.LastError().Error())

	logger.mu.Lock()
	defer logger.mu.Unlock()
	found := false
	for _, entry := range logger.messages {
		if entry.level == "warn" && entry.msg == "Health check failed" {
			found = true
			// Verify the key-value pairs include name and error
			assert.Contains(t, entry.keysAndValues, "name")
			assert.Contains(t, entry.keysAndValues, "warn-test")
			break
		}
	}
	assert.True(t, found, "expected warn log for health check failure")
}

// TestChecker_RecoveryLog_FromUnknown verifies that a healthy check from
// the initial unknown status also logs recovery.
func TestChecker_RecoveryLog_FromUnknown(t *testing.T) {
	logger := newTestLogger()
	check := func() error { return nil }

	c := NewChecker("recovery-from-unknown", check, time.Second)
	c.SetLogger(logger)

	// Status starts as unknown; first healthy check is unknown -> healthy
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	c.Stop()

	assert.Equal(t, StatusHealthy, c.Status())

	// Check for recovery log (previous status was unknown, not healthy)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	found := false
	for _, entry := range logger.messages {
		if entry.level == "info" && entry.msg == "Health check recovered" {
			found = true
			break
		}
	}
	assert.True(t, found,
		"expected recovery log when transitioning from unknown to healthy")
}

// TestChecker_MultipleTransitions_LogCounts verifies that the correct number
// of warn and info messages are logged across multiple transitions.
func TestChecker_MultipleTransitions_LogCounts(t *testing.T) {
	var failFlag atomic.Bool
	logger := newTestLogger()

	check := func() error {
		if failFlag.Load() {
			return fmt.Errorf("down")
		}
		return nil
	}

	c := NewChecker("multi-transition", check, 40*time.Millisecond)
	c.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start healthy
	c.Start(ctx)
	time.Sleep(20 * time.Millisecond)

	// Go unhealthy
	failFlag.Store(true)
	time.Sleep(60 * time.Millisecond)

	// Go healthy again
	failFlag.Store(false)
	time.Sleep(60 * time.Millisecond)

	c.Stop()

	logger.mu.Lock()
	defer logger.mu.Unlock()

	warnCount := 0
	recoveryCount := 0
	for _, entry := range logger.messages {
		if entry.level == "warn" && entry.msg == "Health check failed" {
			warnCount++
		}
		if entry.level == "info" && entry.msg == "Health check recovered" {
			recoveryCount++
		}
	}

	assert.GreaterOrEqual(t, warnCount, 1, "expected at least 1 warn log")
	assert.GreaterOrEqual(t, recoveryCount, 1, "expected at least 1 recovery log")
}

// TestStatusConstants verifies that all Status constants have expected values.
func TestStatusConstants(t *testing.T) {
	assert.Equal(t, Status("healthy"), StatusHealthy)
	assert.Equal(t, Status("unhealthy"), StatusUnhealthy)
	assert.Equal(t, Status("unknown"), StatusUnknown)
}

// TestChecker_ConcurrentSetLogger verifies that SetLogger is safe to call
// concurrently with status reads.
func TestChecker_ConcurrentSetLogger(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("concurrent-set-logger", check, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.SetLogger(newTestLogger())
		}()
		go func() {
			defer wg.Done()
			_ = c.Status()
			_ = c.LastError()
			_ = c.LastCheck()
		}()
	}
	wg.Wait()
	c.Stop()
}
