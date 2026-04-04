# User Guide — Recovery

`digital.vasic.recovery` is a generic, reusable Go module for application-level fault tolerance.
It provides named circuit breakers with structured logging and state-change callbacks, periodic
health checking with status transition tracking, and a unified resilience facade that composes
both. The module wraps `digital.vasic.concurrency`'s lower-level circuit breaker engine and adds
an application-oriented API surface with registries, observers, and a clean facade pattern.

## Installation

```bash
go get digital.vasic.recovery
```

If you are working within the Catalogizer monorepo, the module is resolved via a `replace`
directive in `catalog-api/go.mod`:

```
replace digital.vasic.recovery => ../Recovery
```

## Package Overview

| Package | Import Path | Purpose |
|---------|-------------|---------|
| `breaker` | `digital.vasic.recovery/pkg/breaker` | Named circuit breakers with logging, callbacks, statistics, and a registry |
| `health` | `digital.vasic.recovery/pkg/health` | Periodic health checks with configurable interval and status tracking |
| `facade` | `digital.vasic.recovery/pkg/facade` | Unified resilience API composing breakers and health checkers |

---

## Quick Start

### pkg/breaker — Circuit Breaker

Create a standalone circuit breaker and wrap function calls with fault tolerance:

```go
package main

import (
    "errors"
    "fmt"
    "time"

    "digital.vasic.recovery/pkg/breaker"
)

// Implement the Logger interface with your preferred logging library.
type stdLogger struct{}

func (l *stdLogger) Info(msg string, kv ...interface{}) { fmt.Println("INFO", msg, kv) }
func (l *stdLogger) Warn(msg string, kv ...interface{}) { fmt.Println("WARN", msg, kv) }
func (l *stdLogger) Debug(msg string, kv ...interface{}) { fmt.Println("DEBUG", msg, kv) }

func main() {
    logger := &stdLogger{}

    cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
        Name:         "payment-api",
        MaxFailures:  3,
        ResetTimeout: 30 * time.Second,
        Logger:       logger,
    })

    err := cb.Execute(func() error {
        // Call external service here.
        return callPaymentAPI()
    })
    if err != nil {
        fmt.Println("call failed or circuit is open:", err)
    }

    fmt.Println("state:", cb.GetState())    // "closed", "half-open", or "open"
    fmt.Println("failures:", cb.GetFailures())
}
```

Use `CircuitBreakerManager` when you need multiple named breakers across your application:

```go
manager := breaker.NewCircuitBreakerManager(logger)

// GetOrCreate is idempotent — safe to call from multiple goroutines.
cb := manager.GetOrCreate("smb-nas", breaker.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 60 * time.Second,
})

_ = cb.Execute(func() error {
    return readFromNAS()
})

// Inspect all registered breakers.
for name, stats := range manager.GetStats() {
    fmt.Printf("breaker %q: %v\n", name, stats)
}
```

### pkg/health — Health Checker

Create a periodic health check against any dependency:

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "digital.vasic.recovery/pkg/health"
)

func main() {
    db, _ := sql.Open("sqlite3", "catalogizer.db")

    checker := health.NewChecker("database", func() error {
        return db.Ping()
    }, 10*time.Second)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    checker.Start(ctx) // Runs immediately, then every 10 s in background.

    time.Sleep(100 * time.Millisecond)

    switch checker.Status() {
    case health.StatusHealthy:
        fmt.Println("database is healthy")
    case health.StatusUnhealthy:
        fmt.Println("database is unhealthy:", checker.LastError())
    case health.StatusUnknown:
        fmt.Println("check has not run yet")
    }

    checker.Stop()
}
```

### pkg/facade — Resilience Facade

For most applications, use the `Resilience` facade which manages both breakers and health checks
behind a single entry point:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "digital.vasic.recovery/pkg/breaker"
    "digital.vasic.recovery/pkg/facade"
    "digital.vasic.recovery/pkg/health"
)

func main() {
    logger := &myLogger{}
    r := facade.New(logger)
    defer r.Stop()

    // Register a health check.
    r.AddHealthCheck("redis", func() error {
        return pingRedis()
    }, 15*time.Second)

    // Execute a function through a named circuit breaker.
    err := r.Execute("catalog-api", func() error {
        return fetchCatalogData()
    })
    if err != nil {
        fmt.Println("catalog-api unavailable:", err)
    }

    // Retrieve aggregated stats.
    stats := r.Stats()
    fmt.Printf("breakers: %v\n", stats["breakers"])
    fmt.Printf("health:   %v\n", stats["health"])
}
```

---

## Advanced Usage

### State Change Callbacks

Register an Observer callback to react to circuit breaker state transitions. This is useful for
emitting metrics, alerting, or automatically pausing downstream work:

```go
cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
    Name:         "smb-nas",
    MaxFailures:  5,
    ResetTimeout: 60 * time.Second,
    Logger:       logger,
})

cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    fmt.Printf("[%s] state: %s -> %s\n", name, from, to)

    // Example: emit a Prometheus counter on state change.
    circuitStateChanges.WithLabelValues(name, from.String(), to.String()).Inc()

    // Example: page on-call when circuit opens.
    if to == breaker.StateOpen {
        alertOncall(name)
    }
})
```

### Health-Driven Breaker Decisions

Combine health checks with circuit breakers to prevent calls to known-unhealthy dependencies:

```go
r := facade.New(logger)
defer r.Stop()

// Track database health.
var dbHealthy = true
r.AddHealthCheck("database", func() error {
    err := db.Ping()
    dbHealthy = (err == nil)
    return err
}, 5*time.Second)

// Gate execution on health status.
err := r.Execute("database", func() error {
    if !dbHealthy {
        return fmt.Errorf("skipping: database known unhealthy")
    }
    return runQuery(db, "SELECT ...")
})
```

