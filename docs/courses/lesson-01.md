# Lesson 1: Circuit Breakers

## Objectives

- Understand the circuit breaker state machine
- Create and configure named circuit breakers
- Use the `CircuitBreakerManager` registry
- Register state-change callbacks

## Concepts

### The State Machine

A circuit breaker has three states:

- **Closed** -- Normal operation. Every call passes through. Failures are counted.
- **Open** -- Too many failures. Calls are rejected immediately with an error.
- **HalfOpen** -- After `ResetTimeout`, one probe request is allowed. Success closes the circuit; failure reopens it.

### Configuration

```go
type CircuitBreakerConfig struct {
    Name         string
    MaxFailures  int           // default: 5
    ResetTimeout time.Duration // default: 60s
    Logger       Logger        // nil = silent
}
```

### The Registry

`CircuitBreakerManager` is a thread-safe `map[string]*CircuitBreaker`. Use `GetOrCreate` to obtain or create a breaker by name:

```go
mgr := breaker.NewCircuitBreakerManager(logger)
cb := mgr.GetOrCreate("service-x", config)
```

This ensures each service has exactly one breaker instance.

## Code Walkthrough

### Creating a breaker

```go
cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
    Name:         "payment-api",
    MaxFailures:  3,
    ResetTimeout: 30 * time.Second,
})
```

### Executing with protection

```go
err := cb.Execute(func() error {
    return callPaymentService()
})
```

The breaker wraps `digital.vasic.concurrency`'s engine, adding logging on every call and firing callbacks on state transitions.

### Observing state changes

```go
cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    metrics.Record("circuit_state_change", name, from.String(), to.String())
})
```

### Inspecting statistics

```go
stats := cb.GetStats()
// {"name":"payment-api", "state":"closed", "failures":0, "max_failures":3, "reset_timeout":"30s"}
```

### Resetting

```go
cb.Reset() // force back to closed state
mgr.Reset() // reset all managed breakers
```

## Summary

Circuit breakers prevent cascading failures by fast-failing when a dependency is unhealthy. The `breaker` package adds named registration, structured logging, and callbacks on top of the core engine.
