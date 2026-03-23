# Scheduled Tasks and Script Compilation

Scheduled tasks are the core automation primitive in Aviary. They let agents do work on a timer or in response to file changes — without requiring a human to be in the conversation. The task scheduler and precompiler together make it practical to run high-frequency background work at near-zero cost.

## Task Types

Aviary runs two kinds of scheduled work:

**Prompt tasks** send a natural-language prompt to the agent's LLM at each trigger. The model reads the prompt, calls tools as needed, and produces a response. Every execution draws tokens.

**Script tasks** run a sandboxed Lua script directly against the agent's MCP tools. No LLM is involved at runtime. Executions are deterministic and cost zero tokens.

Both types support the same triggers, deliver output to the same targets, and appear identically in the job queue. The difference is entirely in how they execute.

## Triggering Tasks

Tasks are defined under an agent in `aviary.yaml`. Three trigger modes are supported:

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

## Inspecting Compile Attempts

The control panel System area shows compile attempt records for every task. Each record includes the compiler's analysis, whether the task was promoted to a script, and the reason if it was not. You can also query compile records through the MCP:

```
task_compile_query  — list compile attempts by agent, date, or status
task_compile_get    — fetch the full record for a specific attempt
```

This makes it straightforward to audit which tasks are running as scripts, which fell back to prompt mode, and why.
