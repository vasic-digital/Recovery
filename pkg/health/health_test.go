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

type testLogger struct {
	mu       sync.Mutex
	messages []logEntry
}

type logEntry struct {
	level         string
	msg           string
	keysAndValues []interface{}
}

func newTestLogger() *testLogger {
	return &testLogger{}
}

func (l *testLogger) Info(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, logEntry{"info", msg, keysAndValues})
}

func (l *testLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, logEntry{"warn", msg, keysAndValues})
}

func TestNewChecker(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("db", check, 5*time.Second)

	require.NotNil(t, c)
	assert.Equal(t, "db", c.Name())
	assert.Equal(t, StatusUnknown, c.Status())
	assert.Nil(t, c.LastError())
	assert.True(t, c.LastCheck().IsZero())
}

func TestChecker_StartStop(t *testing.T) {
	var count atomic.Int32
	check := func() error {
		count.Add(1)
		return nil
	}

	c := NewChecker("counter", check, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)

	// Wait for a few checks to run
	time.Sleep(180 * time.Millisecond)
	c.Stop()

	// Should have run at least twice (immediate + at least one tick)
	assert.GreaterOrEqual(t, count.Load(), int32(2))
	assert.Equal(t, StatusHealthy, c.Status())
}

func TestChecker_HealthyToUnhealthy(t *testing.T) {
	var failAfter atomic.Bool

	check := func() error {
		if failAfter.Load() {
			return fmt.Errorf("connection refused")
		}
		return nil
	}

	logger := newTestLogger()
	c := NewChecker("svc", check, 50*time.Millisecond)
	c.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)

	// Wait for initial healthy check
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, StatusHealthy, c.Status())
	assert.Nil(t, c.LastError())

	// Start failing
	failAfter.Store(true)
	time.Sleep(80 * time.Millisecond)

	assert.Equal(t, StatusUnhealthy, c.Status())
	require.NotNil(t, c.LastError())
	assert.Equal(t, "connection refused", c.LastError().Error())

	c.Stop()
}

func TestChecker_UnhealthyToHealthy(t *testing.T) {
	var healthy atomic.Bool

	check := func() error {
		if !healthy.Load() {
			return fmt.Errorf("down")
		}
		return nil
	}

	logger := newTestLogger()
	c := NewChecker("recover", check, 50*time.Millisecond)
	c.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)

	// Initially unhealthy
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, StatusUnhealthy, c.Status())

	// Recover
	healthy.Store(true)
	time.Sleep(80 * time.Millisecond)

	assert.Equal(t, StatusHealthy, c.Status())
	assert.Nil(t, c.LastError())

	c.Stop()

	// Verify recovery was logged
	logger.mu.Lock()
	defer logger.mu.Unlock()
	found := false
	for _, entry := range logger.messages {
		if entry.level == "info" && entry.msg == "Health check recovered" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected recovery log message")
}

func TestChecker_ConcurrentStatusReads(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("concurrent", check, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
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

func TestChecker_ContextCancellation(t *testing.T) {
	var count atomic.Int32
	check := func() error {
		count.Add(1)
		return nil
	}

	c := NewChecker("ctx-cancel", check, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)

	// Cancel context should stop checking
	cancel()
	time.Sleep(100 * time.Millisecond)

	snapshot := count.Load()
	time.Sleep(100 * time.Millisecond)

	// Count should not increase after cancellation
	assert.Equal(t, snapshot, count.Load())
}

func TestChecker_StopIdempotent(t *testing.T) {
	check := func() error { return nil }
	c := NewChecker("idempotent", check, time.Second)

	ctx := context.Background()
	c.Start(ctx)

	// Multiple stops should not panic
	c.Stop()
	c.Stop()
	c.Stop()
}
