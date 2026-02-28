# Test Coverage Design — 2026-02-28

## Goal

Achieve ≥90% line coverage on every testable Go file in the Aviary codebase,
with ≥10% of test functions per package being integration tests that wire ≥2
real components together.

## Scope

### Included (target ≥90% per file)

| Package | Files |
|---|---|
| `internal/store` | `store.go`, `json.go`, `jsonl.go` |
| `internal/domain` | `agent.go`, `channel.go`, `memory.go`, `model.go`, `scheduler.go`, `session.go` |
| `internal/config` | `config.go`, `schema.go`, `watcher.go` |
| `internal/auth` | `store.go`, `resolver.go`, `file.go` |
| `internal/memory` | `manager.go`, `search.go`, `compactor.go` |
| `internal/scheduler` | `queue.go`, `scheduler.go`, `cron.go`, `watcher.go`, `worker.go` |
| `internal/server` | `auth.go`, `pid.go` |
| `internal/llm` | `provider.go` |
| `internal/mcp` | `dispatch.go`, `tools.go`, `deps.go` |

### Excluded (require external services)

- `internal/browser/*` — requires Chrome/chromedp
- `internal/channels/*` — requires Slack, Discord, Signal API tokens
- `internal/llm/anthropic.go`, `openai.go`, `gemini.go`, `stdio.go`
- `internal/server/server.go`, `tls.go`, `embed.go`
- `cmd/aviary/cmd/*` — CLI Cobra integration

## Approach

**Approach A** with table-driven tests and integration subtests per package.

### Test file structure

One `*_test.go` file per package:
- Table-driven subtests (`t.Run(...)`) for all multi-input functions
- At least one `t.Run("integration/...")` subtest per package that wires ≥2 real components

### Mocking strategy

- **`auth.Store`**: inline `mockStore` struct in test files
- **`llm.Provider`**: inline `mockProvider` with configurable `Stream()` behavior
- **`agent.Manager`** (scheduler tests): thin mock returning a mock runner
- **File I/O**: `t.TempDir()` + `t.Setenv("XDG_CONFIG_HOME", dir)` to redirect the data dir

### Coverage measurement

```sh
go test -coverprofile=coverage.out \
  -coverpkg=./internal/store/...,./internal/config/...,./internal/auth/...,\
./internal/memory/...,./internal/scheduler/...,./internal/server/...,\
./internal/llm/...,./internal/mcp/...,./internal/domain/... \
  ./internal/...
go tool cover -func=coverage.out
```

## Implementation Order

1. `internal/store` — foundation; other packages depend on it
2. `internal/domain` — type tests, constants
3. `internal/config` — config load/defaults
4. `internal/auth` — resolver + file store
5. `internal/memory` — manager + search + compactor
6. `internal/scheduler` — queue + cron + watcher + worker
7. `internal/server` — auth middleware + pid
8. `internal/llm` — factory ForModel
9. `internal/mcp` — dispatch + tools + deps
