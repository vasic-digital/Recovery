package breaker

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger implements Logger for test assertions.
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

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{Name: "test"})
	require.NotNil(t, cb)
	assert.Equal(t, "test", cb.name)
	assert.Equal(t, 5, cb.maxFailures)
	assert.Equal(t, 60*time.Second, cb.resetTimeout)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	logger := newTestLogger()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  3,
		ResetTimeout: time.Second,
		Logger:       logger,
	})

	err := cb.Execute(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, 0, cb.GetFailures())
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	logger := newTestLogger()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  5,
		ResetTimeout: time.Second,
		Logger:       logger,
	})

	err := cb.Execute(func() error { return fmt.Errorf("fail") })
	require.Error(t, err)
	assert.Equal(t, "fail", err.Error())
	assert.Equal(t, 1, cb.GetFailures())
	assert.Equal(t, StateClosed, cb.GetState())

	// Verify logger received a warn message
	logger.mu.Lock()
	defer logger.mu.Unlock()
	found := false
	for _, entry := range logger.messages {
		if entry.level == "warn" && entry.msg == "Circuit breaker recorded failure" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected warn log for failure")
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  3,
		ResetTimeout: 10 * time.Second,
	})

	// Start closed
	assert.Equal(t, StateClosed, cb.GetState())

	// Accumulate failures up to threshold
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("error") })
	}

	// Should be open after MaxFailures
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	var mu sync.Mutex
	var transitions []struct {
		name     string
		from, to CircuitState
	}

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "cb-callback",
		MaxFailures:  2,
		ResetTimeout: 10 * time.Second,
	})

	cb.SetStateChangeCallback(func(name string, from, to CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, struct {
			name     string
			from, to CircuitState
		}{name, from, to})
	})

	// Two failures should trigger closed -> open
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("error") })
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, transitions, 1)
	assert.Equal(t, "cb-callback", transitions[0].name)
	assert.Equal(t, StateClosed, transitions[0].from)
	assert.Equal(t, StateOpen, transitions[0].to)
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "stats-test",
		MaxFailures:  10,
		ResetTimeout: 30 * time.Second,
	})

	// Cause one failure
	_ = cb.Execute(func() error { return fmt.Errorf("error") })

	stats := cb.GetStats()
	assert.Equal(t, "stats-test", stats["name"])
	assert.Equal(t, "closed", stats["state"])
	assert.Equal(t, 1, stats["failures"])
	assert.Equal(t, 10, stats["max_failures"])
	assert.Equal(t, 30*time.Second, stats["reset_timeout"])
}

func TestCircuitBreaker_Reset(t *testing.T) {
	logger := newTestLogger()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "reset-test",
		MaxFailures:  1,
		ResetTimeout: 10 * time.Second,
		Logger:       logger,
	})

	// Trip breaker
	_ = cb.Execute(func() error { return fmt.Errorf("error") })
	assert.Equal(t, StateOpen, cb.GetState())

	// Reset
	cb.Reset()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, 0, cb.GetFailures())
}

func TestCircuitBreakerManager_GetOrCreate(t *testing.T) {
	logger := newTestLogger()
	mgr := NewCircuitBreakerManager(logger)

	cb1 := mgr.GetOrCreate("breaker-a", CircuitBreakerConfig{MaxFailures: 3})
	require.NotNil(t, cb1)
	assert.Equal(t, "breaker-a", cb1.name)

	// Second call with same name returns same instance
	cb2 := mgr.GetOrCreate("breaker-a", CircuitBreakerConfig{MaxFailures: 10})
	assert.Equal(t, cb1, cb2)

	// Different name creates a different breaker
	cb3 := mgr.GetOrCreate("breaker-b", CircuitBreakerConfig{MaxFailures: 7})
	assert.NotEqual(t, cb1, cb3)
	assert.Equal(t, "breaker-b", cb3.name)
}

func TestCircuitBreakerManager_Get(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	// Not found returns nil
	assert.Nil(t, mgr.Get("nonexistent"))

	// Create one, then find it
	mgr.GetOrCreate("existing", CircuitBreakerConfig{MaxFailures: 3})
	cb := mgr.Get("existing")
	require.NotNil(t, cb)
	assert.Equal(t, "existing", cb.name)
}

func TestCircuitBreakerManager_GetAll(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	mgr.GetOrCreate("a", CircuitBreakerConfig{})
	mgr.GetOrCreate("b", CircuitBreakerConfig{})
	mgr.GetOrCreate("c", CircuitBreakerConfig{})

	all := mgr.GetAll()
	assert.Len(t, all, 3)
	assert.Contains(t, all, "a")
	assert.Contains(t, all, "b")
	assert.Contains(t, all, "c")
}

func TestCircuitBreakerManager_GetStats(t *testing.T) {
	mgr := NewCircuitBreakerManager(nil)

	cb := mgr.GetOrCreate("stats-breaker", CircuitBreakerConfig{MaxFailures: 5})
	_ = cb.Execute(func() error { return fmt.Errorf("error") })

	stats := mgr.GetStats()
	require.Contains(t, stats, "stats-breaker")

	breakerStats, ok := stats["stats-breaker"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "stats-breaker", breakerStats["name"])
	assert.Equal(t, 1, breakerStats["failures"])
}

func TestCircuitBreakerManager_Reset(t *testing.T) {
	logger := newTestLogger()
	mgr := NewCircuitBreakerManager(logger)

	cb1 := mgr.GetOrCreate("r1", CircuitBreakerConfig{MaxFailures: 1})
	cb2 := mgr.GetOrCreate("r2", CircuitBreakerConfig{MaxFailures: 1})

	// Trip both breakers
	_ = cb1.Execute(func() error { return fmt.Errorf("error") })
	_ = cb2.Execute(func() error { return fmt.Errorf("error") })
	assert.Equal(t, StateOpen, cb1.GetState())
	assert.Equal(t, StateOpen, cb2.GetState())

	// Reset all
	mgr.Reset()
	assert.Equal(t, StateClosed, cb1.GetState())
	assert.Equal(t, StateClosed, cb2.GetState())
}
