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

**Returns:** JSON array of tasks, disabled ones included with `enabled: false`, with their trigger fields, `project` and `model` for project tasks, a `description` of the trigger, `next_run`, `last_fire`, and `history` (the last 20 fires, each with `fire_time`, `reason`, `status`, `summary` and `duration_ms`).

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
| `content` | string | yes | Prompt text (for `type=prompt` and `type=project`) or Lua source (for `type=script`) |
| `type` | string | | `"prompt"` (default), `"script"` or `"project"` |
| `project` | string | | For `type=project`: the watched project (`owner/repo`) whose board runs the prompt |
| `model` | string | | For `type=project`: a model override, such as `claude/opus`, that the project's agent runs the task with instead of its own model (default: the agent's model) |
| `in` | string | | Delay for a one-time run (e.g. `"5m"`, `"2h"`) |
| `schedule` | string | | Cron expression for recurring runs (5-field or 6-field with leading seconds) |
| `name` | string | | Task name (required to persist as a recurring task in config) |
| `target` | string | | Output delivery target |
| `trigger_type` | string | | `"cron"` or `"watch"` |
| `trigger` | string | | `once`, `interval`, `daily`, `weekly` or `cron`; inferred from the fields below when omitted. `message` defines a [workflow](/guide/workflows) instead |
| `channel` | string | | Workflows: the agent's channel whose messages run it |
| `entry` | string | | Workflows: the conversation on the channel to watch (see `workflow_entries`); empty watches every conversation |
| `match` | string | | Workflows: case-insensitive wildcard (`*`, `?`) the whole message must match; empty matches every message |
| `every` | string | | Interval period (`90s`, `30m`, `2h`, `1d`) |
| `at` | string | | Daily or weekly time of day, `HH:MM` (comma-separate several) |
| `days` | string | | Weekly days, e.g. `mon,wed,fri` |
| `timezone` | string | | IANA zone the trigger is evaluated in (default: local) |
| `start_at` | string | | Once-trigger time, or earliest fire of a recurring trigger |
| `run_once` | bool | | Fire only once |
| `overlap` | string | | `queue` (default) or `skip` while a previous run is active |
| `missed` | string | | `skip` (default) or `run_once` for fires missed while stopped or asleep |
| `precompile` | bool | | Override automatic precompilation for this task. Set to `false` to keep a prompt task as a prompt task, or a project task always on the model. A precompiled project task keeps its prompt; see [Precompiled project tasks](/guide/scheduled-tasks#precompiled-project-tasks). |
| `run_discovery` | bool | | Run the task immediately to discover its compiled form |

**Returns:** JSON job object (for one-time tasks) or compilation result (for recurring tasks with precompute enabled).

**Side effects:** Queues a job. For recurring named tasks, updates `aviary.yaml`. When `scheduler.precompute_tasks` is enabled, prompt tasks trigger an LLM compile call to convert the prompt to a Lua script unless `precompile=false` is provided for that task.

**Workflows:** with `trigger=message` the task runs when a matching message arrives on `channel`, and replaces the agent's own reply to it. A workflow takes no schedule, `in`, `every`, `at`, `days`, `start_at`, `run_once` or `missed`. A prompt workflow keeps its prompt; with `precompile` (default `scheduler.precompute_tasks`) a deterministic one runs a compiled script on each matching message and falls back to the model when the script fails; see [Precompiled workflows](/guide/workflows#precompiled-workflows). Its `target` may also be `reply`, which answers in the conversation that triggered the run, and `model` overrides the agent's model. `overlap` applies as for scheduled tasks.

**Notes:** `in` and a schedule trigger are mutually exclusive — set exactly one per call. To both create a recurring task and fire it once immediately for testing, make two separate `task_schedule`/`task_run` calls rather than passing both fields at once. For `type=script`, `content` must be Aviary embedded Lua — not shell commands or shebang scripts. Do not add timezone conversion logic unless the task explicitly requires it.

---

## task_update

Change fields of an existing task. Only the fields passed change; an empty string clears a field.

**Arguments:** `name` (required; `agent/task` or a task name), `agent`, and any of `enabled`, `content`, `target`, `schedule`, `trigger`, `every`, `at`, `days`, `timezone`, `start_at`, `run_once`, `overlap`, `missed`, `watch`, `project`, `model`, `precompile` (prompt and project tasks, scheduled or workflows: turns the precompiled script on or off; script tasks have no prompt to compile), and for workflows `channel`, `entry` and `match`.

**Side effects:** Writes the task's markdown file in the agent's `tasks/` directory and reloads the scheduler.

---

## task_delete

Delete a scheduled task or workflow definition. With `delete_data: true` its session, with its messages and media, and its run history are deleted too. Without it they stay on disk, and its session becomes a regular chat titled `<task> (archived)`, so it never lingers in Chats as if the task still existed. Running jobs keep running; use `task_stop` for those. Tasks defined inline in `aviary.yaml` must be removed there.

**Arguments:** `name` (required), `agent`, `delete_data`.

---

## delete_preview

Count what deleting a task, workflow, channel or agent, or removing a Code Loop project, with `delete_data: true` removes. It deletes nothing. The web UI's delete confirmations show these counts.

**Arguments:** `kind` (required; `task`, `workflow`, `channel`, `agent` or `project`), `name` (required; the task, which may be `agent/task`, the channel or agent name, or the project's `owner/repo`), `agent` (the channel's agent, or the task's agent when its name is ambiguous).

**Returns:** `{ "sessions", "messages", "runs", "files" }`. For a project, `runs` counts its job records and `files` its job transcripts plus clones.

---

## task_session

Return a task's session, which every run of the task shares, creating it when the task has not run yet. The session is named and titled after the task, and a rename moves it to the new name.

**Arguments:** `name` (required; `agent/task` or a task name), `agent`.

**Returns:** `{ "id", "agent_id", "name", "title", "is_processing" }`.

---

## task_preview

Return the next fires of a saved task, or of an unsaved trigger.

**Arguments:** `name` for a saved task, or the trigger fields of `task_schedule`; `count` (default 3, at most 20).

**Returns:**

```json
{
  "description": "weekly on Mon, Fri at 09:00 (UTC)",
  "timezone": "UTC",
  "next": ["2026-11-20T09:00:00Z", "2026-11-23T09:00:00Z", "2026-11-27T09:00:00Z"]
}
```

---

## task_upcoming

Return the next fires across enabled tasks, soonest first. The Code Loop board's "Up next" strip calls it with `project`.

**Arguments:** `project` (only that project's scheduled prompts), `limit` (default 5), `per_task` (default 3).

**Returns:**

```json
[{ "task_id": "assistant/nightly-triage", "name": "nightly-triage", "project": "lsegal/aviary", "at": "2026-11-20T02:00:00-05:00" }]
```

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

## task_no_reply

Only in a scheduled task or workflow run: end the run without replying, so nothing is sent to its target or the conversation that triggered it, even if the run writes a final message afterwards. The run log records the skip and its reason (delivery status `skipped`). Precompiled scripts call it as `tool.task_no_reply({reason = "..."})`. Outside a run it returns an error. Agent tool allowlists never hide it. See [Not replying](/guide/workflows#not-replying).

**Arguments:** `reason` (required), a short note on why, such as `message was negative`.

---

## task_compile_query

Return task compile attempt records, optionally filtered.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | | Filter to a specific compile attempt ID |
| `start` | string | | Start of date range (RFC3339 or `YYYY-MM-DD`) |
| `end` | string | | End of date range (RFC3339 or `YYYY-MM-DD`, which includes that whole day) |
| `status` | string | | Filter by status: `"succeeded"`, `"skipped"`, `"failed"` |
| `agent` | string | | Filter by agent name (case-insensitive) |
| `task` | string | | Filter by task name (case-insensitive) |

An unparseable `start` or `end` is an error rather than being ignored.

**Returns:** JSON array of `TaskCompile` records, without their `stages`, which can be hundreds of kilobytes each. Use `task_compile_get` for a record's stages.

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
| `task` | string | | Filter by task ID (`agent/task`) |

**Returns:** JSON array of job objects. A job a task fire created has a `trigger`: `schedule`, `missed` (a catch-up fire), `watch` or `manual`.

```json
[
  {
    "id": "01HZ...",
    "task": "daily-summary",
    "agent": "assistant",
    "status": "completed",
    "trigger": "schedule",
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
| `end` | string | | End of date range (RFC3339 or `YYYY-MM-DD`, which includes that whole day) |
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
