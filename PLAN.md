# Aviary Implementation Plan

## Context

Aviary is a greenfield AI agent orchestrator. The repository currently contains only three documentation files (README.md, ARCHITECTURE.md, DOMAIN_MODEL.md) and zero source code. This plan translates those specs into an ordered, phased implementation that can be executed incrementally—each phase produces working, testable software and is a prerequisite for the next.

The output of this plan is `D:\github\lsegal\aviary\PLAN.md`, a living document committed to the repo to guide the development team.

**Critical source files:**
- `D:\github\lsegal\aviary\README.md` — CLI signatures, YAML schema, UX requirements
- `D:\github\lsegal\aviary\ARCHITECTURE.md` — component constraints, MCP-only HTTP, hot-reload semantics
- `D:\github\lsegal\aviary\DOMAIN_MODEL.md` — canonical entity relationships driving migrations and domain types

---

## Project Structure

```
aviary/
├── go.mod                          # module github.com/lsegal/aviary, go 1.23+
├── go.sum
├── Makefile                        # build, test, generate, web:build, web:dev
├── PLAN.md
│
├── cmd/aviary/
│   ├── main.go
│   └── cmd/
│       ├── root.go                 # global flags: --config, --server, --token
│       ├── start.go                # bypasses MCP (per ARCHITECTURE.md)
│       ├── stop.go                 # bypasses MCP
│       ├── status.go
│       ├── agent.go                # agent {list,run,stop}
│       ├── task.go                 # task {list,run,stop}
│       ├── job.go                  # job {list,logs}
│       ├── browser.go              # browser {open,click,type,screenshot,close}
│       ├── memory.go               # memory {search,show,clear}
│       ├── auth.go                 # auth {login,set,get,list,delete}
│       └── configure.go            # configure {agents,channels,models,scheduler,auth}
│
├── internal/
│   ├── config/
│   │   ├── config.go               # Config struct, Load(), Default()
│   │   ├── schema.json             # JSON schema for aviary.yaml validation
│   │   ├── schema.go               # embeds schema.json, Validate()
│   │   └── watcher.go              # fsnotify hot-reload, 300ms debounce
│   ├── domain/
│   │   ├── agent.go                # Agent, AgentState
│   │   ├── channel.go              # Channel, ChannelType
│   │   ├── session.go              # Session, Message
│   │   ├── scheduler.go            # ScheduledTask, Job, Run
│   │   ├── model.go                # Model, Provider
│   │   └── memory.go               # MemoryPool, MemoryEntry
│   ├── store/
│   │   ├── store.go                # DataDir(), EnsureDirs(); path constants
│   │   ├── json.go                 # ReadJSON/WriteJSON/DeleteJSON helpers; atomic write via temp+rename
│   │   └── jsonl.go                # AppendJSONL/ReadJSONL for append-only logs (memory, job logs)
│   ├── server/
│   │   ├── server.go               # HTTPS server, route mounting
│   │   ├── tls.go                  # self-signed cert generation, persistence
│   │   ├── auth.go                 # token generation, bearer middleware, login cookie
│   │   └── pid.go                  # PID file for start/stop
│   ├── mcp/
│   │   ├── server.go               # MCP server setup (modelcontextprotocol/go-sdk)
│   │   ├── tools.go                # all tool registrations
│   │   ├── client.go               # InProcessClient (direct function calls)
│   │   ├── proxy.go                # RemoteClient (HTTPS to running server)
│   │   └── dispatch.go             # Dispatcher: selects in-process vs remote
│   ├── auth/
│   │   ├── store.go                # AuthStore interface
│   │   ├── keychain.go             # go-keyring backend
│   │   ├── file.go                 # JSON file backend (~/.config/aviary/auth.json)
│   │   ├── resolver.go             # parse "auth:<provider>:<name>"
│   │   └── oauth.go                # browser OAuth flow (local callback server)
│   ├── agent/
│   │   ├── manager.go              # AgentManager: registry, Reconcile(cfg)
│   │   ├── runner.go               # AgentRunner: Prompt(), Stop(), parallel via goroutines
│   │   ├── session.go              # SessionManager: create/resume, persist
│   │   ├── stream.go               # StreamEvent types, fan-out to consumers
│   │   └── skills.go               # AgentSkill loader (SKILL.md discovery)
│   ├── llm/
│   │   ├── provider.go             # LLMProvider interface + factory
│   │   ├── openai.go               # openai/openai-go adapter
│   │   ├── anthropic.go            # anthropics/anthropic-sdk-go adapter
│   │   ├── gemini.go               # OpenAI-compat endpoint (reuses openai.go)
│   │   └── stdio.go                # subprocess adapter (claude CLI, codex, etc.)
│   ├── channels/
│   │   ├── channel.go              # Channel interface: Start, Stop, Send, OnMessage
│   │   ├── slack.go                # slack-go/slack, allowFrom filtering
│   │   ├── discord.go              # bwmarrin/discordgo, allowFrom filtering
│   │   └── signal.go               # stub; deferred (see Decisions)
│   ├── scheduler/
│   │   ├── queue.go                # JobQueue backed by JSON files in ~/.config/aviary/jobs/; re-entrant, retryable
│   │   ├── worker.go               # WorkerPool (configurable concurrency)
│   │   ├── cron.go                 # robfig/cron v3 wrapper
│   │   ├── watcher.go              # fsnotify file-trigger, glob matching, debounce
│   │   └── scheduler.go            # Scheduler: orchestrates all three + Reconcile(cfg)
│   ├── browser/
│   │   ├── manager.go              # launch Chromium with --profile-directory, CDP port
│   │   ├── session.go              # BrowserSession lifecycle (chromedp.Context)
│   │   └── ops.go                  # Navigate, Click, Type, Screenshot, Close, EvalJS
│   └── memory/
│       ├── manager.go              # Append, LoadContext (sliding window), GetPool
│       ├── search.go               # in-memory keyword search over loaded entries
│       └── compactor.go            # summarize via LLM, replace oldest N messages
│
└── web/
    ├── package.json                # vue@3, vite, vue-router@4, pinia, shadcn-vue, tailwind
    ├── vite.config.ts              # output web/dist/; dev proxy /mcp → localhost:16677
    ├── src/
    │   ├── composables/
    │   │   ├── useMCP.ts           # fetch + EventSource; injects Bearer token from cookie
    │   │   └── useStream.ts        # SSE helper
    │   ├── stores/                 # pinia: auth, agents, tasks, memory
    │   ├── views/                  # ChatView, AgentsView, TasksView, SessionsView
    │   └── components/             # ChatWindow, AgentSidebar, TaskPanel, JobLogs, LoginScreen
    └── dist/                       # go:embed target
```

