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

## Universal Mandatory Constraints

These rules are inherited from the cross-project Universal Mandatory Development Constraints (canonical source: `/tmp/UNIVERSAL_MANDATORY_RULES.md`, derived from the HelixAgent root `CLAUDE.md`). They are non-negotiable across every project, submodule, and sibling repository. Project-specific addenda are welcome but cannot weaken or override these.

### Hard Stops (permanent, non-negotiable)

1. **NO CI/CD pipelines.** No `.github/workflows/`, `.gitlab-ci.yml`, `Jenkinsfile`, `.travis.yml`, `.circleci/`, or any automated pipeline. No Git hooks either. All builds and tests run manually or via Makefile / script targets.
2. **NO HTTPS for Git.** SSH URLs only (`git@github.com:…`, `git@gitlab.com:…`, etc.) for clones, fetches, pushes, and submodule operations. Including for public repos. SSH keys are configured on every service.
3. **NO manual container commands.** Container orchestration is owned by the project's binary / orchestrator (e.g. `make build` → `./bin/<app>`). Direct `docker`/`podman start|stop|rm` and `docker-compose up|down` are prohibited as workflows. The orchestrator reads its configured `.env` and brings up everything.

### Mandatory Development Standards

1. **100% Test Coverage.** Every component MUST have unit, integration, E2E, automation, security/penetration, and benchmark tests. No false positives. Mocks/stubs ONLY in unit tests; all other test types use real data and live services.
2. **Challenge Coverage.** Every component MUST have Challenge scripts (`./challenges/scripts/`) validating real-life use cases. No false success — validate actual behavior, not return codes.
3. **Real Data.** Beyond unit tests, all components MUST use actual API calls, real databases, live services. No simulated success. Fallback chains tested with actual failures.
4. **Health & Observability.** Every service MUST expose health endpoints. Circuit breakers for all external dependencies. Prometheus / OpenTelemetry integration where applicable.
5. **Documentation & Quality.** Update `CLAUDE.md`, `AGENTS.md`, and relevant docs alongside code changes. Pass language-appropriate format/lint/security gates. Conventional Commits: `<type>(<scope>): <description>`.
6. **Validation Before Release.** Pass the project's full validation suite (`make ci-validate-all`-equivalent) plus all challenges (`./challenges/scripts/run_all_challenges.sh`).
7. **No Mocks or Stubs in Production.** Mocks, stubs, fakes, placeholder classes, TODO implementations are STRICTLY FORBIDDEN in production code. All production code is fully functional with real integrations. Only unit tests may use mocks/stubs.
8. **Comprehensive Verification.** Every fix MUST be verified from all angles: runtime testing (actual HTTP requests / real CLI invocations), compile verification, code structure checks, dependency existence checks, backward compatibility, and no false positives in tests or challenges. Grep-only validation is NEVER sufficient.
9. **Resource Limits for Tests & Challenges (CRITICAL).** ALL test and challenge execution MUST be strictly limited to 30-40% of host system resources. Use `GOMAXPROCS=2`, `nice -n 19`, `ionice -c 3`, `-p 1` for `go test`. Container limits required. The host runs mission-critical processes — exceeding limits causes system crashes.
10. **Bugfix Documentation.** All bug fixes MUST be documented in `docs/issues/fixed/BUGFIXES.md` (or the project's equivalent) with root cause analysis, affected files, fix description, and a link to the verification test/challenge.
11. **Real Infrastructure for All Non-Unit Tests.** Mocks/fakes/stubs/placeholders MAY be used ONLY in unit tests (files ending `_test.go` run under `go test -short`, equivalent for other languages). ALL other test types — integration, E2E, functional, security, stress, chaos, challenge, benchmark, runtime verification — MUST execute against the REAL running system with REAL containers, REAL databases, REAL services, and REAL HTTP calls. Non-unit tests that cannot connect to real services MUST skip (not fail).
12. **Reproduction-Before-Fix (CONST-032 — MANDATORY).** Every reported error, defect, or unexpected behavior MUST be reproduced by a Challenge script BEFORE any fix is attempted. Sequence: (1) Write the Challenge first. (2) Run it; confirm fail (it reproduces the bug). (3) Then write the fix. (4) Re-run; confirm pass. (5) Commit Challenge + fix together. The Challenge becomes the regression guard for that bug forever.
13. **Concurrent-Safe Containers (Go-specific, where applicable).** Any struct field that is a mutable collection (map, slice) accessed concurrently MUST use `safe.Store[K,V]` / `safe.Slice[T]` from `digital.vasic.concurrency/pkg/safe` (or the project's equivalent primitives). Bare `sync.Mutex + map/slice` combinations are prohibited for new code.

### Definition of Done (universal)

A change is NOT done because code compiles and tests pass. "Done" requires pasted terminal output from a real run, produced in the same session as the change.

- **No self-certification.** Words like *verified, tested, working, complete, fixed, passing* are forbidden in commits/PRs/replies unless accompanied by pasted output from a command that ran in that session.
- **Demo before code.** Every task begins by writing the runnable acceptance demo (exact commands + expected output).
- **Real system, every time.** Demos run against real artifacts.
- **Skips are loud.** `t.Skip` / `@Ignore` / `xit` / `describe.skip` without a trailing `SKIP-OK: #<ticket>` comment break validation.
- **Evidence in the PR.** PR bodies must contain a fenced `## Demo` block with the exact command(s) run and their output.
