# Changelog — Recovery

All notable changes to `digital.vasic.recovery` are documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [1.1.0] - 2026-04-03

### Added

- Edge case tests for concurrent breaker access across all three packages.
- Coverage tests (`*_coverage_test.go`) for `pkg/breaker`, `pkg/health`, and `pkg/facade`
  exercising boundary conditions, nil-logger paths, and zero-value configuration defaults.
- Version alignment with Catalogizer v2.2.0 build 17.

### Changed

- Go toolchain requirement updated to Go 1.25 (from 1.24) to align with all other
  `digital.vasic.*` modules in the Catalogizer monorepo.

---

## [1.0.0] - 2026-03-06

### Added

- **`pkg/breaker`**: `CircuitBreaker` struct wrapping `digital.vasic.concurrency/pkg/breaker`
  with application-level structured logging and named identification.
- **`pkg/breaker`**: Configurable failure threshold (`MaxFailures`) and reset timeout
  (`ResetTimeout`) via `CircuitBreakerConfig`.
- **`pkg/breaker`**: `CircuitBreakerManager` providing a thread-safe named registry with
  `GetOrCreate` idempotency semantics.
- **`pkg/breaker`**: `GetState()`, `GetFailures()`, `GetStats()`, and `Reset()` introspection
  methods on `CircuitBreaker`.
- **`pkg/breaker`**: `SetStateChangeCallback` — Observer pattern hook invoked on every state
  transition, receiving the breaker name and the previous and new `CircuitState`.
- **`pkg/breaker`**: `CircuitState` type with constants `StateClosed`, `StateHalfOpen`,
  `StateOpen` and a `String()` method.
- **`pkg/breaker`**: `Logger` interface (`Info`, `Warn`, `Debug`) decoupling the package from
  any specific logging library.
- **`pkg/health`**: `Checker` struct for periodic health checking with configurable polling
  interval.
- **`pkg/health`**: `CheckFunc` type alias (`func() error`) for health probe functions.
- **`pkg/health`**: `Status` type with constants `StatusHealthy`, `StatusUnhealthy`,
  `StatusUnknown`.
- **`pkg/health`**: `Start(ctx context.Context)` — runs the first check synchronously, then
  launches a background goroutine for subsequent polls.
- **`pkg/health`**: `Stop()` — idempotent shutdown of the background goroutine.
- **`pkg/health`**: `Status()`, `LastError()`, `LastCheck()`, `Name()` accessors, all
  protected by `sync.RWMutex`.
- **`pkg/health`**: `SetLogger(Logger)` — post-construction logger injection.
- **`pkg/health`**: `Logger` interface (`Info`, `Warn`) for health-check log events.
- **`pkg/facade`**: `Resilience` struct composing a `CircuitBreakerManager` and a set of
  `health.Checker` instances behind a unified API.
- **`pkg/facade`**: `New(logger breaker.Logger) *Resilience` constructor with an internal
  cancellable context.
- **`pkg/facade`**: `GetOrCreateBreaker` — delegates to the embedded manager, returning a
  `*breaker.CircuitBreaker` for direct callback and stats access.
- **`pkg/facade`**: `AddHealthCheck` — registers and immediately starts a named health checker;
  replaces any existing checker with the same name.
- **`pkg/facade`**: `Execute(breakerName, fn)` — runs a function through a named (or
  auto-created) circuit breaker.
- **`pkg/facade`**: `Stats()` — aggregated snapshot of all breaker and health-checker state.
- **`pkg/facade`**: `Stop()` — cancels the internal context and calls `Stop()` on all health
  checkers.
- Full table-driven test suite with `github.com/stretchr/testify` across all three packages.
- Race-detector-clean concurrent access tests for `CircuitBreakerManager` and `Resilience`.
- `go.mod` with `replace digital.vasic.concurrency => ../Concurrency` for monorepo resolution.
- `ARCHITECTURE.md`, `CLAUDE.md`, `README.md`, `AGENTS.md` module documentation.
- `Upstreams/` configuration for multi-remote push (GitHub + GitLab).
