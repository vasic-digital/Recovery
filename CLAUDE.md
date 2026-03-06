# CLAUDE.md - Recovery Module

## Overview

`digital.vasic.recovery` is a generic, reusable Go module for application-level fault tolerance. It provides named circuit breakers, periodic health checking, and a unified resilience facade.

**Module**: `digital.vasic.recovery` (Go 1.24+)
**Depends on**: `digital.vasic.concurrency`

## Build & Test

```bash
go build ./...
go test ./... -count=1 -race
go test ./... -short              # Unit tests only
go test -bench=. ./...            # Benchmarks
```

## Code Style

- Standard Go conventions, `gofmt` formatting
- Imports grouped: stdlib, third-party, internal (blank line separated)
- Line length <= 100 chars
- Naming: `camelCase` private, `PascalCase` exported, acronyms all-caps
- Errors: always check, wrap with `fmt.Errorf("...: %w", err)`
- Tests: table-driven, `testify`, naming `Test<Struct>_<Method>_<Scenario>`

## Package Structure

| Package | Purpose |
|---------|---------|
| `pkg/breaker` | Named circuit breaker with logging, callbacks, statistics, and registry |
| `pkg/health` | Periodic health checker with configurable interval and status tracking |
| `pkg/facade` | Unified resilience API composing breakers and health checkers |

## Key Interfaces

- `Logger` (in breaker and health) -- Structured logging (Info, Warn, Debug)
- `CheckFunc` -- Health probe function returning nil (healthy) or error
- `CircuitBreaker` -- Execute, GetState, GetStats, Reset, SetStateChangeCallback
- `Resilience` -- Facade: GetOrCreateBreaker, AddHealthCheck, Execute, Stats, Stop

## Design Patterns

- **Decorator**: CircuitBreaker wraps concurrency breaker with logging, callbacks, stats
- **Registry**: CircuitBreakerManager maintains named breakers with GetOrCreate
- **Observer**: State-change callbacks and health status transitions
- **Facade**: Resilience unifies breaker management and health checking

## Commit Style

Conventional Commits: `feat(breaker): add exponential backoff`
