# Task and Job Tools

Aviary separates **tasks** (definitions) from **jobs** (executions). A task is the specification — what to run, when, and for which agent. A job is a single run instance created from a task trigger.

For a conceptual overview of task types and scheduling, see the [Scheduled Tasks guide](/guide/scheduled-tasks).

---

## Definitions, Compile Attempts, and Jobs

```
Task definition  ──► Compile attempt  ──► Job execution
(aviary.yaml)        (prompt→script)       (worker pool)
```

- **Task definition** — stored in `aviary.yaml` under `agents[].tasks`. Defines the trigger, prompt or script, and target.
- **Compile attempt** — when `scheduler.precompute_tasks` is enabled, prompt tasks are compiled into Lua scripts ahead of time via an LLM call. The record tracks each stage and stores the resulting script.
- **Job** — a queued or completed execution. One task trigger creates one job; the job carries status, output, and timing.

---

## task_list

List all configured task definitions across all agents.

**Arguments:** none

**Returns:** JSON array of task config objects from `aviary.yaml`.

```json
[
  {
    "name": "daily-summary",
    "agent": "assistant",
    "type": "prompt",
    "schedule": "0 9 * * 1-5",
    "prompt": "Summarize open issues...",
    "enabled": true
  }
]
```

---

## task_run

Immediately trigger a configured task by name, creating and queuing a job.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Task name, optionally prefixed with agent: `"myagent/daily-report"` or just `"daily-report"` |

**Returns:** JSON job object created from the trigger.

---

## task_schedule

Schedule a one-time or recurring task.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `content` | string | yes | Prompt text (for `type=prompt`) or Lua source (for `type=script`) |
| `type` | string | | `"prompt"` (default) or `"script"` |
| `in` | string | | Delay for a one-time run (e.g. `"5m"`, `"2h"`) |
| `schedule` | string | | Cron expression for recurring runs (5-field or 6-field with leading seconds) |
| `name` | string | | Task name (required to persist as a recurring task in config) |
| `target` | string | | Output delivery target |
| `trigger_type` | string | | `"cron"` or `"watch"` |
| `precompile` | bool | | Override automatic prompt-task precompilation for this task. Set to `false` to keep it as a prompt task. |
| `run_discovery` | bool | | Run the task immediately to discover its compiled form |

**Returns:** JSON job object (for one-time tasks) or compilation result (for recurring tasks with precompute enabled).

**Side effects:** Queues a job. For recurring named tasks, updates `aviary.yaml`. When `scheduler.precompute_tasks` is enabled, prompt tasks trigger an LLM compile call to convert the prompt to a Lua script unless `precompile=false` is provided for that task.

**Notes:** For `type=script`, `content` must be Aviary embedded Lua — not shell commands or shebang scripts. Do not add timezone conversion logic unless the task explicitly requires it.

---

## task_stop

Stop scheduled task jobs. Stops all matching pending and running jobs.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | | Task name or `agent/task` path. Omit to stop all jobs. |
| `job_id` | string | | Stop a specific job by ID |

**Returns:** Text with count of stopped jobs.

---

## task_compile_query

Return task compile attempt records, optionally filtered.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | | Filter to a specific compile attempt ID |
| `start` | string | | Start of date range (RFC3339 or `YYYY-MM-DD`) |
| `end` | string | | End of date range |
| `status` | string | | Filter by status: `"pending"`, `"success"`, `"error"` |
| `agent` | string | | Filter by agent name |

**Returns:** JSON array of `TaskCompile` records.

---

## task_compile_get

Return the full stored record for a specific compile attempt, including intermediate stages and the compiled script.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | yes | Compile attempt ID |

**Returns:** JSON `TaskCompile` record with full details.

---

## job_list

List job history across all tasks.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `task` | string | | Filter by task name |

**Returns:** JSON array of job objects.

```json
[
  {
    "id": "01HZ...",
    "task": "daily-summary",
    "agent": "assistant",
    "status": "completed",
    "created_at": "2026-03-22T09:00:00Z",
    "started_at": "2026-03-22T09:00:01Z",
    "finished_at": "2026-03-22T09:00:45Z"
  }
]
```

---

## job_query

Return job records with flexible filtering.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | | Filter to a specific job ID |
| `start` | string | | Start of date range (RFC3339 or `YYYY-MM-DD`) |
| `end` | string | | End of date range |
| `status` | string | | Filter by status: `"pending"`, `"running"`, `"completed"`, `"failed"`, `"stopped"` |
| `agent` | string | | Filter by agent name |

**Returns:** JSON array of job objects.

---

## job_logs

Return captured output for a specific job run.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | yes | Job ID |

**Returns:** Text output captured during the job run. If no explicit output was captured, falls back to the session message log for the job's session.

---

## job_run_now

Immediately run an existing pending job by ID, bypassing its scheduled time and normal concurrency limits.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | yes | Job ID |

**Returns:** JSON updated job object.

**Side effects:** Immediately queues the job for execution in the worker pool.
