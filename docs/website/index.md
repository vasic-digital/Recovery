# digital.vasic.recovery

A Go module providing application-level fault tolerance primitives: named circuit breakers with logging and state-change callbacks, periodic health checkers, and a unified `Resilience` facade.

## Key Features

- **Named Circuit Breakers** -- Registry-based circuit breaker management with automatic creation, structured logging, and state-change callbacks
- **Health Checking** -- Periodic health probes with status tracking (healthy, unhealthy, unknown) and configurable intervals
- **Resilience Facade** -- Single entry point combining circuit breakers and health checkers behind a unified API
- **Structured Logging** -- Pluggable `Logger` interface compatible with zap, slog, zerolog, or any structured logger

## Installation

```bash
go get digital.vasic.recovery
```

Requires Go 1.24+. Depends on `digital.vasic.concurrency` for the underlying circuit breaker engine.

## Package Overview

| Package | Import Path | Purpose |
|---------|-------------|---------|
| `breaker` | `digital.vasic.recovery/pkg/breaker` | Named circuit breaker with logging, callbacks, statistics, and a thread-safe registry |
| `health` | `digital.vasic.recovery/pkg/health` | Periodic health checker with status transitions |
| `facade` | `digital.vasic.recovery/pkg/facade` | `Resilience` struct unifying breakers and health checks |

## Quick Example

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

    res.AddHealthCheck("database", func() error {
        return nil // ping your database here
    }, 10*time.Second)

    res.GetOrCreateBreaker("payment-api", breaker.CircuitBreakerConfig{
        MaxFailures:  3,
        ResetTimeout: 30 * time.Second,
    })

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

    fmt.Println(res.Stats())
}
```
