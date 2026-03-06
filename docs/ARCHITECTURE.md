# Recovery -- Architecture

## Purpose

`digital.vasic.recovery` provides application-level fault tolerance primitives. It wraps the low-level circuit breaker engine from `digital.vasic.concurrency` with structured logging, named registration, state-change callbacks, and periodic health checking. The `facade` package unifies these concerns behind a single `Resilience` API so callers can add circuit breakers and health checks without managing multiple subsystems.

## Package Overview

| Package | Import Path | Responsibility |
|---------|-------------|----------------|
| `breaker` | `digital.vasic.recovery/pkg/breaker` | Named circuit breaker with logging, callbacks, and statistics. `CircuitBreakerManager` provides a thread-safe registry for looking up or creating breakers by name. |
| `health` | `digital.vasic.recovery/pkg/health` | Periodic health checker that polls a `CheckFunc` on a configurable interval and tracks status transitions (unknown, healthy, unhealthy). |
| `facade` | `digital.vasic.recovery/pkg/facade` | `Resilience` struct that composes `breaker.CircuitBreakerManager` and a map of `health.Checker` instances behind a single API. |

## Design Patterns

### Decorator (`breaker`)

`CircuitBreaker` decorates `digital.vasic.concurrency/pkg/breaker.CircuitBreaker` with additional behavior -- structured logging on every call, state-change callbacks, and statistics collection -- without altering the underlying breaker logic.

### Registry (`breaker`)

`CircuitBreakerManager` maintains a `map[string]*CircuitBreaker` protected by `sync.RWMutex`. Callers use `GetOrCreate(name, config)` to obtain a breaker; the manager either returns an existing instance or creates and registers a new one. This avoids scattered breaker construction and ensures a single source of truth.

### Observer (`breaker`, `health`)

- **breaker**: `SetStateChangeCallback` registers a function invoked whenever a circuit transitions between Closed, HalfOpen, and Open states, enabling external systems (metrics, alerting) to react.
- **health**: The `Checker` goroutine periodically runs a `CheckFunc` and logs status transitions, acting as an observable source of health events.

### Facade (`facade`)

`Resilience` hides the complexity of managing breakers and health checkers behind three methods: `GetOrCreateBreaker`, `AddHealthCheck`, and `Execute`. Callers do not need to import or coordinate the `breaker` and `health` packages directly.

## Dependency Diagram

```
+-----------------------------+
|       Consumer code         |
+-----------------------------+
              |
              | uses Resilience API
              v
+-----------------------------+
|     pkg/facade              |
|       Resilience            |
+-----------------------------+
        |              |
        v              v
+--------------+  +-------------+
| pkg/breaker  |  | pkg/health  |
| CBManager    |  | Checker     |
| CircuitBreak.|  |             |
+--------------+  +-------------+
        |
        | wraps (Decorator)
        v
+-----------------------------+
| digital.vasic.concurrency   |
| pkg/breaker                 |
| CircuitBreaker (engine)     |
+-----------------------------+
```

## Key Interfaces

### Logger (defined in both `breaker` and `health`)

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Warn(msg string, keysAndValues ...interface{})
    Debug(msg string, keysAndValues ...interface{})  // breaker only
}
```

Decouples the module from any specific logging framework. Pass any structured logger (zap, slog, zerolog) that satisfies this interface.

### CheckFunc (`health`)

```go
type CheckFunc func() error
```

A health probe function. Return `nil` for healthy, an error for unhealthy. The `Checker` calls this at the configured interval.

### CircuitBreaker public API (`breaker`)

```go
func (cb *CircuitBreaker) Execute(fn func() error) error
func (cb *CircuitBreaker) GetState() CircuitState        // Closed | HalfOpen | Open
func (cb *CircuitBreaker) GetFailures() int
func (cb *CircuitBreaker) GetStats() map[string]interface{}
func (cb *CircuitBreaker) Reset()
func (cb *CircuitBreaker) SetStateChangeCallback(fn func(name string, from, to CircuitState))
```

### Resilience public API (`facade`)

```go
func New(logger breaker.Logger) *Resilience
func (r *Resilience) GetOrCreateBreaker(name string, cfg breaker.CircuitBreakerConfig) *breaker.CircuitBreaker
func (r *Resilience) AddHealthCheck(name string, check health.CheckFunc, interval time.Duration)
func (r *Resilience) Execute(breakerName string, fn func() error) error
func (r *Resilience) Stats() map[string]interface{}
func (r *Resilience) Stop()
```

## Usage Example

```go
package main

import (
    "fmt"
    "net/http"
    "time"

    "digital.vasic.recovery/pkg/breaker"
    "digital.vasic.recovery/pkg/facade"
)

func main() {
    res := facade.New(nil) // nil logger = silent
    defer res.Stop()

    // Register a health check that polls every 10 seconds
    res.AddHealthCheck("database", func() error {
        // Ping database
        return nil
    }, 10*time.Second)

    // Create a circuit breaker for an external API
    res.GetOrCreateBreaker("payment-api", breaker.CircuitBreakerConfig{
        MaxFailures:  3,
        ResetTimeout: 30 * time.Second,
    })

    // Execute a call through the circuit breaker
    err := res.Execute("payment-api", func() error {
        resp, err := http.Get("https://api.example.com/charge")
        if err != nil {
            return err
        }
        resp.Body.Close()
        return nil
    })

    if err != nil {
        fmt.Println("Call failed or circuit open:", err)
    }

    // Inspect aggregated stats
    stats := res.Stats()
    fmt.Println(stats)
}
```
