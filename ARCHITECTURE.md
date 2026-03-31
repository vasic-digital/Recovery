# Architecture -- Recovery

## Purpose

Generic, reusable Go module for application-level fault tolerance. Provides named circuit breakers with structured logging and state-change callbacks, periodic health checking with status transition tracking, and a unified resilience facade composing both.

## Structure

```
pkg/
  breaker/   Named circuit breaker with logging, callbacks, statistics, and CircuitBreakerManager registry
  health/    Periodic health checker polling a CheckFunc on configurable intervals with status tracking
  facade/    Resilience struct composing breaker manager and health checkers behind a single API
```

## Key Components

- **`breaker.CircuitBreaker`** -- Wraps `digital.vasic.concurrency` breaker with structured logging, state-change callbacks, and statistics (total calls, failures, successes)
- **`breaker.CircuitBreakerManager`** -- Thread-safe registry for named breakers with GetOrCreate semantics
- **`health.HealthChecker`** -- Periodic poller that calls a CheckFunc at intervals and tracks status transitions (unknown, healthy, unhealthy)
- **`facade.Resilience`** -- Unified API: GetOrCreateBreaker, AddHealthCheck, Execute (through circuit breaker), Stats, Stop

## Data Flow

```
facade.Execute("payment-api", fn) -> manager.GetOrCreate("payment-api") -> breaker.Execute(fn)
    |
    Closed state: run fn -> success/failure counter
    Open state: reject immediately -> ErrCircuitOpen
    HalfOpen state: allow limited requests -> transition based on result

facade.AddHealthCheck("database", checkFn, 10s) -> health.NewChecker(checkFn, 10s)
    |
    periodic: checkFn() -> healthy/unhealthy -> status transition callback
```

## Dependencies

- `digital.vasic.concurrency` -- Base circuit breaker implementation
- `github.com/stretchr/testify` -- Test assertions

## Testing Strategy

Table-driven tests with `testify` and race detection. Tests cover circuit breaker state transitions, named breaker registry, health checker polling with status transitions, facade composition, and concurrent access patterns.
