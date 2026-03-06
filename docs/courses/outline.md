# Course: Fault Tolerance in Go with Recovery

Learn to build resilient services using `digital.vasic.recovery` -- circuit breakers, health checks, and the Resilience facade.

## Lessons

1. **Circuit Breakers** -- The `CircuitBreaker` struct, state machine (Closed/Open/HalfOpen), configuration, and the `CircuitBreakerManager` registry.
2. **Health Checking** -- The `Checker` type, periodic polling, status transitions, and structured logging.
3. **The Resilience Facade** -- Combining breakers and health checks behind a single API, stats aggregation, and graceful shutdown.
