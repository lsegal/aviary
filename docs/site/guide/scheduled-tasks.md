# Scheduled Tasks and Script Compilation

Scheduled tasks are the core automation primitive in Aviary. They let agents do work on a timer or in response to file changes — without requiring a human to be in the conversation. The task scheduler and precompiler together make it practical to run high-frequency background work at near-zero cost.

## Task Types

Aviary runs two kinds of scheduled work:

**Prompt tasks** send a natural-language prompt to the agent's LLM at each trigger. The model reads the prompt, calls tools as needed, and produces a response. Every execution draws tokens.

**Script tasks** run a sandboxed Lua script directly against the agent's MCP tools. No LLM is involved at runtime. Executions are deterministic and cost zero tokens.

Both types support the same triggers, deliver output to the same targets, and appear identically in the job queue. The difference is entirely in how they execute.

## Defining Tasks in Files

The preferred way to define tasks is as markdown files inside the agent's `tasks/` directory. This keeps task definitions alongside the agent's workspace and out of `aviary.yaml`.

Each file is named `<task-name>.md` and contains an optional YAML frontmatter block followed by the task prompt as the file body:

```markdown
---
schedule: "*/5 * * * *"
target: slack:#alerts
---

Check the weather forecast and alert me if rain is likely today.
```

Aviary loads all `*.md` files from the agent's `tasks/` directory at startup and on every config reload. The filename stem (`weather-check` for `weather-check.md`) becomes the task name unless the frontmatter overrides it with a `name:` field.

Supported frontmatter fields mirror the `aviary.yaml` task fields:

| Field | Description |
|-------|-------------|
| `name` | Override the task name (defaults to filename stem) |
| `schedule` | Cron expression (5-field or 6-field with leading seconds) |
| `watch` | Glob pattern to watch for file changes |
| `target` | Output delivery route (e.g. `slack:#channel`) |
| `type` | `prompt` (default) or `script` |
| `start_at` | ISO-8601 timestamp for deferred start |
| `run_once` | `true` to disarm after the first execution |
| `enabled` | `false` to disable without deleting the file |

For script tasks, the Lua source goes in the file body — the same position as a prompt:

```markdown
---
type: script
schedule: "0 * * * *"
target: slack:#ops-alerts
---

local result = tool.bash({ command = "df -h /" })
local pct = tonumber(result:match("(%d+)%%"))
if pct and pct > 85 then
  print("Disk usage at " .. pct .. "% — investigate soon.")
else
  print("SKIP")
end
```

The agent's tasks directory is `<data-dir>/agents/<name>/tasks/`, regardless of the agent's `working_dir`.

When the `task_schedule` MCP tool creates a recurring task it writes to the agent's `tasks/` directory automatically. Tasks defined in `aviary.yaml` under `agents[].tasks` are still supported and are merged with any file-based tasks (file definitions take precedence on name conflicts).

## Converting Existing Prompt Tasks to Scripts

Prompt tasks that were already created can be compiled to Lua scripts at any time. This is useful when a task was created before the precompiler ran, when precompilation was disabled at the time, or when you want to retry compilation after improving the prompt.

**CLI:**

```sh
aviary config convert-task-to-script <agent> <task-name>
```

For example:

```sh
aviary config convert-task-to-script assistant weather-check
```

The command returns immediately and runs compilation in the background. Use `task_compile_query` to track progress, or check the **Jobs** page in the web UI. If compilation succeeds, the task is updated in place — YAML-defined tasks stay in `aviary.yaml` and file-based tasks stay in their `.md` file.

**MCP tool:** `config_task_convert_to_script` — accepts `agent` and `task` arguments. Returns a compile ID immediately; track with `task_compile_query`.

**Web UI:** In **Settings → Agents → Tasks**, each prompt-type task shows a **Convert to Script** button. Clicking it starts compilation in the background and opens a notification banner with a link to the Jobs page.

## Moving Tasks from aviary.yaml to Files

Tasks that were originally defined inline in `aviary.yaml` can be migrated to the file-based format without losing any configuration. This is useful when you want to manage tasks as plain files alongside your agent's workspace.