---

## Key Dependencies

| Package | Use |
|---|---|
| `github.com/modelcontextprotocol/go-sdk` | MCP server + client (official SDK) |
| `github.com/spf13/cobra` | CLI command tree |
| `gopkg.in/yaml.v3` | YAML config parsing |
| `github.com/fsnotify/fsnotify` | Config hot-reload + file-watch triggers |
| `github.com/robfig/cron/v3` | Cron expression scheduler |
| `github.com/openai/openai-go` | OpenAI + Gemini (compat) LLM calls |
| `github.com/anthropics/anthropic-sdk-go` | Anthropic/Claude LLM calls |
| `github.com/chromedp/chromedp` | CDP browser automation |
| `github.com/zalando/go-keyring` | System keychain (macOS/Windows/Linux) |
| `github.com/slack-go/slack` | Slack channel integration |
| `github.com/bwmarrin/discordgo` | Discord channel integration |
| `github.com/charmbracelet/huh` | Interactive configure wizard TUI |
| `golang.org/x/crypto` | Token generation, file auth encryption |

---

## Phases

### Phase 0 — Scaffold
**Goal:** `go build ./...` works. `aviary --help` prints all subcommands. Config loads and validates.

- `go.mod`, `Makefile`
- `cmd/aviary/main.go` + all `cmd/` stubs (cobra skeleton, no logic)
- `internal/config/config.go` — full `Config` struct matching README YAML schema
- `internal/config/schema.json` — JSON schema for validation
- `internal/domain/*.go` — all domain structs (no DB, no logic)
- `.golangci.yml`

### Phase 1 — Persistence
**Goal:** JSON file store initialized. All domain types have CRUD via simple file helpers. Data directory structure established.

