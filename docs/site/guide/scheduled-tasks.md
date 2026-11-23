# Scheduled Tasks and Script Compilation

Scheduled tasks are the core automation primitive in Aviary. They let agents do work on a timer or in response to file changes — without requiring a human to be in the conversation. The task scheduler and precompiler together make it practical to run high-frequency background work at near-zero cost.

## Task Types

Aviary runs three kinds of scheduled work:

**Prompt tasks** send a natural-language prompt to the agent's LLM at each trigger. The model reads the prompt, calls tools as needed, and produces a response. Every execution draws tokens.

**Script tasks** run a sandboxed Lua script directly against the agent's MCP tools. No LLM is involved at runtime. Executions are deterministic and cost zero tokens.

**Project tasks** run a prompt as a [Code Loop](./projects) job instead of sending it to the agent's LLM. Each fire takes a slot on the project's board like any job, runs the project's agent on the prompt in an isolated clone of the repository (in place of [`/gh-fix`](/skills/gh-fix/)), and finishes when the agent's run ends. See [Scheduled prompts](#scheduled-prompts).

All types support the same triggers and appear identically in the job queue. The difference is entirely in how they execute.

## Defining Tasks in Files

The preferred way to define tasks is as markdown files inside the agent's `tasks/` directory. This keeps task definitions alongside the agent's workspace and out of `aviary.yaml`.

Each file is named `<task-name>.md` and contains an optional YAML frontmatter block followed by the task prompt as the file body:

```markdown
---
schedule: "*/5 * * * *"
target: channel:Alerts
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
| `target` | Output delivery route (for example `channel:Alerts`) |
| `type` | `prompt` (default) or `script` |
| `start_at` | ISO-8601 timestamp for deferred start |
| `run_once` | `true` to disarm after the first execution |
| `enabled` | `false` to disable without deleting the file |

For script tasks, the Lua source goes in the file body — the same position as a prompt:

```markdown
---
type: script
schedule: "0 * * * *"
target: channel:Ops Alerts
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

If you want a prompt task to stay as a prompt even when global precompilation is enabled, call `task_schedule` with `precompile=false`.

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

**Web UI:** In **Agents → *agent* → Scheduled Tasks**, each prompt-type task shows a **Convert to Script** button. Clicking it starts compilation in the background and opens a notification banner with a link to the Jobs page.

## Migrating Inline Tasks

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

**Web UI:** In **Agents → *agent* → Scheduled Tasks**, each inline task has a **Move to File** button that performs the same operation.

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

### Schedule triggers

Instead of a cron expression, a task can pick its trigger with `trigger` (inferred from the other fields when omitted):

| Trigger | Fields | Example |
| --- | --- | --- |
| `once` | `start_at` | `start_at: "2026-12-01T09:00"` |
| `interval` | `every` (`90s`, `30m`, `2h`, `1d`), optional `start_at` anchor | `every: 2h` |
| `daily` | `at` (`HH:MM`, comma-separate several) | `at: "09:00, 17:30"` |
| `weekly` | `days` and `at` | `days: mon,wed,fri` |
| `cron` | `schedule` | `schedule: "0 9 * * 1-5"` |

Every trigger is evaluated in `timezone`, an IANA zone such as `America/Toronto` (default: the host's local zone). A wall-clock time skipped by a daylight-saving change fires at the first time after the jump. `start_at` also delays the first fire of a recurring trigger, and `run_once: true` makes any trigger fire once.

Three settings control what happens when things do not go to plan:

- **`overlap`**: what a fire does while the previous run is still pending or running. `queue` (default) queues another run; `skip` drops the fire.
- **`missed`**: what happens to fires that were due while Aviary was stopped or the machine slept. `skip` (default) drops them; `run_once` runs a single catch-up run, however many fires were missed. Aviary persists each task's last fire, and checks for overdue fires every 30 seconds so it notices a wake from sleep.
- **`idle_timeout`**: how long a prompt run may go without any output (reply text, status or tool call) before Aviary stops it and marks the attempt failed with "agent did not start" or "run timed out: no output received for …". Defaults to `15m`; `"0"` disables it. A failed attempt is retried while attempts remain.

```yaml
tasks:
  - name: standup
    trigger: weekly
    days: mon,tue,wed,thu,fri
    at: "09:15"
    timezone: America/Toronto
    overlap: skip
    missed: run_once
    idle_timeout: 30m
    prompt: "Summarize what changed yesterday."
```

Each task keeps a run history of its last 20 fires: fire time, what fired it, status, a one-line summary and duration. `task_list` returns it with the next run time.

A running job streams its output, a line at a time, into the **Live Output** pane of the run details panel. When an attempt fails, the panel shows why, and the failure reason becomes the run-history summary.

### Managing schedules

| Tool | CLI | Purpose |
| --- | --- | --- |
| `task_schedule` | `aviary task add [agent] <name> <prompt> --every 2h` | Create or replace a task |
| `task_list` | `aviary task list` | Tasks with next run and last result |
| `task_update` | `aviary task update <name> --at 10:00` | Change fields; an empty value clears one, `enabled=false` pauses |
| `task_preview` | `aviary task preview [<name>] [--every 15m]` | Next fires of a saved task or unsaved trigger (default 3) |
| `task_run` | `aviary task run <name>` | Run now, ignoring the overlap policy |
| `task_delete` | `aviary task rm <name>` | Delete a task file and its history |
| `task_session` | | The task's session, which every run shares, titled with the task's name |

| `task_upcoming` | | Next fires across enabled tasks (default 5, at most 3 per task), optionally for one project |

`aviary task add` and `update` take every trigger field as a flag (`--schedule`, `--trigger`, `--every`, `--at`, `--days`, `--timezone`, `--start-at`, `--overlap`, `--missed`); `--days` also accepts ranges such as `mon-fri`. The agent may be omitted when only one agent exists.

### Scheduled prompts

A `project` task runs its prompt on the board of a watched project, always on the project's agent. `project` names the project (its `owner/repo` target) and `model` optionally overrides that agent's model for the task's runs, exactly like a channel's model override: a `<provider>/<model>` string such as `claude/opus` or `anthropic/claude-sonnet-5-5`, validated the same way. Without it the agent runs the task with its own model. A model of a provider reached through a CLI runs the job on that CLI; any other model runs it on the agent itself with the overridden model, which needs the project to be attached to an agent. Task files and configuration written with the older `run_agent` or `cli` keys still load into `model` (a value naming only a CLI, such as `codex`, loads as `cli/codex`), and are saved back as `model`.

```yaml
tasks:
  - name: nightly-triage
    type: project
    project: lsegal/aviary
    model: claude/opus
    at: "02:00"
    timezone: America/Toronto
    overlap: skip
    prompt: "Triage issues opened since the last run and label them."
```

```bash
aviary task add assistant nightly-triage "Triage new issues" --project lsegal/aviary --model claude/opus --at 02:00
```

Core's scheduler owns the schedule; Code Loop only runs each fire as a job. The board shows each project task as one **Scheduled** card, however often it fires: it shows the next run (or **Paused**, or **Queued** while a fire waits on the overlap policy), and the last run's result (✓ succeeded, ✖ failed, ⚠ fell back to the model), when it ran and its one-line summary. A run in flight shows on the same card as **Running**, with its elapsed time and live output, and when it ends the card goes back to Scheduled with the new last run; runs never leave cards of their own, and finished runs never count under **Done**. A task whose last run failed also shows under **Failed** until a run succeeds or the failure is dismissed. The card's detail view lists the recent runs with their time, duration, result and summary, each opening its transcript. Stop cancels the run and Retry fires the task again through the scheduler, so the rerun lands in the task's run history; a scheduled job cannot be paused, and the shutdown checkpoint skips it. The run's last line of output becomes its run-history summary. Project tasks need a schedule trigger, and the project must be watched when the task fires.

Project tasks are scheduled tasks like any other: there is one list of them, one scheduler that runs them, and they are created and edited under **Agents → *agent* → Scheduled Tasks**: set **Task type** to **Project**, then pick the watched project and, optionally, a **Model override**, the same model picker channels use, which the project's agent runs the task with instead of its own model.

The project's **Schedules** tab is a second place to create and edit the same tasks. It lists the scheduled tasks that target the project with enable toggles and their last results. **New schedule** opens the same **Add task** dialog the **+** on **Scheduled Tasks** opens, with the task already set to type `project` and to target the project, so it creates a project scheduled task that also shows in **Scheduled Tasks**. Selecting a task edits every field with a preview of the next fires, the recent runs, Run now, Save, Delete, and a link to the task in **Scheduled Tasks**; the **Scheduled Tasks** editor links back to the project's Schedules tab. The board's **Up next** strip shows the project's next five runs, at most three per schedule, and hides when no schedule is enabled.

### Task pages and sessions

Every run of a task shares one session, named and titled after the task. In the control panel it lives under the task in the sidebar's **Scheduled Tasks** section, not in **Chats**. Renaming the task moves its session, and its run history, to the new name. A deleted task's session stays in **Chats**.

Clicking the task in the sidebar opens its page, a dashboard for that one task:

- **Header** — the task's name, its schedule with the next fire, and whether it is enabled or paused, with **Run now**, **Pause**/**Resume**, and **Stop** while a run is active. The chat icon shows or hides the chat panel, and the gear opens the task's settings.
- **Stats** — a row of pills: queued, running, succeeded and failed runs, success rate, the last run and its result, the next run, and the average duration. Clicking a count filters the run history.
- **Current run** — while a run is active, its status, start time, elapsed time and live logs, which keep scrolling to the newest line until you scroll up, with copy and stop buttons. When nothing is running, the last run's logs and result show instead.
- **Run history** — every run, newest first, with its start time, duration, result (succeeded, failed, canceled or skipped), and trigger (schedule, catch-up, file watch or manual), twenty at a time and filterable by result. A failed or skipped run shows why. **Open run** opens a run's full logs at its own URL, `/agents/<agent>/tasks/<task>/runs/<job id>`.
- **Chat** — the task's session, in a panel along the bottom. Drag its top edge to resize it. On a phone it opens as a bottom sheet. Whether it shows and how tall it is are remembered in the browser.

### Workflows

A task with `trigger: message` is a workflow: it runs when a matching message arrives on one of the agent's channels instead of on a schedule, and replaces the agent's own reply to that message. Workflows are managed from their channel's settings and stay out of the Scheduled Tasks list. See [Workflows](./channels.md#workflows).

## Output Delivery

Tasks that produce output can deliver it to a channel automatically. Set `target` to a channel route:

```yaml
tasks:
  - name: weather-monitor
    schedule: "*/5 * * * *"
    prompt: "…"
    target: channel:Weather Alerts
```

A channel target picks one of the agent's channels by name: `channel:<name>`. The output lands in that channel's own session, under the agent's **Channels** sidebar section, and is sent on to the channel's `deliver_to` conversation. See [Delivery Targets](/guide/channels#delivery-targets).

- For sessions, use `session:<session-name>`
- For a Code Loop job, use `codeloop:<project>#<number>`, such as `codeloop:owner/repo#123`
- In the control panel, a task's **Send Via** picks **Session**, **Code Loop job**, or one of the agent's channels by name

### Sending output to a Code Loop job

A `codeloop:<project>#<number>` target hands the output to a [Code Loop](/guide/code-loop) job the way a comment from an allowed commenter reaches it: an issue job, a Discussion job, or a [scheduled project run](#scheduled-prompts). `<project>` is the project as configured on an agent (`owner/repo`, or a project board URL), and saving a target whose project no agent has is rejected. For issue and Discussion jobs, what happens depends on the job's state:

- **Running or waiting:** the agent is interrupted and resumed in the same session with the output, so it sees it on its next turn.
- **Paused, queued, or failed:** the output is queued and the job keeps its state. The agent sees it when the job next runs. A failed Discussion job runs again when the next poll finds the thread still unanswered.
- **Complete:** the delivery fails with an error in the run log. A Discussion job is complete once its agent has answered the thread.
- **No longer exists:** the delivery fails, and the task's page warns that its job is gone.

Two optional settings change the paused and complete cases. Add them after the job number as `?resume`, `?reopen`, or `?resume,reopen` (for example `codeloop:owner/repo#123?resume,reopen`), or tick **Resume the job if it is paused** and **Reopen the job if it is complete** under the job picker:

- **Resume:** a paused job is resumed when output arrives, as the board's Resume button does, and the agent sees the output on its first turn. If the job cannot be resumed right now, the output stays queued.
- **Reopen:** a completed issue job is retried with the output, as the board's Retry button does, instead of failing the delivery. An answered Discussion job is never retried, so output to it still fails.

A scheduled project run's number is its fire time, so any run of a schedule names the whole schedule, and output to it is never rejected for a finished run:

- **A run in flight:** the agent is interrupted and resumed in the same session with the output, as for an issue job. A run still waiting for an agent slot starts with the output instead.
- **Between runs:** the output is queued for the schedule, and its next run starts with it, added to the end of the task's prompt. That run uses the model even when the task is [precompiled](#precompiled-project-tasks), because a script can't read the output. Output that arrives while a precompiled script runs, or too late for the run to see it, also waits for the next run.

The **Code Loop job** picker lists issue jobs, Discussion jobs, and schedules, one entry per schedule, and stores the schedule's latest run. A target saved from an earlier run still names the schedule, including after a restart, as long as the run is in the task's run history.

The job's transcript labels the output with the task or workflow that sent it, linking to the run. Each run's log records the delivery ("delivered to Code Loop owner/repo#123", linking to the job), and every delivery is written to the usage log.

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

### Precompiled project tasks

[Project tasks](#scheduled-prompts) can be precompiled too. Turn on **Try to precompile** in the **Add task** dialog or in the project board's schedule editor, or pass `precompile=true` to `task_schedule` or `task_update`. New project tasks follow `precompute_tasks`; a project task without `precompile` set stays on the model. Both editors show the same live feedback for project tasks as for agent tasks: ✔ deterministic, ◐ partial, or ✖ not deterministic, with the per-run token estimate and the monthly projection.

A project task is not rewritten into a script task. It keeps its prompt and stores the compiled script beside it (`compiled` and `compiled_key` in its definition, written by Aviary). The prompt is analyzed and compiled against the tools of the agent the task belongs to, with the clone it runs in described. Each run then:

1. Recompiles first if the prompt, the project, or the agent's tools (including its allowed exec commands) changed since the last compile. A prompt that is not deterministic is not analyzed again until one of them changes.
2. Makes a fresh clone of the project under Code Loop's clone directories and runs the script there, as the task's agent. Its tool calls are granted the clone. The script always has `exec`, even when the agent's own permissions don't include it: commands run in the clone with gh's authentication and can't use a `cwd` outside it, and an agent's allowed exec commands, when it has any, still apply. The compiler, `task_analyze`, and the script all see this same tool list. `AVIARY_CHECKOUT_DIRECTORY` and `GH_REPO` name the clone and the repository. `exec` runs commands directly, not through a shell, so the compiler is given the project's `owner/repo` and writes it literally in commands (for example `gh api repos/lsegal/aviary/commits/main`). The clone is removed when the script ends.
3. Falls back to a normal model run on the project's agent if the script, the clone, or the compile fails. The run shows **Succeeded via fallback** in amber instead of **Succeeded**, and the task page counts it under **Script failures**. Its result and chat message are the model's result plus a short note such as "Precompiled script failed (gh 404), fell back to the model; details in the run logs". The script's full error, including any Lua traceback, is only in the run's logs.

A compile is rejected, and the task keeps running on the model, when the script only raises an error, when it names the repository through gh's `{owner}/{repo}` placeholders or a shell variable such as `$GH_REPO` (which `exec` doesn't expand), or when the compiler's own reason says the task can't be done deterministically or should stay a prompt task.

A script that fails on its first run after a compile is set aside at once, and the task stays on the model. A script that has completed before is turned off after it fails 3 runs in a row: Aviary turns `precompile` off for the task. Either way the task page shows why (`precompile_note` in its definition). Turn precompile off and on again to compile a fresh script.

Run results, run history, usage, and Send To delivery are the same as for a project task that is not precompiled. A deterministic run records no model usage. A task whose work needs judgment, such as editing code and opening pull requests, is reported as not deterministic, with the analyzer's reason, and keeps running on the model.

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
        target: channel:Weather Alerts
```

At task creation, the precompiler analyzes the prompt. It identifies three deterministic steps: fetch the forecast with a fixed tool call, filter by a fixed threshold, and print a message or skip. Because all three steps are deterministic, it compiles the task to a Lua script and rewrites the task definition:

```yaml
agents:
  - name: assistant
    tasks:
      - name: rain-alert
        type: script
        schedule: "*/5 * * * *"
        target: channel:Weather Alerts
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
    target: channel:Ops Alerts
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
- `trigger` — in a [workflow](./channels.md#workflows) run, the message that triggered it: `trigger.message` (`text`, `sender`, `from_agent`, `timestamp`, `attachments`), `trigger.before` (the messages just before it), and `trigger.channel`, `.channel_type` and `.entry`; `nil` in other runs

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
