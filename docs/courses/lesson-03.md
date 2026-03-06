# Lesson 3: The Resilience Facade

## Objectives

- Use the `Resilience` struct to manage breakers and health checks together
- Aggregate statistics from all components
- Implement graceful shutdown

## Concepts

### The Facade Pattern

`facade.Resilience` composes `CircuitBreakerManager` and a map of `health.Checker` instances. Callers interact with a single struct instead of managing breakers and checkers separately.

### API Surface

```go
func New(logger breaker.Logger) *Resilience
func (r *Resilience) GetOrCreateBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker
func (r *Resilience) AddHealthCheck(name string, check CheckFunc, interval time.Duration)
func (r *Resilience) Execute(breakerName string, fn func() error) error
func (r *Resilience) Stats() map[string]interface{}
func (r *Resilience) Stop()
```

## Code Walkthrough

### Initialization

```go
res := facade.New(myLogger)
defer res.Stop() // cancels all health checkers
```

`New` creates an internal `context.WithCancel` that governs all health checker goroutines.

### Registering components

```go
// Health checks
res.AddHealthCheck("postgres", dbPingFn, 10*time.Second)
res.AddHealthCheck("redis", redisPingFn, 15*time.Second)

// Circuit breakers with custom config
res.GetOrCreateBreaker("payment", breaker.CircuitBreakerConfig{
    MaxFailures: 3, ResetTimeout: 30 * time.Second,
})
```

If you call `AddHealthCheck` with a name that already exists, the previous checker is stopped and replaced.

### Executing protected calls

```go
err := res.Execute("payment", func() error {
    return chargeCustomer(amount)
})
```

If `"payment"` has no breaker yet, one is created with default config.

### Aggregating stats

```go
stats := res.Stats()
```

Returns:

```json
{
    "breakers": {
        "payment": {"name":"payment", "state":"closed", "failures":0, ...}
    },
    "health": {
        "postgres": {"status":"healthy", "last_check":"2026-03-06T12:00:00Z"},
        "redis": {"status":"healthy", "last_check":"2026-03-06T12:00:00Z"}
    }
}
```

### Graceful shutdown

```go
res.Stop()
```

This cancels the internal context (stopping all health checker goroutines) and explicitly calls `Stop()` on each checker.

## Summary

The Resilience facade simplifies fault tolerance by providing a single API for circuit breakers and health checks. Use `Stats()` to expose system health and `Stop()` for clean shutdown.
