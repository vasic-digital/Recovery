# Getting Started

## Install

```bash
go get digital.vasic.recovery
```

## Using the Resilience Facade

The simplest way to use the module is through the `facade.Resilience` API, which manages both circuit breakers and health checks.

```go
import (
    "digital.vasic.recovery/pkg/breaker"
    "digital.vasic.recovery/pkg/facade"
)

res := facade.New(myLogger) // pass nil for silent operation
defer res.Stop()
```

### Add a health check

```go
res.AddHealthCheck("redis", func() error {
    return redisClient.Ping(ctx).Err()
}, 15*time.Second)
```

The checker runs immediately, then repeats every 15 seconds. It tracks transitions between `StatusUnknown`, `StatusHealthy`, and `StatusUnhealthy`.

### Execute through a circuit breaker

```go
err := res.Execute("external-api", func() error {
    resp, err := http.Get("https://api.example.com/data")
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    // process response
    return nil
})
```

If the breaker does not exist, `Execute` creates one with default configuration (5 max failures, 60s reset timeout).

### Create a breaker with custom config

```go
res.GetOrCreateBreaker("payment-api", breaker.CircuitBreakerConfig{
    MaxFailures:  3,
    ResetTimeout: 30 * time.Second,
})
```

### Inspect stats

```go
stats := res.Stats()
// {"breakers": {"payment-api": {...}}, "health": {"redis": {"status": "healthy", ...}}}
```

## Using Breakers Directly

For finer control, use the `breaker` package directly.

```go
import "digital.vasic.recovery/pkg/breaker"

mgr := breaker.NewCircuitBreakerManager(myLogger)
cb := mgr.GetOrCreate("my-service", breaker.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 60 * time.Second,
})

cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    log.Printf("Breaker %s: %s -> %s", name, from, to)
})

err := cb.Execute(func() error {
    return callExternalService()
})
```

### Circuit States

| State | Behavior |
|-------|----------|
| `StateClosed` | Normal operation. Requests pass through. Failures are counted. |
| `StateOpen` | Requests are rejected immediately. Resets after `ResetTimeout`. |
| `StateHalfOpen` | One probe request is allowed. Success closes the circuit; failure reopens it. |
