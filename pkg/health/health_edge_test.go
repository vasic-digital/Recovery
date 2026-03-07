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

func TestChecker_StatusTransitions(t *testing.T) {
	// Control what the check returns: 0 = healthy, 1 = unhealthy
	var failFlag atomic.Bool
	logger := newTestLogger()

	check := func() error {
		if failFlag.Load() {
			return fmt.Errorf("service down")
		}
		return nil
	}

	c := NewChecker("transition-test", check, 50*time.Millisecond)
	c.SetLogger(logger)

	// Before starting, status should be unknown
	assert.Equal(t, StatusUnknown, c.Status())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start: immediate check runs (healthy), so unknown -> healthy
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, StatusHealthy, c.Status())
	assert.Nil(t, c.LastError())

	// Transition to unhealthy
	failFlag.Store(true)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StatusUnhealthy, c.Status())
	require.NotNil(t, c.LastError())
	assert.Equal(t, "service down", c.LastError().Error())

	// Transition back to healthy
	failFlag.Store(false)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StatusHealthy, c.Status())
	assert.Nil(t, c.LastError())

	c.Stop()

	// Verify recovery was logged (unhealthy -> healthy)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	recoveryCount := 0
	for _, entry := range logger.messages {
		if entry.level == "info" && entry.msg == "Health check recovered" {
			recoveryCount++
		}
	}
	assert.GreaterOrEqual(t, recoveryCount, 1,
		"expected at least one recovery log message")
}

func TestChecker_RapidStartStop(t *testing.T) {
	var count atomic.Int32

	check := func() error {
		count.Add(1)
		return nil
	}

	// Create and start/stop the checker multiple times in sequence.
	// Each iteration needs a fresh checker because Stop closes stopCh permanently.
	for i := 0; i < 5; i++ {
		c := NewChecker(
			fmt.Sprintf("rapid-%d", i),
			check,
			50*time.Millisecond,
		)
		ctx, cancel := context.WithCancel(context.Background())

		c.Start(ctx)
		// Let at least the immediate check run
		time.Sleep(20 * time.Millisecond)
		c.Stop()
		cancel()
	}

	// Each start should run at least the immediate check
	assert.GreaterOrEqual(t, count.Load(), int32(5),
		"expected at least 5 checks (one per start)")
}

func TestChecker_ConcurrentStatusReads_WithFlapping(t *testing.T) {
	var toggle atomic.Bool

	check := func() error {
		if toggle.Load() {
			return fmt.Errorf("flapping")
		}
		return nil
	}

	c := NewChecker("concurrent-read", check, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)

	// Toggle health state rapidly in the background
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			toggle.Store(i%2 == 0)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// 50 goroutines reading status concurrently
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				status := c.Status()
				// Status should always be one of the defined values
				assert.Contains(t, []Status{StatusHealthy, StatusUnhealthy, StatusUnknown}, status)
				_ = c.LastError()
				_ = c.LastCheck()
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	<-done
	c.Stop()
}

func TestChecker_CheckFuncPanics(t *testing.T) {
	check := func() error {
		panic("health check exploded")
	}

	c := NewChecker("panic-check", check, time.Second)

	// The health checker's runCheck() calls check() directly without recover,
	// so a panic will propagate. Verify this behavior.
	assert.Panics(t, func() {
		ctx := context.Background()
		c.Start(ctx)
		// Start runs runCheck() immediately and synchronously before spawning
		// the goroutine, so the panic occurs during Start.
	})
}

func TestChecker_NilLogger(t *testing.T) {
	var failFlag atomic.Bool

	check := func() error {
		if failFlag.Load() {
			return fmt.Errorf("fail")
		}
		return nil
	}

	// Create checker without setting a logger (logger remains nil)
	c := NewChecker("nil-logger", check, 50*time.Millisecond)
	// Deliberately do NOT call c.SetLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start should not panic even with nil logger
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, StatusHealthy, c.Status())

	// Transition to unhealthy (triggers logger.Warn path) -- should not panic
	failFlag.Store(true)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StatusUnhealthy, c.Status())

	// Transition back to healthy (triggers logger.Info recovery path) -- should not panic
	failFlag.Store(false)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, StatusHealthy, c.Status())

	c.Stop()
}
