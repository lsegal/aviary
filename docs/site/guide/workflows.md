# Workflows

A workflow is an automation that works like a [scheduled task](./scheduled-tasks.md), but a message on a channel triggers it instead of a schedule. When someone posts a message that matches, the workflow runs its prompt (or script) with that message as context and decides how to respond: reply in the same conversation, stay silent, or send its output somewhere else.

Typical uses:

- Answer every positive message in a Signal group with a friendly note.
- Start a release when someone writes `deploy staging` in a Slack channel.
- Copy messages from a family group into a private session for a daily summary.
- Log every message on an entry without saying anything back.

Workflows belong to an agent and watch one of that agent's [channels](./channels.md). Each run goes through the same job queue as scheduled tasks, with the same run history, logs, statuses, overlap handling and [precompilation](#precompiled-workflows).

## Setting up a workflow

Open **Channels**, pick the channel, open its **Settings** and choose the **Workflows** tab. There you add, edit, pause, resume and delete the channel's workflows. **Add workflow** on the Channels overview asks which channel to watch, then opens the same tab.

![The Workflows tab of a channel's settings](/screenshots/workflows-tab-light.png){.light-only}
![The Workflows tab of a channel's settings](/screenshots/workflows-tab-dark.png){.dark-only}

The form is the task form, with a trigger in place of the schedule:

- **Source entry** is the conversation to watch: a Slack or Discord channel, or a Signal contact or group. The picker lists the conversations the channel has received messages on ([`workflow_entries`](/reference/mcp/browser-and-channels#workflow-entries)). Empty watches every conversation on the channel.
- **Text match** is a case-insensitive wildcard over the whole message text. `*` matches any run of characters, including none and line breaks, and `?` matches exactly one. `*crash*` matches every message containing "crash", and `deploy *` matches messages that start with "deploy ". Empty matches every message, like `*`.
- **Would match** tests the current text match against the entry's recent messages as you type ([`workflow_preview`](/reference/mcp/browser-and-channels#workflow-preview)).
- **Type** is a prompt (default) or a Lua script, as for tasks. A prompt run gets the triggering message, and the few messages before it, below the prompt.
- **Model override** runs the workflow on a different model than the agent's.
- **Try to precompile** and its live estimate: as you type, the prompt is analyzed (debounced, and a newer edit cancels the analysis in flight). The form shows whether the prompt can be precompiled, the reason when it can't, and the estimated tokens and cost of each run. The estimate includes the triggering message and the messages before it, so it is higher than for the same scheduled task. When the workflow will be precompiled, the estimate is 0 tokens per run, with the model's cost beside it for comparison. Analyses are cached on the server, so reopening a workflow whose prompt hasn't changed shows its last result, marked **Cached · checked 2h ago**, without calling the model; **Re-check** analyzes it again. See [Live feedback in the editor](./scheduled-tasks#live-feedback-in-the-editor) and [Precompiled workflows](#precompiled-workflows).
- **Send Via** is the output target:
  - **silent** — deliver nothing; the run history still records the output.
  - **reply to the source conversation** — answer in the conversation that triggered the run, in its thread when it came from one. The reply also shows in that conversation's chat.
  - a **session** — deliver into another session, such as a private summary session.
  - a [**Code Loop job**](./scheduled-tasks.md#sending-output-to-a-code-loop-job) — hand the output to a job like an allowed commenter's comment.
  - another **channel** — deliver into that channel's own session.
- **While a run is active** picks what a new match does while an earlier run is still queued or running: queue another run (default) or skip it.

You can also create and change workflows from chat or another MCP client with [`task_schedule`](/reference/mcp/tasks-and-jobs#task-schedule) and [`task_update`](/reference/mcp/tasks-and-jobs#task-update) using `trigger=message`, or in YAML (see [Configuration](#configuration)).

### Deleting a workflow

The Edit workflow form ends with a **Danger Zone** whose **Delete workflow** deletes the workflow. It only appears when editing an existing workflow. It and the **Delete** in the workflows list open the same in-app confirmation, with the **Delete session data and files** checkbox described in [Deleting data with an object](./control-panel#deleting-data-with-an-object). When the workflow has a run pending or in progress, the confirmation says so: deleting stops it, and the workflow won't run again. Confirming calls [`task_delete`](/reference/mcp/tasks-and-jobs#task-delete) and then [`task_stop`](/reference/mcp/tasks-and-jobs#task-stop); if stopping fails, the workflow is still deleted and an error toast says so. Afterwards the form goes back to the workflows list and shows a "Deleted workflow `<name>`" toast.

## Reply behavior

When a message matches one or more **active** workflows, **the workflows replace the agent's own reply**:

- Every matching workflow runs, in name order, and the agent does not answer the message itself. Whether anything is said back is up to each workflow's Send Via.
- The message is still recorded in the channel's conversation, marked **Handled by workflow** with a link to each run.
- The agent replies as usual only when no active workflow matches.
- **Paused workflows never match**, so a message that matches only paused workflows gets the normal reply.
- A workflow whose text match is `*` or empty takes over **every** message on its entry, and the form says so. Use a narrower match if you still want the agent to chat normally on that entry.

### Not replying

A workflow's final message is sent verbatim as its reply, so a run that decides not to answer must not explain itself in its final message: that explanation would be the reply. Instead the run calls the [`task_no_reply`](/reference/mcp/tasks-and-jobs#task-no-reply) tool with a short reason, and the prompt every delivering run gets says so. A run that called it sends nothing, whatever it wrote afterwards.

Its run log shows **Skipped reply:** with the reason, its row in the workflow's run history says "Skipped reply: …", and the workflow's card says its last run did not reply, with the reason on hover. The message stays **Handled by workflow**, so the agent does not reply to it either. Scheduled tasks use the same tool when there is nothing to report.

## Loop safety

Workflows never run on the agent's own messages, such as a Signal message sent from the channel's own number or a workflow's reply. A workflow therefore cannot trigger itself, or another workflow, with its output. [`workflow_preview`](/reference/mcp/browser-and-channels#workflow-preview) leaves those messages out for the same reason.

Workflows also only see messages the channel accepts through its [`allow_from`](./channels.md) rules.

## Run history and debugging

### Workflow cards

In the channel's chat, its workflows show as cards above the conversation, active ones first. **Hide paused** filters out paused workflows, and the strip collapses to icons; both choices are remembered. While a workflow is triggered (a run queued) or running, a colored segment travels clockwise around its card, amber while triggered and blue while running. Under `prefers-reduced-motion` the card gets a static colored border instead. A card also shows the last run's result, including a run that fell back to the model or skipped its reply.

The Channels overview lists every workflow of the agent the same way.

![Workflow cards above a channel's conversation](/screenshots/workflow-strip-light.png){.light-only}
![Workflow cards above a channel's conversation](/screenshots/workflow-strip-dark.png){.dark-only}

### The workflow's page

Clicking a card opens the workflow's page: live logs for the current run, previous runs with their results (succeeded, failed, succeeded via fallback, skipped reply), and **Run now**, **Pause** and settings.

Each run records its **trigger context**: the channel and entry, the sender's display name (never a raw ID or phone number), the message and its attachments, timestamps, and the messages just before and after it. Open a run to see its trigger context beside its logs and its success or error.

![A workflow's page with its run history and trigger context](/screenshots/workflow-detail-light.png){.light-only}
![A workflow's page with its run history and trigger context](/screenshots/workflow-detail-dark.png){.dark-only}

### Usage logs

Usage records attribute each run's tokens to the workflow and the channel that fired it, so **Usage** lists a workflow run as `Workflow <name> · <channel>`. Every delivery is written to the usage log as well.

## Precompiled workflows

A prompt workflow can be precompiled like a [project task](./scheduled-tasks.md#precompiled-project-tasks). **Try to precompile** follows `scheduler.precompute_tasks` for a new workflow (on by default), and `task_schedule` and `task_update` take `precompile`. Saving compiles the prompt in the background when it passes the determinism gate. The workflow keeps its prompt, and each matching message then:

1. Runs the compiled script, which reads the message from the Lua global `trigger` (`trigger.message.text`, `.sender`, `trigger.before`, `trigger.channel`, `trigger.entry`). What the script prints is the workflow's reply; a script that decides not to reply calls `tool.task_no_reply({reason = "..."})` instead (see [Not replying](#not-replying)).
2. Falls back to a normal model run on the prompt if the script or the compile fails. The run shows **Succeeded via fallback**, the script's error is in the run's logs, and the workflow's card says its last run fell back to the model.

A failing script never turns precompile off: each failed run falls back to the model, and the workflow shows **Script failing** with the last error and **Recompile**, exactly as for [project tasks](/guide/scheduled-tasks#precompiled-project-tasks). Workflow cards, the Workflows tab and the workflow's page show a badge with the compiled state (**Compiled**, **Compiling…**, **Script failing**, **Not precompiled** or **Runs on model**), and the page also counts compile attempts, fallback runs and script failures.

## Examples

### Auto-respond to positive messages

The `be-nice` pattern: watch a Signal group, read every message, and answer only the positive ones.

```yaml
- name: be-nice
  trigger: message
  channel: Family Signal
  entry: "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MA=="  # the group ID, from workflow_entries
  match: "*"
  target: reply
  prompt: |
    If this message is positive or shares good news, reply with one short,
    warm sentence. Otherwise call task_no_reply with a short reason.
```

Because the match is `*`, this workflow takes over the whole group: the agent's normal chat reply is skipped for every message there.

### Keyword-triggered release

Run a release when someone writes `deploy <environment>` in a Slack channel, and report back in the thread.

```yaml
- name: deploy
  trigger: message
  channel: Team Slack
  entry: C0123456789
  match: "deploy *"
  target: reply
  overlap: skip
  prompt: |
    The message names an environment after "deploy". Run the release script
    for that environment and reply with the result and a link to the logs.
```

Other messages on the channel get the agent's normal reply, and `overlap: skip` drops a second `deploy` while one is still running.

### Route a Signal group into another session

Forward messages from a busy group into a private session without replying in the group.

```yaml
- name: school-updates
  trigger: message
  channel: Family Signal
  entry: "c2Nob29sZ3JvdXBpZDEyMzQ1Njc4OTA="
  match: "*school*"
  target: session:school-notes
  prompt: Summarize this message in one line for my school notes.
```

### Silent logger

Record every message on an entry without answering. A `silent` target delivers nothing, but every run's output stays in its run history.

```yaml
- name: log-support
  trigger: message
  channel: Team Slack
  entry: C0456789012
  target: silent
  type: script
  prompt: |
    print((trigger.message.sender or "unknown") .. ": " .. trigger.message.text)
```

A silent logger with an empty match also takes over the entry, so the agent stops replying there. Give it a narrower match to keep normal replies.

## Configuration

In `aviary.yaml` or a task file in the agent's `tasks/` directory, a workflow is a task with `trigger: message`:

```yaml
agents:
  - name: lobby
    tasks:
      - name: triage-crashes
        trigger: message
        channel: Team Slack   # the agent's channel, by name or ID
        entry: C0123456789    # optional: one conversation on it
        match: "*crash*"      # optional: empty matches every message
        target: reply         # silent (empty), reply, session:<name>, channel:<name>, codeloop:<owner/repo>#<number>, notify or notify?failures
        overlap: queue        # optional: queue (default) or skip
        model: anthropic/claude-haiku-4-5  # optional model override
        precompile: true      # optional: run a compiled script, falling back to the model
        enabled: true         # false pauses the workflow
        prompt: Triage this crash report and reply with next steps.
```

A workflow takes no `schedule`, `every`, `at`, `days`, `start_at`, `run_once` or `missed`. Workflows stay out of the Scheduled Tasks list. Renaming a channel moves its workflows along, and removing a channel deletes them. See [`agents[].tasks`](/reference/config#agents-tasks) for every field.

## MCP tools

| Tool | Use |
| --- | --- |
| [`task_schedule`](/reference/mcp/tasks-and-jobs#task-schedule) | Create a workflow with `trigger=message`, `channel`, `entry`, `match` and `target` |
| [`task_update`](/reference/mcp/tasks-and-jobs#task-update) | Change a workflow's fields, or pause it with `enabled=false` |
| [`task_delete`](/reference/mcp/tasks-and-jobs#task-delete) | Delete a workflow |
| [`task_run`](/reference/mcp/tasks-and-jobs#task-run) | Run a workflow now |
| [`workflow_entries`](/reference/mcp/browser-and-channels#workflow-entries) | List the conversations a channel has received messages on, for picking a source entry |
| [`workflow_preview`](/reference/mcp/browser-and-channels#workflow-preview) | Test a text match against a channel's recent messages |
| [`task_no_reply`](/reference/mcp/tasks-and-jobs#task-no-reply) | Inside a run: end it without replying |
| [`job_list`](/reference/mcp/tasks-and-jobs#job-list), [`job_logs`](/reference/mcp/tasks-and-jobs#job-logs) | Inspect a workflow's runs and their logs |