- `internal/store/store.go` — `DataDir()`, `EnsureDirs()`, directory constants (`jobs/`, `sessions/`, `memory/`)
- `internal/store/json.go` — `ReadJSON[T]`, `WriteJSON`, `DeleteJSON`, `ListJSON`; atomic writes via temp file + rename
- `internal/store/jsonl.go` — `AppendJSONL`, `ReadJSONL[T]` for append-only logs
- Data layout under `~/.config/aviary/`:
  - `jobs/<job-id>.json` — job record with `status`, `attempts`, `next_retry_at`, `locked_at`
  - `sessions/<session-id>.jsonl` — message log (one JSON object per line)
  - `memory/<pool-id>.jsonl` — memory entries (append-only, compactor rewrites)

### Phase 2 — Server + Auth
**Goal:** `aviary start`/`aviary stop` work. HTTPS on 16677. Token auth enforced. MCP endpoint exists (no tools yet).

- `internal/server/{server,tls,auth,pid}.go`
- `internal/auth/{store,keychain,file,resolver}.go`
- `internal/mcp/server.go` (registers placeholder tool)
- `cmd/aviary/cmd/{start,stop,auth}.go`
- First run: generate token → print to stdout; TLS cert generated + cached

### Phase 3 — MCP Bridge
**Goal:** All CLI subcommands exist with real argument signatures. MCP dispatch (in-process vs remote) works. Every domain operation registered as an MCP tool (stub implementations).

- `internal/mcp/{tools,client,proxy,dispatch}.go`
- All `cmd/aviary/cmd/*.go` updated to use `mcp.Dispatcher`
- `Client` interface: `CallTool()`, `StreamTool()`
- `Dispatcher.Resolve()` checks PID file to pick in-process vs remote

### Phase 4 — Config Hot-Reload + Agent Manager
**Goal:** Config watcher reconciles agents live. `aviary agent list` returns real agents. Agents have lifecycle (start/stop/reload) but no LLM calls yet.

- `internal/config/watcher.go` (fsnotify, 300ms debounce)
- `internal/agent/{manager,runner,session}.go`
- `AgentManager.Reconcile(cfg)` — adds/updates/removes agents idempotently
- Server wires `config.Watcher` → `agent.Manager.Reconcile`

### Phase 5 — LLM Providers + Agent Execution
**Goal:** `aviary agent run <name> "<message>"` streams a real LLM response. Parallel prompts work. Stop works.

- `internal/llm/{provider,openai,anthropic,gemini,stdio}.go`
- `LLMProvider` interface: `Stream(ctx, req) (<-chan StreamEvent, error)`
- `AgentRunner.Prompt()` — goroutine per prompt, `activePrompts sync.WaitGroup`
- `AgentRunner.Stop()` — closes `stopCh`, cancels all in-flight contexts
- `StreamEvent` fan-out: terminal (CLI), channels, scheduler

### Phase 6 — Scheduler + Job Queue
**Goal:** Cron and file-watch tasks trigger, enqueue jobs, execute via agent, retry with backoff. Survives restart.

- `internal/scheduler/{queue,worker,cron,watcher,scheduler}.go`
- `JobQueue` — JSON-file-backed (`~/.config/aviary/jobs/`); one file per job; `RecoverStuck()` on startup scans for jobs with `locked_at` older than timeout
- Queue ops: `Enqueue` writes new file; `Claim` rewrites file atomically with `locked_at`; `Complete`/`Fail` update status field; `List` reads all files in the directory
- Retry: exponential backoff starting 30s, max 1h; indefinite for throttle errors; configurable `maxRetries` for others
- `Scheduler.Reconcile(cfg)` — adds/removes cron entries and file watches idempotently
- `aviary task` and `aviary job` CLI commands read directly from job JSON files

### Phase 7 — Memory
**Goal:** Agent sessions persist messages. Memory is searchable. Long conversations compact automatically.

- `internal/memory/{manager,search,compactor}.go`
- `MemoryManager.LoadContext(poolID, maxTokens)` — reads pool JSONL, sliding window newest-first until token budget
- `MemoryManager.Compact(poolID)` — summarize oldest N messages via LLM, rewrite JSONL with summary entry replacing them
- `Search(poolID, query)` — loads entries into memory, case-insensitive keyword match across content fields
- Agent runner calls `Append()` after each exchange, `LoadContext()` before each prompt

