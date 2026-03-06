# Lesson 2: Health Checking

## Objectives

- Create periodic health checkers
- Understand status transitions
- Integrate with structured logging

## Concepts

### The Checker

`health.Checker` runs a `CheckFunc` at a configurable interval and tracks status:

```go
type CheckFunc func() error
```

Return `nil` for healthy, an error for unhealthy.

### Status Values

| Status | Meaning |
|--------|---------|
| `StatusUnknown` | Initial state, no check has run yet |
| `StatusHealthy` | Last check returned nil |
| `StatusUnhealthy` | Last check returned an error |

### Lifecycle

1. `NewChecker(name, checkFn, interval)` -- creates the checker in `StatusUnknown`
2. `Start(ctx)` -- runs the check immediately, then repeats on interval
3. The checker logs warnings on failure and info on recovery
4. `Stop()` -- halts the ticker goroutine

## Code Walkthrough

### Creating a health checker

```go
checker := health.NewChecker("postgres", func() error {
    return db.PingContext(ctx)
}, 10*time.Second)
```

### Adding a logger

```go
checker.SetLogger(myLogger)
```

The logger receives structured messages:
- `"Health check failed"` with `"name"` and `"error"` keys on failure
- `"Health check recovered"` with `"name"` and `"previous_status"` on recovery

### Starting and querying

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

checker.Start(ctx)

// Query status at any time
status := checker.Status()     // StatusHealthy | StatusUnhealthy | StatusUnknown
lastErr := checker.LastError() // nil if healthy
lastCheck := checker.LastCheck() // time.Time of last run
```

### Stopping

```go
checker.Stop() // idempotent, safe to call multiple times
```

Cancelling the context also stops the checker.

## Practice Exercise

1. Create a health checker for a mock database that alternates between healthy and unhealthy every 2 checks. Set the interval to 100ms. After 1 second, verify `Status()` reflects the current state.
2. Set a logger and verify log messages include "Health check failed" on failure and "Health check recovered" on recovery transitions.
3. Test graceful shutdown: start a checker, cancel its context, and verify the checker stops polling. Call `Stop()` again to verify idempotency.
