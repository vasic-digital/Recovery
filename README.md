# Recovery

Generic, reusable Go module for application-level fault tolerance. Provides named circuit breakers with structured logging, state-change callbacks, periodic health checking, and a unified resilience facade.

**Module**: `digital.vasic.recovery`

## Packages

- **pkg/breaker** -- Named circuit breaker with logging, callbacks, and statistics. `CircuitBreakerManager` provides a thread-safe registry for looking up or creating breakers by name.
- **pkg/health** -- Periodic health checker that polls a `CheckFunc` on a configurable interval and tracks status transitions (unknown, healthy, unhealthy).
- **pkg/facade** -- `Resilience` struct that composes breaker manager and health checkers behind a single API.

## Quick Start

```go
import (
    "digital.vasic.recovery/pkg/breaker"
    "digital.vasic.recovery/pkg/facade"
)

res := facade.New(nil) // nil logger = silent
defer res.Stop()

// Register a health check
res.AddHealthCheck("database", func() error {
    return db.Ping()
}, 10*time.Second)

// Execute through circuit breaker
err := res.Execute("payment-api", func() error {
    resp, err := http.Get("https://api.example.com/charge")
    if err != nil { return err }
    resp.Body.Close()
    return nil
})
```

## Testing

```bash
go test ./... -count=1 -race
```