### Phase 8 — Channels
**Goal:** Agents respond in Slack and Discord. `allowFrom` filters enforced.

- `internal/channels/{channel,slack,discord,signal}.go`
- `Channel` interface: `Start`, `Stop`, `Send`, `OnMessage`
- Message routing: `OnMessage` → `AgentRunner.Prompt()` → stream → `Send()`
- `AgentManager.Reconcile()` starts/stops channels per config change
- Signal: stub that logs a warning; deferred

### Phase 9 — Browser Control
**Goal:** `aviary browser` commands work. Agents can invoke browser tools via MCP.

- `internal/browser/{manager,session,ops}.go`
- Launches Chromium with `--profile-directory` (separate from user profile)
- All ops registered as MCP tools; all wired to `cmd/aviary/cmd/browser.go`

### Phase 10 — Web Control Panel
**Goal:** Full Vue SPA embedded in Go binary. Login, chat, tasks, agents, sessions all functional.

- `web/` — Vue 3 + Vite + shadcn-vue + Pinia
- Go: `//go:embed web/dist` → `http.FileServer` for non-`/mcp` routes
- Go: `POST /api/login` validates token, sets `Secure; SameSite=Strict` cookie
- `useMCP.ts` — fetch + EventSource with cookie-based auth
- Dev: `make web:dev` runs `vite dev` (HMR) proxying `/mcp` to Go server

### Phase 11 — Skills, Configure Wizard, Polish
**Goal:** AgentSkill dynamic loading. `aviary configure` wizard. OAuth flows. Production hardening.

- `internal/agent/skills.go` — discovers SKILL.md files, registers as MCP tools per agent
- `internal/auth/oauth.go` — local callback server + browser open + token exchange + store
- `cmd/aviary/cmd/configure.go` — `charmbracelet/huh` wizard; writes to `aviary.yaml`
- `log/slog` structured logging throughout
- `install.sh` / `install.ps1`

---

## Phase Dependency Order

```
Phase 0 (Scaffold)
  └─► Phase 1 (Persistence)
        └─► Phase 2 (Server + Auth)
              ├─► Phase 3 (MCP Bridge)
              │     └─► Phase 4 (Config + Agent Manager)
              │           └─► Phase 5 (LLM Execution)
              │                 ├─► Phase 6 (Scheduler)    ─┐
              │                 ├─► Phase 7 (Memory)        ├─ parallel
              │                 └─► Phase 8 (Channels)     ─┘
              └─► Phase 9 (Browser)  [parallel with 5–8]
Phase 10 (Web UI)   [can start after Phase 3; needs Phase 5 for real data]
Phase 11 (Polish)   [needs all prior phases]
```

---

## Open Decisions

These should be resolved before the relevant phase begins:

| # | Decision | Recommendation | Phase |
|---|---|---|---|
| 1 | MCP SDK: `modelcontextprotocol/go-sdk` vs `mark3labs/mcp-go` | Official go-sdk | 3 |
| 2 | Signal integration: signal-cli bridge, signald, or defer | Defer to post-launch | 8 |
| 3 | Memory compaction trigger: automatic (background) vs on-demand (pre-prompt) | On-demand pre-prompt; threshold configurable | 7 |
| 4 | Gemini: native SDK vs OpenAI-compat endpoint | OpenAI-compat (zero extra code) | 5 |
| 5 | Data directory: `~/.config/aviary/` vs XDG data dir | `~/.config/aviary/` (single location, simple) | 1 |

---

## Verification

Each phase is complete when:
- `go build ./...` still compiles with no errors
- Phase-specific integration test passes (see `_test.go` files per package)
- `aviary start` + the new feature works end-to-end via CLI
- Config hot-reload still works (edit `aviary.yaml`, verify change reflected without restart)

Full system verification (after Phase 11):
1. `aviary configure` — complete wizard, inspect `aviary.yaml`
2. `aviary start` — confirm token printed, HTTPS on 16677
3. `aviary agent run assistant "hello"` — confirm streaming LLM response
4. Add a cron task to config — confirm it fires without restart
5. Add a `watch:` task — create a file in the watched dir, confirm it triggers
6. Open `https://localhost:16677` — login, chat with agent, view task logs
7. `aviary browser open https://example.com` — confirm CDP session opens
