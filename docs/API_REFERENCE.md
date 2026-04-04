# API Reference — Recovery

Module: `digital.vasic.recovery` | Requires: Go 1.25+

---

## Package `digital.vasic.recovery/pkg/breaker`

Provides a named circuit breaker with structured logging, state-change callbacks, statistics,
and a thread-safe registry.

---

### Constants

#### `CircuitState`

```go
type CircuitState int
```

Represents the operational state of a circuit breaker.

| Constant | Value | Description |
|----------|-------|-------------|
| `StateClosed` | `0` | Normal operation — all requests pass through |
| `StateHalfOpen` | `1` | Probing state — a limited number of requests pass through to test recovery |
| `StateOpen` | `2` | Failing state — all requests are rejected immediately without calling the wrapped function |

```go
func (s CircuitState) String() string
```

Returns a human-readable state name: `"closed"`, `"half-open"`, or `"open"`.

---

### Interfaces

#### `Logger`

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Warn(msg string, keysAndValues ...interface{})
    Debug(msg string, keysAndValues ...interface{})
}
```

Structured logger interface that decouples this package from any specific logging implementation.
Implementations must accept a message string followed by alternating key/value pairs.

The circuit breaker emits log events at these levels:

| Level | Trigger |
|-------|---------|
| `Debug` | Successful `Execute` call |
| `Warn` | Failed `Execute` call |
| `Info` | State transition (closed ↔ half-open ↔ open) |
| `Info` | Manual `Reset()` |

---

### Types

#### `CircuitBreakerConfig`

```go
type CircuitBreakerConfig struct {
    Name         string
    MaxFailures  int
    ResetTimeout time.Duration
    Logger       Logger
}
```

Configuration for a `CircuitBreaker`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Name` | `string` | — | Human-readable identifier used in logs and stats |
| `MaxFailures` | `int` | `5` | Number of consecutive failures before the circuit opens |
| `ResetTimeout` | `time.Duration` | `60s` | Duration the circuit stays open before transitioning to half-open |
| `Logger` | `Logger` | `nil` | Structured logger; if nil, logging is disabled |

---

#### `CircuitBreaker`

```go
type CircuitBreaker struct { ... }
```

Wraps `digital.vasic.concurrency/pkg/breaker.CircuitBreaker` with application-level features:
structured logging, named identification, and state-change observer callbacks.

##### `NewCircuitBreaker`

```go
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker
```

Creates a new circuit breaker. `MaxFailures <= 0` defaults to `5`.
`ResetTimeout <= 0` defaults to `60s`.

```go
cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
    Name:         "payment-api",
    MaxFailures:  3,
    ResetTimeout: 30 * time.Second,
    Logger:       logger,
})
```

---

##### `Execute`

```go
func (cb *CircuitBreaker) Execute(fn func() error) error
```

Wraps `fn` with circuit breaker protection.

- **Closed**: calls `fn`; increments failure counter on error.
- **Open**: returns the upstream `ErrOpen` error immediately without calling `fn`.
- **HalfOpen**: calls `fn`; success transitions to closed, failure returns to open.

Logs success at `Debug` and failure at `Warn`. Logs and fires the state-change callback when
the state transitions.

Returns the error from `fn`, or the circuit-open error from the underlying concurrency module.

```go
err := cb.Execute(func() error {
    return callExternalService()
})
if err != nil {
    // Either the function failed, or the circuit is open.
}
```

---

##### `GetState`

```go
func (cb *CircuitBreaker) GetState() CircuitState
```

Returns the current `CircuitState` (`StateClosed`, `StateHalfOpen`, or `StateOpen`).

```go
if cb.GetState() == breaker.StateOpen {
    log.Println("circuit is open — serving from cache")
}
```

---

##### `GetFailures`

```go
func (cb *CircuitBreaker) GetFailures() int
```

Returns the current consecutive failure count. Resets to `0` when the circuit closes.

---

##### `GetStats`

```go
func (cb *CircuitBreaker) GetStats() map[string]interface{}
```

Returns a snapshot of circuit breaker statistics.

| Key | Type | Description |
|-----|------|-------------|
| `"name"` | `string` | Breaker name |
| `"state"` | `string` | Current state as a string |
| `"failures"` | `int` | Current consecutive failure count |
| `"max_failures"` | `int` | Configured failure threshold |
| `"reset_timeout"` | `time.Duration` | Configured reset timeout |

```go
stats := cb.GetStats()
fmt.Printf("state=%s failures=%d\n", stats["state"], stats["failures"])
```

---

##### `Reset`

```go
func (cb *CircuitBreaker) Reset()
```

Forces the circuit breaker back to the closed state, clearing the failure counter. Logs the
manual reset at `Info` level.

