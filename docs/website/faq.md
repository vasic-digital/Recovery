# FAQ

## What happens when Execute is called with a breaker name that does not exist?

The `Resilience.Execute` method calls `GetOrCreate` internally, so a new breaker with default configuration (5 max failures, 60s reset timeout) is created automatically. To use custom settings, call `GetOrCreateBreaker` with your desired config before the first `Execute`.

## How does the circuit breaker reset work?

When consecutive failures reach `MaxFailures`, the circuit transitions to `StateOpen`. After `ResetTimeout` elapses, it moves to `StateHalfOpen`, allowing one probe request. If that request succeeds, the circuit closes (resets failures to zero). If it fails, the circuit reopens for another `ResetTimeout` period.

## Can I use my own logger?

Yes. Implement the `Logger` interface with `Info`, `Warn`, and `Debug` methods that accept a message and variadic key-value pairs:

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Warn(msg string, keysAndValues ...interface{})
    Debug(msg string, keysAndValues ...interface{})
}
```

Pass `nil` to suppress all logging.

## Is it safe to call Execute from multiple goroutines?

Yes. Both `CircuitBreaker` and `CircuitBreakerManager` use `sync.RWMutex` for thread safety. The underlying `digital.vasic.concurrency` breaker engine is also goroutine-safe.

## How do I monitor breaker and health check status?

Call `res.Stats()` to get a map with two keys: `"breakers"` (per-breaker name, state, failures, config) and `"health"` (per-checker name, status, last check time). Wire this into your `/health` or `/metrics` endpoint.
