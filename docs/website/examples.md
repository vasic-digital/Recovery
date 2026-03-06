# Examples

## 1. Circuit Breaker with State Callbacks

Monitor state transitions and integrate with metrics or alerting.

```go
package main

import (
    "fmt"
    "time"

    "digital.vasic.recovery/pkg/breaker"
)

func main() {
    mgr := breaker.NewCircuitBreakerManager(nil)

    cb := mgr.GetOrCreate("api-gateway", breaker.CircuitBreakerConfig{
        MaxFailures:  3,
        ResetTimeout: 10 * time.Second,
    })

    cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
        fmt.Printf("[%s] state changed: %s -> %s\n", name, from, to)
    })

    // Simulate failures
    for i := 0; i < 5; i++ {
        err := cb.Execute(func() error {
            return fmt.Errorf("connection refused")
        })
        fmt.Printf("call %d: err=%v, state=%s, failures=%d\n",
            i+1, err, cb.GetState(), cb.GetFailures())
    }

    // After max failures, the circuit opens and rejects immediately
    stats := cb.GetStats()
    fmt.Println("Stats:", stats)
}
```

## 2. Health Check with Logger

Periodic health checks with structured logging.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "digital.vasic.recovery/pkg/health"
)

type consoleLogger struct{}

func (l *consoleLogger) Info(msg string, kv ...interface{}) {
    fmt.Printf("INFO: %s %v\n", msg, kv)
}
func (l *consoleLogger) Warn(msg string, kv ...interface{}) {
    fmt.Printf("WARN: %s %v\n", msg, kv)
}

func main() {
    checker := health.NewChecker("database", func() error {
        // Replace with actual database ping
        return nil
    }, 5*time.Second)

    checker.SetLogger(&consoleLogger{})

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    checker.Start(ctx)
    defer checker.Stop()

    time.Sleep(12 * time.Second)
    fmt.Printf("Status: %s, Last check: %s\n",
        checker.Status(), checker.LastCheck().Format(time.RFC3339))
}
```

## 3. Full Resilience Setup

Combine breakers and health checks in a production service.

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
    res := facade.New(nil)
    defer res.Stop()

    // Health checks for infrastructure
    res.AddHealthCheck("postgres", func() error {
        return nil // db.Ping(ctx)
    }, 10*time.Second)

    res.AddHealthCheck("redis", func() error {
        return nil // redis.Ping(ctx)
    }, 10*time.Second)

    // Circuit breakers for external APIs
    res.GetOrCreateBreaker("payment", breaker.CircuitBreakerConfig{
        MaxFailures: 3, ResetTimeout: 30 * time.Second,
    })
    res.GetOrCreateBreaker("email", breaker.CircuitBreakerConfig{
        MaxFailures: 5, ResetTimeout: 60 * time.Second,
    })

    // Use in HTTP handler
    http.HandleFunc("/charge", func(w http.ResponseWriter, r *http.Request) {
        err := res.Execute("payment", func() error {
            // call payment API
            return nil
        })
        if err != nil {
            http.Error(w, "payment unavailable", http.StatusServiceUnavailable)
            return
        }
        fmt.Fprintln(w, "OK")
    })

    fmt.Println("Stats:", res.Stats())
}
```