```go
cb.Reset() // Use after a known transient outage is resolved.
```

---

##### `SetStateChangeCallback`

```go
func (cb *CircuitBreaker) SetStateChangeCallback(callback func(name string, from, to CircuitState))
```

Registers an Observer callback invoked synchronously on every state transition. The callback
receives the breaker name and the previous and new states.

Only one callback can be registered. Calling `SetStateChangeCallback` a second time replaces
the first.

```go
cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    metrics.RecordStateChange(name, from.String(), to.String())
    if to == breaker.StateOpen {
        alert.Page("circuit open: " + name)
    }
})
```

---

#### `CircuitBreakerManager`

```go
type CircuitBreakerManager struct { ... }
```

Thread-safe Registry that manages a set of named circuit breakers.

##### `NewCircuitBreakerManager`

```go
func NewCircuitBreakerManager(logger Logger) *CircuitBreakerManager
```

Creates a new manager with an empty registry. The `logger` is inherited by breakers that do not
supply their own logger in `CircuitBreakerConfig`.

```go
manager := breaker.NewCircuitBreakerManager(logger)
```

---

##### `GetOrCreate`

```go
func (m *CircuitBreakerManager) GetOrCreate(name string, config CircuitBreakerConfig) *CircuitBreaker
```

Retrieves the named breaker if it exists, or creates and registers a new one. The `config.Name`
field is overwritten with `name`. Safe to call concurrently.

```go
cb := manager.GetOrCreate("smb-nas", breaker.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 60 * time.Second,
})
```

---

##### `Get`

```go
func (m *CircuitBreakerManager) Get(name string) *CircuitBreaker
```

Retrieves a breaker by name. Returns `nil` if no breaker with that name has been registered.

```go
if cb := manager.Get("smb-nas"); cb != nil {
    fmt.Println(cb.GetState())
}
```

---

##### `GetAll`

```go
func (m *CircuitBreakerManager) GetAll() map[string]*CircuitBreaker
```

Returns a shallow copy of the internal registry map. Safe to iterate without holding the lock.

---

##### `GetStats`

```go
func (m *CircuitBreakerManager) GetStats() map[string]interface{}
```

Returns aggregated statistics for all registered breakers, keyed by breaker name. Each value is
the `map[string]interface{}` returned by the individual breaker's `GetStats()`.

```go
for name, stats := range manager.GetStats() {
    fmt.Printf("%s: %v\n", name, stats)
}
```

---

##### `Reset`

```go
func (m *CircuitBreakerManager) Reset()
```

Resets all registered breakers to the closed state. Logs the bulk reset at `Info` level.

---

## Package `digital.vasic.recovery/pkg/health`

Provides periodic health checking with configurable polling intervals and status transition
tracking.

---

### Constants

#### `Status`

```go
type Status string
```

Represents the health status of a component.

| Constant | Value | Description |
|----------|-------|-------------|
| `StatusHealthy` | `"healthy"` | Last check succeeded |
| `StatusUnhealthy` | `"unhealthy"` | Last check returned an error |
| `StatusUnknown` | `"unknown"` | No check has completed yet |

---

### Interfaces

#### `Logger`

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Warn(msg string, keysAndValues ...interface{})
}
```

Structured logger interface. The health checker emits `Warn` on unhealthy results and `Info`
when the status recovers to healthy.

#### `CheckFunc`

```go
type CheckFunc func() error
```

A function that probes the health of a component. Return `nil` if the component is healthy, or
a descriptive error if it is not.

---

### Types

#### `Checker`

```go
type Checker struct { ... }
```

Periodically runs a `CheckFunc` and tracks its result. Starts in `StatusUnknown`.

##### `NewChecker`

```go
func NewChecker(name string, check CheckFunc, interval time.Duration) *Checker
```

Creates a new health checker. Does not start the background goroutine — call `Start` to begin
polling.

| Parameter | Description |
|-----------|-------------|
| `name` | Human-readable identifier for logs and registry lookups |
| `check` | The probe function to call on each polling cycle |
| `interval` | How often to call `check` after the first immediate invocation |

```go
checker := health.NewChecker("database", func() error {
    return db.Ping()
}, 10*time.Second)
```

---

##### `SetLogger`

```go
func (c *Checker) SetLogger(logger Logger)
```

Sets the logger after construction. Thread-safe; can be called at any time.

---

##### `Start`

```go
func (c *Checker) Start(ctx context.Context)
```

Begins periodic health checking. Runs the first check synchronously before returning, then
launches a background goroutine that polls at `interval`. The goroutine stops when either
`ctx` is cancelled or `Stop` is called. Non-blocking after the first check.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

checker.Start(ctx)
// First check has already run at this point.
```

---

##### `Stop`

```go
func (c *Checker) Stop()
```

