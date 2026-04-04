# Contributing — Recovery

Thank you for contributing to `digital.vasic.recovery`. This document covers prerequisites,
development workflow, code standards, testing requirements, and the pre-commit checklist.

---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.25+ | Build and test |
| `digital.vasic.concurrency` | latest | Required dependency (resolved via `replace` directive) |
| `github.com/stretchr/testify` | v1.11+ | Test assertions |

The module depends on `digital.vasic.concurrency` via a monorepo `replace` directive. Before
contributing, clone the full Catalogizer repository with submodules so that the sibling
`Concurrency/` directory is present:

```bash
git clone --recurse-submodules git@github.com:vasic-digital/Catalogizer.git
cd Catalogizer/Recovery
```

If you cloned without `--recurse-submodules`, initialise the submodules manually:

```bash
git submodule init && git submodule update --recursive
```

---

## Development Workflow

### 1. Build

```bash
cd Recovery
go build ./...
```

### 2. Run Tests

Run the full test suite with the race detector enabled (mandatory before every commit):

```bash
go test ./... -count=1 -race
```

Run only short (unit) tests to skip any slow or integration paths:

```bash
go test ./... -short
```

Run benchmarks:

```bash
go test -bench=. ./...
```

Run a single test by name:

```bash
go test -v -run TestCircuitBreaker_Execute_OpensAfterMaxFailures ./pkg/breaker/
```

### 3. Vet and Format

```bash
gofmt -w ./...
go vet ./...
```

Both must produce zero output before a PR can be merged.

---

## Code Standards

### Formatting

- Use `gofmt` (not `goimports`) for formatting. The CI gate runs `gofmt -l` and fails on any diff.
- Maximum line length: **100 characters**. Exceptions: import paths, struct tags, and generated
  code.

### Imports

Group imports with a blank line between groups:

```go
import (
    "context"
    "sync"
    "time"

    "digital.vasic.concurrency/pkg/breaker"

    "digital.vasic.recovery/pkg/health"
)
```

Order: standard library → third-party → internal (`digital.vasic.*`).

### Naming

| Kind | Convention | Example |
|------|-----------|---------|
| Exported types | `PascalCase` | `CircuitBreaker` |
| Unexported fields | `camelCase` | `maxFailures` |
| Acronyms | all-caps | `SMBClient`, `NASPath` |
| Interfaces | noun or `-er` suffix | `Logger`, `Checker` |

### Error Handling

- Always check errors — never use `_` to discard an error that could indicate a problem.
- Wrap errors with context using `fmt.Errorf`:

  ```go
  if err := cb.Execute(fn); err != nil {
      return fmt.Errorf("smb read failed: %w", err)
  }
  ```

- Sentinel errors (e.g. `ErrOpen` from the concurrency module) must be checked with
  `errors.Is`, never by string comparison.

### Concurrency

- All shared state must be protected by a `sync.Mutex` or `sync.RWMutex`. Use `RWMutex` for
  read-heavy paths (e.g. registry lookups in `CircuitBreakerManager`).
- Never hold a lock across a function call that may block or call back into the same type.
- Use `sync.Once` for idempotent one-time operations (e.g. `Stop`).

### Commit Style

This repository follows [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>
```

| Type | When to use |
|------|-------------|
| `feat` | New feature or exported symbol |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `test` | Adding or improving tests |
| `refactor` | Code change with no behaviour change |
| `chore` | Build, tooling, dependency updates |

Examples:

```
feat(breaker): add exponential backoff on half-open probe
fix(health): prevent double-close of stop channel
docs(facade): add graceful shutdown example to user guide
test(breaker): cover nil-logger path in Execute
```

---

## Testing Requirements

### Table-Driven Tests

All tests must be table-driven using `testify`:

```go
func TestCircuitBreaker_Execute_OpensAfterMaxFailures(t *testing.T) {
    tests := []struct {
        name        string
        maxFailures int
        wantState   breaker.CircuitState
    }{
        {"opens after 3 failures", 3, breaker.StateOpen},
        {"opens after 5 failures", 5, breaker.StateOpen},
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            cb := breaker.NewCircuitBreaker(breaker.CircuitBreakerConfig{
                MaxFailures:  tc.maxFailures,
                ResetTimeout: time.Second,
            })
            for i := 0; i < tc.maxFailures; i++ {
                _ = cb.Execute(func() error { return errors.New("fail") })
            }
            assert.Equal(t, tc.wantState, cb.GetState())
        })
    }
}
```

Test function naming: `Test<Type>_<Method>_<Scenario>`.

### Race Detector

All tests must pass under `-race`. The CI gate runs `go test -race ./...` on every PR.

### Mock Logger

Use a simple in-test mock that satisfies the `Logger` interface rather than importing a third-
party mock library:

```go
type mockLogger struct {
    mu      sync.Mutex
    entries []string
}

func (m *mockLogger) Info(msg string, kv ...interface{})  { m.record("INFO", msg) }
func (m *mockLogger) Warn(msg string, kv ...interface{})  { m.record("WARN", msg) }
func (m *mockLogger) Debug(msg string, kv ...interface{}) { m.record("DEBUG", msg) }

func (m *mockLogger) record(level, msg string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.entries = append(m.entries, level+": "+msg)
}
```

### Coverage

Aim for greater than or equal to 90% statement coverage in each package. Generate a coverage
report with:

```bash
go test -coverprofile=cover.out ./...
go tool cover -html=cover.out
```

---

## Documentation Requirements

- Every exported symbol must have a Go doc comment beginning with the symbol name.
- Non-obvious internal functions should have an explanatory comment.
- If you add a new feature, update `docs/USER_GUIDE.md` with a usage example and
  `docs/API_REFERENCE.md` with the full method signature.
- Add an entry to `docs/CHANGELOG.md` under `## [Unreleased]` describing the change.

---

## Integration Testing Notes

The `replace` directive in `go.mod` resolves `digital.vasic.concurrency` to the local
`../Concurrency` directory. Integration tests that exercise the full concurrency + recovery
stack should:

1. Run from the `Recovery/` directory so that the `replace` directive takes effect.
2. Not rely on any network access — all dependencies are local.
3. Use short timeouts (1–5 s) so the suite stays fast.

If you modify the `digital.vasic.concurrency` module in the same PR, update the `Concurrency/`
submodule reference in the root repo and verify both modules build cleanly together:

```bash
cd Concurrency && go test ./... -race
cd ../Recovery  && go test ./... -race
```

---

## Pre-commit Checklist

Before opening a pull request, verify every item:

- [ ] `gofmt -l ./...` produces no output (zero diff)
- [ ] `go vet ./...` produces no output
- [ ] `go build ./...` succeeds with zero warnings
- [ ] `go test ./... -count=1 -race` passes with zero failures and zero race conditions
- [ ] All new exported symbols have Go doc comments
- [ ] `docs/CHANGELOG.md` updated under `## [Unreleased]`
- [ ] `docs/USER_GUIDE.md` and `docs/API_REFERENCE.md` updated if the public API changed
- [ ] Commit message follows Conventional Commits format
- [ ] No secrets, `.env` files, or API keys included in any tracked file
