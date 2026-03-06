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

// testLogger implements breaker.Logger for tests.
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

func (l *testLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, logEntry{"debug", msg, keysAndValues})
}

func TestNew(t *testing.T) {
	r := New(newTestLogger())
	require.NotNil(t, r)
	defer r.Stop()
}

func TestResilience_GetOrCreateBreaker(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	cb := r.GetOrCreateBreaker("test-breaker", breaker.CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: time.Second,
	})
	require.NotNil(t, cb)
	assert.Equal(t, breaker.StateClosed, cb.GetState())

	// Same name returns same instance
	cb2 := r.GetOrCreateBreaker("test-breaker", breaker.CircuitBreakerConfig{})
	assert.Equal(t, cb, cb2)
}

func TestResilience_AddHealthCheck(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	var count atomic.Int32
	r.AddHealthCheck("db", func() error {
		count.Add(1)
		return nil
	}, 50*time.Millisecond)

	// Wait for checks to run
	time.Sleep(130 * time.Millisecond)

	assert.GreaterOrEqual(t, count.Load(), int32(2))
}

func TestResilience_AddHealthCheck_ReplacesExisting(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	var count1 atomic.Int32
	r.AddHealthCheck("svc", func() error {
		count1.Add(1)
		return nil
	}, 50*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	snapshot := count1.Load()

	// Replace the checker
	var count2 atomic.Int32
	r.AddHealthCheck("svc", func() error {
		count2.Add(1)
		return nil
	}, 50*time.Millisecond)

	time.Sleep(80 * time.Millisecond)

	// Old checker should have stopped (count1 should not increase much)
	assert.LessOrEqual(t, count1.Load(), snapshot+1)
	// New checker should be running
	assert.GreaterOrEqual(t, count2.Load(), int32(1))
}

func TestResilience_Execute_Success(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	err := r.Execute("exec-breaker", func() error {
		return nil
	})
	require.NoError(t, err)
}

func TestResilience_Execute_Failure(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	err := r.Execute("exec-fail", func() error {
		return fmt.Errorf("operation failed")
	})
	require.Error(t, err)
	assert.Equal(t, "operation failed", err.Error())
}

func TestResilience_Execute_CircuitOpens(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	// Create breaker with low threshold
	r.GetOrCreateBreaker("low-thresh", breaker.CircuitBreakerConfig{
		MaxFailures:  2,
		ResetTimeout: 10 * time.Second,
	})

	// Trip it
	_ = r.Execute("low-thresh", func() error { return fmt.Errorf("err") })
	_ = r.Execute("low-thresh", func() error { return fmt.Errorf("err") })

	// Next call should be rejected by open circuit
	called := false
	err := r.Execute("low-thresh", func() error {
		called = true
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
	assert.False(t, called)
}

func TestResilience_Stats(t *testing.T) {
	r := New(newTestLogger())
	defer r.Stop()

	r.GetOrCreateBreaker("b1", breaker.CircuitBreakerConfig{MaxFailures: 5})
	r.AddHealthCheck("h1", func() error { return nil }, 50*time.Millisecond)

	// Wait for at least one health check
	time.Sleep(80 * time.Millisecond)

	stats := r.Stats()
	require.Contains(t, stats, "breakers")
	require.Contains(t, stats, "health")

	breakers, ok := stats["breakers"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, breakers, "b1")

	healthStats, ok := stats["health"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, healthStats, "h1")

	h1Stats, ok := healthStats["h1"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", h1Stats["status"])
}

func TestResilience_Stop(t *testing.T) {
	r := New(newTestLogger())

	var count atomic.Int32
	r.AddHealthCheck("stop-test", func() error {
		count.Add(1)
		return nil
	}, 50*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	r.Stop()

	snapshot := count.Load()
	time.Sleep(150 * time.Millisecond)

	// Count should not increase after stop
	assert.Equal(t, snapshot, count.Load())
}