Alternatively, retrieve the health checker status directly from the stats map:

```go
stats := r.Stats()
healthMap := stats["health"].(map[string]interface{})
dbHealth := healthMap["database"].(map[string]interface{})
if dbHealth["status"] == "unhealthy" {
    log.Println("database unhealthy — skipping batch job")
    return
}
```

### Accessing a Named Breaker Directly

When you need fine-grained control (for example to read stats or register a callback) after
creating a breaker through the facade, retrieve it from the embedded manager:

```go
cb := r.GetOrCreateBreaker("smb-nas", breaker.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 30 * time.Second,
})

cb.SetStateChangeCallback(func(name string, from, to breaker.CircuitState) {
    // React to state transitions.
})

_ = r.Execute("smb-nas", func() error {
    return readSMBFile("/mnt/nas/media/movie.mkv")
})
```

### Graceful Shutdown

Always call `Resilience.Stop()` (or use `defer`) to cancel background health-check goroutines
and release resources:

```go
r := facade.New(logger)
defer r.Stop()  // Cancels all health-check goroutines.
```

For standalone `health.Checker` instances, call `Stop()` directly:

```go
ctx, cancel := context.WithCancel(context.Background())
checker := health.NewChecker("redis", pingRedis, 10*time.Second)
checker.Start(ctx)

// On shutdown:
cancel()       // Stops via context cancellation.
checker.Stop() // Also closes the internal stop channel.
```

---

## Integration with Catalogizer

### SMB Retry and Circuit Breaker

In `catalog-api/internal/smb/`, SMB connections already use exponential backoff. Wrap SMB
calls with a circuit breaker to fast-fail during sustained NAS outages:

```go
// In your SMB client setup:
r := facade.New(logger)

err := r.Execute("synology-nas", func() error {
    return smbClient.ReadDir("/media/movies")
})
if err != nil {
    // Circuit is open — serve from offline cache instead.
    return offlineCache.GetDir("/media/movies")
}
```

### API Circuit Breakers

Protect outbound calls to metadata providers (TMDB, MusicBrainz, OpenLibrary) with per-provider
circuit breakers so a single unavailable provider does not block the full aggregation pipeline:

```go
const (
    tmdbBreaker        = "tmdb"
    musicBrainzBreaker = "musicbrainz"
    openLibraryBreaker = "openlibrary"
)

// Create breakers once at startup.
r := facade.New(logger)
defer r.Stop()

// TMDB: fail fast after 3 errors, probe every 2 minutes.
r.GetOrCreateBreaker(tmdbBreaker, breaker.CircuitBreakerConfig{
    MaxFailures:  3,
    ResetTimeout: 2 * time.Minute,
})

// In the aggregation pipeline:
var metadata *TMDBMetadata
err := r.Execute(tmdbBreaker, func() error {
    var e error
    metadata, e = tmdbClient.GetMovie(ctx, imdbID)
    return e
})
if err != nil {
    logger.Warn("TMDB unavailable, skipping metadata enrichment", "err", err)
    // Graceful degradation — continue without metadata.
}
```

---

## Best Practices

- **Name breakers meaningfully**: use the dependency name (e.g. `"synology-nas"`, `"tmdb"`,
  `"postgres"`) so logs and stats are self-documenting.
- **Tune thresholds per dependency**: a local Postgres should tolerate fewer failures before
  opening than a remote third-party API.
- **Use the facade for most cases**: `facade.Resilience` manages lifetimes for you. Reach into
  `pkg/breaker` or `pkg/health` directly only when you need lower-level control.
- **Always stop health checkers**: leaked goroutines from health checkers will hold resources.
  Use `defer r.Stop()` at the construction site.
- **Log at the right level**: the `Logger` interface emits `Debug` for successes, `Warn` for
  failures, and `Info` for state transitions. Wire it to your application logger to preserve
  context fields.
- **Test with race detector**: run `go test -race ./...` — all concurrent access in this module
  is guarded by mutexes, and tests verify this.
- **Do not share `CircuitBreakerConfig` instances**: `GetOrCreate` reads the config once at
  creation time; modifying a config struct after passing it has no effect.

---

## FAQ

**Q: What error is returned when a circuit is open?**

The underlying `digital.vasic.concurrency` breaker returns `vasicbreaker.ErrOpen`. This error
propagates unchanged through `CircuitBreaker.Execute` and `Resilience.Execute`.

**Q: Can I reset a breaker programmatically?**

Yes. Call `cb.Reset()` on a `*CircuitBreaker`, or `manager.Reset()` to reset all managed
breakers at once. The `Resilience` facade does not expose a `Reset` method directly; obtain the
breaker via `GetOrCreateBreaker` first.

**Q: Is `CircuitBreakerManager` goroutine-safe?**

Yes. All reads and writes to the internal breaker map are protected by a `sync.RWMutex`. It is
safe to call `GetOrCreate`, `Get`, `GetAll`, `GetStats`, and `Reset` from multiple goroutines
concurrently.

**Q: Does `health.Checker` block on `Start`?**

No. `Start` runs the first check synchronously, then launches a background goroutine for
subsequent polls. The call returns immediately after the first check completes.

**Q: What happens if I call `Stop` twice on a `health.Checker`?**

`Stop` is idempotent. The second call is a no-op because it reads the channel before closing.

**Q: Can I replace the logger after construction?**

For `health.Checker`, yes — call `SetLogger` at any time (it is mutex-protected). For
`CircuitBreaker` and `CircuitBreakerManager`, the logger is set at construction and is not
replaceable.