**CLI:**

```sh
aviary config move-task-to-file <agent> <task-name>
```

For example:

```sh
aviary config move-task-to-file assistant daily-report
```

This removes the `daily-report` task from `aviary.yaml` and writes it as `~/.config/aviary/agents/assistant/tasks/daily-report.md`. All task fields (schedule, prompt, target, etc.) are preserved in the file's frontmatter and body.

**MCP tool:** `config_task_move_to_file` — accepts `agent` and `task` arguments.

**Web UI:** In **Settings → Agents → Tasks**, each inline task has a **Move to File** button that performs the same operation.

## Triggering Tasks

Tasks are defined under an agent in `aviary.yaml` or as markdown files in the agent's `tasks/` directory (see above). Three trigger modes are supported:

**Cron schedule** — runs on a fixed interval using a standard 5-field cron expression or a 6-field expression with a leading seconds field.

```yaml
agents:
  - name: assistant
    tasks:
      - name: weather-monitor
        schedule: "*/5 * * * *"   # every 5 minutes
        prompt: "Check the weather…"
```

**File watch** — fires when a file matching a glob pattern is created or modified. The glob is resolved relative to the agent's data directory.

```yaml
tasks:
  - name: process-report
    watch: "incoming/*.csv"
    prompt: "Summarize the CSV that just arrived and email a report."
```

**One-shot** — runs once, either at the next cron tick or at a specific time, then disarms itself.

```yaml
tasks:
  - name: release-announcement
    schedule: "0 9 * * MON"
    run_once: true
    prompt: "Post the weekly release notes to Slack."
```

## Output Delivery

Tasks that produce output can deliver it to a channel automatically. Set `target` to a channel route:

```yaml
tasks:
  - name: weather-monitor
    schedule: "*/5 * * * *"
    prompt: "…"
    target: slack:#weather-alerts
```

Any text printed from a script — or returned from a prompt run — is delivered to that route. If the output is `SKIP` or empty, delivery is suppressed. This lets scripts opt out of noisy no-op notifications cheaply.

## The Precompiler

When a new prompt task is saved, Aviary's precompiler runs a one-time analysis pass. It asks:

> Can this task be expressed as a fixed sequence of tool calls that produce the right output without further LLM judgment at runtime?

Tasks that qualify are **deterministic**: the tool calls and their arguments are fully knowable from the prompt, even if the data returned by those tools changes from run to run. Checking a fixed URL, querying a fixed API endpoint, reading a fixed file, and filtering results against a fixed threshold are all deterministic steps. Open-ended reasoning, fuzzy interpretation, and undiscoverable selectors are not.

When the precompiler decides a task can be compiled, it:

1. Generates a Lua script that reproduces the task's behavior exactly.
2. Validates the Lua syntax without executing it.
3. Writes the script back into the task definition.
4. Marks the task as a script type — future executions run Lua with zero token usage.

The compilation itself costs tokens once. For any task running more than a few times per day, that cost is recovered quickly.

Precompilation is on by default. To disable it globally:

```yaml
scheduler:
  precompute_tasks: false
```

## Example: Rain Alert Monitor

Consider a request like:

> "I want you to check the weather every 5 minutes and let me know if there are any days where it might rain (>30% chance)."

Submitted as a prompt task, this becomes:

```yaml
agents:
  - name: assistant
    tasks:
      - name: rain-alert
        schedule: "*/5 * * * *"
        prompt: >
          Check the weather forecast for the next 7 days. If any day has a
          precipitation probability above 30%, send me an alert listing those
          days and their percentages. Otherwise, do nothing.
        target: slack:#weather-alerts
```

At task creation, the precompiler analyzes the prompt. It identifies three deterministic steps: fetch the forecast with a fixed tool call, filter by a fixed threshold, and print a message or skip. Because all three steps are deterministic, it compiles the task to a Lua script and rewrites the task definition:

