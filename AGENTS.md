# AGENTS.md - Recovery Module Multi-Agent Coordination Guide

## Overview

This document provides guidance for AI agents (Claude Code, Copilot, Cursor, etc.) working on the `digital.vasic.recovery` module. It defines responsibilities, boundaries, and coordination protocols to prevent conflicts when multiple agents operate concurrently.

## Module Identity

- **Module path**: `digital.vasic.recovery`
- **Language**: Go 1.24+
- **Dependencies**: `digital.vasic.concurrency`, `github.com/stretchr/testify`
- **Packages**: `pkg/breaker`, `pkg/health`, `pkg/facade`

## Package Ownership Boundaries

### `pkg/breaker` -- Named Circuit Breaker

- **Scope**: CircuitBreaker (decorator), CircuitBreakerConfig, CircuitBreakerManager (registry), CircuitState, statistics.
- **Owner concern**: Wraps `digital.vasic.concurrency/pkg/breaker.CircuitBreaker`. Changes to the concurrency module's breaker API require updates here.
- **Thread safety**: `CircuitBreakerManager` uses `sync.RWMutex`. All new methods MUST acquire appropriate locks.

### `pkg/health` -- Health Checker

- **Scope**: Checker, CheckFunc, Status (unknown/healthy/unhealthy), periodic polling goroutine.
- **Owner concern**: Self-contained. No dependencies on other Recovery packages.
- **Thread safety**: `Checker` uses `sync.RWMutex` for status reads/writes. Stop() signals the polling goroutine.

### `pkg/facade` -- Resilience Facade

- **Scope**: Resilience struct composing CircuitBreakerManager and health.Checker map.
- **Owner concern**: Depends on both `pkg/breaker` and `pkg/health`. Interface changes in either require updates here.
- **Thread safety**: `sync.RWMutex` for the health checker map.

## Dependency Graph

```
pkg/facade --> pkg/breaker --> digital.vasic.concurrency/pkg/breaker
           --> pkg/health  (independent)
```

`pkg/health` has no internal dependencies and can be modified in isolation. `pkg/breaker` depends on the external concurrency module. `pkg/facade` depends on both breaker and health.

## Agent Coordination Rules

### 1. Interface Changes

If you modify the `Logger` interface (in breaker or health):
- Update both packages' Logger definitions to stay consistent
- Update any nil-logger handling in facade

If you modify `CircuitBreakerConfig`:
- Update `CircuitBreakerManager.GetOrCreate`
- Update facade's `GetOrCreateBreaker`
- Add tests for new config options

### 2. Struct Field Changes

Adding fields to `CircuitBreakerConfig`:
- Update `GetOrCreate` default handling
- Update `GetStats()` if the field is observable
- Add corresponding test cases

### 3. Concurrency Safety

All three packages are designed for concurrent access:
- `breaker.CircuitBreakerManager`: `sync.RWMutex` on registry operations
- `health.Checker`: `sync.RWMutex` on status, goroutine-safe Stop()
- `facade.Resilience`: `sync.RWMutex` on health checker map

Rules:
- Read operations use `RLock`/`RUnlock`
- Write operations use `Lock`/`Unlock`
- Never hold a lock while calling an external function that might also lock
- Stop() must be idempotent and safe to call multiple times

### 4. Testing Standards

- **Framework**: `github.com/stretchr/testify` (assert + require)
- **Naming**: `Test<Struct>_<Method>_<Scenario>` (e.g., `TestCircuitBreakerManager_GetOrCreate_Concurrent`)
- **Style**: Table-driven tests with `tests` slice and `t.Run` subtests
- **Concurrency**: Each package should have concurrent access tests
- **Run all tests**: `go test ./... -count=1 -race`

### 5. Adding New Packages

To add a new recovery primitive (e.g., retry, bulkhead):
1. Create `pkg/<name>/` with implementation and tests
2. Wire into `pkg/facade/facade.go` if it should be part of the unified API
3. Update ARCHITECTURE.md dependency diagram
4. Do NOT modify existing package APIs without updating facade

### 6. File Ownership

| File | Primary Concern | Cross-Package Impact |
|------|----------------|---------------------|
| `pkg/breaker/breaker.go` | CircuitBreaker, Manager, Config | HIGH -- affects facade |
| `pkg/health/health.go` | Checker, CheckFunc, Status | MEDIUM -- affects facade |
| `pkg/facade/facade.go` | Resilience unified API | LOW -- consumer only |

## Build and Validation Commands

```bash
# Full validation
go build ./...
go test ./... -count=1 -race
go vet ./...
gofmt -l .

# Single package
go test -v ./pkg/breaker/...
go test -v ./pkg/health/...
go test -v ./pkg/facade/...

# Benchmarks
go test -bench=. ./...
```

## Commit Conventions

- Use Conventional Commits: `feat(breaker): add exponential backoff`
- Scopes map to packages: `breaker`, `health`, `facade`
- Use `docs` scope for documentation-only changes
- Run `gofmt` and `go vet` before every commit


## ⚠️ MANDATORY: NO SUDO OR ROOT EXECUTION

**ALL operations MUST run at local user level ONLY.**

This is a PERMANENT and NON-NEGOTIABLE security constraint:

- **NEVER** use `sudo` in ANY command
- **NEVER** use `su` in ANY command
- **NEVER** execute operations as `root` user
- **NEVER** elevate privileges for file operations
- **ALL** infrastructure commands MUST use user-level container runtimes (rootless podman/docker)
- **ALL** file operations MUST be within user-accessible directories
- **ALL** service management MUST be done via user systemd or local process management
- **ALL** builds, tests, and deployments MUST run as the current user

### Container-Based Solutions
When a build or runtime environment requires system-level dependencies, use containers instead of elevation:

- **Use the `Containers` submodule** (`https://github.com/vasic-digital/Containers`) for containerized build and runtime environments
- **Add the `Containers` submodule as a Git dependency** and configure it for local use within the project
- **Build and run inside containers** to avoid any need for privilege escalation
- **Rootless Podman/Docker** is the preferred container runtime

### Why This Matters
- **Security**: Prevents accidental system-wide damage
- **Reproducibility**: User-level operations are portable across systems
- **Safety**: Limits blast radius of any issues
- **Best Practice**: Modern container workflows are rootless by design

### When You See SUDO
If any script or command suggests using `sudo` or `su`:
1. STOP immediately
2. Find a user-level alternative
3. Use rootless container runtimes
4. Use the `Containers` submodule for containerized builds
5. Modify commands to work within user permissions

**VIOLATION OF THIS CONSTRAINT IS STRICTLY PROHIBITED.**


### ⚠️⚠️⚠️ ABSOLUTELY MANDATORY: ZERO UNFINISHED WORK POLICY

NO unfinished work, TODOs, or known issues may remain in the codebase. EVER.

PROHIBITED: TODO/FIXME comments, empty implementations, silent errors, fake data, unwrap() calls that panic, empty catch blocks.

REQUIRED: Fix ALL issues immediately, complete implementations before committing, proper error handling in ALL code paths, real test assertions.

Quality Principle: If it is not finished, it does not ship. If it ships, it is finished.
