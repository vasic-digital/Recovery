# Course: Fault Tolerance with Circuit Breakers and Health Checks in Go

## Module Overview

This course covers the `digital.vasic.recovery` module, which provides application-level fault tolerance primitives. You will learn to wrap circuit breakers with structured logging and callbacks, build periodic health checkers, and unify both behind a single Resilience facade. The module builds on `digital.vasic.concurrency/pkg/breaker` as its low-level engine.

## Prerequisites

- Intermediate Go knowledge (interfaces, goroutines, sync primitives)
- Understanding of circuit breaker pattern concepts
- Familiarity with health check patterns
- Go 1.24+ installed

## Lessons

| # | Title | Duration |
|---|-------|----------|
| 1 | Named Circuit Breakers with Logging and Callbacks | 40 min |
| 2 | Periodic Health Checking | 35 min |
| 3 | The Resilience Facade | 35 min |

## Source Files

- `pkg/breaker/` -- Named circuit breaker, manager registry, state-change callbacks
- `pkg/health/` -- Periodic health checker with status transitions
- `pkg/facade/` -- Resilience struct unifying breakers and health checks