Stops the background polling goroutine by closing the internal stop channel. Idempotent — safe
to call multiple times.

---

##### `Status`

```go
func (c *Checker) Status() Status
```

Returns the current `Status`. Thread-safe.

```go
if checker.Status() == health.StatusUnhealthy {
    log.Println("dependency is down:", checker.LastError())
}
```

---

##### `LastError`

```go
func (c *Checker) LastError() error
```

Returns the error from the most recent check, or `nil` if the last check succeeded or no check
has run yet. Thread-safe.

---

##### `LastCheck`

```go
func (c *Checker) LastCheck() time.Time
```

Returns the timestamp of the most recent check. Returns the zero `time.Time` if no check has
run yet. Thread-safe.

---

##### `Name`

```go
func (c *Checker) Name() string
```

Returns the checker's name as provided at construction.

---

## Package `digital.vasic.recovery/pkg/facade`

Provides a unified resilience API that composes circuit breakers and health checkers behind a
single entry point.

---

### Types

#### `Resilience`

```go
type Resilience struct { ... }
```

Unified fault tolerance facade. Owns a `CircuitBreakerManager` and a set of `health.Checker`
instances, all sharing a single cancellable context.

##### `New`

```go
func New(logger breaker.Logger) *Resilience
```

Creates a new `Resilience` facade with an internal context. The `logger` is passed to the
`CircuitBreakerManager` and is inherited by all breakers that do not supply their own logger.

```go
r := facade.New(logger)
defer r.Stop()
```

---

##### `GetOrCreateBreaker`

```go
func (r *Resilience) GetOrCreateBreaker(name string, cfg breaker.CircuitBreakerConfig) *breaker.CircuitBreaker
```

Retrieves or creates a named circuit breaker. Delegates to the embedded
`CircuitBreakerManager.GetOrCreate`. Useful when you need direct access to a breaker to register
a callback or read stats.

```go
cb := r.GetOrCreateBreaker("tmdb", breaker.CircuitBreakerConfig{
    MaxFailures:  3,
    ResetTimeout: 2 * time.Minute,
})
cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    metrics.RecordCircuitState(name, to.String())
})
```

---

##### `AddHealthCheck`

```go
func (r *Resilience) AddHealthCheck(name string, check health.CheckFunc, interval time.Duration)
```

Registers and immediately starts a periodic health check. If a checker with the same name
already exists, it is stopped before the new one is started. Thread-safe.

```go
r.AddHealthCheck("redis", func() error {
    return redisClient.Ping(context.Background()).Err()
}, 15*time.Second)
```

---

##### `Execute`

```go
func (r *Resilience) Execute(breakerName string, fn func() error) error
```

Runs `fn` through the named circuit breaker. If no breaker exists for `breakerName`, one is
created with default configuration (`MaxFailures=5`, `ResetTimeout=60s`). Returns the error
from `fn`, or the circuit-open error if the breaker is open.

```go
err := r.Execute("catalog-api", func() error {
    return client.FetchMediaList(ctx)
})
```

---

##### `Stats`

```go
func (r *Resilience) Stats() map[string]interface{}
```

Returns aggregated statistics for all circuit breakers and health checkers.

The returned map has two top-level keys:

| Key | Type | Description |
|-----|------|-------------|
| `"breakers"` | `map[string]interface{}` | Per-breaker stats from `CircuitBreakerManager.GetStats()` |
| `"health"` | `map[string]interface{}` | Per-checker snapshot with `"status"` and `"last_check"` |

```go
stats := r.Stats()

breakers := stats["breakers"].(map[string]interface{})
health   := stats["health"].(map[string]interface{})

for name, s := range health {
    entry := s.(map[string]interface{})
    fmt.Printf("health[%s]: status=%s last_check=%v\n",
        name, entry["status"], entry["last_check"])
}
```

---

##### `Stop`

```go
func (r *Resilience) Stop()
```

Cancels the internal context, which signals all health-check goroutines to exit. Also calls
`Stop()` on each registered `health.Checker` directly. Should be called (typically via `defer`)
when the application shuts down.

```go
r := facade.New(logger)
defer r.Stop() // Always stop to release goroutines.
```

---

## Error Reference

| Error | Source | Description |
|-------|--------|-------------|
| `vasicbreaker.ErrOpen` | `digital.vasic.concurrency/pkg/breaker` | Returned by `Execute` when the circuit is open. Propagated unchanged through `CircuitBreaker.Execute` and `Resilience.Execute`. |

Import the concurrency module to check for this specific error:

```go
import vasicbreaker "digital.vasic.concurrency/pkg/breaker"

err := cb.Execute(fn)
if errors.Is(err, vasicbreaker.ErrOpen) {
    // Circuit is open — handle fast-fail path.
}
```