```yaml
agents:
  - name: assistant
    tasks:
      - name: rain-alert
        type: script
        schedule: "*/5 * * * *"
        target: slack:#weather-alerts
        script: |
          local result = tool.web_fetch({
            url = "https://api.open-meteo.com/v1/forecast"
              .. "?latitude=40.71&longitude=-74.01"
              .. "&daily=precipitation_probability_max"
              .. "&forecast_days=7&timezone=America%2FNew_York",
            format = "json"
          })

          local data = json.decode(result)
          local dates = data.daily.time
          local probs = data.daily.precipitation_probability_max

          local rainy = {}
          for i, prob in ipairs(probs) do
            if prob > 30 then
              table.insert(rainy, string.format("%s: %d%%", dates[i], prob))
            end
          end

          if #rainy == 0 then
            print("SKIP")
            return
          end

          print("Rain alert — days above 30% chance:")
          for _, day in ipairs(rainy) do
            print("  • " .. day)
          end
```

Every trigger after compilation runs this script directly. No LLM is invoked. The script calls `tool.web_fetch`, decodes the JSON, filters the results, and either prints `SKIP` (suppressing delivery) or emits the alert.

### Token Usage Comparison

| Mode | Tokens per run | Runs per day | Tokens per day |
|------|---------------|--------------|----------------|
| Prompt task | ~5,000 avg † | 288 | ~1,440,000 |
| Script task (compiled) | **0** | 288 | **0** |

† Measured from real Aviary usage data across simple scheduled tasks (URL checks, API polls). Research tasks and multi-step workflows run significantly higher.

The precompiler runs once at task creation. Depending on the complexity of the compilation pass, that costs roughly 2,000–8,000 tokens total — recovered within the first hour of a 5-minute recurring task.

## Writing Scripts Directly

You can skip the precompiler entirely and write the Lua script yourself. Set `type: script` and provide the `script` field directly:

```yaml
tasks:
  - name: disk-check
    type: script
    schedule: "0 * * * *"
    target: slack:#ops-alerts
    script: |
      local result = tool.bash({ command = "df -h /" })
      local pct = tonumber(result:match("(%d+)%%"))
      if pct and pct > 85 then
        print("Disk usage at " .. pct .. "% — investigate soon.")
      else
        print("SKIP")
      end
```

The Lua runtime exposes:

- `tool.<name>(args)` — call any MCP tool the agent has access to; returns a string or a decoded JSON table
- `json.encode(val)` / `json.decode(str)` — convert between Lua values and JSON
- `os.date(fmt)` / `os.time()` / `os.difftime(t2, t1)` — time utilities
- `print(...)` — emit output; joined lines are delivered to `target` when the run completes
- `environment.agent_id`, `.session_id`, `.task_id`, `.job_id` — runtime context

File I/O, `os.execute`, `loadfile`, and `dofile` are disabled. Scripts run fully sandboxed with access only to the tools explicitly granted to their agent.

## Managing the Job Queue

The job queue shows all pending, running, completed, and failed jobs. You can inspect and control jobs through the control panel or directly via MCP tools.

**Stopping a job** — cancel a specific pending or running job by ID:

```
job_stop id=<job-id>
```

The job transitions to `canceled` status. If the job is actively running, its context is interrupted and the agent session is stopped. If the job is pending, it is removed from the queue without executing.

**Running a pending job immediately** — bypass the scheduled time and concurrency limits:

```
job_run_now id=<job-id>
```

**Querying jobs** — list jobs by date range, status, or agent:

```
job_query start=2024-01-01 end=2024-01-31
```

**Stopping all jobs for a task** — cancel every pending and running job for a given task:

```
task_stop name=<task-name>
task_stop name=<agent/task>
task_stop           # stops all pending and running jobs
```

The control panel Jobs page also provides a **Stop** button next to any running job and a **Run Now** button for pending jobs.

## Inspecting Compile Attempts

The control panel System area shows compile attempt records for every task. Each record includes the compiler's analysis, whether the task was promoted to a script, and the reason if it was not. You can also query compile records through the MCP:

```
task_compile_query  — list compile attempts by agent, date, or status
task_compile_get    — fetch the full record for a specific attempt
```

This makes it straightforward to audit which tasks are running as scripts, which fell back to prompt mode, and why.
